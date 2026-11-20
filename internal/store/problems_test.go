// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package store

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestProblemStates(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	c := &Cluster{Name: "test-" + NewID(), Provider: ProviderExternal}
	if err := s.CreateCluster(ctx, c); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.DeleteCluster(context.Background(), c.ID) })
	states := func() map[string]*ProblemState {
		t.Helper()
		list, err := s.ProblemStates(ctx, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]*ProblemState{}
		for _, p := range list {
			out[p.Fingerprint] = p
		}
		return out
	}
	start := time.Now().UTC().Truncate(time.Second)
	rf := ProblemKey{Fingerprint: "f1", Code: "topic-rf-one", Object: "topic:orders"}
	skew := ProblemKey{Fingerprint: "f2", Code: "replica-skew", Object: "cluster:" + c.ID}

	if err := s.SeeProblems(ctx, c.ID, []ProblemKey{rf, skew}, start); err != nil {
		t.Fatal(err)
	}
	if err := s.SeeProblems(ctx, c.ID, []ProblemKey{rf}, start.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if err := s.SeeProblems(ctx, c.ID, []ProblemKey{rf}, start.Add(30*time.Second)); err != nil {
		t.Fatal(err)
	}
	got := states()
	if len(got) != 2 || !got["f1"].FirstSeen.Equal(start) || !got["f1"].LastSeen.Equal(start.Add(time.Minute)) ||
		got["f1"].Code != rf.Code || got["f1"].Object != rf.Object || !got["f2"].LastSeen.Equal(start) {
		t.Fatalf("states after sightings: %+v %+v", got["f1"], got["f2"])
	}

	p, err := s.AcknowledgeProblem(ctx, c.ID, rf, "alice", start.Add(2*time.Minute))
	if err != nil || p.AcknowledgedBy != "alice" || p.AcknowledgedAt == nil || !p.FirstSeen.Equal(start) {
		t.Fatalf("AcknowledgeProblem = %+v, %v", p, err)
	}
	until := start.Add(time.Hour)
	if p, err = s.MuteProblem(ctx, c.ID, rf, until, start.Add(3*time.Minute)); err != nil || p.MutedUntil == nil ||
		!p.MutedUntil.Equal(until) || p.AcknowledgedBy != "alice" {
		t.Fatalf("MuteProblem = %+v, %v", p, err)
	}
	if p, err = s.UnacknowledgeProblem(ctx, c.ID, "f1"); err != nil || p.AcknowledgedBy != "" || p.AcknowledgedAt != nil {
		t.Fatalf("UnacknowledgeProblem = %+v, %v", p, err)
	}
	if p, err = s.UnmuteProblem(ctx, c.ID, "f1"); err != nil || p.MutedUntil != nil {
		t.Fatalf("UnmuteProblem = %+v, %v", p, err)
	}
	if _, err := s.UnmuteProblem(ctx, c.ID, "missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("UnmuteProblem of a finding without a state = %v, want ErrNotFound", err)
	}

	// Acknowledging or muting a finding that has no state yet creates it.
	fresh := ProblemKey{Fingerprint: "f3", Code: "agent-not-seen", Object: "node:n1"}
	if p, err = s.MuteProblem(ctx, c.ID, fresh, until, start); err != nil || !p.FirstSeen.Equal(start) || p.Code != fresh.Code {
		t.Fatalf("MuteProblem of a new finding = %+v, %v", p, err)
	}

	if n, err := s.ForgetProblems(ctx, start.Add(30*time.Second)); err != nil || n < 2 {
		t.Fatalf("ForgetProblems = %d, %v", n, err)
	}
	if got := states(); len(got) != 1 || got["f1"] == nil {
		t.Errorf("states after forgetting: %v", got)
	}
	if err := s.DeleteCluster(ctx, c.ID); err != nil {
		t.Fatal(err)
	}
	if got := states(); len(got) != 0 {
		t.Errorf("the states of a deleted cluster remain: %v", got)
	}
}
