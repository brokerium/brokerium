// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package balancer computes partition reassignment plans (ADR 0006). It is pure and
// deterministic: no I/O, no clock, no randomness.
package balancer

import (
	"errors"
	"fmt"
	"math"
	"sort"
)

// Modes of a plan.
const (
	ModeEven     = "even"     // balance replicas, disk, traffic, topic spread and leaders
	ModeReplicas = "replicas" // balance replicas only, keep preferred leaders where possible
	ModeLeaders  = "leaders"  // only reorder replicas to balance preferred leaders (no data movement)
	ModeEvacuate = "evacuate" // only move replicas off evacuated brokers
)

// Broker is a broker in the model.
type Broker struct {
	ID            int32
	Rack          string
	CapacityBytes int64
	Alive         bool
	// Excluded brokers receive no new replicas (cordoned, draining, decommissioning, dead).
	Excluded bool
}

// Partition is a partition in the model.
type Partition struct {
	Topic         string
	Partition     int32
	Replicas      []int32
	SizeBytes     int64
	BytesInPerSec float64
}

// Model is the input of the planner.
type Model struct {
	Brokers    []Broker
	Partitions []Partition
}

// Weights tune the soft goals.
type Weights struct {
	Replicas float64
	Disk     float64
	BytesIn  float64
	Topic    float64
}

// DefaultWeights favor replica counts, then disk, then traffic and topic spread.
var DefaultWeights = Weights{Replicas: 2.0, Disk: 1.0, BytesIn: 0.5, Topic: 0.5}

// Options control a plan.
type Options struct {
	Mode string
	// RackAware keeps replicas of a partition on distinct racks whenever the input allowed it.
	RackAware bool
	// MaxMoves limits replica moves of the soft phase (0 means 1000).
	MaxMoves int
	// Topics limits balancing to these topics (nil means all).
	Topics map[string]bool
	// Evacuate lists brokers that must end up without replicas.
	Evacuate []int32
	Weights  Weights
}

// Move changes the replica list of one partition.
type Move struct {
	Topic        string  `json:"topic"`
	Partition    int32   `json:"partition"`
	From         []int32 `json:"from"`
	To           []int32 `json:"to"`
	SizeBytes    int64   `json:"sizeBytes"`
	ReplicaMoves int     `json:"replicaMoves"`
	LeaderChange bool    `json:"leaderChange"`
}

// BrokerLoad is the load of one broker.
type BrokerLoad struct {
	ID            int32   `json:"id"`
	Rack          string  `json:"rack"`
	Replicas      int     `json:"replicas"`
	Leaders       int     `json:"leaders"`
	DataBytes     int64   `json:"dataBytes"`
	BytesInPerSec float64 `json:"bytesInPerSec"`
	Excluded      bool    `json:"excluded"`
}

// Stats describe a distribution.
type Stats struct {
	Brokers         []BrokerLoad `json:"brokers"`
	ReplicaSpread   int          `json:"replicaSpread"`
	LeaderSpread    int          `json:"leaderSpread"`
	DataSpreadBytes int64        `json:"dataSpreadBytes"`
	Score           float64      `json:"score"`
}

// Plan is the result of the planner.
type Plan struct {
	Moves           []Move   `json:"moves"`
	Before          Stats    `json:"before"`
	After           Stats    `json:"after"`
	DataToMoveBytes int64    `json:"dataToMoveBytes"`
	Warnings        []string `json:"warnings"`
}

type state struct {
	brokers   []Broker
	index     map[int32]int // broker id -> position in brokers
	eligible  []bool        // by position
	evacuate  map[int32]bool
	parts     []Partition
	replicas  [][]int32 // current assignment by partition index
	inScope   []bool
	replicaN  []float64 // per broker
	data      []float64
	bytesIn   []float64
	topicN    map[string][]float64
	topicSize map[string]int
	opts      Options
	w         Weights
	totals    struct{ replicas, data, bytesIn float64 }
}

// Compute builds a plan for the model.
func Compute(m Model, o Options) (*Plan, error) {
	if len(m.Brokers) == 0 {
		return nil, errors.New("no brokers")
	}
	if o.Mode == "" {
		o.Mode = ModeEven
	}
	if o.MaxMoves <= 0 {
		o.MaxMoves = 1000
	}
	if o.Weights == (Weights{}) {
		o.Weights = DefaultWeights
	}
	s := newState(m, o)
	plan := &Plan{Warnings: []string{}}
	plan.Before = s.stats()

	if s.eligibleCount() == 0 {
		return nil, errors.New("no eligible brokers: every broker is excluded, evacuated or offline")
	}
	s.evacuatePhase(plan)
	if o.Mode == ModeEven || o.Mode == ModeReplicas {
		s.softPhase(o.Mode == ModeEven)
	}
	if o.Mode == ModeEven || o.Mode == ModeLeaders {
		s.leaderPhase()
	}
	s.rackWarnings(plan)

	for i, p := range s.parts {
		if equal(p.Replicas, s.replicas[i]) {
			continue
		}
		mv := Move{Topic: p.Topic, Partition: p.Partition, From: p.Replicas, To: s.replicas[i], SizeBytes: p.SizeBytes,
			LeaderChange: len(p.Replicas) > 0 && len(s.replicas[i]) > 0 && p.Replicas[0] != s.replicas[i][0]}
		for _, r := range s.replicas[i] {
			if !contains(p.Replicas, r) {
				mv.ReplicaMoves++
			}
		}
		plan.DataToMoveBytes += int64(mv.ReplicaMoves) * p.SizeBytes
		plan.Moves = append(plan.Moves, mv)
	}
	if plan.Moves == nil {
		plan.Moves = []Move{}
	}
	plan.After = s.stats()
	return plan, nil
}

// Apply evaluates explicit target replica lists keyed by "topic-partition" and returns the
// resulting plan with before and after statistics.
func Apply(m Model, targets map[string][]int32, o Options) (*Plan, error) {
	if o.Weights == (Weights{}) {
		o.Weights = DefaultWeights
	}
	s := newState(m, o)
	plan := &Plan{Warnings: []string{}, Moves: []Move{}}
	plan.Before = s.stats()
	for pi, p := range s.parts {
		target, ok := targets[fmt.Sprintf("%s-%d", p.Topic, p.Partition)]
		if !ok {
			continue
		}
		if len(target) != len(p.Replicas) {
			return nil, fmt.Errorf("%s-%d: changing the replication factor is not supported (%d -> %d)",
				p.Topic, p.Partition, len(p.Replicas), len(target))
		}
		for _, r := range s.replicas[pi] {
			s.add(pi, r, -1)
		}
		s.replicas[pi] = append([]int32(nil), target...)
		for _, r := range target {
			s.add(pi, r, 1)
		}
		mv := Move{Topic: p.Topic, Partition: p.Partition, From: p.Replicas, To: target, SizeBytes: p.SizeBytes,
			LeaderChange: p.Replicas[0] != target[0]}
		for _, r := range target {
			if !contains(p.Replicas, r) {
				mv.ReplicaMoves++
			}
		}
		if o.RackAware {
			distinct := func(rs []int32) int {
				m := map[string]bool{}
				for _, r := range rs {
					m[s.rack(r)] = true
				}
				return len(m)
			}
			if distinct(target) < distinct(p.Replicas) {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf("%s-%d: the new replicas span fewer racks", p.Topic, p.Partition))
			}
		}
		plan.DataToMoveBytes += int64(mv.ReplicaMoves) * p.SizeBytes
		plan.Moves = append(plan.Moves, mv)
	}
	plan.After = s.stats()
	return plan, nil
}

func newState(m Model, o Options) *state {
	s := &state{opts: o, w: o.Weights, index: map[int32]int{}, evacuate: map[int32]bool{},
		topicN: map[string][]float64{}, topicSize: map[string]int{}}
	s.brokers = append([]Broker(nil), m.Brokers...)
	sort.Slice(s.brokers, func(i, j int) bool { return s.brokers[i].ID < s.brokers[j].ID })
	for _, id := range o.Evacuate {
		s.evacuate[id] = true
	}
	s.eligible = make([]bool, len(s.brokers))
	for i, b := range s.brokers {
		s.index[b.ID] = i
		s.eligible[i] = b.Alive && !b.Excluded && !s.evacuate[b.ID]
	}
	s.parts = append([]Partition(nil), m.Partitions...)
	sort.Slice(s.parts, func(i, j int) bool {
		a, b := s.parts[i], s.parts[j]
		return a.Topic < b.Topic || (a.Topic == b.Topic && a.Partition < b.Partition)
	})
	n := len(s.brokers)
	s.replicaN, s.data, s.bytesIn = make([]float64, n), make([]float64, n), make([]float64, n)
	s.replicas = make([][]int32, len(s.parts))
	s.inScope = make([]bool, len(s.parts))
	for i, p := range s.parts {
		s.replicas[i] = append([]int32(nil), p.Replicas...)
		s.inScope[i] = o.Topics == nil || o.Topics[p.Topic]
		if s.topicN[p.Topic] == nil {
			s.topicN[p.Topic] = make([]float64, n)
		}
		s.topicSize[p.Topic] += len(p.Replicas)
		for _, r := range p.Replicas {
			s.add(i, r, 1)
		}
	}
	return s
}

func (s *state) add(pi int, broker int32, sign float64) {
	bi, ok := s.index[broker]
	if !ok {
		return
	}
	p := s.parts[pi]
	s.replicaN[bi] += sign
	s.data[bi] += sign * float64(p.SizeBytes)
	s.bytesIn[bi] += sign * p.BytesInPerSec
	s.topicN[p.Topic][bi] += sign
	s.totals.replicas += sign
	s.totals.data += sign * float64(p.SizeBytes)
	s.totals.bytesIn += sign * p.BytesInPerSec
}

func (s *state) eligibleCount() int {
	n := 0
	for _, e := range s.eligible {
		if e {
			n++
		}
	}
	return n
}

func (s *state) rack(id int32) string {
	if bi, ok := s.index[id]; ok {
		return s.brokers[bi].Rack
	}
	return ""
}

// rackOK reports whether replacing replica `from` by `to` keeps rack diversity of the set.
func (s *state) rackOK(set []int32, from, to int32) bool {
	if !s.opts.RackAware {
		return true
	}
	rt := s.rack(to)
	if rt == s.rack(from) {
		return true
	}
	for _, r := range set {
		if r != from && s.rack(r) == rt {
			return false
		}
	}
	return true
}

func (s *state) canPlace(pi, bi int, set []int32, from int32) bool {
	b := s.brokers[bi]
	if !s.eligible[bi] || contains(set, b.ID) || !s.rackOK(set, from, b.ID) {
		return false
	}
	if b.CapacityBytes > 0 && s.data[bi]+float64(s.parts[pi].SizeBytes) > 0.9*float64(b.CapacityBytes) {
		return false
	}
	return true
}

// evacuatePhase moves every replica off evacuated, excluded-and-listed or unknown brokers.
func (s *state) evacuatePhase(plan *Plan) {
	for pi := range s.parts {
		for ri, r := range s.replicas[pi] {
			bi, known := s.index[r]
			if known && !s.evacuate[r] {
				continue
			}
			best, bestScore := -1, math.Inf(1)
			for cand := range s.brokers {
				if !s.canPlace(pi, cand, s.replicas[pi], r) {
					continue
				}
				score := s.replicaN[cand] + s.data[cand]/math.Max(1, s.meanPositive(s.data))
				if known && s.brokers[cand].Rack == s.brokers[bi].Rack {
					score -= 0.5
				}
				if score < bestScore {
					best, bestScore = cand, score
				}
			}
			p := s.parts[pi]
			if best < 0 {
				plan.Warnings = append(plan.Warnings, fmt.Sprintf(
					"cannot move %s-%d off broker %d: no eligible broker satisfies replication factor, rack and capacity constraints",
					p.Topic, p.Partition, r))
				continue
			}
			s.add(pi, r, -1)
			s.replicas[pi][ri] = s.brokers[best].ID
			s.add(pi, s.brokers[best].ID, 1)
		}
	}
}

func (s *state) meanPositive(v []float64) float64 {
	sum, n := 0.0, 0
	for i, x := range v {
		if s.eligible[i] {
			sum += x
			n++
		}
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}

// objective returns the weighted normalized variance of loads over eligible brokers.
func (s *state) objective() float64 {
	return s.w.Replicas*s.normVar(s.replicaN) + s.w.Disk*s.normVar(s.data) + s.w.BytesIn*s.normVar(s.bytesIn) +
		s.w.Topic*s.topicTerm()
}

func (s *state) normVar(v []float64) float64 {
	mean := s.meanPositive(v)
	if mean <= 0 {
		return 0
	}
	sum, n := 0.0, 0
	for i, x := range v {
		if s.eligible[i] {
			d := (x - mean) / mean
			sum += d * d
			n++
		}
	}
	return sum / float64(n)
}

func (s *state) topicTerm() float64 {
	total := 0.0
	names := make([]string, 0, len(s.topicN))
	for t := range s.topicN {
		names = append(names, t)
	}
	sort.Strings(names)
	for _, t := range names {
		total += s.normVar(s.topicN[t]) * float64(s.topicSize[t]) / math.Max(1, s.totals.replicas)
	}
	return total
}

// varDelta is the change of a normalized variance term when amount d moves from a load xa to
// a load xb, with the mean m over n eligible brokers unchanged.
func varDelta(d, xa, xb, m float64, n int) float64 {
	if m <= 0 || n == 0 || d == 0 {
		return 0
	}
	return 2 * d * (xb - xa + d) / (float64(n) * m * m)
}

type means struct {
	n                     int
	replicas, data, bytes float64
	topic                 map[string]float64
}

func (s *state) means() means {
	m := means{n: s.eligibleCount(), replicas: s.meanPositive(s.replicaN), data: s.meanPositive(s.data),
		bytes: s.meanPositive(s.bytesIn), topic: map[string]float64{}}
	for t, v := range s.topicN {
		m.topic[t] = s.meanPositive(v)
	}
	return m
}

func (s *state) topicWeight(t string) float64 {
	return s.w.Topic * float64(s.topicSize[t]) / math.Max(1, s.totals.replicas)
}

// moveDelta is the objective change of moving one replica of partition pi from broker index a
// to broker index b (both eligible).
func (s *state) moveDelta(pi, a, b int, m means) float64 {
	p := s.parts[pi]
	tv := s.topicN[p.Topic]
	return s.w.Replicas*varDelta(1, s.replicaN[a], s.replicaN[b], m.replicas, m.n) +
		s.w.Disk*varDelta(float64(p.SizeBytes), s.data[a], s.data[b], m.data, m.n) +
		s.w.BytesIn*varDelta(p.BytesInPerSec, s.bytesIn[a], s.bytesIn[b], m.bytes, m.n) +
		s.topicWeight(p.Topic)*varDelta(1, tv[a], tv[b], m.topic[p.Topic], m.n)
}

// swapDelta is the objective change of moving a replica of p1 from a to b and a replica of p2
// from b to a.
func (s *state) swapDelta(p1, p2, a, b int, m means) float64 {
	x, y := s.parts[p1], s.parts[p2]
	d := s.w.Disk*varDelta(float64(x.SizeBytes-y.SizeBytes), s.data[a], s.data[b], m.data, m.n) +
		s.w.BytesIn*varDelta(x.BytesInPerSec-y.BytesInPerSec, s.bytesIn[a], s.bytesIn[b], m.bytes, m.n)
	if x.Topic != y.Topic {
		tx, ty := s.topicN[x.Topic], s.topicN[y.Topic]
		d += s.topicWeight(x.Topic)*varDelta(1, tx[a], tx[b], m.topic[x.Topic], m.n) +
			s.topicWeight(y.Topic)*varDelta(1, ty[b], ty[a], m.topic[y.Topic], m.n)
	}
	return d
}

func (s *state) replace(pi int, from, to int32) {
	for i, r := range s.replicas[pi] {
		if r == from {
			s.add(pi, from, -1)
			s.replicas[pi][i] = to
			s.add(pi, to, 1)
			return
		}
	}
}

// secondaryDelta is the change of disk, traffic and topic-spread terms for a single move.
func (s *state) secondaryDelta(pi, a, b int, m means) float64 {
	p := s.parts[pi]
	tv := s.topicN[p.Topic]
	return s.w.Disk*varDelta(float64(p.SizeBytes), s.data[a], s.data[b], m.data, m.n) +
		s.w.BytesIn*varDelta(p.BytesInPerSec, s.bytesIn[a], s.bytesIn[b], m.bytes, m.n) +
		s.topicWeight(p.Topic)*varDelta(1, tv[a], tv[b], m.topic[p.Topic], m.n)
}

// softPhase balances goals in priority order, like a goal hierarchy: first replica counts (only
// moves that strictly shrink the spread, choosing the one that helps secondary goals most), then
// disk, traffic and topic spread through swaps that keep replica counts unchanged.
func (s *state) softPhase(withSwaps bool) {
	totalData := math.Max(1, s.totals.data)
	moves := 0
	for ; moves < s.opts.MaxMoves; moves++ {
		m := s.means()
		bestDelta, bestPi, bestFrom, bestTo := math.Inf(1), -1, int32(0), -1
		for pi := range s.parts {
			if !s.inScope[pi] {
				continue
			}
			set := s.replicas[pi]
			for _, r := range set {
				src, ok := s.index[r]
				if !ok || !s.eligible[src] {
					continue
				}
				for to := range s.brokers {
					if to == src || s.replicaN[src]-s.replicaN[to] < 2 || !s.canPlace(pi, to, set, r) {
						continue
					}
					delta := s.secondaryDelta(pi, src, to, m) + 1e-6*float64(s.parts[pi].SizeBytes)/totalData -
						1e-3*(s.replicaN[src]-s.replicaN[to])
					if delta < bestDelta {
						bestDelta, bestPi, bestFrom, bestTo = delta, pi, r, to
					}
				}
			}
		}
		if bestPi < 0 {
			break
		}
		s.replace(bestPi, bestFrom, s.brokers[bestTo].ID)
	}
	if !withSwaps {
		return
	}
	for ; moves < s.opts.MaxMoves; moves += 2 {
		if !s.bestSwap() {
			return
		}
	}
}

func (s *state) bestSwap() bool {
	const epsilon = 1e-12
	m := s.means()
	order := []int{}
	for i := range s.brokers {
		if s.eligible[i] {
			order = append(order, i)
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if s.data[order[i]] != s.data[order[j]] {
			return s.data[order[i]] > s.data[order[j]]
		}
		return order[i] < order[j]
	})
	on := func(bi int) []int {
		var out []int
		id := s.brokers[bi].ID
		for pi := range s.parts {
			if s.inScope[pi] && contains(s.replicas[pi], id) {
				out = append(out, pi)
			}
		}
		return out
	}
	for x := 0; x < len(order); x++ {
		for y := len(order) - 1; y > x; y-- {
			a, b := order[x], order[y]
			idA, idB := s.brokers[a].ID, s.brokers[b].ID
			best, bp1, bp2 := -epsilon, -1, -1
			pa, pb := on(a), on(b)
			for _, p1 := range pa {
				if contains(s.replicas[p1], idB) || !s.canPlace(p1, b, s.replicas[p1], idA) {
					continue
				}
				for _, p2 := range pb {
					if p1 == p2 || contains(s.replicas[p2], idA) || !s.canPlace(p2, a, s.replicas[p2], idB) {
						continue
					}
					if d := s.swapDelta(p1, p2, a, b, m); d < best {
						best, bp1, bp2 = d, p1, p2
					}
				}
			}
			if bp1 >= 0 {
				s.replace(bp1, idA, idB)
				s.replace(bp2, idB, idA)
				return true
			}
		}
	}
	return false
}

// leaderPhase reorders replica lists so that preferred leaders spread evenly over eligible
// brokers. It never changes replica sets.
func (s *state) leaderPhase() {
	leaders := make([]float64, len(s.brokers))
	for pi := range s.parts {
		if len(s.replicas[pi]) > 0 {
			if bi, ok := s.index[s.replicas[pi][0]]; ok {
				leaders[bi]++
			}
		}
	}
	for pass := 0; pass < 3; pass++ {
		changed := false
		for pi := range s.parts {
			set := s.replicas[pi]
			if !s.inScope[pi] || len(set) < 2 {
				continue
			}
			cur, ok := s.index[set[0]]
			if !ok {
				continue
			}
			best := cur
			for _, r := range set[1:] {
				bi, ok := s.index[r]
				if !ok || !s.eligible[bi] {
					continue
				}
				if leaders[bi]+1 < leaders[best] || (!s.eligible[best] && s.eligible[bi]) {
					best = bi
				}
			}
			if best == cur {
				continue
			}
			leaders[cur]--
			leaders[best]++
			bestID := s.brokers[best].ID
			reordered := []int32{bestID}
			for _, r := range set {
				if r != bestID {
					reordered = append(reordered, r)
				}
			}
			s.replicas[pi] = reordered
			changed = true
		}
		if !changed {
			return
		}
	}
}

func (s *state) rackWarnings(plan *Plan) {
	if !s.opts.RackAware {
		return
	}
	racks := map[string]int{}
	for i, b := range s.brokers {
		if s.eligible[i] && b.Rack != "" {
			racks[b.Rack]++
		}
	}
	if len(racks) < 2 {
		return
	}
	maxRF := 0
	for pi := range s.parts {
		maxRF = max(maxRF, len(s.replicas[pi]))
	}
	if maxRF < len(racks) {
		return
	}
	minR, maxR := math.MaxInt, 0
	var short []string
	for r, n := range racks {
		minR, maxR = min(minR, n), max(maxR, n)
		_ = r
	}
	if minR == maxR {
		return
	}
	for r, n := range racks {
		if n == minR {
			short = append(short, r)
		}
	}
	sort.Strings(short)
	plan.Warnings = append(plan.Warnings, fmt.Sprintf(
		"rack-aware placement: with replication factor %d over %d racks every partition keeps one replica per rack, so rack(s) %v with %d broker(s) carry more replicas per broker; add brokers to these racks for an even spread",
		maxRF, len(racks), short, minR))
}

func (s *state) stats() Stats {
	st := Stats{}
	leaders := make([]int, len(s.brokers))
	for pi := range s.parts {
		if len(s.replicas[pi]) > 0 {
			if bi, ok := s.index[s.replicas[pi][0]]; ok {
				leaders[bi]++
			}
		}
	}
	minR, maxR, minL, maxL := math.MaxInt, 0, math.MaxInt, 0
	var minD, maxD int64 = math.MaxInt64, 0
	for i, b := range s.brokers {
		bl := BrokerLoad{ID: b.ID, Rack: b.Rack, Replicas: int(s.replicaN[i]), Leaders: leaders[i],
			DataBytes: int64(s.data[i]), BytesInPerSec: s.bytesIn[i], Excluded: !s.eligible[i]}
		st.Brokers = append(st.Brokers, bl)
		if !s.eligible[i] {
			continue
		}
		minR, maxR = min(minR, bl.Replicas), max(maxR, bl.Replicas)
		minL, maxL = min(minL, bl.Leaders), max(maxL, bl.Leaders)
		minD, maxD = min(minD, bl.DataBytes), max(maxD, bl.DataBytes)
	}
	if maxR >= minR {
		st.ReplicaSpread, st.LeaderSpread, st.DataSpreadBytes = maxR-minR, maxL-minL, maxD-minD
	}
	st.Score = math.Round(s.objective()*1e6) / 1e6
	return st
}

func contains(set []int32, x int32) bool {
	for _, v := range set {
		if v == x {
			return true
		}
	}
	return false
}

func equal(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
