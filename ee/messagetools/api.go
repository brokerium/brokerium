// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package messagetools

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"

	"github.com/brokerium/brokerium/internal/api"
	"github.com/brokerium/brokerium/internal/store"
)

// Route is an HTTP route of the message tools.
type Route struct {
	Pattern string
	Role    string
	Handler http.HandlerFunc
}

// Routes returns the HTTP routes of the message tools; the caller adds the license check.
func Routes(s *api.Server) []Route {
	h := &handlers{srv: s}
	return []Route{
		{"POST /api/v1/clusters/{id}/topics/{topic}/delete-records", store.RoleOperator, h.deleteRecords},
		{"POST /api/v1/clusters/{id}/topics/{topic}/search", store.RoleViewer, h.search},
		{"POST /api/v1/clusters/{id}/topics/{topic}/export", store.RoleViewer, h.export},
	}
}

type handlers struct {
	srv *api.Server
}

// writeError answers with the status that matches an error of the message tools.
func writeError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrBadDeleteRequest), errors.Is(err, ErrBadFilter), errors.Is(err, ErrBadSelection):
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, ErrCompactedTopic):
		api.WriteError(w, http.StatusConflict, "compacted_topic", err.Error())
	case errors.Is(err, kerr.UnknownTopicOrPartition):
		api.WriteError(w, http.StatusNotFound, "not_found", "topic not found")
	default:
		api.KafkaError(w, err)
	}
}

func (h *handlers) deleteRecords(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := h.srv.Capable(w, r, "manageTopics")
	if !ok {
		return
	}
	topic := r.PathValue("topic")
	var req DeleteRequest
	if !api.Decode(w, r, &req) {
		return
	}
	if !req.DryRun && req.Confirm != topic {
		api.WriteError(w, http.StatusBadRequest, "confirmation_required", "repeat the topic name in confirm to delete records")
		return
	}
	ctx, cancel := api.RequestTimeout(r)
	defer cancel()
	res, err := DeleteRecords(ctx, cl.Adm, topic, req)
	if !req.DryRun {
		h.srv.Audit(r, "topic.delete-records", r.PathValue("id"), topic, deleteAudit(req, res), auditResult(res, err))
	}
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, res)
}

func (h *handlers) search(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := h.srv.Capable(w, r, "browseMessages")
	if !ok {
		return
	}
	var req SearchRequest
	if !api.Decode(w, r, &req) {
		return
	}
	profile, err := h.srv.Profile(r)
	if err != nil {
		api.WriteStoreError(w, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), maxSearchTimeout+time.Minute)
	defer cancel()
	res, err := Search(ctx, cl.Adm, profile, r.PathValue("topic"), req)
	if err != nil {
		writeError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, res)
}

func (h *handlers) export(w http.ResponseWriter, r *http.Request) {
	cl, _, ok := h.srv.Capable(w, r, "browseMessages")
	if !ok {
		return
	}
	topic := r.PathValue("topic")
	var req ExportRequest
	if !api.Decode(w, r, &req) {
		return
	}
	format, err := lookupFormat(req.Format)
	if err != nil {
		writeError(w, err)
		return
	}
	match, err := req.compile(req.FromTime, req.ToTime)
	if err != nil {
		writeError(w, err)
		return
	}
	profile, err := h.srv.Profile(r)
	if err != nil {
		api.WriteStoreError(w, err)
		return
	}
	ctx, cancel := api.RequestTimeout(r)
	ranges, err := Ranges(ctx, cl.Adm, topic, req.Selection)
	cancel()
	if err != nil {
		writeError(w, err)
		return
	}
	limit := bounded(req.Limit, maxExportRecords, maxExportRecords)
	limits := Limits{Time: bounded(time.Duration(req.TimeoutSeconds)*time.Second, defaultExportTimeout, maxExportTimeout)}
	// Kafka topic names contain only letters, digits, '.', '_' and '-', which are safe here.
	w.Header().Set("Content-Type", format.contentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, topic, format.extension))
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	out := format.open(w)
	var exported int64
	var writeErr error
	res, scanErr := Scan(r.Context(), profile, topic, ranges, limits, func(rec *kgo.Record) bool {
		if !match(rec) {
			return true
		}
		if writeErr = out.write(rec); writeErr != nil {
			return false
		}
		exported++
		if exported%1000 == 0 {
			_ = rc.Flush()
		}
		return exported < limit
	})
	if writeErr == nil {
		writeErr = out.close()
	}
	h.srv.Audit(r, "topic.export", r.PathValue("id"), topic, map[string]any{"format": format.extension, "records": exported,
		"scanned": res.Scanned, "complete": res.Complete, "stopped": res.Stopped}, errors.Join(scanErr, writeErr))
}
