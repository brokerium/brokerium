// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"reflect"
	"testing"

	"github.com/bvaliev/brokerium/internal/balancer"
	"github.com/bvaliev/brokerium/internal/cluster"
)

func kinds(steps []StepSpec) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		out = append(out, s.Kind)
	}
	return out
}

func TestReassignSteps(t *testing.T) {
	t.Parallel()
	copyMove := func(p int32) balancer.Move {
		return balancer.Move{Topic: "t", Partition: p, From: []int32{1, 2}, To: []int32{1, 3}, ReplicaMoves: 1}
	}
	leaderMove := func(p int32) balancer.Move {
		return balancer.Move{Topic: "t", Partition: p, From: []int32{1, 2}, To: []int32{2, 1}, LeaderChange: true}
	}
	tests := []struct {
		name  string
		moves []balancer.Move
		batch int
		want  []string
	}{
		{"data moves are throttled and the throttle is always removed", []balancer.Move{copyMove(0), copyMove(1), copyMove(2)}, 2,
			[]string{KindHealthGate, KindThrottle,
				KindReassign, KindWaitReassign, KindHealthGate,
				KindReassign, KindWaitReassign, KindHealthGate,
				KindClearThrottle, KindElectPreferred}},
		{"leader-only moves skip throttles", []balancer.Move{leaderMove(0), leaderMove(1)}, 0,
			[]string{KindHealthGate, KindReassign, KindWaitReassign, KindHealthGate, KindElectPreferred}},
		{"one data move among leader moves needs throttles", []balancer.Move{leaderMove(0), copyMove(1)}, 0,
			[]string{KindHealthGate, KindThrottle, KindReassign, KindWaitReassign, KindHealthGate, KindClearThrottle, KindElectPreferred}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			steps := reassignStepsURP(tc.moves, 0, tc.batch, false)
			if got := kinds(steps); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("kinds = %v\nwant    %v", got, tc.want)
			}
			for _, s := range steps {
				if s.Kind == KindClearThrottle && !s.AlwaysRun {
					t.Error("clear-throttle must always run")
				}
				if s.Kind == KindThrottle && s.Args.(ThrottleArgs).RateBytesPerSec != DefaultThrottle {
					t.Errorf("default throttle not applied: %+v", s.Args)
				}
			}
		})
	}
}

func TestHealthProblems(t *testing.T) {
	t.Parallel()
	ok := cluster.Health{QuorumLeader: 1, Voters: 3}
	if p := HealthProblems(ok, false); len(p) != 0 {
		t.Errorf("healthy cluster fails the gate: %v", p)
	}
	urp := ok
	urp.UnderReplicated = 2
	if p := HealthProblems(urp, false); len(p) != 1 {
		t.Errorf("under-replicated partitions pass the gate: %v", p)
	}
	if p := HealthProblems(urp, true); len(p) != 0 {
		t.Errorf("allowURP still blocks: %v", p)
	}
	bad := cluster.Health{QuorumLeader: -1, Voters: 3, OfflinePartitions: 1, UnderMinISR: 1, VotersLagging: 1}
	if p := HealthProblems(bad, true); len(p) != 4 {
		t.Errorf("expected 4 problems, got %v", p)
	}
}
