// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/store"
)

func TestInstallCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		api  NodesAPI
		want string
	}{
		{"direct", NodesAPI{PublicURL: "https://brokerium.example.com/", AgentEndpoint: "agents.example.com:8443"},
			"curl -fsSL https://brokerium.example.com/api/v1/agent/install.sh | sudo BROKERIUM_AGENT_SERVER=agents.example.com:8443 BROKERIUM_AGENT_TOKEN=brk1.t sh"},
		{"proxy", NodesAPI{PublicURL: "https://brokerium.example.com", AgentEndpoint: "agents.example.com:8443",
			Proxy: "http://proxy.corp:3128", NoProxy: "localhost,.corp"},
			"HTTPS_PROXY='http://proxy.corp:3128' NO_PROXY='localhost,.corp' curl -fsSL https://brokerium.example.com/api/v1/agent/install.sh | " +
				"sudo HTTPS_PROXY='http://proxy.corp:3128' NO_PROXY='localhost,.corp' BROKERIUM_AGENT_SERVER=agents.example.com:8443 BROKERIUM_AGENT_TOKEN=brk1.t sh"},
		{"quote in the proxy", NodesAPI{PublicURL: "https://b", AgentEndpoint: "a:1", Proxy: "http://u:p'w@proxy:3128"},
			`HTTPS_PROXY='http://u:p'\''w@proxy:3128' curl -fsSL https://b/api/v1/agent/install.sh | sudo HTTPS_PROXY='http://u:p'\''w@proxy:3128' BROKERIUM_AGENT_SERVER=a:1 BROKERIUM_AGENT_TOKEN=brk1.t sh`},
		{"insecure transport", NodesAPI{PublicURL: "https://brokerium.example.com", AgentEndpoint: "agents.example.com:8443",
			Transport: "insecure", InsecureEndpoint: "agents.example.com:443", InsecureTLS: "system"},
			"curl -fsSL https://brokerium.example.com/api/v1/agent/install.sh | sudo BROKERIUM_AGENT_TRANSPORT=insecure " +
				"BROKERIUM_AGENT_TLS=system BROKERIUM_AGENT_SERVER=agents.example.com:443 BROKERIUM_AGENT_TOKEN=brk1.t sh"},
		{"insecure transport through an HTTP proxy", NodesAPI{PublicURL: "https://b", AgentEndpoint: "a:8443",
			Proxy: "http://proxy.corp:3128", Transport: "insecure", InsecureEndpoint: "agents.corp:80", InsecureTLS: "none"},
			"HTTPS_PROXY='http://proxy.corp:3128' curl -fsSL https://b/api/v1/agent/install.sh | sudo HTTPS_PROXY='http://proxy.corp:3128' " +
				"BROKERIUM_AGENT_TRANSPORT=insecure BROKERIUM_AGENT_TLS=none BROKERIUM_AGENT_SERVER=agents.corp:80 BROKERIUM_AGENT_TOKEN=brk1.t sh"},
		{"mutual TLS keeps the gateway endpoint", NodesAPI{PublicURL: "https://b", AgentEndpoint: "a:8443", Transport: "mtls",
			InsecureEndpoint: "agents.corp:443", InsecureTLS: "system"},
			"curl -fsSL https://b/api/v1/agent/install.sh | sudo BROKERIUM_AGENT_SERVER=a:8443 BROKERIUM_AGENT_TOKEN=brk1.t sh"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.api.installCommand("brk1.t"); got != tt.want {
				t.Errorf("installCommand =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func TestInstallScriptSyntax(t *testing.T) {
	t.Parallel()
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skip("no sh")
	}
	path := filepath.Join(t.TempDir(), "install.sh")
	if err := os.WriteFile(path, []byte(installScript), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command(sh, "-n", path).CombinedOutput(); err != nil {
		t.Fatalf("sh -n: %v: %s", err, out)
	}
}

// TestInstallScriptPrivileges checks what the installer lets the agent do as root and that the
// agent unit keeps sudo working: options that set the no new privileges flag or restrict the
// capabilities of root make every start and stop of Kafka fail.
func TestInstallScriptPrivileges(t *testing.T) {
	t.Parallel()
	const rule = "brokerium-agent ALL=(root) NOPASSWD: $SYSTEMCTL start brokerium-kafka.service, " +
		"$SYSTEMCTL stop brokerium-kafka.service, $SYSTEMCTL restart brokerium-kafka.service\n"
	if strings.Count(installScript, "NOPASSWD") != 1 || !strings.Contains(installScript, "\n"+rule) {
		t.Errorf("the sudo rule must allow only %q", rule)
	}
	_, unit, _ := strings.Cut(installScript, `cat > "$AGENT_UNIT" <<EOF`)
	unit, _, found := strings.Cut(unit, "\nEOF\n")
	if !found || !strings.Contains(unit, "\nUser=brokerium-agent\n") || !strings.Contains(unit, "\nNoNewPrivileges=no\n") {
		t.Fatalf("agent unit without User=brokerium-agent and NoNewPrivileges=no:%s", unit)
	}
	for _, option := range []string{"SystemCallFilter", "SystemCallArchitectures", "RestrictAddressFamilies", "RestrictNamespaces",
		"PrivateDevices", "ProtectKernelTunables", "ProtectKernelModules", "ProtectKernelLogs", "ProtectClock",
		"MemoryDenyWriteExecute", "RestrictRealtime", "RestrictSUIDSGID", "LockPersonality", "DynamicUser", "PrivateUsers",
		"CapabilityBoundingSet", "AmbientCapabilities"} {
		if strings.Contains(unit, "\n"+option+"=") {
			t.Errorf("the agent unit sets %s", option)
		}
	}
}

func TestJoinTokenRequests(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{"without a name", `{}`, ""},
		{"without a name for a cluster", `{"clusterId": "c1", "ttlMinutes": 30}`, ""},
		{"with a name", `{"nodeName": "Kafka_17.example.com"}`, ""},
		{"for a node to enroll again", `{"nodeId": "n1"}`, ""},
		{"for a node with an empty cluster", `{"nodeId": "n1", "clusterId": ""}`, ""},
		{"an invalid name", `{"nodeName": "kafka 17"}`, "nodeName must be 1-63 letters"},
		{"a name that is too long", `{"nodeName": "` + strings.Repeat("k", 64) + `"}`, "nodeName must be 1-63 letters"},
		{"a node id and a name", `{"nodeId": "n1", "nodeName": "kafka-17"}`, "leave nodeName and clusterId empty"},
		{"a node id and a cluster", `{"nodeId": "n1", "clusterId": "c1"}`, "leave nodeName and clusterId empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var req joinTokenRequest
			if err := json.Unmarshal([]byte(tt.body), &req); err != nil {
				t.Fatal(err)
			}
			err := req.validate()
			if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("validate() = %v, want %q", err, tt.want)
			}
			if tt.want != "" {
				rec := serve(t, (&NodesAPI{}).handleJoinToken, http.MethodPost, "/api/v1/join-tokens", tt.body)
				if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tt.want) {
					t.Errorf("handler answered %d %s", rec.Code, rec.Body)
				}
			}
		})
	}
}

func TestNodeUpdates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		body string
		want string
	}{
		{"a name", `{"name": "kafka-17"}`, ""},
		{"an advertised host", `{"advertisedHost": "kafka-17.example.com"}`, ""},
		{"an IPv6 address", `{"advertisedHost": "2001:db8::17"}`, ""},
		{"the reported host name again", `{"advertisedHost": ""}`, ""},
		{"a rack", `{"rack": "rack-a"}`, ""},
		{"no rack", `{"rack": ""}`, ""},
		{"everything", `{"name": "k", "advertisedHost": "192.0.2.17", "rack": "r1"}`, ""},
		{"nothing", `{}`, "give name, advertisedHost or rack"},
		{"an empty name", `{"name": ""}`, "name must be 1-63 letters"},
		{"a name with a slash", `{"name": "kafka/17"}`, "name must be 1-63 letters"},
		{"an advertised host with a port", `{"advertisedHost": "kafka-17:9092"}`, "advertisedHost: host"},
		{"an advertised host with an underscore", `{"advertisedHost": "kafka_17"}`, "neither a DNS name nor an IP address"},
		{"a rack with a space", `{"rack": "rack a"}`, "use at most 64 letters, digits"},
		{"a rack that is too long", `{"rack": "` + strings.Repeat("r", 65) + `"}`, "at most 64"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var u nodeUpdate
			if err := json.Unmarshal([]byte(tt.body), &u); err != nil {
				t.Fatal(err)
			}
			err := u.validate()
			if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
				t.Fatalf("validate() = %v, want %q", err, tt.want)
			}
			if tt.want != "" {
				rec := serve(t, (&NodesAPI{}).handleUpdate, http.MethodPatch, "/api/v1/nodes/n1", tt.body)
				if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tt.want) {
					t.Errorf("handler answered %d %s", rec.Code, rec.Body)
				}
			}
		})
	}
	if rec := serve(t, (&NodesAPI{}).handleUpdate, http.MethodPatch, "/api/v1/nodes/n1", `{"role": "broker"}`); rec.Code != http.StatusBadRequest {
		t.Errorf("an unknown field: %d", rec.Code)
	}
}

func TestNodeUpdateApply(t *testing.T) {
	t.Parallel()
	str := func(s string) *string { return &s }
	tests := []struct {
		name     string
		hostname string
		update   nodeUpdate
		want     store.Node
	}{
		{"name and rack", "kafka-1", nodeUpdate{Name: str("broker-a"), Rack: str("r2")},
			store.Node{Name: "broker-a", AdvertisedHost: "old.example.com", Rack: "r2"}},
		{"advertised host", "kafka-1", nodeUpdate{AdvertisedHost: str("kafka-1.example.com")},
			store.Node{Name: "kafka-1", AdvertisedHost: "kafka-1.example.com", Rack: "r1"}},
		{"back to the reported host name", "kafka-1", nodeUpdate{AdvertisedHost: str("")},
			store.Node{Name: "kafka-1", AdvertisedHost: "kafka-1", Rack: "r1"}},
		{"a reported host name that cannot be advertised", "kafka_1", nodeUpdate{AdvertisedHost: str("")},
			store.Node{Name: "kafka-1", AdvertisedHost: "", Rack: "r1"}},
		{"no rack", "kafka-1", nodeUpdate{Rack: str("")}, store.Node{Name: "kafka-1", AdvertisedHost: "old.example.com"}},
	}
	for _, tt := range tests {
		n := &store.Node{Name: "kafka-1", Hostname: tt.hostname, AdvertisedHost: "old.example.com", Rack: "r1"}
		tt.update.apply(n)
		if n.Name != tt.want.Name || n.AdvertisedHost != tt.want.AdvertisedHost || n.Rack != tt.want.Rack {
			t.Errorf("%s: %q %q %q, want %q %q %q", tt.name, n.Name, n.AdvertisedHost, n.Rack, tt.want.Name, tt.want.AdvertisedHost, tt.want.Rack)
		}
	}
}

// serve calls a handler of a route with a JSON body.
func serve(t *testing.T, h http.HandlerFunc, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc(method+" "+strings.Replace(path, "/n1", "/{id}", 1), h)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(method, path, strings.NewReader(body)))
	return rec
}

func TestCertificateState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	tests := []struct {
		notAfter *time.Time
		want     string
	}{
		{nil, ""},
		{at(100 * 24 * time.Hour), "ok"},
		{at(30 * 24 * time.Hour), "expiring"},
		{at(time.Minute), "expiring"},
		{at(0), "expired"},
		{at(-time.Hour), "expired"},
	}
	for _, tt := range tests {
		if got := certificateState(&store.Node{CertNotAfter: tt.notAfter}, now, 30*24*time.Hour); got != tt.want {
			t.Errorf("certificateState(%v) = %q, want %q", tt.notAfter, got, tt.want)
		}
	}
}
