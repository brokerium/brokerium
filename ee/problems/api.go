// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"errors"
	"net/http"
	"time"

	"github.com/brokerium/brokerium/internal/api"
	"github.com/brokerium/brokerium/internal/store"
)

// Routes returns the HTTP routes of the problems; the caller adds the license check.
func (s *Service) Routes(srv *api.Server) []api.Route {
	h := &handlers{svc: s, srv: srv}
	return []api.Route{
		{Pattern: "GET /api/v1/problems", Role: store.RoleViewer, Handler: h.counts},
		{Pattern: "GET /api/v1/clusters/{id}/problems", Role: store.RoleViewer, Handler: h.list},
		{Pattern: "POST /api/v1/clusters/{id}/problems/{fingerprint}/acknowledge", Role: store.RoleOperator, Handler: h.acknowledge},
		{Pattern: "DELETE /api/v1/clusters/{id}/problems/{fingerprint}/acknowledge", Role: store.RoleOperator, Handler: h.unacknowledge},
		{Pattern: "POST /api/v1/clusters/{id}/problems/{fingerprint}/mute", Role: store.RoleOperator, Handler: h.mute},
		{Pattern: "DELETE /api/v1/clusters/{id}/problems/{fingerprint}/mute", Role: store.RoleOperator, Handler: h.unmute},
	}
}

type handlers struct {
	svc *Service
	srv *api.Server
}

// Report lists the problems of a cluster. Muted problems are listed on request only.
type Report struct {
	Problems []Problem `json:"problems"`
	// Muted is the number of muted problems.
	Muted       int       `json:"muted"`
	EvaluatedAt time.Time `json:"evaluatedAt"`
}

func writeError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotCurrent) {
		api.WriteError(w, http.StatusNotFound, "not_found", err.Error())
		return
	}
	api.WriteStoreError(w, err)
}

func (h *handlers) counts(w http.ResponseWriter, r *http.Request) {
	counts, err := h.svc.Counts(r.Context())
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, map[string]any{"clusters": counts})
}

func (h *handlers) list(w http.ResponseWriter, r *http.Request) {
	problems, at, err := h.svc.Problems(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, err)
		return
	}
	withMuted := r.URL.Query().Get("muted") == "true"
	rep := Report{Problems: []Problem{}, EvaluatedAt: at}
	for _, p := range problems {
		if p.MutedUntil != nil {
			rep.Muted++
			if !withMuted {
				continue
			}
		}
		rep.Problems = append(rep.Problems, p)
	}
	api.WriteJSON(w, http.StatusOK, rep)
}

// auditDetail describes the problem of an audit entry.
func auditDetail(code, object string) map[string]any {
	return map[string]any{"code": code, "object": object}
}

func (h *handlers) acknowledge(w http.ResponseWriter, r *http.Request) {
	id, fingerprint := r.PathValue("id"), r.PathValue("fingerprint")
	f, err := h.svc.current(r.Context(), id, fingerprint)
	if err != nil {
		writeError(w, err)
		return
	}
	st, err := h.svc.store.AcknowledgeProblem(r.Context(), id, f.key(), api.UserFrom(r.Context()).Username, h.svc.now())
	h.srv.Audit(r, "problem.acknowledge", id, fingerprint, auditDetail(f.Code, f.Object.String()), err)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, st)
}

func (h *handlers) unacknowledge(w http.ResponseWriter, r *http.Request) {
	id, fingerprint := r.PathValue("id"), r.PathValue("fingerprint")
	st, err := h.svc.store.UnacknowledgeProblem(r.Context(), id, fingerprint)
	var detail map[string]any
	if st != nil {
		detail = auditDetail(st.Code, st.Object)
	}
	h.srv.Audit(r, "problem.unacknowledge", id, fingerprint, detail, err)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, st)
}

type muteRequest struct {
	Until time.Time `json:"until"`
}

func (h *handlers) mute(w http.ResponseWriter, r *http.Request) {
	id, fingerprint := r.PathValue("id"), r.PathValue("fingerprint")
	var req muteRequest
	if !api.Decode(w, r, &req) {
		return
	}
	now := h.svc.now()
	if !req.Until.After(now) {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "until must be a time in the future")
		return
	}
	f, err := h.svc.current(r.Context(), id, fingerprint)
	if err != nil {
		writeError(w, err)
		return
	}
	st, err := h.svc.store.MuteProblem(r.Context(), id, f.key(), req.Until, now)
	detail := auditDetail(f.Code, f.Object.String())
	detail["until"] = req.Until
	h.srv.Audit(r, "problem.mute", id, fingerprint, detail, err)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, st)
}

func (h *handlers) unmute(w http.ResponseWriter, r *http.Request) {
	id, fingerprint := r.PathValue("id"), r.PathValue("fingerprint")
	st, err := h.svc.store.UnmuteProblem(r.Context(), id, fingerprint)
	var detail map[string]any
	if st != nil {
		detail = auditDetail(st.Code, st.Object)
	}
	h.srv.Audit(r, "problem.unmute", id, fingerprint, detail, err)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, st)
}
