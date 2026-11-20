// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brokerium/brokerium/ee/monitoring/exposition"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/config"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/metrics"
	"github.com/brokerium/brokerium/internal/secrets"
	"github.com/brokerium/brokerium/internal/store"
)

// fakeStore keeps the rows of the monitoring feature in memory.
type fakeStore struct {
	mu         sync.Mutex
	settings   map[string]*store.Setting
	monitoring map[string]*store.ClusterMonitoring
	clusters   []*store.Cluster
	operations []*store.Operation
}

func newFakeStore(clusters ...*store.Cluster) *fakeStore {
	return &fakeStore{settings: map[string]*store.Setting{}, monitoring: map[string]*store.ClusterMonitoring{}, clusters: clusters}
}

func (f *fakeStore) GetSetting(_ context.Context, key string) (*store.Setting, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st, ok := f.settings[key]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *st
	return &cp, nil
}

func (f *fakeStore) SetSetting(_ context.Context, st *store.Setting) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	st.UpdatedAt = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	cp := *st
	f.settings[st.Key] = &cp
	return nil
}

func (f *fakeStore) DeleteSetting(_ context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.settings, key)
	return nil
}

func (f *fakeStore) GetClusterMonitoring(_ context.Context, clusterID string) (*store.ClusterMonitoring, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, ok := f.monitoring[clusterID]
	if !ok {
		return nil, store.ErrNotFound
	}
	cp := *m
	return &cp, nil
}

func (f *fakeStore) SetClusterMonitoring(_ context.Context, m *store.ClusterMonitoring) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	found := false
	for _, c := range f.clusters {
		found = found || c.ID == m.ClusterID
	}
	if !found {
		return store.ErrNotFound
	}
	cp := *m
	f.monitoring[m.ClusterID] = &cp
	return nil
}

func (f *fakeStore) ListClusters(context.Context) ([]*store.Cluster, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.clusters, nil
}

func (f *fakeStore) OperationsInStatus(_ context.Context, statuses ...string) ([]*store.Operation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.Operation
	for _, op := range f.operations {
		for _, s := range statuses {
			if op.Status == s {
				out = append(out, op)
			}
		}
	}
	return out, nil
}

// fakeClusters serves fixed views.
type fakeClusters struct {
	views     map[string]*cluster.View
	listeners []func(*cluster.View)
}

func (f *fakeClusters) View(_ context.Context, id string, _ time.Duration) (*cluster.View, error) {
	v, ok := f.views[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	return v, nil
}

func (f *fakeClusters) OnRefresh(fn func(*cluster.View)) {
	f.listeners = append(f.listeners, fn)
}

func (f *fakeClusters) refresh(v *cluster.View) {
	for _, fn := range f.listeners {
		fn(v)
	}
}

// alphaView is a KRaft cluster with three brokers and dedicated controllers whose endpoints the
// quorum reports.
func alphaView() *cluster.View {
	c := &store.Cluster{ID: "c-alpha", Name: "alpha", Provider: store.ProviderExternal, Flavor: store.FlavorApache,
		BootstrapServers: []string{"kafka-1.example.com:9092"}, KafkaClusterID: "kafka-id-1"}
	snap := &kafka.Snapshot{
		CollectedAt: time.Date(2026, 11, 20, 11, 59, 50, 0, time.UTC),
		Brokers: []kafka.Broker{
			{ID: 101, Host: "kafka-1.example.com", Port: 9092, Alive: true, DiskTotalBytes: 1000, DiskUsableBytes: 400, DataBytes: 500,
				LogDirs: []kafka.LogDir{{Path: "/data"}}},
			{ID: 102, Host: "kafka-2.example.com", Port: 9092, Alive: true},
			{ID: 103, Host: "kafka-3.example.com", Port: 9092, Fenced: true},
		},
		Quorum: &kafka.Quorum{LeaderID: 1, Voters: []kafka.QuorumReplica{
			{NodeID: 1, Role: "leader", Endpoints: []kafka.Endpoint{{Name: "CONTROLLER", Host: "controller-1.example.com", Port: 9093}}},
		}},
		Groups: []kafka.Group{
			{ID: "billing", Type: kafka.GroupTypeConsumer, Members: 2, Lag: 7, TopicLag: map[string]int64{"orders": 5, "audit": 2}},
			{ID: "broken", Type: kafka.GroupTypeConsumer, Error: "not authorized"},
		},
	}
	return &cluster.View{Cluster: c, Snapshot: snap, UpdatedAt: time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC),
		Health: cluster.Health{Status: cluster.HealthWarning, Brokers: 3, BrokersAlive: 2, BrokersFenced: 1, Topics: 4, Partitions: 12,
			UnderReplicated: 2, UnderMinISR: 1, Voters: 1}}
}

// promAnswer answers a query of the fake Prometheus with a status and a JSON body.
type promAnswer func(endpoint string, form url.Values) (int, string)

// fakeProm is a Prometheus HTTP API that records the queries it receives.
type fakeProm struct {
	*httptest.Server
	mu       sync.Mutex
	requests []promRequest
	answer   promAnswer
}

type promRequest struct {
	Method, Endpoint, Auth string
	Form                   url.Values
}

func newFakeProm(t *testing.T, answer promAnswer) *fakeProm {
	t.Helper()
	f := &fakeProm{answer: answer}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.Close)
	return f
}

func (f *fakeProm) serve(w http.ResponseWriter, r *http.Request) {
	endpoint := r.URL.Path[strings.LastIndex(r.URL.Path, "/api/v1/")+len("/api/v1/"):]
	body, _ := io.ReadAll(r.Body)
	form, _ := url.ParseQuery(string(body))
	f.mu.Lock()
	f.requests = append(f.requests, promRequest{Method: r.Method, Endpoint: endpoint, Auth: r.Header.Get("Authorization"), Form: form})
	answer := f.answer
	f.mu.Unlock()
	status, out := answer(endpoint, form)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, out)
}

func (f *fakeProm) queries() []promRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]promRequest(nil), f.requests...)
}

func (f *fakeProm) setAnswer(answer promAnswer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.answer = answer
}

// standardAnswer answers like a Prometheus that scrapes exporters 1.x with the KRaft
// configuration on three brokers.
func standardAnswer(endpoint string, form url.Values) (int, string) {
	query := form.Get("query")
	switch {
	case endpoint == "status/buildinfo":
		return http.StatusOK, `{"status":"success","data":{"version":"3.7.3"}}`
	case query == "vector(1)":
		return http.StatusOK, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1763640000,"1"]}]}}`
	case strings.HasPrefix(query, "count by (__name__)"):
		return http.StatusOK, `{"status":"success","data":{"resultType":"vector","result":[
			{"metric":{"__name__":"jvm_memory_used_bytes"},"value":[1763640000,"9"]},
			{"metric":{"__name__":"kafka_server_raft_metrics_current_leader"},"value":[1763640000,"3"]},
			{"metric":{"__name__":"kafka_server_replicamanager_partitioncount"},"value":[1763640000,"3"]}]}}`
	case strings.HasPrefix(query, "topk("):
		return http.StatusOK, `{"status":"success","data":{"resultType":"vector","result":[
			{"metric":{"topic":"payments.v1"},"value":[1763640000,"10"]},
			{"metric":{"topic":"orders"},"value":[1763640000,"30"]}]}}`
	case endpoint == "query_range" && strings.Contains(query, "by (instance)"):
		return http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[
			{"metric":{"instance":"kafka-2.example.com:9141"},"values":[[1763636400,"2"],[1763636415,"NaN"]]},
			{"metric":{"instance":"kafka-10.example.com:9141"},"values":[[1763636400,"5"]]},
			{"metric":{"instance":"kafka-1.example.com:9141"},"values":[[1763636400,"1"],[1763636415,"1.5"]]}]}}`
	case endpoint == "query_range" && strings.Contains(query, "by (topic)"):
		return http.StatusOK, `{"status":"success","data":{"resultType":"matrix","result":[
			{"metric":{"topic":"payments.v1"},"values":[[1763636400,"1"]]},
			{"metric":{"topic":"orders"},"values":[[1763636400,"3"]]}]}}`
	case endpoint == "query_range":
		return http.StatusOK, fmt.Sprintf(`{"status":"success","warnings":["fake warning"],"data":{"resultType":"matrix","result":[
			{"metric":{},"values":[[1763636400,"%d"],[1763636415,"4"]]}]}}`, len(query)%7)
	}
	return http.StatusBadRequest, `{"status":"error","errorType":"bad_data","error":"unexpected query"}`
}

func testBox(t *testing.T) *secrets.Box {
	t.Helper()
	box, err := secrets.New(make([]byte, 32))
	if err != nil {
		t.Fatal(err)
	}
	return box
}

// newTestService builds a service over fakes; the clock stands at 2026-11-20 12:00 UTC.
func newTestService(t *testing.T, cfg config.Server, st *fakeStore, clusters *fakeClusters) *Service {
	t.Helper()
	svc, err := New(Deps{Config: cfg, Store: st, Box: testBox(t), Clusters: clusters, Sampler: metrics.NewSampler(5 * time.Minute),
		Log: slog.New(slog.DiscardHandler), Registry: &exposition.Registry{}})
	if err != nil {
		t.Fatal(err)
	}
	svc.now = func() time.Time { return time.Date(2026, 11, 20, 12, 0, 7, 0, time.UTC) }
	return svc
}
