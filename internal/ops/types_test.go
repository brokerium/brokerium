// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"reflect"
	"testing"

	"github.com/brokerium/brokerium/internal/balancer"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
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
			steps := reassignSteps(tc.moves, 0, tc.batch, HealthGateArgs{})
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
	view := func(change func(v *cluster.View)) *cluster.View {
		v := &cluster.View{Health: cluster.Health{Mode: kafka.ModeKRaft, QuorumLeader: 1, Controller: 1, Voters: 3},
			Snapshot: &kafka.Snapshot{Mode: kafka.ModeKRaft, Brokers: []kafka.Broker{
				{ID: 101, Alive: true, DiskTotalBytes: 100, DiskUsableBytes: 50},
				{ID: 102, Alive: true, DiskTotalBytes: 100, DiskUsableBytes: 50},
				{ID: 103, Alive: true},
			}}}
		change(v)
		return v
	}
	tests := []struct {
		name string
		v    *cluster.View
		args HealthGateArgs
		want []string
	}{
		{"healthy", view(func(*cluster.View) {}), HealthGateArgs{}, nil},
		{"under-replicated partitions", view(func(v *cluster.View) { v.Health.UnderReplicated = 2 }), HealthGateArgs{},
			[]string{"2 under-replicated partition(s)"}},
		{"under-replicated partitions allowed", view(func(v *cluster.View) { v.Health.UnderReplicated = 2 }),
			HealthGateArgs{AllowUnderReplicated: true}, nil},
		{"partitions and the quorum", view(func(v *cluster.View) {
			v.Health.QuorumLeader, v.Health.OfflinePartitions, v.Health.UnderMinISR, v.Health.VotersLagging = -1, 1, 1, 1
		}), HealthGateArgs{AllowUnderReplicated: true}, []string{"1 offline partition(s)", "1 partition(s) under min.insync.replicas",
			"no controller quorum leader", "1 lagging controller voter(s)"}},
		{"a fenced broker", view(func(v *cluster.View) { v.Snapshot.Brokers[2].Fenced, v.Snapshot.Brokers[2].Alive = true, false }),
			HealthGateArgs{}, []string{"broker(s) 103 fenced or offline"}},
		{"the fenced broker the operation replaces", view(func(v *cluster.View) {
			v.Snapshot.Brokers[2].Fenced, v.Snapshot.Brokers[2].Alive = true, false
		}), HealthGateArgs{ExpectFenced: []int32{103}}, nil},
		{"another fenced broker than the one replaced", view(func(v *cluster.View) {
			v.Snapshot.Brokers[1].Fenced, v.Snapshot.Brokers[2].Fenced = true, true
		}), HealthGateArgs{ExpectFenced: []int32{103}}, []string{"broker(s) 102 fenced or offline"}},
		{"full disks", view(func(v *cluster.View) {
			v.Snapshot.Brokers[0].DiskUsableBytes, v.Snapshot.Brokers[1].DiskUsableBytes = 10, 7
		}), HealthGateArgs{}, []string{"broker(s) 101 (90%), 102 (93%) with volumes at least 90% full"}},
		{"ZooKeeper with a controller", view(func(v *cluster.View) {
			v.Health = cluster.Health{Mode: kafka.ModeZooKeeper, QuorumLeader: -1, Controller: 101}
		}), HealthGateArgs{}, nil},
		{"ZooKeeper without a controller", view(func(v *cluster.View) {
			v.Health = cluster.Health{Mode: kafka.ModeZooKeeper, QuorumLeader: -1, Controller: -1}
		}), HealthGateArgs{}, []string{"no active controller"}},
	}
	for _, tc := range tests {
		if got := HealthProblems(tc.v, tc.args); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s: HealthProblems = %q, want %q", tc.name, got, tc.want)
		}
	}
}
