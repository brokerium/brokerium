// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package ee

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"github.com/brokerium/brokerium/internal/config"
	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/ops"
)

type fakeManager struct{ allowed bool }

func (f fakeManager) Allows(string) bool       { return f.allowed }
func (fakeManager) Info() *edition.LicenseInfo { return nil }
func (fakeManager) Set(context.Context, string, string) (*edition.LicenseInfo, error) {
	return nil, errors.New("not implemented")
}
func (fakeManager) Clear(context.Context, string) error { return errors.New("not implemented") }

// The tests replace the process-wide license manager and therefore do not run in parallel.

func TestRequireFeature(t *testing.T) {
	t.Cleanup(func() { edition.SetManager(nil) })
	h := RequireFeature(edition.MessageTools, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	tests := []struct {
		name    string
		manager edition.Manager
		status  int
	}{
		{"no license manager", nil, http.StatusForbidden},
		{"license without the feature", fakeManager{allowed: false}, http.StatusForbidden},
		{"license with the feature", fakeManager{allowed: true}, http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			edition.SetManager(tc.manager)
			rec := httptest.NewRecorder()
			h(rec, httptest.NewRequest(http.MethodGet, "/api/v1/x", nil))
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d", rec.Code, tc.status)
			}
			if tc.status != http.StatusForbidden {
				return
			}
			var body struct {
				Error struct{ Code, Message string } `json:"error"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != "license_required" || body.Error.Message == "" {
				t.Errorf("body = %s", rec.Body)
			}
		})
	}
}

func TestSetup(t *testing.T) {
	t.Cleanup(func() { edition.SetManager(nil) })
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	log := slog.New(slog.DiscardHandler)
	engine := ops.New(nil, nil, nil, log)
	exts, err := Setup(ctx, Deps{Config: config.Server{License: "brk-lic-1.x.y"}, Engine: engine, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	if len(exts) != 1 || !slices.ContainsFunc(engine.Types(), func(op ops.OpType) bool { return op.Type == "copy-messages" }) {
		t.Errorf("Setup returned %d extensions and registered %d operation types", len(exts), len(engine.Types()))
	}
	m := edition.CurrentManager()
	if m == nil {
		t.Fatal("Setup did not install the license manager")
	}
	if info := m.Info(); info == nil || info.Source != edition.SourceEnvironment || info.State != edition.StateInvalid {
		t.Errorf("Info = %+v", info)
	}
	if edition.Name != edition.Enterprise {
		t.Errorf("edition.Name = %s in an Enterprise build", edition.Name)
	}
}
