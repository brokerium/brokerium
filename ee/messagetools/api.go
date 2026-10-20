// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package messagetools

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/twmb/franz-go/pkg/kerr"

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
	}
}

type handlers struct {
	srv *api.Server
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
	switch {
	case errors.Is(err, ErrBadDeleteRequest):
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
	case errors.Is(err, ErrCompactedTopic):
		api.WriteError(w, http.StatusConflict, "compacted_topic", err.Error())
	case errors.Is(err, kerr.UnknownTopicOrPartition):
		api.WriteError(w, http.StatusNotFound, "not_found", "topic not found")
	case err != nil:
		api.KafkaError(w, err)
	default:
		api.WriteJSON(w, http.StatusOK, res)
	}
}

func deleteAudit(req DeleteRequest, res *DeleteResult) map[string]any {
	detail := map[string]any{}
	switch {
	case req.All:
		detail["mode"] = "all"
	case req.BeforeTimestamp != nil:
		detail["mode"], detail["beforeTimestamp"] = "time", *req.BeforeTimestamp
	default:
		detail["mode"] = "offsets"
	}
	if res != nil {
		before := map[int32]int64{}
		for _, p := range res.Partitions {
			if p.Error == "" {
				before[p.Partition] = p.DeleteBefore
			}
		}
		detail["records"], detail["deleteBefore"] = res.Records, before
	}
	return detail
}

func auditResult(res *DeleteResult, err error) error {
	if err == nil && res != nil && res.Failed > 0 {
		return fmt.Errorf("%d of %d partitions failed", res.Failed, len(res.Partitions))
	}
	return err
}
