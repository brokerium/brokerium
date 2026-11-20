// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
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

func TestUninstallCommand(t *testing.T) {
	t.Parallel()
	tests := []struct {
		api  NodesAPI
		want string
	}{
		{NodesAPI{PublicURL: "https://brokerium.example.com/"},
			"curl -fsSL https://brokerium.example.com/api/v1/agent/uninstall.sh | sudo sh -s --"},
		{NodesAPI{PublicURL: "https://b", Proxy: "http://proxy.corp:3128", NoProxy: "localhost"},
			"HTTPS_PROXY='http://proxy.corp:3128' NO_PROXY='localhost' curl -fsSL https://b/api/v1/agent/uninstall.sh | sudo sh -s --"},
	}
	for _, tt := range tests {
		if got := tt.api.uninstallCommand(); got != tt.want {
			t.Errorf("uninstallCommand =\n%s\nwant\n%s", got, tt.want)
		}
	}
}

// nodeStore keeps nodes like the store.
type nodeStore map[string]*store.Node

func (m nodeStore) GetNode(_ context.Context, id string) (*store.Node, error) {
	if n, ok := m[id]; ok {
		return n, nil
	}
	return nil, store.ErrNotFound
}

func (m nodeStore) DeleteNode(_ context.Context, id string) error {
	if _, ok := m[id]; !ok {
		return store.ErrNotFound
	}
	delete(m, id)
	return nil
}

// endedSessions records the nodes whose sessions were ended.
type endedSessions []string

func (s *endedSessions) NodeDeleted(id string) { *s = append(*s, id) }

func TestDeleteNode(t *testing.T) {
	t.Parallel()
	cluster := "c1"
	kafkaID := func(id int32) *int32 { return &id }
	inCluster := func(known bool, err error) memberCheck {
		return func(context.Context, string, int32) (bool, error) { return known, err }
	}
	tests := []struct {
		name      string
		node      store.Node
		inCluster memberCheck
		// refused is part of the refusal; empty when the node is deleted.
		refused string
	}{
		{"an unassigned node", store.Node{}, nil, ""},
		{"a node reserved for a cluster", store.Node{ClusterID: &cluster}, nil, ""},
		{"a broker", store.Node{ClusterID: &cluster, Role: "broker", KafkaNodeID: kafkaID(101)}, nil,
			"runs broker 101 of a cluster; decommission it first with drain-broker"},
		{"a controller whose agent is not connected", store.Node{ClusterID: &cluster, Role: "controller", KafkaNodeID: kafkaID(1)}, nil,
			"decommission it first with remove-controller"},
		{"a broker without a Kafka node id", store.Node{ClusterID: &cluster, Role: "broker"}, nil, "runs broker of a cluster"},
		{"a combined node without a Kafka node id", store.Node{ClusterID: &cluster, Role: "combined"}, nil,
			"its cluster cannot be asked about it"},
		{"a combined node in its cluster", store.Node{ClusterID: &cluster, Role: "combined", KafkaNodeID: kafkaID(1)},
			inCluster(true, nil), "Brokerium does not decommission combined nodes"},
		{"a combined node whose cluster cannot be checked", store.Node{ClusterID: &cluster, Role: "combined", KafkaNodeID: kafkaID(1)},
			inCluster(false, errors.New("connection refused")), "connection refused"},
		{"a combined node that left its cluster", store.Node{ClusterID: &cluster, Role: "combined", KafkaNodeID: kafkaID(1)},
			inCluster(false, nil), ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			n := tt.node
			n.ID, n.Name = "n1", "kafka-1"
			nodes, ended := nodeStore{"n1": &n, "n2": {ID: "n2", Name: "kafka-2"}}, &endedSessions{}
			got, err := deleteNode(context.Background(), nodes, ended, tt.inCluster, "n1")
			if got == nil || got.ID != "n1" {
				t.Fatalf("deleteNode returned node %v", got)
			}
			var inUse *nodeInUseError
			if tt.refused == "" {
				if err != nil {
					t.Fatalf("deleteNode = %v, want the node deleted", err)
				}
				if _, ok := nodes["n1"]; ok || !slices.Equal(*ended, []string{"n1"}) {
					t.Errorf("record kept %v, sessions ended %v; want the record deleted and the session of n1 ended", ok, *ended)
				}
				return
			}
			if !errors.As(err, &inUse) || !strings.Contains(err.Error(), tt.refused) {
				t.Fatalf("deleteNode = %v, want a refusal with %q", err, tt.refused)
			}
			if _, ok := nodes["n1"]; !ok || len(*ended) != 0 {
				t.Errorf("a refused node: record kept %v, sessions ended %v", ok, *ended)
			}
		})
	}
	if n, err := deleteNode(context.Background(), nodeStore{}, &endedSessions{}, nil, "n1"); n != nil || !errors.Is(err, store.ErrNotFound) {
		t.Errorf("deleting an unknown node: %v %v, want ErrNotFound", n, err)
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
		role     string
		update   nodeUpdate
		want     store.Node
		err      string
	}{
		{"name and rack", "kafka-1", "", nodeUpdate{Name: str("broker-a"), Rack: str("r2")},
			store.Node{Name: "broker-a", AdvertisedHost: "old.example.com", Rack: "r2"}, ""},
		{"advertised host", "kafka-1", "broker", nodeUpdate{AdvertisedHost: str("kafka-1.example.com")},
			store.Node{Name: "kafka-1", AdvertisedHost: "kafka-1.example.com", Rack: "r1"}, ""},
		{"back to the reported host name", "kafka-1", "broker", nodeUpdate{AdvertisedHost: str("")},
			store.Node{Name: "kafka-1", AdvertisedHost: "kafka-1", Rack: "r1"}, ""},
		{"a reported host name that cannot be advertised", "kafka_1", "", nodeUpdate{AdvertisedHost: str("")},
			store.Node{Name: "kafka-1", AdvertisedHost: "kafka_1", Rack: "r1"}, ""},
		{"a node that runs Kafka and reported a host name that cannot be advertised", "kafka_1", "controller",
			nodeUpdate{AdvertisedHost: str("")}, store.Node{}, "the node runs Kafka"},
		{"no rack", "kafka-1", "", nodeUpdate{Rack: str("")}, store.Node{Name: "kafka-1", AdvertisedHost: "old.example.com"}, ""},
	}
	for _, tt := range tests {
		n := &store.Node{Name: "kafka-1", Hostname: tt.hostname, Role: tt.role, AdvertisedHost: "old.example.com", Rack: "r1"}
		err := tt.update.apply(n)
		if (tt.err == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.err)) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.err)
			continue
		}
		if err != nil {
			continue
		}
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
