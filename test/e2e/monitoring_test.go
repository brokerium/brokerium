// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e && enterprise

package e2e

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/edition"
)

// fakePrometheus answers the queries of the monitoring feature with fixed series and records them.
type fakePrometheus struct {
	*httptest.Server
	mu      sync.Mutex
	queries []string
	auth    []string
}

func newFakePrometheus(t *testing.T) *fakePrometheus {
	t.Helper()
	f := &fakePrometheus{}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		form, _ := url.ParseQuery(string(body))
		query := form.Get("query")
		f.mu.Lock()
		f.queries = append(f.queries, query)
		f.auth = append(f.auth, r.Header.Get("Authorization"))
		f.mu.Unlock()
		now := time.Now().Unix()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/status/buildinfo"):
			_, _ = io.WriteString(w, `{"status":"success","data":{"version":"3.0.0-e2e"}}`)
		case query == "vector(1)":
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[%d,"1"]}]}}`, now)
		case strings.HasPrefix(query, "count by (__name__)"):
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"vector","result":[
				{"metric":{"__name__":"jvm_memory_used_bytes"},"value":[%d,"2"]},
				{"metric":{"__name__":"kafka_server_replicamanager_partitioncount"},"value":[%d,"1"]}]}}`, now, now)
		case strings.HasSuffix(r.URL.Path, "/query_range"):
			start, _ := strconv.ParseFloat(form.Get("start"), 64)
			_, _ = fmt.Fprintf(w, `{"status":"success","data":{"resultType":"matrix","result":[
				{"metric":{"instance":"localhost:9141"},"values":[[%d,"1"],[%d,"2"]]}]}}`, int64(start), int64(start)+60)
		default:
			_, _ = io.WriteString(w, `{"status":"success","data":{"resultType":"vector","result":[]}}`)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

func (f *fakePrometheus) last() (query, auth string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.queries) == 0 {
		return "", ""
	}
	return f.queries[len(f.queries)-1], f.auth[len(f.auth)-1]
}

type clusterMonitoring struct {
	Source struct {
		Configured bool   `json:"configured"`
		Origin     string `json:"origin"`
	} `json:"source"`
	Selector struct {
		Effective string `json:"effective"`
		Custom    string `json:"custom"`
		Default   string `json:"default"`
	} `json:"selector"`
	Profile *struct {
		Exporter string `json:"exporter"`
		Brokers  int    `json:"brokers"`
		Error    string `json:"error"`
	} `json:"profile"`
	Panels []struct {
		ID string `json:"id"`
	} `json:"panels"`
}

// TestEnterpriseMonitoring covers the monitoring of Brokerium Enterprise on the lab cluster beta
// against a fake Prometheus that the test serves on the loopback interface, so the server must run
// on this host: the license gate, the data source with sealed credentials, the default and custom
// selectors, panel queries and GET /metrics. With BROKERIUM_E2E_METRICS_TOKEN, the token the server
// got as BROKERIUM_METRICS_TOKEN, it scrapes the endpoint; without, it expects the endpoint off.
//
//	go test -tags e2e,enterprise -run TestEnterpriseMonitoring ./test/e2e
func TestEnterpriseMonitoring(t *testing.T) {
	c := newClient(t)
	var lic licenseView
	c.must(http.StatusOK, "GET", "/api/v1/license", nil, &lic)
	if lic.Edition != edition.Enterprise {
		t.Skipf("the server runs the %s edition", lic.Edition)
	}
	if host, _, err := net.SplitHostPort(strings.TrimPrefix(strings.TrimPrefix(c.base, "http://"), "https://")); err != nil ||
		(host != "localhost" && host != "127.0.0.1") {
		t.Skipf("the fake Prometheus listens on this host, but the server runs at %s", c.base)
	}
	if lic.License != nil {
		if lic.License.Source != edition.SourceDatabase || lic.License.Customer != e2eCustomer {
			t.Skipf("the server has a license key (source %s); run against a server without one", lic.License.Source)
		}
		c.must(http.StatusNoContent, "DELETE", "/api/v1/license", nil, nil)
	}
	beta := c.clusterID("beta")
	base := "/api/v1/clusters/" + beta + "/monitoring"
	token := os.Getenv("BROKERIUM_E2E_METRICS_TOKEN")
	scrape := func(bearer string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, c.base+"/metrics", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+bearer)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		raw, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(raw)
	}

	refused := func(when string) {
		t.Helper()
		for _, path := range []string{base, base + "/panels/cluster-bytes", "/api/v1/monitoring"} {
			if status, raw := c.send("GET", path, nil); status != http.StatusForbidden || errorCode(raw) != "license_required" {
				t.Errorf("%s: GET %s = %d %s, want 403 license_required", when, path, status, raw)
			}
		}
		if token != "" {
			if status, body := scrape(token); status != http.StatusForbidden || !strings.Contains(body, "license_required") {
				t.Errorf("%s: GET /metrics = %d %s, want 403", when, status, body)
			}
		}
	}
	refused("without a license")
	if token == "" {
		if status, _ := scrape("anything"); status != http.StatusNotFound {
			t.Errorf("GET /metrics without a token on the server = %d, want 404", status)
		}
	} else if status, _ := scrape(token + "-wrong"); status != http.StatusUnauthorized {
		t.Errorf("GET /metrics with a wrong token = %d, want 401", status)
	}

	key := signLicense(t, testSigningKey(t), edition.Monitoring)
	if status, raw := c.send("PUT", "/api/v1/license", map[string]string{"key": key}); status != http.StatusOK {
		t.Fatalf("adding a key of the test key: %d %s; is the server built with -tags enterprise,licensetest?", status, raw)
	}
	t.Cleanup(func() { c.do("DELETE", "/api/v1/license", nil, nil) })

	var settings struct {
		Source struct {
			Configured  bool   `json:"configured"`
			Origin      string `json:"origin"`
			Auth        string `json:"auth"`
			PasswordSet bool   `json:"passwordSet"`
		} `json:"source"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/monitoring", nil, &settings)
	if settings.Source.Origin == "environment" {
		t.Skip("BROKERIUM_PROMETHEUS_URL sets the data source of the server")
	}
	// Left over by an interrupted run.
	c.must(http.StatusOK, "PUT", base, map[string]any{"selector": ""}, nil)
	prom := newFakePrometheus(t)
	status, raw := c.send("PUT", "/api/v1/monitoring/source", map[string]any{"url": prom.URL, "auth": "basic",
		"username": "e2e", "password": "e2e-secret"})
	if status != http.StatusOK || strings.Contains(string(raw), "e2e-secret") {
		t.Fatalf("storing the data source = %d %s", status, raw)
	}
	finished := false
	t.Cleanup(func() {
		if !finished {
			c.do("PUT", base, map[string]any{"selector": ""}, nil)
			c.do("DELETE", "/api/v1/monitoring/source", nil, nil)
		}
	})
	if err := json.Unmarshal(raw, &settings); err != nil || !settings.Source.Configured || settings.Source.Auth != "basic" ||
		!settings.Source.PasswordSet {
		t.Errorf("stored source = %+v, %v", settings.Source, err)
	}
	var test struct {
		OK      bool   `json:"ok"`
		Version string `json:"version"`
		Error   string `json:"error"`
	}
	c.must(http.StatusOK, "POST", "/api/v1/monitoring/source/test", map[string]any{}, &test)
	if !test.OK || test.Version != "3.0.0-e2e" {
		t.Errorf("test of the data source = %+v", test)
	}
	if _, auth := prom.last(); auth != "Basic ZTJlOmUyZS1zZWNyZXQ=" {
		t.Errorf("the server sent Authorization %q", auth)
	}

	var cv struct {
		Snapshot struct {
			Brokers []struct {
				ID   int32  `json:"id"`
				Host string `json:"host"`
			} `json:"brokers"`
		} `json:"snapshot"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/clusters/"+beta, nil, &cv)
	var m clusterMonitoring
	c.must(http.StatusOK, "GET", base, nil, &m)
	if !m.Source.Configured || m.Source.Origin != "database" || m.Selector.Custom != "" || m.Selector.Effective != m.Selector.Default ||
		m.Profile == nil || m.Profile.Exporter != "1.x" || m.Profile.Brokers != 1 || len(m.Panels) == 0 {
		t.Fatalf("monitoring of beta = %+v", m)
	}
	for _, b := range cv.Snapshot.Brokers {
		instance := strings.ReplaceAll(regexp.QuoteMeta(net.JoinHostPort(b.Host, "9141")), `\`, `\\`)
		if !strings.Contains(m.Selector.Default, instance) {
			t.Errorf("the default selector %s misses broker %d at %s", m.Selector.Default, b.ID, b.Host)
		}
	}

	var data struct {
		Unit   string `json:"unit"`
		Step   int    `json:"step"`
		Series []struct {
			Name   string       `json:"name"`
			Points [][2]float64 `json:"points"`
		} `json:"series"`
	}
	c.must(http.StatusOK, "GET", base+"/panels/broker-heap?range=6h", nil, &data)
	if data.Unit != "bytes" || data.Step != 60 || len(data.Series) != 1 || len(data.Series[0].Points) != 2 {
		t.Errorf("broker-heap = %+v", data)
	}
	if query, _ := prom.last(); query != `sum by (instance) (jvm_memory_used_bytes{`+m.Selector.Default+`,area="heap"})` {
		t.Errorf("query of broker-heap = %s", query)
	}

	c.must(http.StatusOK, "PUT", base, map[string]any{"selector": "{job='kafka-e2e'}"}, &m)
	if m.Selector.Effective != `job="kafka-e2e"` || m.Selector.Custom != `job="kafka-e2e"` {
		t.Errorf("selector after PUT = %+v", m.Selector)
	}
	c.must(http.StatusOK, "GET", base+"/panels/cluster-bytes", nil, &data)
	if query, _ := prom.last(); !strings.Contains(query, `{job="kafka-e2e",topic=""}`) {
		t.Errorf("query of cluster-bytes = %s", query)
	}
	if status, raw := c.send("PUT", base, map[string]any{"selector": `job="a"} or vector(1) or {x="b"`}); status != http.StatusBadRequest {
		t.Errorf("a selector with PromQL around it = %d %s, want 400", status, raw)
	}
	if status, raw := c.send("GET", base+"/panels/nope", nil); status != http.StatusNotFound {
		t.Errorf("an unknown panel = %d %s, want 404", status, raw)
	}

	if token != "" {
		status, body := scrape(token)
		for _, want := range []string{
			`brokerium_cluster_info{cluster="beta"`,
			`brokerium_cluster_health_status{cluster="beta"}`,
			`brokerium_cluster_reachable{cluster="beta"} 1`,
			`brokerium_cluster_brokers_alive{cluster="beta"}`,
			`brokerium_cluster_operations{cluster="beta",status="running"}`,
		} {
			if status != http.StatusOK || !strings.Contains(body, want) {
				t.Errorf("GET /metrics = %d, missing %s", status, want)
			}
		}
	}

	c.must(http.StatusOK, "PUT", base, map[string]any{"selector": ""}, nil)
	c.must(http.StatusNoContent, "DELETE", "/api/v1/monitoring/source", nil, nil)
	finished = true
	c.must(http.StatusNoContent, "DELETE", "/api/v1/license", nil, nil)
	refused("after removing the license")
}
