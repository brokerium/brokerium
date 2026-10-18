// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/brokerium/brokerium/internal/balancer"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
)

// PlanDoc is the plan shown to the user before confirmation.
type PlanDoc struct {
	Summary         string          `json:"summary"`
	Warnings        []string        `json:"warnings"`
	Irreversible    bool            `json:"irreversible"`
	Moves           []balancer.Move `json:"moves,omitempty"`
	Before          *balancer.Stats `json:"before,omitempty"`
	After           *balancer.Stats `json:"after,omitempty"`
	DataToMoveBytes int64           `json:"dataToMoveBytes"`
	Details         map[string]any  `json:"details,omitempty"`
}

// Defaults for reassignments.
const (
	DefaultThrottle  = 50 * 1024 * 1024
	DefaultBatchSize = 25
)

type rebalanceParams struct {
	Mode                string   `json:"mode"`
	ExcludeBrokers      []int32  `json:"excludeBrokers"`
	Topics              []string `json:"topics"`
	MaxMoves            int      `json:"maxMoves"`
	ThrottleBytesPerSec int64    `json:"throttleBytesPerSec"`
	BatchSize           int      `json:"batchSize"`
	RackAware           *bool    `json:"rackAware"`
}

type moveParams struct {
	Moves               []kafka.Assignment `json:"moves"`
	ThrottleBytesPerSec int64              `json:"throttleBytesPerSec"`
	BatchSize           int                `json:"batchSize"`
}

type brokerParams struct {
	BrokerID            *int32 `json:"brokerId"`
	Cordon              bool   `json:"cordon"`
	Decommission        bool   `json:"decommission"`
	ThrottleBytesPerSec int64  `json:"throttleBytesPerSec"`
}

type nodeParams struct {
	NodeID *int32 `json:"nodeId"`
}

type topicsParams struct {
	Topics []string `json:"topics"`
}

func builtinTypes() []OpType {
	return []OpType{
		{Type: "rebalance", Title: "Rebalance", Scope: "cluster", Requires: []string{"reassign"},
			Description: "Move replicas and leaders so that brokers carry an even load (replicas, disk, traffic, topic spread).",
			Plan:        planRebalance},
		{Type: "move-partitions", Title: "Move partitions", Scope: "cluster", Requires: []string{"reassign"},
			Description: "Set the replica list of chosen partitions by hand.", Plan: planMovePartitions},
		{Type: "elect-leaders", Title: "Elect preferred leaders", Scope: "cluster", Requires: []string{"electLeaders"},
			Description: "Give leadership back to the preferred (first) replica of each partition.", Plan: planElectLeaders},
		{Type: "cordon-broker", Title: "Cordon or uncordon broker", Scope: "broker", Requires: []string{"cordon"},
			Description: "Stop placing new replicas on a broker (KIP-1066), or allow it again.", Plan: planCordon},
		{Type: "drain-broker", Title: "Drain broker", Scope: "broker", Requires: []string{"reassign", "cordon"},
			Description: "Cordon a broker and move all its replicas away; optionally decommission it (stop and unregister).",
			Plan:        planDrain},
		{Type: "unregister-broker", Title: "Unregister broker", Scope: "broker", Requires: []string{"unregisterBrokers"},
			Irreversible: true, Description: "Remove the registration of a stopped broker that holds no replicas.",
			Plan: planUnregister},
		{Type: "remove-controller", Title: "Remove controller voter", Scope: "controller", Requires: []string{"removeVoters"},
			Description: "Remove a voter from the dynamic KRaft quorum (the node keeps running as an observer until stopped).",
			Plan:        planRemoveController},
		{Type: "finalize-upgrade", Title: "Finalize upgrade", Scope: "cluster", Requires: []string{"finalizeFeatures"},
			Irreversible: true, Description: "Raise metadata.version and other features to the highest level all nodes support.",
			Plan: planFinalize},
	}
}

func parse[T any](raw json.RawMessage) (T, error) {
	var v T
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("invalid params: %w", err)
	}
	return v, nil
}

// modelFrom converts a snapshot into a balancer model. Cordoned and fenced brokers receive no
// new replicas; excluded brokers too.
func modelFrom(p *Planner, exclude map[int32]bool) balancer.Model {
	m := balancer.Model{}
	var rates map[string]float64
	if p.Engine.Sampler != nil {
		if r := p.Engine.Sampler.Rates(p.Cluster.ID); r != nil {
			rates = map[string]float64{}
			for k, v := range r.Partitions {
				rates[k] = v.BytesInPerSec
			}
		}
	}
	for _, b := range p.View.Snapshot.Brokers {
		m.Brokers = append(m.Brokers, balancer.Broker{ID: b.ID, Rack: b.Rack, CapacityBytes: b.DiskTotalBytes,
			Alive: b.Alive && !b.Fenced, Excluded: b.Cordoned || exclude[b.ID]})
	}
	for _, t := range p.View.Snapshot.Topics {
		for _, part := range t.Partitions {
			key := fmt.Sprintf("%s-%d", t.Name, part.ID)
			m.Partitions = append(m.Partitions, balancer.Partition{Topic: t.Name, Partition: part.ID,
				Replicas: part.Replicas, SizeBytes: part.SizeBytes, BytesInPerSec: rates[key]})
		}
	}
	return m
}

func racksKnown(v *cluster.View) bool {
	for _, b := range v.Snapshot.Brokers {
		if b.Rack == "" {
			return false
		}
	}
	return len(v.Snapshot.Brokers) > 0
}

// reassignStepsURP turns moves into throttled, batched, health-gated steps.
func reassignStepsURP(moves []balancer.Move, throttle int64, batch int, allowURP bool) []StepSpec {
	if throttle <= 0 {
		throttle = DefaultThrottle
	}
	if batch <= 0 {
		batch = DefaultBatchSize
	}
	var assignments []kafka.Assignment
	copiesData := false
	for _, mv := range moves {
		assignments = append(assignments, kafka.Assignment{Topic: mv.Topic, Partition: mv.Partition, Replicas: mv.To})
		copiesData = copiesData || mv.ReplicaMoves > 0
	}
	throttleArgs := ThrottleArgs{RateBytesPerSec: throttle, Assignments: assignments}
	steps := []StepSpec{
		{Name: "Check cluster health", Kind: KindHealthGate, Args: HealthGateArgs{AllowUnderReplicated: allowURP}},
	}
	if copiesData {
		steps = append(steps, StepSpec{Name: fmt.Sprintf("Throttle replication to %s/s", humanBytes(throttle)), Kind: KindThrottle, Args: throttleArgs})
	}
	for i := 0; i < len(assignments); i += batch {
		end := min(i+batch, len(assignments))
		part := assignments[i:end]
		n := i/batch + 1
		steps = append(steps,
			StepSpec{Name: fmt.Sprintf("Start batch %d (%d partitions)", n, len(part)), Kind: KindReassign, Args: ReassignArgs{Assignments: part}},
			StepSpec{Name: fmt.Sprintf("Wait for batch %d", n), Kind: KindWaitReassign, Args: ReassignArgs{Assignments: part}},
			StepSpec{Name: fmt.Sprintf("Check health after batch %d", n), Kind: KindHealthGate, Args: HealthGateArgs{AllowUnderReplicated: allowURP}},
		)
	}
	if copiesData {
		steps = append(steps, StepSpec{Name: "Remove replication throttles", Kind: KindClearThrottle, AlwaysRun: true, Args: throttleArgs})
	}
	return append(steps, StepSpec{Name: "Elect preferred leaders", Kind: KindElectPreferred, Args: ReassignArgs{Assignments: assignments}})
}

// ReassignSteps exposes reassignSteps to node lifecycle operations. allowURP lets the health
// gates pass while partitions are under-replicated, for replacing a failed broker.
func ReassignSteps(moves []balancer.Move, throttle int64, batch int, allowURP bool) []StepSpec {
	return reassignStepsURP(moves, throttle, batch, allowURP)
}

// EvacuateSteps plans moving every replica off the given brokers: throttled, batched
// reassignment steps followed by a check that each broker is empty.
func EvacuateSteps(p *Planner, brokers []int32) ([]StepSpec, *balancer.Plan, error) {
	exclude := map[int32]bool{}
	for _, b := range brokers {
		exclude[b] = true
	}
	plan, err := balancer.Compute(modelFrom(p, exclude), balancer.Options{
		Mode: balancer.ModeEvacuate, Evacuate: brokers, RackAware: racksKnown(p.View)})
	if err != nil {
		return nil, nil, err
	}
	for _, w := range plan.Warnings {
		if strings.HasPrefix(w, "cannot move") {
			return nil, nil, fmt.Errorf("brokers %v cannot be emptied: %s", brokers, w)
		}
	}
	steps := reassignStepsURP(plan.Moves, 0, 0, false)
	for _, b := range brokers {
		steps = append(steps, StepSpec{Name: fmt.Sprintf("Verify broker %d is empty", b), Kind: KindVerifyEmpty, Args: BrokerArgs{BrokerID: b}})
	}
	return steps, plan, nil
}

// PlanFinalize is the built-in finalize-upgrade planner, raising features through the Admin API.
func PlanFinalize(ctx context.Context, p *Planner, raw json.RawMessage) (*Planned, error) {
	return planFinalize(ctx, p, raw)
}

func planRebalance(_ context.Context, p *Planner, raw json.RawMessage) (*Planned, error) {
	params, err := parse[rebalanceParams](raw)
	if err != nil {
		return nil, err
	}
	if params.Mode == "" {
		params.Mode = balancer.ModeEven
	}
	switch params.Mode {
	case balancer.ModeEven, balancer.ModeReplicas, balancer.ModeLeaders:
	default:
		return nil, fmt.Errorf("unknown mode %q", params.Mode)
	}
	exclude := map[int32]bool{}
	for _, b := range params.ExcludeBrokers {
		exclude[b] = true
	}
	rackAware := racksKnown(p.View)
	if params.RackAware != nil {
		rackAware = *params.RackAware
	}
	opts := balancer.Options{Mode: params.Mode, RackAware: rackAware, MaxMoves: params.MaxMoves}
	if len(params.Topics) > 0 {
		opts.Topics = map[string]bool{}
		for _, t := range params.Topics {
			opts.Topics[t] = true
		}
	}
	plan, err := balancer.Compute(modelFrom(p, exclude), opts)
	if err != nil {
		return nil, err
	}
	return planFromMoves(p, plan, fmt.Sprintf("Rebalance (%s)", params.Mode), params.ThrottleBytesPerSec, params.BatchSize, nil)
}

func planFromMoves(p *Planner, plan *balancer.Plan, title string, throttle int64, batch int, extra []StepSpec) (*Planned, error) {
	moved := 0
	for _, m := range plan.Moves {
		moved += m.ReplicaMoves
	}
	leaderOnly := len(plan.Moves) - countWithData(plan.Moves)
	doc := PlanDoc{Warnings: plan.Warnings, Moves: plan.Moves, Before: &plan.Before, After: &plan.After,
		DataToMoveBytes: plan.DataToMoveBytes, Details: map[string]any{"rackAware": racksKnown(p.View)}}
	if len(plan.Moves) == 0 {
		doc.Summary = title + ": the cluster is already balanced, nothing to move"
		return &Planned{Doc: doc, Steps: extra}, nil
	}
	doc.Summary = fmt.Sprintf("%s: %d partition(s) change, %d replica move(s) (%s), %d leader-only change(s); replica spread %d -> %d, leader spread %d -> %d",
		title, len(plan.Moves), moved, humanBytes(plan.DataToMoveBytes), leaderOnly,
		plan.Before.ReplicaSpread, plan.After.ReplicaSpread, plan.Before.LeaderSpread, plan.After.LeaderSpread)
	steps := reassignStepsURP(plan.Moves, throttle, batch, false)
	return &Planned{Doc: doc, Steps: append(steps, extra...)}, nil
}

func countWithData(moves []balancer.Move) int {
	n := 0
	for _, m := range moves {
		if m.ReplicaMoves > 0 {
			n++
		}
	}
	return n
}

func planMovePartitions(_ context.Context, p *Planner, raw json.RawMessage) (*Planned, error) {
	params, err := parse[moveParams](raw)
	if err != nil {
		return nil, err
	}
	if len(params.Moves) == 0 {
		return nil, errors.New("no moves given")
	}
	brokers := map[int32]kafka.Broker{}
	for _, b := range p.View.Snapshot.Brokers {
		brokers[b.ID] = b
	}
	targets := map[string][]int32{}
	for _, mv := range params.Moves {
		seen := map[int32]bool{}
		for _, r := range mv.Replicas {
			b, ok := brokers[r]
			switch {
			case !ok:
				return nil, fmt.Errorf("%s: broker %d does not exist", mv.Key(), r)
			case seen[r]:
				return nil, fmt.Errorf("%s: broker %d listed twice", mv.Key(), r)
			case !b.Alive || b.Fenced:
				return nil, fmt.Errorf("%s: broker %d is offline", mv.Key(), r)
			}
			seen[r] = true
		}
		targets[mv.Key()] = mv.Replicas
	}
	// Kafka 4.3 accepts reassignments onto cordoned brokers (lab finding 4), so check it here.
	for _, t := range p.View.Snapshot.Topics {
		for _, part := range t.Partitions {
			key := fmt.Sprintf("%s-%d", t.Name, part.ID)
			target, ok := targets[key]
			if !ok {
				continue
			}
			for _, r := range target {
				if brokers[r].Cordoned && !containsInt(part.Replicas, r) {
					return nil, fmt.Errorf("%s: broker %d is cordoned", key, r)
				}
			}
		}
	}
	plan, err := balancer.Apply(modelFrom(p, nil), targets, balancer.Options{RackAware: racksKnown(p.View)})
	if err != nil {
		return nil, err
	}
	if len(plan.Moves) != len(targets) {
		return nil, errors.New("some partitions in the request do not exist")
	}
	return planFromMoves(p, plan, "Move partitions", params.ThrottleBytesPerSec, params.BatchSize, nil)
}

func planElectLeaders(_ context.Context, p *Planner, raw json.RawMessage) (*Planned, error) {
	params, err := parse[topicsParams](raw)
	if err != nil {
		return nil, err
	}
	scope := map[string]bool{}
	for _, t := range params.Topics {
		scope[t] = true
	}
	var parts []kafka.Assignment
	for _, t := range p.View.Snapshot.Topics {
		if len(scope) > 0 && !scope[t.Name] {
			continue
		}
		for _, part := range t.Partitions {
			if len(part.Replicas) > 0 && part.Leader != part.Replicas[0] {
				parts = append(parts, kafka.Assignment{Topic: t.Name, Partition: part.ID, Replicas: part.Replicas})
			}
		}
	}
	doc := PlanDoc{Summary: fmt.Sprintf("Elect preferred leaders for %d partition(s) led by a non-preferred replica", len(parts)),
		Details: map[string]any{"partitions": len(parts)}}
	if len(parts) == 0 {
		doc.Summary = "Every partition is already led by its preferred replica"
		return &Planned{Doc: doc}, nil
	}
	return &Planned{Doc: doc, Steps: []StepSpec{
		{Name: "Elect preferred leaders", Kind: KindElectPreferred, Args: ReassignArgs{Assignments: parts}},
	}}, nil
}

func (p *Planner) broker(id *int32) (kafka.Broker, error) {
	if id == nil {
		return kafka.Broker{}, errors.New("brokerId is required")
	}
	for _, b := range p.View.Snapshot.Brokers {
		if b.ID == *id {
			return b, nil
		}
	}
	return kafka.Broker{}, fmt.Errorf("broker %d is not registered", *id)
}

func planCordon(_ context.Context, p *Planner, raw json.RawMessage) (*Planned, error) {
	params, err := parse[brokerParams](raw)
	if err != nil {
		return nil, err
	}
	b, err := p.broker(params.BrokerID)
	if err != nil {
		return nil, err
	}
	verb := "Uncordon"
	if params.Cordon {
		verb = "Cordon"
	}
	return &Planned{Doc: PlanDoc{Summary: fmt.Sprintf("%s broker %d", verb, b.ID)}, Steps: []StepSpec{
		{Name: fmt.Sprintf("%s broker %d", verb, b.ID), Kind: KindCordon, Args: BrokerArgs{BrokerID: b.ID, Cordon: params.Cordon}},
	}}, nil
}

func planDrain(_ context.Context, p *Planner, raw json.RawMessage) (*Planned, error) {
	params, err := parse[brokerParams](raw)
	if err != nil {
		return nil, err
	}
	b, err := p.broker(params.BrokerID)
	if err != nil {
		return nil, err
	}
	if !b.Alive || b.Fenced {
		return nil, fmt.Errorf("broker %d is offline; use replace-broker or move its partitions to other brokers", b.ID)
	}
	plan, err := balancer.Compute(modelFrom(p, map[int32]bool{b.ID: true}), balancer.Options{
		Mode: balancer.ModeEvacuate, Evacuate: []int32{b.ID}, RackAware: racksKnown(p.View)})
	if err != nil {
		return nil, err
	}
	for _, w := range plan.Warnings {
		if strings.HasPrefix(w, "cannot move") {
			return nil, fmt.Errorf("broker %d cannot be drained: %s", b.ID, w)
		}
	}
	title := fmt.Sprintf("Drain broker %d", b.ID)
	if params.Decommission {
		title = fmt.Sprintf("Decommission broker %d", b.ID)
	}
	extra := []StepSpec{{Name: fmt.Sprintf("Verify broker %d is empty", b.ID), Kind: KindVerifyEmpty, Args: BrokerArgs{BrokerID: b.ID}}}
	if params.Decommission {
		// Remove the cordon while the broker is registered: after unregistering, the config can no
		// longer be changed but survives and would cordon a future broker with the same id.
		extra = append(extra,
			StepSpec{Name: fmt.Sprintf("Remove cordon of broker %d", b.ID), Kind: KindCordon, Args: BrokerArgs{BrokerID: b.ID, Cordon: false}},
			stopBrokerStep(p, b.ID),
			StepSpec{Name: fmt.Sprintf("Unregister broker %d", b.ID), Kind: KindUnregisterBroker, Args: BrokerArgs{BrokerID: b.ID}},
		)
	}
	planned, err := planFromMoves(p, plan, title, params.ThrottleBytesPerSec, 0, extra)
	if err != nil {
		return nil, err
	}
	cordon := StepSpec{Name: fmt.Sprintf("Cordon broker %d", b.ID), Kind: KindCordon, Args: BrokerArgs{BrokerID: b.ID, Cordon: true}}
	planned.Steps = append([]StepSpec{cordon}, planned.Steps...)
	planned.Doc.Irreversible = params.Decommission
	return planned, nil
}

// stopBrokerStep stops the broker through the node manager when available, otherwise waits for
// an operator to stop it.
func stopBrokerStep(p *Planner, broker int32) StepSpec {
	if p.Engine.stopStep != nil {
		if s, ok := p.Engine.stopStep(p, broker); ok {
			return s
		}
	}
	return StepSpec{Name: fmt.Sprintf("Wait until broker %d is stopped", broker), Kind: KindWaitBrokerDown,
		Args: BrokerArgs{BrokerID: broker, Message: fmt.Sprintf("Stop broker %d now (controlled shutdown)", broker)}}
}

func planUnregister(_ context.Context, p *Planner, raw json.RawMessage) (*Planned, error) {
	params, err := parse[brokerParams](raw)
	if err != nil {
		return nil, err
	}
	b, err := p.broker(params.BrokerID)
	if err != nil {
		return nil, err
	}
	if b.Alive && !b.Fenced {
		return nil, fmt.Errorf("broker %d is serving; drain and stop it first", b.ID)
	}
	if left := replicasOn(p.View.Snapshot, b.ID); len(left) > 0 {
		return nil, fmt.Errorf("broker %d is still assigned %d replica(s); replace the broker or move its partitions first", b.ID, len(left))
	}
	doc := PlanDoc{Summary: fmt.Sprintf("Unregister stopped broker %d", b.ID), Irreversible: true}
	if b.Cordoned {
		doc.Warnings = append(doc.Warnings, "the broker is cordoned; the cordon config survives unregistering and will apply to a future broker with the same id")
	}
	return &Planned{Doc: doc, Steps: []StepSpec{
		{Name: fmt.Sprintf("Unregister broker %d", b.ID), Kind: KindUnregisterBroker, Args: BrokerArgs{BrokerID: b.ID}},
	}}, nil
}

func planRemoveController(_ context.Context, p *Planner, raw json.RawMessage) (*Planned, error) {
	params, err := parse[nodeParams](raw)
	if err != nil {
		return nil, err
	}
	q := p.View.Snapshot.Quorum
	if q == nil || params.NodeID == nil {
		return nil, errors.New("nodeId is required and the quorum must be describable")
	}
	v, ok := q.Voter(*params.NodeID)
	if !ok {
		return nil, fmt.Errorf("node %d is not a voter", *params.NodeID)
	}
	if len(q.Voters) <= 1 {
		return nil, errors.New("cannot remove the last voter")
	}
	if v.DirectoryID == "" {
		return nil, errors.New("the voter has no directory id (static quorum); migrate the quorum first")
	}
	doc := PlanDoc{Summary: fmt.Sprintf("Remove controller %d from the quorum (%d -> %d voters)", v.NodeID, len(q.Voters), len(q.Voters)-1)}
	if (len(q.Voters)-1)%2 == 0 {
		doc.Warnings = append(doc.Warnings, fmt.Sprintf("%d voters tolerate as many failures as %d", len(q.Voters)-1, len(q.Voters)-2))
	}
	steps := []StepSpec{
		{Name: "Check cluster health", Kind: KindHealthGate, Args: HealthGateArgs{}},
		{Name: fmt.Sprintf("Remove voter %d", v.NodeID), Kind: KindRemoveVoter, Args: VoterArgs{NodeID: v.NodeID, DirectoryID: v.DirectoryID}},
	}
	if p.Engine.stopControllerStep != nil {
		if s, ok := p.Engine.stopControllerStep(p, v.NodeID); ok {
			steps = append(steps, s)
		}
	}
	return &Planned{Doc: doc, Steps: steps}, nil
}

func planFinalize(ctx context.Context, p *Planner, _ json.RawMessage) (*Planned, error) {
	features, err := p.Client.Features(ctx)
	if err != nil {
		return nil, err
	}
	var steps []StepSpec
	var changes []string
	doc := PlanDoc{Irreversible: true, Details: map[string]any{}}
	sort.Slice(features, func(i, j int) bool {
		return features[i].Name == "metadata.version" && features[j].Name != "metadata.version"
	})
	for _, f := range features {
		if f.Finalized >= f.SupportedMax {
			continue
		}
		if f.Name == "kraft.version" {
			doc.Warnings = append(doc.Warnings, "kraft.version is not raised here; use migrate-quorum to move a static quorum to a dynamic one")
			continue
		}
		if err := p.Client.UpdateFeature(ctx, f.Name, f.SupportedMax, true); err != nil {
			doc.Warnings = append(doc.Warnings, fmt.Sprintf("%s cannot be raised to %d yet: %v", f.Name, f.SupportedMax, err))
			continue
		}
		from, to := fmt.Sprint(f.Finalized), fmt.Sprint(f.SupportedMax)
		if f.Name == "metadata.version" {
			from, to = cluster.MetadataVersionName(f.Finalized), cluster.MetadataVersionName(f.SupportedMax)
		}
		changes = append(changes, fmt.Sprintf("%s %s -> %s", f.Name, from, to))
		steps = append(steps, StepSpec{Name: fmt.Sprintf("Finalize %s = %s", f.Name, to), Kind: KindUpdateFeature,
			Args: FeatureArgs{Feature: f.Name, Level: f.SupportedMax}})
	}
	if len(steps) == 0 {
		doc.Summary = "All features are already at the highest level supported by every node"
		return &Planned{Doc: doc}, nil
	}
	doc.Summary = "Finalize upgrade: " + strings.Join(changes, ", ")
	doc.Warnings = append(doc.Warnings, "metadata.version cannot be downgraded after finalization when the versions carry metadata changes (4.3, 4.4)")
	steps = append([]StepSpec{{Name: "Check cluster health", Kind: KindHealthGate, Args: HealthGateArgs{}}}, steps...)
	return &Planned{Doc: doc, Steps: steps}, nil
}

func containsInt(set []int32, x int32) bool {
	for _, v := range set {
		if v == x {
			return true
		}
	}
	return false
}

func humanBytes(b int64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := int64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(b)/float64(div), "KMGTPE"[exp])
}
