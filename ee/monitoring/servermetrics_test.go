// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/ee/monitoring/exposition"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/config"
	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/store"
)

type fakeManager struct{ allowed bool }

func (f fakeManager) Allows(feature string) bool { return f.allowed && feature == edition.Monitoring }
func (fakeManager) Info() *edition.LicenseInfo   { return nil }
func (fakeManager) Set(context.Context, string, string) (*edition.LicenseInfo, error) {
	return nil, errors.New("not implemented")
}
func (fakeManager) Clear(context.Context, string) error { return errors.New("not implemented") }

// The tests of GET /metrics replace the process-wide license manager and therefore do not run
// in parallel.

func TestMetricsEndpoint(t *testing.T) {
	t.Cleanup(func() { edition.SetManager(nil) })
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	if err := os.WriteFile(tokenFile, []byte("from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		cfg     config.Server
		allowed bool
		auth    string
		status  int
		code    string
	}{
		{"off without a token", config.Server{}, true, "Bearer x", http.StatusNotFound, "not_found"},
		{"no token sent", config.Server{MetricsToken: "secret"}, true, "", http.StatusUnauthorized, "unauthenticated"},
		{"wrong token", config.Server{MetricsToken: "secret"}, true, "Bearer secreT", http.StatusUnauthorized, "unauthenticated"},
		{"session token instead", config.Server{MetricsToken: "secret"}, true, "secret", http.StatusUnauthorized, "unauthenticated"},
		{"license without monitoring", config.Server{MetricsToken: "secret"}, false, "Bearer secret", http.StatusForbidden, "license_required"},
		{"token of the environment", config.Server{MetricsToken: "secret"}, true, "Bearer secret", http.StatusOK, ""},
		{"token file", config.Server{MetricsTokenFile: tokenFile}, true, "Bearer from-file", http.StatusOK, ""},
		{"unreadable token file", config.Server{MetricsTokenFile: filepath.Join(dir, "missing")}, true, "Bearer x",
			http.StatusServiceUnavailable, "metrics_unavailable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			edition.SetManager(fakeManager{allowed: tt.allowed})
			svc := newTestService(t, tt.cfg, newFakeStore(), &fakeClusters{})
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tt.auth != "" {
				req.Header.Set("Authorization", tt.auth)
			}
			rec := httptest.NewRecorder()
			svc.serveMetrics(rec, req)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if tt.code != "" && !strings.Contains(rec.Body.String(), `"code":"`+tt.code+`"`) {
				t.Errorf("body = %s, want the code %s", rec.Body, tt.code)
			}
			if tt.status == http.StatusUnauthorized && rec.Header().Get("WWW-Authenticate") == "" {
				t.Error("401 without WWW-Authenticate")
			}
			if tt.status == http.StatusOK && (rec.Header().Get("Content-Type") != exposition.ContentType ||
				!strings.Contains(rec.Body.String(), "brokerium_build_info{")) {
				t.Errorf("metrics = %s %s", rec.Header().Get("Content-Type"), rec.Body)
			}
		})
	}
}

func TestCollect(t *testing.T) {
	t.Parallel()
	alpha := alphaView()
	beta := &store.Cluster{ID: "c-beta", Name: "beta", Provider: store.ProviderAgent, Flavor: store.FlavorApache}
	gamma := &store.Cluster{ID: "c-gamma", Name: "gamma", Provider: store.ProviderStrimzi, Flavor: store.FlavorApache}
	st := newFakeStore(alpha.Cluster, beta, gamma)
	st.operations = []*store.Operation{{ClusterID: "c-alpha", Status: store.OpRunning}, {ClusterID: "c-beta", Status: store.OpPaused}}
	clusters := &fakeClusters{}
	svc := newTestService(t, config.Server{}, st, clusters)
	clusters.refresh(alpha)
	clusters.refresh(&cluster.View{Cluster: beta, Error: "dial tcp: connection refused", UpdatedAt: time.Unix(1795176000, 0),
		Health: cluster.Health{Status: cluster.HealthUnreachable}, Snapshot: &kafka.Snapshot{Quorum: &kafka.Quorum{LeaderID: 1}}})
	start := time.Date(2026, 11, 20, 11, 55, 0, 0, time.UTC)
	partition := func(end, size int64) kafka.Topic {
		return kafka.Topic{Name: "orders", Partitions: []kafka.Partition{{ID: 0, Replicas: []int32{101, 102}, EndOffset: end, SizeBytes: size}}}
	}
	svc.sampler.Observe("c-alpha", &kafka.Snapshot{CollectedAt: start, Topics: []kafka.Topic{partition(0, 0)}})
	svc.sampler.Observe("c-alpha", &kafka.Snapshot{CollectedAt: start.Add(100 * time.Second), Topics: []kafka.Topic{partition(500, 10000)}})

	var b strings.Builder
	if err := exposition.Write(&b, svc.registry.Gather(context.Background())); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, line := range []string{
		`brokerium_cluster_info{cluster="alpha",provider="external",flavor="apache",kafka_cluster_id="kafka-id-1"} 1`,
		`brokerium_cluster_info{cluster="gamma",provider="strimzi",flavor="apache",kafka_cluster_id=""} 1`,
		`brokerium_cluster_health_status{cluster="alpha"} 1`,
		`brokerium_cluster_health_status{cluster="beta"} 2`,
		`brokerium_cluster_reachable{cluster="alpha"} 1`,
		`brokerium_cluster_reachable{cluster="beta"} 0`,
		`brokerium_cluster_last_snapshot_timestamp_seconds{cluster="beta"} 1.795176e+09`,
		`brokerium_cluster_operations{cluster="alpha",status="running"} 1`,
		`brokerium_cluster_operations{cluster="beta",status="paused"} 1`,
		`brokerium_cluster_operations{cluster="gamma",status="pending"} 0`,
		`brokerium_cluster_brokers{cluster="alpha"} 3`,
		`brokerium_cluster_brokers_alive{cluster="alpha"} 2`,
		`brokerium_cluster_brokers_fenced{cluster="alpha"} 1`,
		`brokerium_cluster_partitions_under_replicated{cluster="alpha"} 2`,
		`brokerium_cluster_partitions_under_min_isr{cluster="alpha"} 1`,
		`brokerium_cluster_partitions_offline{cluster="alpha"} 0`,
		`brokerium_cluster_quorum_voters{cluster="alpha"} 1`,
		`brokerium_broker_disk_used_bytes{cluster="alpha",broker="101"} 600`,
		`brokerium_broker_disk_total_bytes{cluster="alpha",broker="101"} 1000`,
		`brokerium_broker_log_size_bytes{cluster="alpha",broker="101"} 500`,
		`brokerium_broker_estimated_bytes_in_per_second{cluster="alpha",broker="101"} 100`,
		`brokerium_broker_estimated_messages_in_per_second{cluster="alpha",broker="102"} 5`,
		`brokerium_topic_estimated_bytes_in_per_second{cluster="alpha",topic="orders"} 100`,
		`brokerium_topic_estimated_messages_in_per_second{cluster="alpha",topic="orders"} 5`,
		`brokerium_consumer_group_lag{cluster="alpha",group="billing",topic="audit"} 2`,
		`brokerium_consumer_group_lag{cluster="alpha",group="billing",topic="orders"} 5`,
		`brokerium_consumer_group_members{cluster="alpha",group="billing"} 2`,
		"# TYPE brokerium_cluster_health_status gauge",
	} {
		if !strings.Contains(out, line+"\n") {
			t.Errorf("missing %s", line)
		}
	}
	for _, absent := range []string{
		`brokerium_cluster_brokers{cluster="beta"}`,
		`brokerium_cluster_health_status{cluster="gamma"}`,
		`group="broken"`,
		`brokerium_broker_disk_used_bytes{cluster="alpha",broker="102"}`,
		`partition=`,
	} {
		if strings.Contains(out, absent) {
			t.Errorf("unexpected %s", absent)
		}
	}
	for _, f := range svc.registry.Gather(context.Background()) {
		if !f.Valid() {
			t.Errorf("invalid family %s", f.Name)
		}
	}
}
