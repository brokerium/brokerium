// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bvaliev/brokerium/internal/balancer"
	"github.com/bvaliev/brokerium/internal/cluster"
	"github.com/bvaliev/brokerium/internal/kafka"
)

// ClusterState is a refreshed view with the set of live brokers.
type ClusterState struct {
	View  *cluster.View
	Alive map[int32]bool
}

// View exposes the refreshed cluster view to steps implemented outside this package.
func (x *Exec) View(ctx context.Context) (*ClusterState, error) {
	v, err := x.view(ctx)
	if err != nil {
		return nil, err
	}
	return &ClusterState{View: v, Alive: aliveBrokers(v)}, nil
}

// RunStep runs a built-in step kind with the given args inside another step.
func (x *Exec) RunStep(ctx context.Context, kind string, args any) (any, error) {
	fn, ok := x.Engine.kinds[kind]
	if !ok {
		return nil, fmt.Errorf("unknown step kind %s", kind)
	}
	raw, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	return fn(ctx, x, raw)
}

// InlineRebalance plans a rebalance from the current state and executes it within one step:
// throttle, batches with health gates, cleanup of throttles in every case, preferred election.
func InlineRebalance(ctx context.Context, x *Exec, mode string, throttle int64) (any, error) {
	v, err := x.view(ctx)
	if err != nil {
		return nil, err
	}
	p := &Planner{Engine: x.Engine, Cluster: x.Cluster, Client: x.Client, View: v}
	plan, err := balancer.Compute(modelFrom(p, nil), balancer.Options{Mode: mode, RackAware: racksKnown(v)})
	if err != nil {
		return nil, err
	}
	if len(plan.Moves) == 0 {
		return map[string]any{"moves": 0, "summary": "already balanced"}, nil
	}
	if throttle <= 0 {
		throttle = DefaultThrottle
	}
	var assignments []kafka.Assignment
	for _, m := range plan.Moves {
		assignments = append(assignments, kafka.Assignment{Topic: m.Topic, Partition: m.Partition, Replicas: m.To})
	}
	targs := ThrottleArgs{RateBytesPerSec: throttle, Assignments: assignments}
	defer func() {
		cctx := context.WithoutCancel(ctx)
		_, _ = x.RunStep(cctx, KindClearThrottle, targs)
	}()
	if _, err := x.RunStep(ctx, KindThrottle, targs); err != nil {
		return nil, err
	}
	for i := 0; i < len(assignments); i += DefaultBatchSize {
		batch := assignments[i:min(i+DefaultBatchSize, len(assignments))]
		x.Progress(map[string]any{"batch": i/DefaultBatchSize + 1, "partitions": len(batch), "totalMoves": len(assignments)})
		if _, err := x.RunStep(ctx, KindReassign, ReassignArgs{Assignments: batch}); err != nil {
			return nil, err
		}
		if _, err := x.RunStep(ctx, KindWaitReassign, ReassignArgs{Assignments: batch}); err != nil {
			return nil, err
		}
		if _, err := x.RunStep(ctx, KindHealthGate, HealthGateArgs{}); err != nil {
			return nil, err
		}
	}
	if _, err := x.RunStep(ctx, KindElectPreferred, ReassignArgs{Assignments: assignments}); err != nil {
		return nil, err
	}
	return map[string]any{"moves": len(plan.Moves), "dataToMoveBytes": plan.DataToMoveBytes,
		"replicaSpread": []int{plan.Before.ReplicaSpread, plan.After.ReplicaSpread},
		"leaderSpread":  []int{plan.Before.LeaderSpread, plan.After.LeaderSpread}}, nil
}
