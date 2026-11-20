// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"encoding/json"
	"log/slog"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
)

func planner(v *cluster.View) *ops.Planner {
	return &ops.Planner{Engine: ops.New(nil, nil, nil, slog.New(slog.DiscardHandler)), Cluster: v.Cluster, View: v}
}

func planRF(t *testing.T, v *cluster.View, params string) *ops.Planned {
	t.Helper()
	planned, err := planChangeReplicationFactor(t.Context(), planner(v), json.RawMessage(params))
	if err != nil {
		t.Fatal(err)
	}
	return planned
}

// targets returns the planned replica lists by partition.
func targets(planned *ops.Planned) map[int32][]int32 {
	out := map[int32][]int32{}
	for _, m := range planned.Doc.Moves {
		out[m.Partition] = m.To
	}
	return out
}

func stepKinds(planned *ops.Planned) []string {
	out := []string{}
	for _, s := range planned.Steps {
		out = append(out, s.Kind)
	}
	return out
}

// onBrokers returns a topic whose partition i has the replicas lists[i], led by the first.
func onBrokers(name string, minISR string, lists ...[]int32) kafka.Topic {
	t := kafka.Topic{Name: name, Configs: map[string]string{}, MinISR: 1}
	if minISR != "" {
		t.Configs["min.insync.replicas"] = minISR
	}
	for i, r := range lists {
		t.Partitions = append(t.Partitions, kafka.Partition{ID: int32(i), Leader: r[0], Replicas: r, ISR: slices.Clone(r), SizeBytes: 1 << 20})
	}
	return t
}

func TestRaiseReplicationFactor(t *testing.T) {
	t.Parallel()
	v := view(brokers(3), onBrokers("orders", "2", []int32{101}, []int32{102}, []int32{103}))
	planned := planRF(t, v, `{"topics": ["orders"], "replicationFactor": 3}`)
	want := map[int32][]int32{0: {101, 102, 103}, 1: {102, 101, 103}, 2: {103, 101, 102}}
	got := targets(planned)
	for p, w := range want {
		if got[p][0] != w[0] || len(got[p]) != 3 || !sameSet(got[p], w) {
			t.Errorf("partition %d: %v, want %v (first replica kept)", p, got[p], w)
		}
	}
	if planned.Doc.DataToMoveBytes != 6<<20 || !strings.Contains(planned.Doc.Summary, "6 new replica(s)") {
		t.Errorf("summary %q, data %d", planned.Doc.Summary, planned.Doc.DataToMoveBytes)
	}
	wantKinds := []string{ops.KindHealthGate, ops.KindThrottle, ops.KindReassign, ops.KindWaitReassign, ops.KindHealthGate,
		ops.KindClearThrottle, ops.KindElectPreferred}
	if got := stepKinds(planned); !reflect.DeepEqual(got, wantKinds) {
		t.Errorf("steps %v, want %v", got, wantKinds)
	}
	gate := planned.Steps[0].Args.(ops.HealthGateArgs)
	if !reflect.DeepEqual(gate.IgnoreTopics, []string{"orders"}) || gate.AllowUnderReplicated {
		t.Errorf("health gate %+v", gate)
	}
	if planned.Doc.After == nil || planned.Doc.After.ReplicaSpread != 0 {
		t.Errorf("after %+v", planned.Doc.After)
	}
}

func sameSet(a, b []int32) bool {
	x, y := slices.Clone(a), slices.Clone(b)
	slices.Sort(x)
	slices.Sort(y)
	return slices.Equal(x, y)
}

func TestNewReplicasFollowLoadAndRacks(t *testing.T) {
	t.Parallel()
	bs := brokers(4) // racks r1, r2, r3, r1
	bs[1].Replicas, bs[2].Replicas = 10, 10
	v := view(bs, onBrokers("orders", "", []int32{101}))
	if got := targets(planRF(t, v, `{"topics": ["orders"], "replicationFactor": 2}`))[0]; !reflect.DeepEqual(got, []int32{101, 102}) {
		t.Errorf("rack aware: %v, want a broker on another rack than 101", got)
	}
	if got := targets(planRF(t, v, `{"topics": ["orders"], "replicationFactor": 2, "rackAware": false}`))[0]; !reflect.DeepEqual(got, []int32{101, 104}) {
		t.Errorf("without racks: %v, want the least loaded broker 104", got)
	}

	var lists [][]int32
	for range 6 {
		lists = append(lists, []int32{101})
	}
	v = view(brokers(3), onBrokers("events", "", lists...))
	per := map[int32]int{}
	for _, r := range targets(planRF(t, v, `{"topics": ["events"], "replicationFactor": 2}`)) {
		per[r[1]]++
	}
	if per[102] != 3 || per[103] != 3 {
		t.Errorf("new replicas per broker %v, want 3 on 102 and 103", per)
	}

	cordoned := brokers(3)
	cordoned[2].Cordoned = true
	v = view(cordoned, onBrokers("orders", "", []int32{101}))
	if _, err := planChangeReplicationFactor(t.Context(), planner(v), json.RawMessage(`{"topics": ["orders"], "replicationFactor": 3}`)); err == nil ||
		!strings.Contains(err.Error(), "no other broker can take a replica") {
		t.Errorf("a cordoned broker took a replica: %v", err)
	}
}

func TestLowerReplicationFactor(t *testing.T) {
	t.Parallel()
	topic := onBrokers("orders", "1", []int32{101, 102, 103}, []int32{101, 102, 103}, []int32{101, 102, 103}, []int32{101, 102, 103})
	topic.Partitions[0].Leader, topic.Partitions[0].ISR = 102, []int32{101, 102}
	topic.Partitions[2].Leader = 103
	topic.Partitions[3].Leader = 102
	v := view(brokers(3), topic)
	planned := planRF(t, v, `{"topics": ["orders"], "replicationFactor": 2}`)
	got := targets(planned)
	want := map[int32][]int32{0: {101, 102}, 1: {101, 102}, 2: {101, 103}, 3: {101, 102}}
	for p, w := range want {
		if !reflect.DeepEqual(got[p], w) {
			t.Errorf("partition %d: %v, want %v", p, got[p], w)
		}
	}
	if planned.Doc.DataToMoveBytes != 0 || !strings.Contains(planned.Doc.Summary, "4 replica(s) removed") ||
		!slices.ContainsFunc(planned.Doc.Warnings, func(w string) bool { return strings.Contains(w, "deletes replicas") }) {
		t.Errorf("plan %+v", planned.Doc)
	}
	if kinds := stepKinds(planned); slices.Contains(kinds, ops.KindThrottle) {
		t.Errorf("lowering the replication factor copies no data, so it needs no throttle: %v", kinds)
	}
	if got := targets(planRF(t, v, `{"topics": ["orders"], "replicationFactor": 1}`)); !reflect.DeepEqual(got[0], []int32{102}) ||
		!reflect.DeepEqual(got[2], []int32{103}) || !reflect.DeepEqual(got[1], []int32{101}) {
		t.Errorf("down to one replica the leader stays: %v", got)
	}
}

func TestPlanReplicationFactorRefusals(t *testing.T) {
	t.Parallel()
	offline := onBrokers("offline", "", []int32{101})
	offline.Partitions[0].Leader = -1
	moving := view(brokers(3), onBrokers("orders", "", []int32{101}), offline)
	moving.Snapshot.Reassignments = []kafka.Reassignment{{Topic: "orders", Partition: 0}}
	tests := []struct {
		name, params, err string
	}{
		{"no topics", `{"replicationFactor": 2}`, "topics are required"},
		{"no replication factor", `{"topics": ["offline"]}`, "at least 1"},
		{"unknown topic", `{"topics": ["nope"], "replicationFactor": 2}`, "does not exist"},
		{"listed twice", `{"topics": ["offline", "offline"], "replicationFactor": 2}`, "listed twice"},
		{"more replicas than brokers", `{"topics": ["offline"], "replicationFactor": 4}`, "needs as many serving brokers"},
		{"reassignment in progress", `{"topics": ["orders"], "replicationFactor": 2}`, "is being reassigned"},
		{"offline partition", `{"topics": ["offline"], "replicationFactor": 2}`, "is offline"},
		{"unknown parameter", `{"topics": ["offline"], "replicationFactor": 2, "mode": "x"}`, "invalid params"},
	}
	for _, tc := range tests {
		_, err := planChangeReplicationFactor(t.Context(), planner(moving), json.RawMessage(tc.params))
		if err == nil || !strings.Contains(err.Error(), tc.err) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.err)
		}
	}
}

func TestPlanReplicationFactorWarnings(t *testing.T) {
	t.Parallel()
	v := view(brokers(3), onBrokers("orders", "2", []int32{101, 102, 103}), onBrokers("audit", "2", []int32{101}))
	planned := planRF(t, v, `{"topics": ["orders"], "replicationFactor": 3}`)
	if len(planned.Steps) != 0 || !strings.Contains(planned.Doc.Summary, "already has 3 replica(s)") {
		t.Errorf("nothing to change: %q, %d steps", planned.Doc.Summary, len(planned.Steps))
	}
	planned = planRF(t, v, `{"topics": ["audit"], "replicationFactor": 2}`)
	if !slices.ContainsFunc(planned.Doc.Warnings, func(w string) bool { return strings.Contains(w, "equal to min.insync.replicas") }) {
		t.Errorf("warnings %v", planned.Doc.Warnings)
	}
	details := planned.Doc.Details["topics"].([]topicChange)
	if !reflect.DeepEqual(details, []topicChange{{Topic: "audit", From: 1, To: 2, Partitions: 1}}) {
		t.Errorf("details %+v", details)
	}
}
