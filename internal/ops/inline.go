// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/brokerium/brokerium/internal/balancer"
	"github.com/brokerium/brokerium/internal/cluster"
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

// InlineRebalance plans a rebalance from the current state and executes it within one step, with
// the steps of a planned rebalance: health gates for the brokers that receive replicas next,
// throttle, batches, preferred election, and the throttles removed in every case. The plan and
// the throttle are retried on transient errors; reassignment errors fail the step.
func InlineRebalance(ctx context.Context, x *Exec, mode string, throttle int64) (any, error) {
	var plan *balancer.Plan
	err := transientRetry.run(ctx, x, "the rebalance plan", func() error {
		v, err := x.view(ctx)
		if err != nil {
			return err
		}
		p := &Planner{Engine: x.Engine, Cluster: x.Cluster, Client: x.Client, View: v}
		plan, err = balancer.Compute(modelFrom(p, nil), balancer.Options{Mode: mode, RackAware: racksKnown(v)})
		return err
	})
	if err != nil {
		return nil, err
	}
	if len(plan.Moves) == 0 {
		return map[string]any{"moves": 0, "summary": "already balanced"}, nil
	}
	steps := reassignStepsGated(plan.Moves, throttle, DefaultBatchSize, HealthGateArgs{})
	defer func() {
		cctx := context.WithoutCancel(ctx)
		for _, s := range steps {
			if s.AlwaysRun {
				_, _ = x.RunStep(cctx, s.Kind, s.Args)
			}
		}
	}()
	for _, s := range steps {
		if s.AlwaysRun {
			continue
		}
		x.Progress(map[string]any{"step": s.Name, "totalMoves": len(plan.Moves)})
		run := func() error {
			_, err := x.RunStep(ctx, s.Kind, s.Args)
			return err
		}
		if s.Kind == KindThrottle {
			err = transientRetry.run(ctx, x, "the replication throttle", run)
		} else {
			err = run()
		}
		if err != nil {
			return nil, err
		}
	}
	return map[string]any{"moves": len(plan.Moves), "dataToMoveBytes": plan.DataToMoveBytes,
		"replicaSpread": []int{plan.Before.ReplicaSpread, plan.After.ReplicaSpread},
		"leaderSpread":  []int{plan.Before.LeaderSpread, plan.After.LeaderSpread}}, nil
}
