// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
)

// Step kinds.
const (
	KindHealthGate       = "health-gate"
	KindThrottle         = "throttle"
	KindClearThrottle    = "clear-throttle"
	KindReassign         = "reassign"
	KindWaitReassign     = "wait-reassign"
	KindElectPreferred   = "elect-preferred"
	KindCordon           = "cordon"
	KindVerifyEmpty      = "verify-empty"
	KindWaitBrokerDown   = "wait-broker-offline"
	KindUnregisterBroker = "unregister-broker"
	KindUpdateFeature    = "update-feature"
	KindRemoveVoter      = "remove-voter"
)

// HealthGateArgs configure the health gate.
type HealthGateArgs struct {
	AllowUnderReplicated bool `json:"allowUnderReplicated,omitempty"`
	TimeoutSeconds       int  `json:"timeoutSeconds,omitempty"`
}

// ReassignArgs are the target assignments of one batch.
type ReassignArgs struct {
	Assignments    []kafka.Assignment `json:"assignments"`
	TimeoutSeconds int                `json:"timeoutSeconds,omitempty"`
}

// ThrottleArgs describe the moves to throttle; brokers and replica lists are computed when the
// step runs, so brokers added after planning are throttled too.
type ThrottleArgs struct {
	RateBytesPerSec int64              `json:"rateBytesPerSec"`
	Assignments     []kafka.Assignment `json:"assignments"`
}

// BrokerArgs target one broker.
type BrokerArgs struct {
	BrokerID       int32  `json:"brokerId"`
	Cordon         bool   `json:"cordon,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
	Message        string `json:"message,omitempty"`
}

// FeatureArgs upgrade a feature.
type FeatureArgs struct {
	Feature string `json:"feature"`
	Level   int16  `json:"level"`
}

// VoterArgs remove a voter.
type VoterArgs struct {
	NodeID      int32  `json:"nodeId"`
	DirectoryID string `json:"directoryId"`
}

func builtinSteps() map[string]StepFunc {
	return map[string]StepFunc{
		KindHealthGate:       stepHealthGate,
		KindThrottle:         stepThrottle,
		KindClearThrottle:    stepClearThrottle,
		KindReassign:         stepReassign,
		KindWaitReassign:     stepWaitReassign,
		KindElectPreferred:   stepElectPreferred,
		KindCordon:           stepCordon,
		KindVerifyEmpty:      stepVerifyEmpty,
		KindWaitBrokerDown:   stepWaitBrokerDown,
		KindUnregisterBroker: stepUnregisterBroker,
		KindUpdateFeature:    stepUpdateFeature,
		KindRemoveVoter:      stepRemoveVoter,
	}
}

func decode[T any](raw json.RawMessage) (T, error) {
	var v T
	if len(raw) == 0 {
		return v, nil
	}
	err := json.Unmarshal(raw, &v)
	return v, err
}

// view refreshes and returns the current cluster view.
func (x *Exec) view(ctx context.Context) (*cluster.View, error) {
	v, err := x.Engine.Clusters.Refresh(ctx, x.Cluster.ID)
	if err != nil {
		return nil, err
	}
	if v.Snapshot == nil {
		return nil, fmt.Errorf("cluster unreachable: %s", v.Error)
	}
	return v, nil
}

func aliveBrokers(v *cluster.View) map[int32]bool {
	out := map[int32]bool{}
	for _, b := range v.Snapshot.Brokers {
		if b.Alive && !b.Fenced {
			out[b.ID] = true
		}
	}
	return out
}

// HealthProblems lists the reasons a cluster fails the health gate.
func HealthProblems(h cluster.Health, allowURP bool) []string {
	var p []string
	if h.OfflinePartitions > 0 {
		p = append(p, fmt.Sprintf("%d offline partition(s)", h.OfflinePartitions))
	}
	if h.UnderMinISR > 0 {
		p = append(p, fmt.Sprintf("%d partition(s) under min.insync.replicas", h.UnderMinISR))
	}
	if h.UnderReplicated > 0 && !allowURP {
		p = append(p, fmt.Sprintf("%d under-replicated partition(s)", h.UnderReplicated))
	}
	if h.Voters > 0 && h.QuorumLeader < 0 {
		p = append(p, "no controller quorum leader")
	}
	if h.VotersLagging > 0 {
		p = append(p, fmt.Sprintf("%d lagging controller voter(s)", h.VotersLagging))
	}
	return p
}

func stepHealthGate(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[HealthGateArgs](raw)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(max(args.TimeoutSeconds, 30)) * time.Second
	if args.TimeoutSeconds == 0 {
		timeout = 5 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for {
		v, err := x.view(ctx)
		var problems []string
		if err != nil {
			problems = []string{err.Error()}
		} else {
			problems = HealthProblems(v.Health, args.AllowUnderReplicated)
			if len(v.Snapshot.Reassignments) > 0 {
				problems = append(problems, fmt.Sprintf("%d reassignment(s) in progress", len(v.Snapshot.Reassignments)))
			}
		}
		if len(problems) == 0 {
			return map[string]any{"healthy": true, "checkedAt": time.Now()}, nil
		}
		x.Progress(map[string]any{"healthy": false, "waitingFor": problems})
		if time.Now().After(deadline) {
			return map[string]any{"healthy": false, "problems": problems},
				fmt.Errorf("health gate failed after %s: %s", timeout, strings.Join(problems, "; "))
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func stepThrottle(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[ThrottleArgs](raw)
	if err != nil {
		return nil, err
	}
	v, err := x.view(ctx)
	if err != nil {
		return nil, err
	}
	current, err := x.Client.CurrentReplicas(ctx, topicsOf(args.Assignments)...)
	if err != nil {
		return nil, err
	}
	// Only live brokers are throttled: setting configs on a dead broker hangs (lab finding 3).
	spec := kafka.BuildThrottle(args.RateBytesPerSec, args.Assignments, current, aliveBrokers(v))
	if err := x.Client.ApplyThrottle(ctx, spec); err != nil {
		return nil, err
	}
	return map[string]any{"rateBytesPerSec": spec.RateBytesPerSec, "brokers": spec.Brokers}, nil
}

func stepClearThrottle(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[ThrottleArgs](raw)
	if err != nil {
		return nil, err
	}
	v, err := x.view(ctx)
	if err != nil {
		return nil, err
	}
	alive := aliveBrokers(v)
	involved := map[int32]bool{}
	current, _ := x.Client.CurrentReplicas(ctx, topicsOf(args.Assignments)...)
	for _, a := range args.Assignments {
		for _, r := range append(append([]int32{}, a.Replicas...), current[a.Key()]...) {
			if alive[r] {
				involved[r] = true
			}
		}
	}
	var live []int32
	for b := range involved {
		live = append(live, b)
	}
	sort.Slice(live, func(i, j int) bool { return live[i] < live[j] })
	if err := x.Client.ClearThrottle(ctx, live, topicsOf(args.Assignments)); err != nil {
		return nil, err
	}
	return map[string]any{"brokers": live, "topics": topicsOf(args.Assignments)}, nil
}

func stepReassign(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[ReassignArgs](raw)
	if err != nil {
		return nil, err
	}
	topics := topicsOf(args.Assignments)
	current, err := x.Client.CurrentReplicas(ctx, topics...)
	if err != nil {
		return nil, err
	}
	var todo []kafka.Assignment
	for _, a := range args.Assignments {
		if !sameOrder(current[a.Key()], a.Replicas) {
			todo = append(todo, a)
		}
	}
	if len(todo) == 0 {
		return map[string]any{"started": 0, "alreadyDone": len(args.Assignments)}, nil
	}
	if err := x.Client.AlterAssignments(ctx, todo); err != nil {
		return nil, err
	}
	return map[string]any{"started": len(todo), "alreadyDone": len(args.Assignments) - len(todo)}, nil
}

func stepWaitReassign(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[ReassignArgs](raw)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(args.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 2 * time.Hour
	}
	deadline := time.Now().Add(timeout)
	keys := map[string]bool{}
	for _, a := range args.Assignments {
		keys[a.Key()] = true
	}
	for {
		inProgress, err := x.Client.InProgress(ctx)
		if err == nil {
			remaining := 0
			for k := range inProgress {
				if keys[k] {
					remaining++
				}
			}
			x.Progress(map[string]any{"total": len(keys), "remaining": remaining})
			if remaining == 0 {
				break
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("reassignment did not finish within %s", timeout)
		}
		select {
		case <-ctx.Done():
			// Cancelled by the user: stop the reassignments of this batch.
			cctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if cerr := x.Client.CancelAssignments(cctx, args.Assignments); cerr != nil {
				return nil, errors.Join(ctx.Err(), cerr)
			}
			return map[string]any{"cancelled": true}, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	// Verify the final replica sets against the targets instead of trusting the absence of
	// in-progress entries (lab finding: a failed --execute leaves nothing in progress either).
	// Broker metadata can lag the controller briefly, so retry for up to a minute.
	verifyDeadline := time.Now().Add(time.Minute)
	for {
		current, err := x.Client.CurrentReplicas(ctx, topicsOf(args.Assignments)...)
		if err != nil {
			return nil, err
		}
		var wrong []string
		for _, a := range args.Assignments {
			if !sameSet(current[a.Key()], a.Replicas) {
				wrong = append(wrong, fmt.Sprintf("%s has %v, expected %v", a.Key(), current[a.Key()], a.Replicas))
			}
		}
		if len(wrong) == 0 {
			return map[string]any{"completed": len(args.Assignments)}, nil
		}
		inProgress, _ := x.Client.InProgress(ctx)
		pending := false
		for k := range inProgress {
			pending = pending || keys[k]
		}
		if time.Now().After(verifyDeadline) && !pending {
			sort.Strings(wrong)
			return map[string]any{"mismatched": wrong}, fmt.Errorf("replica sets differ from the plan: %s", strings.Join(wrong, "; "))
		}
		x.Progress(map[string]any{"total": len(keys), "remaining": 0, "verifying": len(wrong)})
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

func stepElectPreferred(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[ReassignArgs](raw)
	if err != nil {
		return nil, err
	}
	if err := x.Client.ElectPreferred(ctx, args.Assignments); err != nil {
		return nil, err
	}
	return map[string]any{"partitions": len(args.Assignments)}, nil
}

func stepCordon(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[BrokerArgs](raw)
	if err != nil {
		return nil, err
	}
	v, err := x.view(ctx)
	if err != nil {
		return nil, err
	}
	if !aliveBrokers(v)[args.BrokerID] {
		if !args.Cordon {
			return map[string]any{"skipped": "broker is offline; its dynamic config cannot be changed until it is back"}, nil
		}
		return nil, fmt.Errorf("broker %d is offline", args.BrokerID)
	}
	if err := x.Client.SetCordon(ctx, args.BrokerID, args.Cordon); err != nil {
		return nil, err
	}
	return map[string]any{"broker": args.BrokerID, "cordoned": args.Cordon}, nil
}

// replicasOn counts replicas assigned to a broker in a snapshot.
func replicasOn(s *kafka.Snapshot, broker int32) []string {
	var out []string
	for _, t := range s.Topics {
		for _, p := range t.Partitions {
			for _, r := range p.Replicas {
				if r == broker {
					out = append(out, fmt.Sprintf("%s-%d", t.Name, p.ID))
				}
			}
		}
	}
	return out
}

func stepVerifyEmpty(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[BrokerArgs](raw)
	if err != nil {
		return nil, err
	}
	v, err := x.view(ctx)
	if err != nil {
		return nil, err
	}
	if left := replicasOn(v.Snapshot, args.BrokerID); len(left) > 0 {
		return map[string]any{"remaining": left}, fmt.Errorf("broker %d still holds %d replica(s)", args.BrokerID, len(left))
	}
	return map[string]any{"broker": args.BrokerID, "empty": true}, nil
}

func stepWaitBrokerDown(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[BrokerArgs](raw)
	if err != nil {
		return nil, err
	}
	timeout := time.Duration(args.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = time.Hour
	}
	deadline := time.Now().Add(timeout)
	for {
		v, err := x.view(ctx)
		if err == nil && !aliveBrokers(v)[args.BrokerID] {
			return map[string]any{"broker": args.BrokerID, "offline": true}, nil
		}
		x.Progress(map[string]any{"waiting": true, "message": args.Message})
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("broker %d is still running after %s", args.BrokerID, timeout)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
}

func stepUnregisterBroker(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[BrokerArgs](raw)
	if err != nil {
		return nil, err
	}
	// A stopped broker stays registered and unfenced until its session times out (about 9 s).
	deadline := time.Now().Add(90 * time.Second)
	var v *cluster.View
	registered := false
	for {
		if v, err = x.view(ctx); err != nil {
			return nil, err
		}
		registered = false
		serving := false
		for _, b := range v.Snapshot.Brokers {
			if b.ID == args.BrokerID {
				registered = true
				serving = b.Alive && !b.Fenced
			}
		}
		if !serving {
			break
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("broker %d is still serving; stop it first", args.BrokerID)
		}
		x.Progress(map[string]any{"waitingFor": fmt.Sprintf("broker %d to be fenced", args.BrokerID)})
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	if !registered {
		return map[string]any{"broker": args.BrokerID, "alreadyUnregistered": true}, nil
	}
	if left := replicasOn(v.Snapshot, args.BrokerID); len(left) > 0 {
		return nil, fmt.Errorf("broker %d is still assigned %d replica(s); move them first", args.BrokerID, len(left))
	}
	if err := x.Client.UnregisterBroker(ctx, args.BrokerID); err != nil {
		return nil, err
	}
	return map[string]any{"broker": args.BrokerID, "unregistered": true}, nil
}

func stepUpdateFeature(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[FeatureArgs](raw)
	if err != nil {
		return nil, err
	}
	features, err := x.Client.Features(ctx)
	if err != nil {
		return nil, err
	}
	for _, f := range features {
		if f.Name == args.Feature && f.Finalized >= args.Level {
			return map[string]any{"feature": f.Name, "level": f.Finalized, "alreadyFinalized": true}, nil
		}
	}
	if err := x.Client.UpdateFeature(ctx, args.Feature, args.Level, false); err != nil {
		return nil, err
	}
	return map[string]any{"feature": args.Feature, "level": args.Level}, nil
}

func stepRemoveVoter(ctx context.Context, x *Exec, raw json.RawMessage) (any, error) {
	args, err := decode[VoterArgs](raw)
	if err != nil {
		return nil, err
	}
	q, err := x.Client.DescribeQuorum(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := q.Voter(args.NodeID); !ok {
		return map[string]any{"nodeId": args.NodeID, "alreadyRemoved": true}, nil
	}
	if len(q.Voters) <= 1 {
		return nil, errors.New("refusing to remove the last voter")
	}
	if err := x.Client.RemoveRaftVoter(ctx, x.Cluster.KafkaClusterID, args.NodeID, args.DirectoryID); err != nil {
		return nil, err
	}
	return map[string]any{"nodeId": args.NodeID, "removed": true}, nil
}

func topicsOf(as []kafka.Assignment) []string {
	m := map[string]bool{}
	for _, a := range as {
		m[a.Topic] = true
	}
	out := make([]string, 0, len(m))
	for t := range m {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

func sameOrder(a, b []int32) bool {
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

func sameSet(a, b []int32) bool {
	if len(a) != len(b) {
		return false
	}
	m := map[int32]bool{}
	for _, x := range a {
		m[x] = true
	}
	for _, x := range b {
		if !m[x] {
			return false
		}
	}
	return true
}
