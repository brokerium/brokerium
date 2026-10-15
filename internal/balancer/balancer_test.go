// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package balancer

import (
	"fmt"
	"math/rand/v2"
	"reflect"
	"testing"
)

// cluster builds n brokers with the given racks and topics with round-robin placement on the
// first `used` brokers.
func cluster(racks []string, used int, topics map[string]int, rf int) Model {
	m := Model{}
	for i, r := range racks {
		m.Brokers = append(m.Brokers, Broker{ID: int32(101 + i), Rack: r, Alive: true})
	}
	names := make([]string, 0, len(topics))
	for t := range topics {
		names = append(names, t)
	}
	k := 0
	for _, t := range []string{"orders", "payments", "clicks"} {
		n, ok := topics[t]
		if !ok {
			continue
		}
		for p := 0; p < n; p++ {
			var reps []int32
			for j := 0; j < rf; j++ {
				reps = append(reps, int32(101+(k+j)%used))
			}
			k++
			m.Partitions = append(m.Partitions, Partition{Topic: t, Partition: int32(p), Replicas: reps,
				SizeBytes: int64(1_000_000 * (1 + p%3))})
		}
	}
	_ = names
	return m
}

func applyPlan(m Model, p *Plan) map[string][]int32 {
	out := map[string][]int32{}
	for _, part := range m.Partitions {
		out[fmt.Sprintf("%s-%d", part.Topic, part.Partition)] = part.Replicas
	}
	for _, mv := range p.Moves {
		out[fmt.Sprintf("%s-%d", mv.Topic, mv.Partition)] = mv.To
	}
	return out
}

func checkInvariants(t *testing.T, m Model, p *Plan, excluded map[int32]bool, rackAware bool) {
	t.Helper()
	racks := map[int32]string{}
	for _, b := range m.Brokers {
		racks[b.ID] = b.Rack
	}
	before := map[string][]int32{}
	for _, part := range m.Partitions {
		before[fmt.Sprintf("%s-%d", part.Topic, part.Partition)] = part.Replicas
	}
	for key, reps := range applyPlan(m, p) {
		if len(reps) != len(before[key]) {
			t.Fatalf("%s: replication factor changed %v -> %v", key, before[key], reps)
		}
		seen := map[int32]bool{}
		for _, r := range reps {
			if seen[r] {
				t.Fatalf("%s: duplicate replica in %v", key, reps)
			}
			seen[r] = true
			if excluded[r] && !contains(before[key], r) {
				t.Fatalf("%s: moved onto excluded broker %d", key, r)
			}
		}
		if rackAware {
			distinct := func(rs []int32) int {
				m := map[string]bool{}
				for _, r := range rs {
					m[racks[r]] = true
				}
				return len(m)
			}
			if distinct(reps) < distinct(before[key]) {
				t.Fatalf("%s: rack diversity reduced %v -> %v", key, before[key], reps)
			}
		}
	}
}

func TestScaleOutWithoutRacksIsEven(t *testing.T) {
	m := cluster([]string{"", "", "", "", ""}, 3, map[string]int{"orders": 12, "payments": 6, "clicks": 24}, 3)
	p, err := Compute(m, Options{Mode: ModeEven})
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, m, p, nil, false)
	if p.After.ReplicaSpread > 1 {
		t.Fatalf("replica spread after = %d, want <= 1; brokers %+v", p.After.ReplicaSpread, p.After.Brokers)
	}
	if p.After.LeaderSpread > 1 {
		t.Fatalf("leader spread after = %d, want <= 1", p.After.LeaderSpread)
	}
	for _, b := range p.After.Brokers {
		if b.Replicas < 25 || b.Replicas > 26 {
			t.Fatalf("broker %d has %d replicas, want 25-26", b.ID, b.Replicas)
		}
	}
}

func TestRackConstraintWarnsAndKeepsDiversity(t *testing.T) {
	m := cluster([]string{"rack-a", "rack-b", "rack-c", "rack-a", "rack-b"}, 3,
		map[string]int{"orders": 12, "payments": 6, "clicks": 24}, 3)
	p, err := Compute(m, Options{Mode: ModeEven, RackAware: true})
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, m, p, nil, true)
	if len(p.Warnings) == 0 {
		t.Fatal("expected a rack constraint warning")
	}
	for _, b := range p.After.Brokers {
		if b.Rack == "rack-c" && b.Replicas != 42 {
			t.Fatalf("lone rack-c broker must keep one replica of every partition, got %d", b.Replicas)
		}
		if b.Rack != "rack-c" && b.Replicas != 21 {
			t.Fatalf("broker %d (%s) has %d replicas, want 21", b.ID, b.Rack, b.Replicas)
		}
	}
	if p.After.LeaderSpread > 1 {
		t.Fatalf("leaders should still balance under rack constraints, spread %d", p.After.LeaderSpread)
	}
}

func TestEvacuateEmptiesBroker(t *testing.T) {
	m := cluster([]string{"", "", "", ""}, 4, map[string]int{"orders": 12, "payments": 6}, 3)
	p, err := Compute(m, Options{Mode: ModeEvacuate, Evacuate: []int32{104}})
	if err != nil {
		t.Fatal(err)
	}
	checkInvariants(t, m, p, map[int32]bool{104: true}, false)
	for key, reps := range applyPlan(m, p) {
		if contains(reps, 104) {
			t.Fatalf("%s still on evacuated broker: %v", key, reps)
		}
	}
	for _, mv := range p.Moves {
		if mv.ReplicaMoves != 1 {
			t.Fatalf("evacuation should move exactly one replica per partition, got %+v", mv)
		}
	}
}

func TestLeadersModeOnlyReorders(t *testing.T) {
	m := cluster([]string{"", "", ""}, 3, map[string]int{"orders": 12}, 3)
	for i := range m.Partitions {
		m.Partitions[i].Replicas = []int32{101, 102, 103}
	}
	p, err := Compute(m, Options{Mode: ModeLeaders})
	if err != nil {
		t.Fatal(err)
	}
	for _, mv := range p.Moves {
		if mv.ReplicaMoves != 0 {
			t.Fatalf("leaders mode moved data: %+v", mv)
		}
	}
	if p.Before.LeaderSpread != 12 || p.After.LeaderSpread > 1 {
		t.Fatalf("leader spread before %d after %d", p.Before.LeaderSpread, p.After.LeaderSpread)
	}
	if p.DataToMoveBytes != 0 {
		t.Fatalf("data to move = %d", p.DataToMoveBytes)
	}
}

func TestNoEligibleBrokersFails(t *testing.T) {
	m := cluster([]string{""}, 1, map[string]int{"orders": 1}, 1)
	if _, err := Compute(m, Options{Evacuate: []int32{101}}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestDeterministic(t *testing.T) {
	m := cluster([]string{"a", "b", "c", "a", "b", "c"}, 3, map[string]int{"orders": 30, "payments": 7, "clicks": 11}, 3)
	p1, _ := Compute(m, Options{Mode: ModeEven, RackAware: true})
	p2, _ := Compute(m, Options{Mode: ModeEven, RackAware: true})
	if !reflect.DeepEqual(p1, p2) {
		t.Fatal("plans differ for identical input")
	}
}

func TestRandomClustersKeepInvariants(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2))
	for iter := 0; iter < 40; iter++ {
		nb := 3 + rng.IntN(6)
		racks := make([]string, nb)
		for i := range racks {
			racks[i] = []string{"a", "b", "c"}[rng.IntN(3)]
		}
		m := Model{}
		for i, r := range racks {
			m.Brokers = append(m.Brokers, Broker{ID: int32(i + 1), Rack: r, Alive: true, Excluded: rng.IntN(8) == 0})
		}
		excluded := map[int32]bool{}
		for _, b := range m.Brokers {
			if b.Excluded {
				excluded[b.ID] = true
			}
		}
		for p := 0; p < 10+rng.IntN(40); p++ {
			rf := 1 + rng.IntN(3)
			perm := rng.Perm(nb)
			var reps []int32
			for j := 0; j < rf && j < nb; j++ {
				reps = append(reps, int32(perm[j]+1))
			}
			m.Partitions = append(m.Partitions, Partition{Topic: fmt.Sprintf("t%d", p%4), Partition: int32(p), Replicas: reps,
				SizeBytes: int64(rng.IntN(10_000_000)), BytesInPerSec: float64(rng.IntN(1000))})
		}
		p, err := Compute(m, Options{Mode: ModeEven, RackAware: true, MaxMoves: 200})
		if err != nil {
			continue
		}
		checkInvariants(t, m, p, excluded, true)
		if p.After.Score > p.Before.Score+1e-9 {
			t.Fatalf("iteration %d: score got worse %f -> %f", iter, p.Before.Score, p.After.Score)
		}
	}
}
