// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/provider/docker"
	"github.com/brokerium/brokerium/internal/store"
)

func TestOperationErrors(t *testing.T) {
	t.Parallel()
	unavailable := docker.Availability{Reason: "Docker clusters need the Docker installation of Brokerium: this server runs in Kubernetes"}.Err()
	tests := []struct {
		err                  error
		planCode, actionCode string
	}{
		{err: unavailable, planCode: "docker_unavailable", actionCode: "docker_unavailable"},
		{err: fmt.Errorf("cluster lab: %w", store.ErrNotFound), planCode: "not_found", actionCode: "not_found"},
		{err: edition.LicenseRequired(edition.MessageTools), planCode: "license_required", actionCode: "license_required"},
		{err: errors.New("brokers must be between 1 and 9"), planCode: "plan_failed", actionCode: "internal"},
		{err: ops.ErrConfirmName, planCode: "plan_failed", actionCode: "confirmation_required"},
	}
	status := map[string]int{"docker_unavailable": http.StatusConflict, "not_found": http.StatusNotFound,
		"license_required": http.StatusForbidden, "plan_failed": http.StatusBadRequest, "internal": http.StatusInternalServerError,
		"confirmation_required": http.StatusBadRequest}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		writePlanError(rec, tt.err)
		if rec.Code != status[tt.planCode] || errorCode(t, rec) != tt.planCode {
			t.Errorf("plan %v: %d %s, want %d %s", tt.err, rec.Code, rec.Body, status[tt.planCode], tt.planCode)
		}
		rec = httptest.NewRecorder()
		(&OperationsAPI{}).reply(rec, nil, tt.err)
		if rec.Code != status[tt.actionCode] || errorCode(t, rec) != tt.actionCode {
			t.Errorf("confirm %v: %d %s, want %d %s", tt.err, rec.Code, rec.Body, status[tt.actionCode], tt.actionCode)
		}
	}
}
