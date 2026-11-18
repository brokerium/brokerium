// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/store"
)

func TestRegisterEnterpriseOperation(t *testing.T) {
	t.Parallel()
	e := New(nil, nil, nil, slog.New(slog.DiscardHandler))
	planned := false
	e.Register([]OpType{{Type: "enterprise-op", Title: "Enterprise", Feature: edition.MessageTools,
		Plan: func(context.Context, *Planner, json.RawMessage) (*Planned, error) {
			planned = true
			return &Planned{}, nil
		}}},
		map[string]StepFunc{"enterprise-step": func(context.Context, *Exec, json.RawMessage) (any, error) { return nil, nil }})
	if _, ok := e.kinds["enterprise-step"]; !ok {
		t.Error("the step kind was not registered")
	}
	for _, typ := range e.Types() {
		raw, _ := json.Marshal(typ)
		if hasFeature := strings.Contains(string(raw), `"feature":"message-tools"`); hasFeature != (typ.Type == "enterprise-op") {
			t.Errorf("type %s: %s", typ.Type, raw)
		}
	}
	if _, err := e.Plan(t.Context(), "c1", "enterprise-op", nil, "alice"); !errors.Is(err, edition.ErrLicenseRequired) {
		t.Fatalf("Plan without a license = %v, want %v", err, edition.ErrLicenseRequired)
	}
	if planned {
		t.Error("the operation was planned without a license")
	}
}

// refusingNodes is a node manager that refuses one step kind.
type refusingNodes struct{ kind string }

func (refusingNodes) StepKinds() map[string]StepFunc                      { return nil }
func (refusingNodes) Types() []OpType                                     { return nil }
func (refusingNodes) StopBrokerStep(*Planner, int32) (StepSpec, bool)     { return StepSpec{}, false }
func (refusingNodes) StopControllerStep(*Planner, int32) (StepSpec, bool) { return StepSpec{}, false }
func (n refusingNodes) CheckPlan(_ context.Context, c *store.Cluster, kinds []string) error {
	if slices.Contains(kinds, n.kind) {
		return fmt.Errorf("%s cannot run on %s now", n.kind, c.Name)
	}
	return nil
}

// guardedNodes is a node manager that refuses every operation on one Kafka node.
type guardedNodes struct {
	refusingNodes
	node int32
}

func (n guardedNodes) CheckNode(_ context.Context, _ *Planner, typ string, id int32) error {
	if id == n.node {
		return fmt.Errorf("%s refused on node %d", typ, id)
	}
	return nil
}

func TestExtendChecksNodes(t *testing.T) {
	t.Parallel()
	e := New(nil, nil, nil, slog.New(slog.DiscardHandler))
	e.Extend(guardedNodes{node: 3})
	snap := &kafka.Snapshot{
		Brokers: []kafka.Broker{{ID: 3, Alive: true}, {ID: 101, Alive: true}, {ID: 102, Alive: true}},
		Quorum: &kafka.Quorum{LeaderID: 1, Voters: []kafka.QuorumReplica{
			{NodeID: 1, DirectoryID: "d1"}, {NodeID: 2, DirectoryID: "d2"}, {NodeID: 3, DirectoryID: "d3"}}},
		Topics: []kafka.Topic{{Name: "t", Partitions: []kafka.Partition{{ID: 0, Leader: 3, Replicas: []int32{3, 101}}}}},
	}
	p := &Planner{Engine: e, Cluster: &store.Cluster{Name: "lab"}, View: &cluster.View{Snapshot: snap}}
	tests := []struct {
		name    string
		plan    func(context.Context, *Planner, json.RawMessage) (*Planned, error)
		params  string
		wantErr string
	}{
		{"drain the guarded node", planDrain, `{"brokerId": 3}`, "drain-broker refused on node 3"},
		{"decommission the guarded node", planDrain, `{"brokerId": 3, "decommission": true}`, "drain-broker refused on node 3"},
		{"drain another broker", planDrain, `{"brokerId": 101}`, ""},
		{"remove the guarded voter", planRemoveController, `{"nodeId": 3}`, "remove-controller refused on node 3"},
		{"remove another voter", planRemoveController, `{"nodeId": 2}`, ""},
	}
	for _, tt := range tests {
		_, err := tt.plan(t.Context(), p, json.RawMessage(tt.params))
		if (err == nil) != (tt.wantErr == "") || (err != nil && err.Error() != tt.wantErr) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.wantErr)
		}
	}
}

func TestExtendChecksPlans(t *testing.T) {
	t.Parallel()
	e := New(nil, nil, nil, slog.New(slog.DiscardHandler))
	if err := e.checkPlan(t.Context(), &store.Cluster{Name: "lab"}, []string{"node-provision"}); err != nil {
		t.Fatalf("checkPlan without node managers = %v", err)
	}
	e.Extend(refusingNodes{kind: "node-provision"})
	tests := []struct {
		kinds   []string
		wantErr string
	}{
		{kinds: []string{KindHealthGate, KindReassign, KindWaitReassign}},
		{kinds: []string{KindHealthGate, "node-provision", "node-start"}, wantErr: "node-provision cannot run on lab now"},
		{kinds: nil},
	}
	for _, tt := range tests {
		err := e.checkPlan(t.Context(), &store.Cluster{Name: "lab"}, tt.kinds)
		if (err == nil) != (tt.wantErr == "") || (err != nil && err.Error() != tt.wantErr) {
			t.Errorf("checkPlan(%v) = %v, want %q", tt.kinds, err, tt.wantErr)
		}
	}
}
