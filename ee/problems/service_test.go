// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/store"
)

// fakeStore keeps problem states of any cluster in memory.
type fakeStore struct {
	mu        sync.Mutex
	nodeLists int
	sightings [][]store.ProblemKey
	states    map[string]*store.ProblemState
}

func (f *fakeStore) ListClusters(context.Context) ([]*store.Cluster, error) {
	return []*store.Cluster{{ID: "c1"}}, nil
}

func (f *fakeStore) ListNodes(context.Context, string) ([]*store.Node, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nodeLists++
	return nil, nil
}

func (f *fakeStore) SeeProblems(_ context.Context, clusterID string, seen []store.ProblemKey, at time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(seen) == 0 {
		return nil
	}
	f.sightings = append(f.sightings, seen)
	for _, k := range seen {
		f.state(clusterID, k, at).LastSeen = at
	}
	return nil
}

// state returns the state of a finding, created at a time when it has none.
func (f *fakeStore) state(clusterID string, k store.ProblemKey, at time.Time) *store.ProblemState {
	if f.states == nil {
		f.states = map[string]*store.ProblemState{}
	}
	st, ok := f.states[clusterID+"/"+k.Fingerprint]
	if !ok {
		st = &store.ProblemState{ClusterID: clusterID, Fingerprint: k.Fingerprint, Code: k.Code, Object: k.Object,
			FirstSeen: at, LastSeen: at}
		f.states[clusterID+"/"+k.Fingerprint] = st
	}
	return st
}

func (f *fakeStore) ProblemStates(_ context.Context, clusterID string) ([]*store.ProblemState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*store.ProblemState
	for _, st := range f.states {
		if clusterID == "" || st.ClusterID == clusterID {
			cp := *st
			out = append(out, &cp)
		}
	}
	return out, nil
}

func (f *fakeStore) AcknowledgeProblem(_ context.Context, clusterID string, k store.ProblemKey, actor string, at time.Time) (*store.ProblemState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state(clusterID, k, at)
	st.AcknowledgedBy, st.AcknowledgedAt = actor, &at
	return st, nil
}

func (f *fakeStore) UnacknowledgeProblem(context.Context, string, string) (*store.ProblemState, error) {
	return nil, store.ErrNotFound
}

func (f *fakeStore) MuteProblem(_ context.Context, clusterID string, k store.ProblemKey, until, now time.Time) (*store.ProblemState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	st := f.state(clusterID, k, now)
	st.MutedUntil = &until
	return st, nil
}

func (f *fakeStore) UnmuteProblem(context.Context, string, string) (*store.ProblemState, error) {
	return nil, store.ErrNotFound
}

func (f *fakeStore) ForgetProblems(context.Context, time.Time) (int64, error) { return 0, nil }

// fakeViews serves fixed views and remembers the refresh callback.
type fakeViews struct {
	views   []*cluster.View
	refresh func(*cluster.View)
}

func (f *fakeViews) OnRefresh(fn func(*cluster.View)) { f.refresh = fn }

func (f *fakeViews) View(_ context.Context, id string, _ time.Duration) (*cluster.View, error) {
	for _, v := range f.views {
		if v.Cluster.ID == id {
			return v, nil
		}
	}
	return nil, store.ErrNotFound
}

func (f *fakeViews) Views(context.Context, time.Duration) ([]*cluster.View, error) {
	return f.views, nil
}

func testService(views ...*cluster.View) (*Service, *fakeStore, *time.Time) {
	st := &fakeStore{}
	s := newService(st, &fakeViews{views: views}, slog.New(slog.DiscardHandler))
	now := testNow
	s.now = func() time.Time { return now }
	return s, st, &now
}

func TestRecordThrottlesLastSeen(t *testing.T) {
	t.Parallel()
	v := view(brokers(3), topic("orders", 1, 3, "1"), topic("payments", 1, 3, "1"))
	s, st, now := testService(v)
	ctx := t.Context()
	fs, err := s.findings(ctx, v)
	if err != nil || len(fs) != 2 {
		t.Fatalf("findings %v, %v", codesOf(fs), err)
	}
	record := func(fs []Finding) {
		t.Helper()
		if err := s.record(ctx, "c1", fs); err != nil {
			t.Fatal(err)
		}
	}
	record(fs)
	*now = now.Add(30 * time.Second)
	record(fs)
	if len(st.sightings) != 1 || len(st.sightings[0]) != 2 {
		t.Fatalf("sightings %v, want one write of both findings", st.sightings)
	}
	fresh := Finding{Fingerprint: "new", Code: "topic-rf-one", Object: Object{Kind: ObjectTopic, ID: "clicks"}}
	record(append(fs, fresh))
	if len(st.sightings) != 2 || len(st.sightings[1]) != 1 || st.sightings[1][0].Fingerprint != "new" {
		t.Fatalf("a new finding is written at once: %v", st.sightings)
	}
	*now = now.Add(31 * time.Second)
	record(append(fs, fresh))
	if len(st.sightings) != 3 || len(st.sightings[2]) != 2 {
		t.Errorf("after a minute the older findings are written again: %v", st.sightings)
	}
}

func TestFindingsOncePerSnapshot(t *testing.T) {
	t.Parallel()
	v := view(brokers(3), topic("orders", 1, 3, "1"))
	s, st, _ := testService(v)
	for range 3 {
		if _, err := s.findings(t.Context(), v); err != nil {
			t.Fatal(err)
		}
	}
	newer := *v
	newer.UpdatedAt = v.UpdatedAt.Add(time.Second)
	if _, err := s.findings(t.Context(), &newer); err != nil {
		t.Fatal(err)
	}
	if st.nodeLists != 2 {
		t.Errorf("the rules ran %d times for 2 snapshots", st.nodeLists)
	}
}

func TestProblemsAndCounts(t *testing.T) {
	t.Parallel()
	v := view(brokers(3), topic("orders", 1, 3, "2"), topic("payments", 1, 3, "1"), topic("audit", 3, 3, "1"))
	s, st, now := testService(v)
	ctx := t.Context()
	problems, at, err := s.Problems(ctx, "c1")
	if err != nil || len(problems) != 3 || !at.Equal(testNow) {
		t.Fatalf("problems %d at %s: %v", len(problems), at, err)
	}
	for _, p := range problems {
		if !p.FirstSeen.Equal(testNow) || !p.LastSeen.Equal(testNow) || p.MutedUntil != nil || p.AcknowledgedBy != "" {
			t.Errorf("a problem without a state: %+v", p)
		}
	}
	earlier := testNow.Add(-time.Hour)
	st.state("c1", problems[0].key(), earlier)
	if _, err := st.AcknowledgeProblem(ctx, "c1", problems[0].key(), "alice", testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MuteProblem(ctx, "c1", problems[1].key(), testNow.Add(time.Hour), testNow); err != nil {
		t.Fatal(err)
	}
	if _, err := st.MuteProblem(ctx, "c1", problems[2].key(), testNow.Add(-time.Minute), testNow); err != nil {
		t.Fatal(err)
	}
	problems, _, err = s.Problems(ctx, "c1")
	if err != nil {
		t.Fatal(err)
	}
	if p := problems[0]; !p.FirstSeen.Equal(earlier) || p.AcknowledgedBy != "alice" || p.AcknowledgedAt == nil {
		t.Errorf("acknowledged problem %+v", p)
	}
	if problems[1].MutedUntil == nil || problems[2].MutedUntil != nil {
		t.Errorf("mutes %v and %v; an expired mute does not count", problems[1].MutedUntil, problems[2].MutedUntil)
	}
	counts, err := s.Counts(ctx)
	if err != nil || len(counts) != 1 {
		t.Fatalf("counts %+v, %v", counts, err)
	}
	if c := counts[0]; c.ClusterID != "c1" || c.Critical != 1 || c.Warning != 0 || c.Info != 1 || c.Muted != 1 {
		t.Errorf("counts %+v, want 1 critical, the muted warning and 1 info", c)
	}
	*now = now.Add(2 * time.Hour)
	if counts, _ := s.Counts(ctx); counts[0].Muted != 0 || counts[0].Warning != 1 {
		t.Errorf("after the mute ended: %+v", counts[0])
	}
	if _, err := s.current(ctx, "c1", "missing"); err != errNotCurrent {
		t.Errorf("current of an unknown fingerprint = %v", err)
	}
	if _, _, err := s.Problems(ctx, "nope"); err != store.ErrNotFound {
		t.Errorf("problems of an unknown cluster = %v", err)
	}
}

func TestRefreshesAreRecorded(t *testing.T) {
	t.Parallel()
	v := view(brokers(3), topic("orders", 1, 3, "1"))
	st := &fakeStore{}
	views := &fakeViews{views: []*cluster.View{v}}
	s := newService(st, views, slog.New(slog.DiscardHandler))
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	s.Start(ctx)
	views.refresh(v)
	deadline := time.Now().Add(5 * time.Second)
	for {
		st.mu.Lock()
		n := len(st.sightings)
		st.mu.Unlock()
		if n == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the refreshed view was not recorded")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
