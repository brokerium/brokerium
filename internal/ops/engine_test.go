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

	"github.com/brokerium/brokerium/internal/edition"
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
