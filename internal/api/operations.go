// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/bvaliev/brokerium/internal/metrics"
	"github.com/bvaliev/brokerium/internal/ops"
	"github.com/bvaliev/brokerium/internal/store"
)

// OperationsAPI exposes the operations engine and traffic estimates.
type OperationsAPI struct {
	Engine  *ops.Engine
	Sampler *metrics.Sampler
	srv     *Server
}

// Register adds the routes.
func (a *OperationsAPI) Register(s *Server) {
	a.srv = s
	s.Route("GET /api/v1/operation-types", store.RoleViewer, a.handleTypes)
	s.Route("POST /api/v1/clusters/{id}/operations", store.RoleOperator, a.handlePlan)
	s.Route("GET /api/v1/clusters/{id}/operations", store.RoleViewer, a.handleList)
	s.Route("GET /api/v1/operations", store.RoleViewer, a.handleList)
	s.Route("GET /api/v1/operations/{op}", store.RoleViewer, a.handleGet)
	s.Route("POST /api/v1/operations/{op}/confirm", store.RoleOperator, a.handleConfirm)
	s.Route("POST /api/v1/operations/{op}/pause", store.RoleOperator, a.action("pause"))
	s.Route("POST /api/v1/operations/{op}/resume", store.RoleOperator, a.action("resume"))
	s.Route("POST /api/v1/operations/{op}/cancel", store.RoleOperator, a.action("cancel"))
	s.Route("DELETE /api/v1/operations/{op}", store.RoleOperator, a.action("discard"))
	s.Route("GET /api/v1/clusters/{id}/rates", store.RoleViewer, a.handleRates)
}

func (a *OperationsAPI) handleTypes(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"types": a.Engine.Types()})
}

type planRequest struct {
	Type   string          `json:"type"`
	Params json.RawMessage `json:"params"`
}

func (a *OperationsAPI) handlePlan(w http.ResponseWriter, r *http.Request) {
	var req planRequest
	if !Decode(w, r, &req) {
		return
	}
	id := r.PathValue("id")
	op, err := a.Engine.Plan(r.Context(), id, req.Type, req.Params, UserFrom(r.Context()).Username)
	a.srv.Audit(r, "operation.plan", id, req.Type, map[string]any{"params": req.Params}, err)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			WriteStoreError(w, err)
			return
		}
		WriteError(w, http.StatusBadRequest, "plan_failed", err.Error())
		return
	}
	WriteJSON(w, http.StatusCreated, op)
}

func (a *OperationsAPI) handleList(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	list, err := a.Engine.Store.ListOperations(r.Context(), r.PathValue("id"), limit)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	if list == nil {
		list = []*store.Operation{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"operations": list})
}

func (a *OperationsAPI) handleGet(w http.ResponseWriter, r *http.Request) {
	op, err := a.Engine.Store.GetOperation(r.Context(), r.PathValue("op"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, op)
}

type confirmRequest struct {
	ConfirmName string `json:"confirmName"`
}

func (a *OperationsAPI) handleConfirm(w http.ResponseWriter, r *http.Request) {
	var req confirmRequest
	if !Decode(w, r, &req) {
		return
	}
	op, err := a.Engine.Confirm(r.Context(), r.PathValue("op"), req.ConfirmName)
	a.srv.Audit(r, "operation.confirm", clusterOf(op), r.PathValue("op"), nil, err)
	a.reply(w, op, err)
}

func (a *OperationsAPI) action(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var op *store.Operation
		var err error
		id := r.PathValue("op")
		switch name {
		case "pause":
			op, err = a.Engine.Pause(r.Context(), id)
		case "resume":
			op, err = a.Engine.Resume(r.Context(), id)
		case "cancel", "discard":
			op, err = a.Engine.Cancel(r.Context(), id)
		}
		a.srv.Audit(r, "operation."+name, clusterOf(op), id, nil, err)
		if name == "discard" && err == nil {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		a.reply(w, op, err)
	}
}

func (a *OperationsAPI) reply(w http.ResponseWriter, op *store.Operation, err error) {
	switch {
	case err == nil:
		WriteJSON(w, http.StatusOK, op)
	case errors.Is(err, store.ErrNotFound):
		WriteStoreError(w, err)
	case errors.Is(err, store.ErrConflict):
		WriteError(w, http.StatusConflict, "operation_running", "another operation is active on this cluster")
	case errors.Is(err, ops.ErrConfirmName):
		WriteError(w, http.StatusBadRequest, "confirmation_required", err.Error())
	case errors.Is(err, ops.ErrInvalidState), errors.Is(err, ops.ErrPlanExpired):
		WriteError(w, http.StatusConflict, "invalid_state", err.Error())
	default:
		WriteError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

func clusterOf(op *store.Operation) string {
	if op == nil {
		return ""
	}
	return op.ClusterID
}

func (a *OperationsAPI) handleRates(w http.ResponseWriter, r *http.Request) {
	rates := a.Sampler.Rates(r.PathValue("id"))
	if rates == nil {
		rates = &metrics.Rates{Brokers: map[string]metrics.RateEntry{}, Topics: map[string]metrics.RateEntry{}}
	}
	WriteJSON(w, http.StatusOK, rates)
}
