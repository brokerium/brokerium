// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/config"
)

type auditEntry struct {
	action, clusterID string
	detail            any
	err               error
}

// testAPI serves the routes of a service without sessions and records the audit entries.
type testAPI struct {
	t     *testing.T
	svc   *Service
	store *fakeStore
	mux   *http.ServeMux
	audit []auditEntry
}

func newTestAPI(t *testing.T, cfg config.Server) *testAPI {
	t.Helper()
	view := alphaView()
	st := newFakeStore(view.Cluster)
	a := &testAPI{t: t, store: st, svc: newTestService(t, cfg, st, &fakeClusters{views: map[string]*cluster.View{view.Cluster.ID: view}}),
		mux: http.NewServeMux()}
	h := &handlers{svc: a.svc, audit: func(_ *http.Request, action, clusterID, _ string, detail any, err error) {
		a.audit = append(a.audit, auditEntry{action: action, clusterID: clusterID, detail: detail, err: err})
	}}
	for _, r := range []struct {
		pattern string
		handler http.HandlerFunc
	}{
		{"GET /api/v1/monitoring", h.settings},
		{"PUT /api/v1/monitoring/source", h.putSource},
		{"DELETE /api/v1/monitoring/source", h.deleteSource},
		{"POST /api/v1/monitoring/source/test", h.testSource},
		{"GET /api/v1/clusters/{id}/monitoring", h.cluster},
		{"PUT /api/v1/clusters/{id}/monitoring", h.putCluster},
		{"GET /api/v1/clusters/{id}/monitoring/panels/{panel}", h.panel},
	} {
		a.mux.HandleFunc(r.pattern, r.handler)
	}
	return a
}

// call sends a request and decodes the JSON answer into out unless it is nil.
func (a *testAPI) call(method, path string, body any, out any) int {
	a.t.Helper()
	var rd *bytes.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	} else {
		rd = bytes.NewReader(nil)
	}
	rec := httptest.NewRecorder()
	a.mux.ServeHTTP(rec, httptest.NewRequest(method, path, rd))
	if out != nil && rec.Body.Len() > 0 {
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			a.t.Fatalf("%s %s: decoding %s: %v", method, path, rec.Body, err)
		}
	}
	return rec.Code
}

type apiError struct {
	Error struct{ Code, Message string } `json:"error"`
}

func TestSourceAPI(t *testing.T) {
	t.Parallel()
	prom := newFakeProm(t, standardAnswer)
	a := newTestAPI(t, config.Server{MetricsToken: "scrape"})
	var settings Settings
	if code := a.call("GET", "/api/v1/monitoring", nil, &settings); code != http.StatusOK || settings.Source.Configured ||
		settings.Source.Origin != OriginNone || !settings.MetricsEndpoint.Enabled || settings.MetricsEndpoint.TokenSource != TokenFromEnvironment {
		t.Fatalf("settings without a source = %d %+v", code, settings)
	}
	var test TestResult
	if a.call("POST", "/api/v1/monitoring/source/test", map[string]any{}, &test); test.OK || !strings.Contains(test.Error, "no Prometheus") {
		t.Errorf("test without a source = %+v", test)
	}
	var e apiError
	if code := a.call("PUT", "/api/v1/monitoring/source", map[string]any{"url": "ftp://x"}, &e); code != http.StatusBadRequest ||
		e.Error.Code != "bad_request" {
		t.Errorf("an invalid source = %d %+v", code, e)
	}

	put := map[string]any{"url": prom.URL, "auth": "basic", "username": "brokerium", "password": "s3cret", "timeoutSeconds": 20}
	var raw json.RawMessage
	if code := a.call("PUT", "/api/v1/monitoring/source", put, &raw); code != http.StatusOK || strings.Contains(string(raw), "s3cret") {
		t.Fatalf("storing a source = %d %s", code, raw)
	}
	if err := json.Unmarshal(raw, &settings); err != nil {
		t.Fatal(err)
	}
	if s := settings.Source; !s.Configured || s.Origin != OriginDatabase || s.Auth != AuthBasic || !s.PasswordSet || s.TokenSet ||
		s.TimeoutSeconds != 20 || s.UpdatedBy != "system" || s.UpdatedAt == nil {
		t.Errorf("stored source = %+v", s)
	}
	if len(a.audit) != 1 || a.audit[0].action != "monitoring.source.update" || a.audit[0].err != nil ||
		strings.Contains(stringOf(a.audit[0].detail), "s3cret") {
		t.Errorf("audit = %+v", a.audit)
	}

	if a.call("POST", "/api/v1/monitoring/source/test", map[string]any{}, &test); !test.OK || test.Version != "3.7.3" {
		t.Errorf("test of the stored source = %+v", test)
	}
	edit := map[string]any{"url": prom.URL + "/", "auth": "basic", "username": "brokerium"}
	if a.call("POST", "/api/v1/monitoring/source/test", edit, &test); !test.OK {
		t.Errorf("test of an edit that keeps the stored password = %+v", test)
	}
	for _, r := range prom.queries() {
		if r.Auth != "Basic YnJva2VyaXVtOnMzY3JldA==" {
			t.Errorf("%s: Authorization %q", r.Endpoint, r.Auth)
		}
	}
	if a.call("POST", "/api/v1/monitoring/source/test", map[string]any{"url": "http://127.0.0.1:1"}, &test); test.OK || test.Error == "" {
		t.Errorf("test of an unreachable Prometheus = %+v", test)
	}

	if code := a.call("DELETE", "/api/v1/monitoring/source", nil, nil); code != http.StatusNoContent {
		t.Errorf("DELETE = %d", code)
	}
	if a.call("GET", "/api/v1/monitoring", nil, &settings); settings.Source.Configured {
		t.Errorf("after DELETE: %+v", settings.Source)
	}
	if len(a.audit) != 2 || a.audit[1].action != "monitoring.source.delete" {
		t.Errorf("audit = %+v", a.audit)
	}
}

func stringOf(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func TestSourceFromEnvironment(t *testing.T) {
	t.Parallel()
	prom := newFakeProm(t, standardAnswer)
	a := newTestAPI(t, config.Server{Prometheus: config.Prometheus{URL: prom.URL}})
	var settings Settings
	if a.call("GET", "/api/v1/monitoring", nil, &settings); !settings.Source.Configured || settings.Source.Origin != OriginEnvironment ||
		settings.MetricsEndpoint.Enabled {
		t.Errorf("settings = %+v", settings)
	}
	var e apiError
	if code := a.call("PUT", "/api/v1/monitoring/source", map[string]any{"url": "http://other"}, &e); code != http.StatusConflict ||
		e.Error.Code != "monitoring_managed_externally" {
		t.Errorf("PUT = %d %+v", code, e)
	}
	if code := a.call("DELETE", "/api/v1/monitoring/source", nil, &e); code != http.StatusConflict {
		t.Errorf("DELETE = %d", code)
	}
	var test TestResult
	if a.call("POST", "/api/v1/monitoring/source/test", map[string]any{"url": "http://ignored"}, &test); !test.OK {
		t.Errorf("the test uses the source of the environment: %+v", test)
	}
	if len(a.audit) != 0 {
		t.Errorf("refused changes were audited: %+v", a.audit)
	}
}

func TestClusterAPI(t *testing.T) {
	t.Parallel()
	prom := newFakeProm(t, standardAnswer)
	a := newTestAPI(t, config.Server{})
	var m ClusterMonitoring
	if code := a.call("GET", "/api/v1/clusters/c-alpha/monitoring", nil, &m); code != http.StatusOK {
		t.Fatalf("GET = %d", code)
	}
	wantDefault := `instance=~"controller-1\\.example\\.com:9141|kafka-1\\.example\\.com:9141|kafka-2\\.example\\.com:9141|kafka-3\\.example\\.com:9141"`
	if m.Source.Configured || m.Profile != nil || m.Selector.Default != wantDefault || m.Selector.Effective != wantDefault ||
		m.Selector.ExporterPort != DefaultExporterPort || len(m.Ranges) != 4 {
		t.Errorf("without a source = %+v", m)
	}
	for _, p := range m.Panels {
		if p.ID == "cluster-quorum" {
			t.Error("the KRaft panel is listed without a profile")
		}
	}
	var e apiError
	if code := a.call("GET", "/api/v1/clusters/c-alpha/monitoring/panels/cluster-bytes", nil, &e); code != http.StatusConflict ||
		e.Error.Code != "monitoring_not_configured" {
		t.Errorf("a panel without a source = %d %+v", code, e)
	}

	if code := a.call("PUT", "/api/v1/monitoring/source", map[string]any{"url": prom.URL}, nil); code != http.StatusOK {
		t.Fatalf("PUT source = %d", code)
	}
	if a.call("GET", "/api/v1/clusters/c-alpha/monitoring", nil, &m); !m.Source.Configured || m.Profile == nil ||
		m.Profile.Exporter != Exporter1 || !m.Profile.KRaft || m.Profile.Brokers != 3 || !containsPanel(m.Panels, "cluster-quorum") {
		t.Errorf("with a source = %+v", m)
	}

	var data PanelData
	if code := a.call("GET", "/api/v1/clusters/c-alpha/monitoring/panels/broker-heap?range=6h", nil, &data); code != http.StatusOK {
		t.Fatalf("panel = %d", code)
	}
	if data.Step != 60 || data.End-data.Start != 6*3600 || len(data.Series) != 3 || data.Series[0].Name != "broker 101" ||
		data.Series[1].Name != "broker 102" {
		t.Errorf("broker-heap = %+v", data)
	}
	last := prom.queries()[len(prom.queries())-1]
	if q := last.Form.Get("query"); q != `sum by (instance) (jvm_memory_used_bytes{`+wantDefault+`,area="heap"})` {
		t.Errorf("query = %s", q)
	}

	if code := a.call("PUT", "/api/v1/clusters/c-alpha/monitoring", map[string]any{"selector": "{job='kafka-eu'}", "exporterPort": 7071}, &m); code != http.StatusOK ||
		m.Selector.Custom != `job="kafka-eu"` || m.Selector.Effective != `job="kafka-eu"` || m.Selector.ExporterPort != 7071 ||
		!strings.Contains(m.Selector.Default, `kafka-1\\.example\\.com:7071`) || m.Selector.UpdatedBy != "system" {
		t.Errorf("PUT selector = %d %+v", code, m.Selector)
	}
	if len(a.audit) != 2 || a.audit[1].action != "cluster.monitoring.update" || a.audit[1].clusterID != "c-alpha" {
		t.Errorf("audit = %+v", a.audit)
	}
	if a.call("GET", "/api/v1/clusters/c-alpha/monitoring/panels/topic-bytes-in", nil, &data); len(data.Series) != 2 ||
		data.Series[0].Name != "orders" {
		t.Errorf("topic-bytes-in = %+v", data)
	}
	last = prom.queries()[len(prom.queries())-1]
	if q := last.Form.Get("query"); !strings.Contains(q, `{job="kafka-eu",topic=~"orders|payments\\.v1"}`) {
		t.Errorf("query of the top topics = %s", q)
	}

	for _, tt := range []struct {
		method, path string
		body         any
		status       int
		code         string
	}{
		{"GET", "/api/v1/clusters/missing/monitoring", nil, http.StatusNotFound, "not_found"},
		{"GET", "/api/v1/clusters/missing/monitoring/panels/cluster-bytes", nil, http.StatusNotFound, "not_found"},
		{"GET", "/api/v1/clusters/c-alpha/monitoring/panels/nope", nil, http.StatusNotFound, "not_found"},
		{"GET", "/api/v1/clusters/c-alpha/monitoring/panels/cluster-bytes?range=2y", nil, http.StatusBadRequest, "bad_request"},
		{"PUT", "/api/v1/clusters/c-alpha/monitoring", map[string]any{"selector": `job="a"} or vector(1)`}, http.StatusBadRequest, "bad_request"},
		{"PUT", "/api/v1/clusters/c-alpha/monitoring", map[string]any{"exporterPort": 70000}, http.StatusBadRequest, "bad_request"},
		{"PUT", "/api/v1/clusters/c-alpha/monitoring", map[string]any{"selector": "x", "unknown": 1}, http.StatusBadRequest, "bad_request"},
		{"PUT", "/api/v1/clusters/missing/monitoring", map[string]any{}, http.StatusNotFound, "not_found"},
	} {
		var e apiError
		if code := a.call(tt.method, tt.path, tt.body, &e); code != tt.status || e.Error.Code != tt.code {
			t.Errorf("%s %s = %d %+v, want %d %s", tt.method, tt.path, code, e, tt.status, tt.code)
		}
	}

	prom.setAnswer(func(string, url.Values) (int, string) {
		return http.StatusUnprocessableEntity, `{"status":"error","errorType":"execution","error":"query processing would load too many samples"}`
	})
	if code := a.call("GET", "/api/v1/clusters/c-alpha/monitoring/panels/cluster-messages", nil, &e); code != http.StatusBadGateway ||
		e.Error.Code != "prometheus_query" || !strings.Contains(e.Error.Message, "too many samples") {
		t.Errorf("a failing query = %d %+v", code, e)
	}
	prom.Close()
	a.svc.forgetSource()
	if code := a.call("GET", "/api/v1/clusters/c-alpha/monitoring/panels/cluster-messages", nil, &e); code != http.StatusBadGateway ||
		e.Error.Code != "prometheus_unreachable" {
		t.Errorf("Prometheus down = %d %+v", code, e)
	}
	if code := a.call("GET", "/api/v1/clusters/c-alpha/monitoring", nil, &m); code != http.StatusOK || m.Profile == nil || m.Profile.Error == "" {
		t.Errorf("the profile of an unreachable Prometheus = %d %+v", code, m.Profile)
	}
}

func containsPanel(panels []Panel, id string) bool {
	for _, p := range panels {
		if p.ID == id {
			return true
		}
	}
	return false
}
