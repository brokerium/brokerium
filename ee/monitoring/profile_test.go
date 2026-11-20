// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/config"
)

func count(name string, v float64) Series {
	return Series{Labels: map[string]string{"__name__": name}, Points: []Point{{T: 1, V: v}}}
}

func TestDetectProfile(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name   string
		series []Series
		want   Profile
	}{
		{"exporter 1.x with KRaft", []Series{count(heapMetric1, 9), count(raftMetric, 3), count(partitionCount, 3)},
			Profile{Exporter: Exporter1, HeapMetric: heapMetric1, KRaft: true, Brokers: 3, DetectedAt: now}},
		{"exporter 0.x with ZooKeeper", []Series{count(heapMetric0, 6), count(partitionCount, 6)},
			Profile{Exporter: Exporter0, HeapMetric: heapMetric0, Brokers: 6, DetectedAt: now}},
		{"mixed generations", []Series{count(heapMetric0, 1), count(heapMetric1, 1)},
			Profile{Exporter: Exporter1, HeapMetric: heapMetric1, DetectedAt: now}},
		{"nothing matches", nil, Profile{DetectedAt: now}},
	}
	for _, tt := range tests {
		q := &recordingQuerier{answer: func(string) []Series { return tt.series }}
		got := detectProfile(context.Background(), q, `job="kafka"`, now)
		if got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, got, tt.want)
		}
		want := `count by (__name__) ({__name__=~"jvm_memory_used_bytes|jvm_memory_bytes_used|kafka_server_raft_metrics_current_leader|` +
			`kafka_server_replicamanager_partitioncount",job="kafka"})`
		if len(q.exprs) != 1 || q.exprs[0] != want {
			t.Errorf("%s: queries %q", tt.name, q.exprs)
		}
	}
	if p := (Profile{}); p.heapMetric() != heapMetric1 || p.idleMetric() != "kafka_server_kafkarequesthandlerpool_requesthandleravgidlepercent_count_total" {
		t.Errorf("an unknown generation uses the names of 1.x: %s, %s", p.heapMetric(), p.idleMetric())
	}
	if p := (Profile{Exporter: Exporter0}); p.idleMetric() != "kafka_server_kafkarequesthandlerpool_requesthandleravgidlepercent_count" {
		t.Errorf("idle metric of 0.x = %s", p.idleMetric())
	}
}

// failingQuerier fails every query.
type failingQuerier struct{ calls int }

func (f *failingQuerier) Query(context.Context, string, time.Time) (*Result, error) {
	f.calls++
	return nil, errors.New("down")
}

func (f *failingQuerier) QueryRange(context.Context, string, time.Time, time.Time, time.Duration) (*Result, error) {
	f.calls++
	return nil, errors.New("down")
}

func TestProfileCache(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, config.Server{}, newFakeStore(), &fakeClusters{})
	now := time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	q := &recordingQuerier{answer: func(string) []Series { return []Series{count(heapMetric1, 1)} }}
	for range 3 {
		if p := svc.profileOf(context.Background(), q, "fp", "c1", `job="a"`); p.Exporter != Exporter1 {
			t.Fatalf("profile = %+v", p)
		}
	}
	if len(q.exprs) != 1 {
		t.Errorf("%d detections within the TTL, want 1", len(q.exprs))
	}
	svc.profileOf(context.Background(), q, "fp", "c1", `job="b"`)
	svc.profileOf(context.Background(), q, "other source", "c1", `job="a"`)
	if len(q.exprs) != 3 {
		t.Errorf("a new selector or source must be detected again: %d detections", len(q.exprs))
	}
	now = now.Add(profileTTL)
	svc.profileOf(context.Background(), q, "fp", "c1", `job="a"`)
	if len(q.exprs) != 4 || len(svc.profiles) != 1 {
		t.Errorf("after the TTL: %d detections, %d cached profiles", len(q.exprs), len(svc.profiles))
	}

	failing := &failingQuerier{}
	if p := svc.profileOf(context.Background(), failing, "fp", "c2", `job="a"`); p.Error != "down" {
		t.Errorf("profile of a failing query = %+v", p)
	}
	svc.profileOf(context.Background(), failing, "fp", "c2", `job="a"`)
	now = now.Add(profileErrorTTL)
	svc.profileOf(context.Background(), failing, "fp", "c2", `job="a"`)
	if failing.calls != 2 {
		t.Errorf("errors are kept for %s: %d calls, want 2", profileErrorTTL, failing.calls)
	}
	svc.forgetSource()
	if len(svc.profiles) != 0 {
		t.Error("forgetSource kept the profiles")
	}
}
