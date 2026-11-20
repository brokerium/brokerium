// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/brokerium/brokerium/internal/api"
	"github.com/brokerium/brokerium/internal/store"
)

// testTimeout bounds a test of a data source.
const testTimeout = 15 * time.Second

// Routes returns the HTTP routes of the monitoring feature; the caller adds the license check.
func (svc *Service) Routes(s *api.Server) []api.Route {
	h := &handlers{svc: svc, audit: s.Audit}
	return []api.Route{
		{Pattern: "GET /api/v1/monitoring", Role: store.RoleAdmin, Handler: h.settings},
		{Pattern: "PUT /api/v1/monitoring/source", Role: store.RoleAdmin, Handler: h.putSource},
		{Pattern: "DELETE /api/v1/monitoring/source", Role: store.RoleAdmin, Handler: h.deleteSource},
		{Pattern: "POST /api/v1/monitoring/source/test", Role: store.RoleAdmin, Handler: h.testSource},
		{Pattern: "GET /api/v1/clusters/{id}/monitoring", Role: store.RoleViewer, Handler: h.cluster},
		{Pattern: "PUT /api/v1/clusters/{id}/monitoring", Role: store.RoleAdmin, Handler: h.putCluster},
		{Pattern: "GET /api/v1/clusters/{id}/monitoring/panels/{panel}", Role: store.RoleViewer, Handler: h.panel},
	}
}

type handlers struct {
	svc   *Service
	audit func(r *http.Request, action, clusterID, target string, detail any, result error)
}

func actor(r *http.Request) string {
	if u := api.UserFrom(r.Context()); u != nil {
		return u.Username
	}
	return "system"
}

// Settings are the monitoring settings of the installation.
type Settings struct {
	Source          SourceView          `json:"source"`
	MetricsEndpoint MetricsEndpointView `json:"metricsEndpoint"`
}

// MetricsEndpointView tells whether GET /metrics is on.
type MetricsEndpointView struct {
	Enabled bool `json:"enabled"`
	// TokenSource is environment (BROKERIUM_METRICS_TOKEN) or file (BROKERIUM_METRICS_TOKEN_FILE).
	TokenSource string `json:"tokenSource,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (h *handlers) settingsOf(ctx context.Context) (Settings, error) {
	var out Settings
	src, err := h.svc.loadSource(ctx)
	if err != nil && src == nil {
		return out, err
	}
	out.Source = viewOf(src)
	if err != nil {
		out.Source.Error = err.Error()
	}
	token, source, err := h.svc.metricsToken()
	out.MetricsEndpoint = MetricsEndpointView{Enabled: token != "", TokenSource: source}
	if err != nil {
		out.MetricsEndpoint.Error = err.Error()
	}
	return out, nil
}

func (h *handlers) settings(w http.ResponseWriter, r *http.Request) {
	out, err := h.settingsOf(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	api.WriteJSON(w, http.StatusOK, out)
}

// storedSource returns the source stored in the database, or nil.
func (h *handlers) storedSource(ctx context.Context) (*Source, error) {
	st, err := h.svc.store.GetSetting(ctx, sourceKey)
	if errors.Is(err, store.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return storedSourceOf(st, h.svc.box)
}

func (h *handlers) putSource(w http.ResponseWriter, r *http.Request) {
	if h.svc.cfg.Prometheus.URL != "" {
		api.WriteError(w, http.StatusConflict, "monitoring_managed_externally", ErrManagedExternally.Error())
		return
	}
	var req SourceRequest
	if !api.Decode(w, r, &req) {
		return
	}
	prev, err := h.storedSource(r.Context())
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	src, err := req.apply(prev)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	st, err := src.setting(h.svc.box, actor(r))
	if err == nil {
		err = h.svc.store.SetSetting(r.Context(), st)
	}
	h.audit(r, "monitoring.source.update", "", src.URL, map[string]any{"url": src.URL, "auth": src.Auth,
		"username": src.Username, "credentialsChanged": req.Password != nil || req.Token != nil,
		"insecureSkipVerify": src.InsecureSkipVerify}, err)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	h.svc.forgetSource()
	h.svc.log.Info("Prometheus data source stored", "url", src.URL, "by", actor(r))
	h.settings(w, r)
}

func (h *handlers) deleteSource(w http.ResponseWriter, r *http.Request) {
	if h.svc.cfg.Prometheus.URL != "" {
		api.WriteError(w, http.StatusConflict, "monitoring_managed_externally", ErrManagedExternally.Error())
		return
	}
	err := h.svc.store.DeleteSetting(r.Context(), sourceKey)
	h.audit(r, "monitoring.source.delete", "", "", nil, err)
	if err != nil {
		api.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	h.svc.forgetSource()
	w.WriteHeader(http.StatusNoContent)
}

// TestResult is the result of a test of a data source.
type TestResult struct {
	OK         bool   `json:"ok"`
	Version    string `json:"version,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Error      string `json:"error,omitempty"`
}

// testSource tests the settings of the request, or the source in use when the request has no
// URL. Credentials missing from the request come from the stored source.
func (h *handlers) testSource(w http.ResponseWriter, r *http.Request) {
	var req SourceRequest
	if !api.Decode(w, r, &req) {
		return
	}
	var src *Source
	var err error
	if req.URL == "" || h.svc.cfg.Prometheus.URL != "" {
		if src, err = h.svc.loadSource(r.Context()); err == nil && src == nil {
			err = ErrNotConfigured
		}
	} else {
		var prev *Source
		if prev, err = h.storedSource(r.Context()); err == nil {
			src, err = req.apply(prev)
		}
	}
	if err != nil {
		api.WriteJSON(w, http.StatusOK, TestResult{Error: err.Error()})
		return
	}
	api.WriteJSON(w, http.StatusOK, testConnection(r.Context(), src))
}

// testConnection runs a query against a source and asks for its version.
func testConnection(ctx context.Context, src *Source) TestResult {
	c, err := NewClient(src)
	if err != nil {
		return TestResult{Error: err.Error()}
	}
	defer c.Close()
	ctx, cancel := context.WithTimeout(ctx, min(src.Timeout, testTimeout))
	defer cancel()
	start := time.Now()
	res, err := c.Query(ctx, "vector(1)", start)
	elapsed := time.Since(start).Milliseconds()
	switch {
	case err != nil:
		return TestResult{Error: err.Error(), DurationMs: elapsed}
	case len(res.Series) != 1 || len(res.Series[0].Points) != 1 || res.Series[0].Points[0].V != 1:
		return TestResult{Error: ErrResponse.Error() + ": vector(1) did not return 1", DurationMs: elapsed}
	}
	version, _ := c.BuildInfo(ctx)
	return TestResult{OK: true, Version: version, DurationMs: elapsed}
}

// SourceState tells the dashboard whether panels can be queried.
type SourceState struct {
	Configured bool   `json:"configured"`
	Origin     string `json:"origin"`
	Error      string `json:"error,omitempty"`
}

// ClusterMonitoring is the monitoring of a cluster: the source, its selector, the profile of its
// exporters and the panels.
type ClusterMonitoring struct {
	Source   SourceState  `json:"source"`
	Selector SelectorView `json:"selector"`
	Profile  *Profile     `json:"profile"`
	Panels   []Panel      `json:"panels"`
	Ranges   []string     `json:"ranges"`
}

func (h *handlers) clusterMonitoring(ctx context.Context, clusterID string) (*ClusterMonitoring, error) {
	t, sel, err := h.svc.targetOf(ctx, clusterID)
	if err != nil {
		return nil, err
	}
	out := &ClusterMonitoring{Selector: sel, Ranges: Ranges(), Source: SourceState{Origin: OriginNone}}
	src, client, err := h.svc.clientFor(ctx)
	if src != nil {
		out.Source = SourceState{Configured: true, Origin: src.Origin}
	}
	switch {
	case errors.Is(err, ErrNotConfigured):
	case err != nil:
		out.Source.Error = err.Error()
	case t.selector != "":
		p := h.svc.profileOf(ctx, client, src.fingerprint(), clusterID, t.selector)
		out.Profile = &p
	}
	out.Panels = panelsFor(out.Profile)
	return out, nil
}

func (h *handlers) cluster(w http.ResponseWriter, r *http.Request) {
	out, err := h.clusterMonitoring(r.Context(), r.PathValue("id"))
	if err != nil {
		api.WriteStoreError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, out)
}

// SelectorRequest replaces the selector or the exporter port of a cluster; empty values restore
// the defaults.
type SelectorRequest struct {
	Selector     string `json:"selector"`
	ExporterPort int    `json:"exporterPort"`
}

func (h *handlers) putCluster(w http.ResponseWriter, r *http.Request) {
	clusterID := r.PathValue("id")
	var req SelectorRequest
	if !api.Decode(w, r, &req) {
		return
	}
	if req.ExporterPort < 0 || req.ExporterPort > 65535 {
		api.WriteError(w, http.StatusBadRequest, "bad_request", "exporterPort must be between 1 and 65535, or 0 for the default")
		return
	}
	selector, err := normalizeSelector(req.Selector)
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	err = h.svc.store.SetClusterMonitoring(r.Context(), &store.ClusterMonitoring{ClusterID: clusterID, Selector: selector,
		ExporterPort: req.ExporterPort, UpdatedBy: actor(r)})
	h.audit(r, "cluster.monitoring.update", clusterID, "", map[string]any{"selector": selector, "exporterPort": req.ExporterPort}, err)
	if err != nil {
		api.WriteStoreError(w, err)
		return
	}
	h.cluster(w, r)
}

func (h *handlers) panel(w http.ResponseWriter, r *http.Request) {
	p, ok := lookupPanel(r.PathValue("panel"))
	if !ok {
		api.WriteError(w, http.StatusNotFound, "not_found", "unknown panel")
		return
	}
	win, err := parseWindow(r.URL.Query(), h.svc.now())
	if err != nil {
		api.WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	clusterID := r.PathValue("id")
	t, _, err := h.svc.targetOf(r.Context(), clusterID)
	if err != nil {
		api.WriteStoreError(w, err)
		return
	}
	src, client, err := h.svc.clientFor(r.Context())
	switch {
	case errors.Is(err, ErrNotConfigured):
		api.WriteError(w, http.StatusConflict, "monitoring_not_configured", err.Error())
		return
	case err != nil:
		api.WriteError(w, http.StatusConflict, "monitoring_unavailable", err.Error())
		return
	case t.selector == "":
		api.WriteError(w, http.StatusConflict, "no_selector",
			"the addresses of the brokers are not known yet; set a selector for the cluster")
		return
	}
	prof := h.svc.profileOf(r.Context(), client, src.fingerprint(), clusterID, t.selector)
	data, err := runPanel(r.Context(), client, p, t, prof, win)
	if err != nil {
		writeQueryError(w, err)
		return
	}
	api.WriteJSON(w, http.StatusOK, data)
}

// writeQueryError answers with the status that matches an error of a query.
func writeQueryError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		api.WriteError(w, http.StatusGatewayTimeout, "prometheus_timeout", err.Error())
	case errors.Is(err, ErrRefused):
		api.WriteError(w, http.StatusBadGateway, "prometheus_refused", err.Error())
	case errors.Is(err, ErrQuery):
		api.WriteError(w, http.StatusBadGateway, "prometheus_query", err.Error())
	case errors.Is(err, ErrUnreachable):
		api.WriteError(w, http.StatusBadGateway, "prometheus_unreachable", err.Error())
	case errors.Is(err, ErrResponse):
		api.WriteError(w, http.StatusBadGateway, "prometheus_response", err.Error())
	default:
		api.WriteError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}
