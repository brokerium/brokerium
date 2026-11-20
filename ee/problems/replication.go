// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"

	"github.com/brokerium/brokerium/internal/balancer"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
)

// ReplicationParams are the parameters of change-replication-factor.
type ReplicationParams struct {
	Topics            []string `json:"topics"`
	ReplicationFactor int      `json:"replicationFactor"`
	// ThrottleBytesPerSec limits the replication to new replicas; 50 MiB/s by default.
	ThrottleBytesPerSec int64 `json:"throttleBytesPerSec,omitempty"`
	// BatchSize is the number of partitions reassigned at a time; 25 by default.
	BatchSize int `json:"batchSize,omitempty"`
	// RackAware spreads the replicas of a partition over racks; on when every broker has a rack.
	RackAware *bool `json:"rackAware,omitempty"`
}

// topicChange describes the replication factor change of one topic in a plan.
type topicChange struct {
	Topic      string `json:"topic"`
	From       int    `json:"from"`
	To         int    `json:"to"`
	Partitions int    `json:"partitions"`
}

func planChangeReplicationFactor(_ context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := decodeParams[ReplicationParams](raw)
	if err != nil {
		return nil, err
	}
	rf := params.ReplicationFactor
	if len(params.Topics) == 0 {
		return nil, errors.New("topics are required")
	}
	if rf < 1 {
		return nil, errors.New("replicationFactor must be at least 1")
	}
	snap := p.View.Snapshot
	topics, err := lookupTopics(snap, params.Topics)
	if err != nil {
		return nil, err
	}
	for _, r := range snap.Reassignments {
		if slices.Contains(params.Topics, r.Topic) {
			return nil, fmt.Errorf("%s-%d is being reassigned; wait until the reassignment finishes", r.Topic, r.Partition)
		}
	}
	if n := servingCount(snap); rf > n {
		return nil, fmt.Errorf("replication factor %d needs as many serving brokers, and the cluster has %d", rf, n)
	}
	rackAware := ops.RacksKnown(p.View)
	if params.RackAware != nil {
		rackAware = *params.RackAware
	}
	pl := newPlacement(snap, rackAware)
	targets := map[string][]int32{}
	changes := make([]topicChange, 0, len(topics))
	lowered := false
	for _, t := range topics {
		change := topicChange{Topic: t.Name, From: replicationFactor(t), To: rf}
		for _, part := range t.Partitions {
			if len(part.Replicas) == rf {
				continue
			}
			if part.Leader < 0 {
				return nil, fmt.Errorf("%s-%d is offline; bring back its brokers before changing the replication factor", t.Name, part.ID)
			}
			var next []int32
			if len(part.Replicas) < rf {
				if next, err = pl.grow(t.Name, part, rf); err != nil {
					return nil, err
				}
			} else {
				next = pl.shrink(part, rf)
				lowered = true
			}
			targets[fmt.Sprintf("%s-%d", t.Name, part.ID)] = next
			change.Partitions++
		}
		changes = append(changes, change)
	}
	plan, err := balancer.Apply(ops.ModelFrom(p, nil), targets, balancer.Options{RackAware: rackAware, ChangeReplicationFactor: true})
	if err != nil {
		return nil, err
	}
	warnings := append([]string{}, plan.Warnings...)
	for _, t := range topics {
		minISR, known := minInsyncReplicas(t)
		switch {
		case !known:
		case rf < minISR:
			warnings = append(warnings, fmt.Sprintf("%s: replication factor %d is below min.insync.replicas %d, so producers with acks=all fail", t.Name, rf, minISR))
		case rf == minISR && rf > 1:
			warnings = append(warnings, fmt.Sprintf("%s: with replication factor %d equal to min.insync.replicas, producers with acks=all fail while a broker of a partition is down", t.Name, rf))
		}
	}
	if lowered {
		warnings = append(warnings, "lowering the replication factor deletes replicas, so partitions survive fewer broker failures")
	}
	doc := ops.PlanDoc{Warnings: warnings, Moves: plan.Moves, Before: &plan.Before, After: &plan.After,
		DataToMoveBytes: plan.DataToMoveBytes, Details: map[string]any{"replicationFactor": rf, "rackAware": rackAware, "topics": changes}}
	what := topicList(params.Topics)
	if len(plan.Moves) == 0 {
		doc.Summary = fmt.Sprintf("Every partition of %s already has %d replica(s)", what, rf)
		return &ops.Planned{Doc: doc}, nil
	}
	added, removed := 0, 0
	for _, m := range plan.Moves {
		added += m.ReplicaMoves
		removed += len(m.From) + m.ReplicaMoves - len(m.To)
	}
	doc.Summary = fmt.Sprintf("Change the replication factor of %s to %d: %d partition(s) change, %d new replica(s) with %s to copy, %d replica(s) removed",
		what, rf, len(plan.Moves), added, ops.HumanBytes(plan.DataToMoveBytes), removed)
	steps := ops.ReassignStepsGated(plan.Moves, params.ThrottleBytesPerSec, params.BatchSize, ops.HealthGateArgs{IgnoreTopics: params.Topics})
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}

// lookupTopics returns the topics with the given names from a snapshot, in that order.
func lookupTopics(s *kafka.Snapshot, names []string) ([]kafka.Topic, error) {
	byName := map[string]kafka.Topic{}
	for _, t := range s.Topics {
		byName[t.Name] = t
	}
	seen := map[string]bool{}
	out := make([]kafka.Topic, 0, len(names))
	for _, name := range names {
		t, ok := byName[name]
		switch {
		case seen[name]:
			return nil, fmt.Errorf("topic %s is listed twice", name)
		case !ok:
			return nil, fmt.Errorf("topic %s does not exist", name)
		case t.Error != "":
			return nil, fmt.Errorf("topic %s cannot be described: %s", name, t.Error)
		}
		seen[name] = true
		out = append(out, t)
	}
	return out, nil
}

// rackPenalty outweighs any load difference, so that a new replica goes to the rack that the
// partition uses least.
const rackPenalty = 1000

// placement chooses the brokers of new replicas and the replicas to remove, and keeps the load
// of the brokers up to date as it goes, so that one plan spreads its new replicas.
type placement struct {
	ids []int32
	// serving brokers are alive and not fenced; eligible ones are also not cordoned and take
	// new replicas.
	serving, eligible  map[int32]bool
	rack               map[int32]string
	replicas, data     map[int32]float64
	meanRepl, meanData float64
	rackAware          bool
}

func newPlacement(s *kafka.Snapshot, rackAware bool) *placement {
	pl := &placement{serving: map[int32]bool{}, eligible: map[int32]bool{}, rack: map[int32]string{},
		replicas: map[int32]float64{}, data: map[int32]float64{}, rackAware: rackAware}
	n := 0
	for _, b := range s.Brokers {
		pl.ids = append(pl.ids, b.ID)
		pl.rack[b.ID] = b.Rack
		pl.replicas[b.ID] = float64(b.Replicas)
		pl.data[b.ID] = float64(b.DataBytes)
		if !serving(b) {
			continue
		}
		pl.serving[b.ID] = true
		if !b.Cordoned {
			pl.eligible[b.ID] = true
			pl.meanRepl += float64(b.Replicas)
			pl.meanData += float64(b.DataBytes)
			n++
		}
	}
	if n > 0 {
		pl.meanRepl /= float64(n)
		pl.meanData /= float64(n)
	}
	slices.Sort(pl.ids)
	return pl
}

// load scores a broker for a new replica by its replicas and data relative to the mean. The
// floors keep new replicas spreading on empty clusters.
func (pl *placement) load(b int32) float64 {
	return pl.replicas[b]/max(pl.meanRepl, 1) + pl.data[b]/max(pl.meanData, 1<<20)
}

// sameRack counts the replicas of set other than b on the rack of b.
func (pl *placement) sameRack(set []int32, b int32) int {
	n := 0
	for _, r := range set {
		if r != b && pl.rack[r] == pl.rack[b] {
			n++
		}
	}
	return n
}

// grow adds replicas to the end of the replica list of a partition until it has n, on the least
// loaded eligible brokers and, when rack aware, on the racks the partition uses least.
func (pl *placement) grow(topic string, part kafka.Partition, n int) ([]int32, error) {
	out := slices.Clone(part.Replicas)
	for len(out) < n {
		best, bestScore := int32(-1), math.Inf(1)
		for _, b := range pl.ids {
			if !pl.eligible[b] || slices.Contains(out, b) {
				continue
			}
			score := pl.load(b)
			if pl.rackAware {
				score += rackPenalty * float64(pl.sameRack(out, b))
			}
			if score < bestScore {
				best, bestScore = b, score
			}
		}
		if best < 0 {
			return nil, fmt.Errorf("%s-%d: no other broker can take a replica; cordoned and stopped brokers take none", topic, part.ID)
		}
		out = append(out, best)
		pl.replicas[best]++
		pl.data[best] += float64(part.SizeBytes)
	}
	return out, nil
}

// shrink removes replicas from a partition until it has n. It keeps the leader and the
// preferred replica while it can, and removes replicas from the end of the list: first those on
// brokers that do not serve, then those out of sync, then, when rack aware, those that share a
// rack with another replica.
func (pl *placement) shrink(part kafka.Partition, n int) []int32 {
	out := slices.Clone(part.Replicas)
	for len(out) > n {
		drop, dropScore := -1, math.MinInt
		for i := len(out) - 1; i >= 0; i-- {
			r := out[i]
			score := 0
			switch {
			case r == part.Leader && pl.serving[r]:
				score = -2000
			case i == 0:
				score = -1000
			}
			if !pl.serving[r] {
				score += 100
			}
			if !slices.Contains(part.ISR, r) {
				score += 10
			}
			if pl.rackAware && pl.sameRack(out, r) > 0 {
				score++
			}
			if score > dropScore {
				drop, dropScore = i, score
			}
		}
		r := out[drop]
		out = slices.Delete(out, drop, drop+1)
		pl.replicas[r]--
		pl.data[r] -= float64(part.SizeBytes)
	}
	return out
}

// topicList names topics for a summary: "topic orders" or "3 topics".
func topicList(names []string) string {
	if len(names) == 1 {
		return "topic " + names[0]
	}
	return fmt.Sprintf("%d topics", len(names))
}
