// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package ops

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/edition"
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
