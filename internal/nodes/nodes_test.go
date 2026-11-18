// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/provider/docker"
	"github.com/brokerium/brokerium/internal/store"
)

func TestRenderController(t *testing.T) {
	t.Parallel()
	s := settingsOf(&store.Cluster{Settings: map[string]any{"portBase": 21000}})
	p := renderProperties(s, true, RoleController, "delta-c1", 1, "", "dynamic", quorumBootstrap([]string{"delta-c1", "delta-c2"}))
	want := map[string]string{
		"node.id":                             "1",
		"process.roles":                       "controller",
		"listeners":                           "CONTROLLER://delta-c1:9093",
		"controller.quorum.bootstrap.servers": "delta-c1:9093,delta-c2:9093",
		"default.replication.factor":          "3",
		"min.insync.replicas":                 "2",
	}
	for k, v := range want {
		if p[k] != v {
			t.Errorf("%s = %q, want %q", k, p[k], v)
		}
	}
	if _, ok := p["controller.quorum.voters"]; ok {
		t.Error("a dynamic quorum must not set controller.quorum.voters")
	}
}

func TestRenderBroker(t *testing.T) {
	t.Parallel()
	s := settingsOf(&store.Cluster{Settings: map[string]any{"portBase": 21000, "replicationFactor": 1}})
	p := renderProperties(s, true, RoleBroker, "delta-b101", 101, "rack-a", "static", "1@delta-c1:9093")
	want := map[string]string{
		"process.roles":                    "broker",
		"advertised.listeners":             "INTERNAL://delta-b101:9092,EXTERNAL://localhost:21101",
		"broker.rack":                      "rack-a",
		"controller.quorum.voters":         "1@delta-c1:9093",
		"offsets.topic.replication.factor": "1",
		"min.insync.replicas":              "1",
		"inter.broker.listener.name":       "INTERNAL",
	}
	for k, v := range want {
		if p[k] != v {
			t.Errorf("%s = %q, want %q", k, p[k], v)
		}
	}
}

func TestRenderAgentNodes(t *testing.T) {
	t.Parallel()
	s := settingsOf(&store.Cluster{Settings: map[string]any{"portBase": 21000}})
	broker := renderProperties(s, false, RoleBroker, "kafka-1.example.net", 101, "", "dynamic", "kafka-c1.example.net:9093")
	if broker["listeners"] != "PLAINTEXT://0.0.0.0:9092" || broker["advertised.listeners"] != "PLAINTEXT://kafka-1.example.net:9092" ||
		broker["inter.broker.listener.name"] != "PLAINTEXT" {
		t.Errorf("agent broker listeners: %v / %v", broker["listeners"], broker["advertised.listeners"])
	}
	ctrl := renderProperties(s, false, RoleController, "kafka-c1.example.net", 1, "", "dynamic", "kafka-c1.example.net:9093")
	if ctrl["listeners"] != "CONTROLLER://0.0.0.0:9093" || ctrl["advertised.listeners"] != "CONTROLLER://kafka-c1.example.net:9093" {
		t.Errorf("agent controller listeners: %v / %v", ctrl["listeners"], ctrl["advertised.listeners"])
	}
}

func TestAddresses(t *testing.T) {
	t.Parallel()
	s := settingsOf(&store.Cluster{Settings: map[string]any{"portBase": 21000}})
	tests := []struct {
		name                       string
		docker, inContainer        bool
		role, node                 string
		id                         int32
		wantBroker, wantController string
	}{
		{name: "agent node", role: RoleBroker, node: "kafka-1.example.net", id: 101,
			wantBroker: "kafka-1.example.net:9092", wantController: "kafka-1.example.net:9093"},
		{name: "agent node, server in a container", inContainer: true, role: RoleBroker, node: "kafka-1.example.net", id: 101,
			wantBroker: "kafka-1.example.net:9092", wantController: "kafka-1.example.net:9093"},
		{name: "agent combined node", role: RoleCombined, node: "kafka-1.example.net", id: 1,
			wantBroker: "kafka-1.example.net:9092", wantController: "kafka-1.example.net:9093"},
		{name: "Docker node, server on the Docker host", docker: true, role: RoleBroker, node: "lab-b101", id: 101,
			wantBroker: "localhost:21101", wantController: "localhost:21101"},
		{name: "Docker controller, server on the Docker host", docker: true, role: RoleController, node: "lab-c2", id: 2,
			wantBroker: "localhost:21002", wantController: "localhost:21002"},
		{name: "Docker combined node, server on the Docker host", docker: true, role: RoleCombined, node: "lab-cb3", id: 3,
			wantBroker: "localhost:21003", wantController: "localhost:21903"},
		{name: "Docker node, server in a container on the lab network", docker: true, inContainer: true, role: RoleBroker, node: "lab-b101", id: 101,
			wantBroker: "lab-b101:9092", wantController: "lab-b101:9093"},
		{name: "Docker combined node, server in a container on the lab network", docker: true, inContainer: true, role: RoleCombined,
			node: "lab-cb3", id: 3, wantBroker: "lab-cb3:9092", wantController: "lab-cb3:9093"},
	}
	for _, tt := range tests {
		m := &Manager{Cfg: Config{ServerInContainer: tt.inContainer}}
		if got := m.brokerAddress(s, tt.docker, tt.node, tt.id); got != tt.wantBroker {
			t.Errorf("%s: broker address %s, want %s", tt.name, got, tt.wantBroker)
		}
		if got := m.controllerAddress(s, tt.docker, tt.role, tt.node, tt.id); got != tt.wantController {
			t.Errorf("%s: controller address %s, want %s", tt.name, got, tt.wantController)
		}
	}
}

func TestRenderCombined(t *testing.T) {
	t.Parallel()
	s := settingsOf(&store.Cluster{Settings: map[string]any{"portBase": 21000}})
	tests := []struct {
		name   string
		docker bool
		host   string
		quorum string
		want   map[string]string
	}{
		{"agent node", false, "kafka-1.example.net", "dynamic", map[string]string{
			"listeners":                           "PLAINTEXT://0.0.0.0:9092,CONTROLLER://0.0.0.0:9093",
			"advertised.listeners":                "PLAINTEXT://kafka-1.example.net:9092,CONTROLLER://kafka-1.example.net:9093",
			"listener.security.protocol.map":      "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT",
			"inter.broker.listener.name":          "PLAINTEXT",
			"controller.quorum.bootstrap.servers": "kafka-1.example.net:9093,kafka-2.example.net:9093",
		}},
		{"agent node with an IPv6 address", false, "2001:db8::1", "dynamic", map[string]string{
			"listeners":            "PLAINTEXT://0.0.0.0:9092,CONTROLLER://0.0.0.0:9093",
			"advertised.listeners": "PLAINTEXT://[2001:db8::1]:9092,CONTROLLER://[2001:db8::1]:9093",
		}},
		{"Docker node", true, "lab-cb3", "dynamic", map[string]string{
			"listeners":                      "INTERNAL://0.0.0.0:9092,EXTERNAL://0.0.0.0:21003,CONTROLLER://lab-cb3:9093",
			"advertised.listeners":           "INTERNAL://lab-cb3:9092,EXTERNAL://localhost:21003,CONTROLLER://lab-cb3:9093",
			"listener.security.protocol.map": "CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT",
			"inter.broker.listener.name":     "INTERNAL",
		}},
		{"static quorum", false, "kafka-1.example.net", "static", map[string]string{
			"controller.quorum.voters": "3@kafka-1.example.net:9093,4@kafka-2.example.net:9093",
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			quorum := "kafka-1.example.net:9093,kafka-2.example.net:9093"
			if tt.quorum == "static" {
				quorum = "3@kafka-1.example.net:9093,4@kafka-2.example.net:9093"
			}
			p := renderProperties(s, tt.docker, RoleCombined, tt.host, 3, "rack-a", tt.quorum, quorum)
			common := map[string]string{
				"node.id":                          "3",
				"process.roles":                    "broker,controller",
				"controller.listener.names":        "CONTROLLER",
				"broker.rack":                      "rack-a",
				"offsets.topic.replication.factor": "3",
				"min.insync.replicas":              "2",
			}
			for _, want := range []map[string]string{common, tt.want} {
				for k, v := range want {
					if p[k] != v {
						t.Errorf("%s = %q, want %q", k, p[k], v)
					}
				}
			}
			if _, ok := p["controller.quorum.voters"]; ok != (tt.quorum == "static") {
				t.Errorf("controller.quorum.voters set: %v, want it only for a static quorum", ok)
			}
		})
	}
}

func TestContainerPorts(t *testing.T) {
	t.Parallel()
	s := settingsOf(&store.Cluster{Settings: map[string]any{"portBase": 21000}})
	tests := []struct {
		role string
		id   int32
		want map[int]int
	}{
		{RoleController, 2, map[int]int{9093: 21002}},
		{RoleBroker, 101, map[int]int{21101: 21101}},
		{RoleCombined, 3, map[int]int{21003: 21003, 9093: 21903}},
	}
	for _, tt := range tests {
		if got := containerPorts(s, tt.role, tt.id); !maps.Equal(got, tt.want) {
			t.Errorf("%s %d publishes %v, want %v", tt.role, tt.id, got, tt.want)
		}
	}
}

func TestClassify(t *testing.T) {
	t.Parallel()
	mn := classify([]*store.Node{
		host("b", "b", runs("c1", RoleBroker, 101)), host("x", "x", runs("c1", RoleCombined, 2)),
		host("c", "c", runs("c1", RoleController, 1)), host("j", "j", joined("c1")),
	})
	ids := func(nodes []*store.Node) []int32 {
		var out []int32
		for _, n := range nodes {
			out = append(out, *n.KafkaNodeID)
		}
		return out
	}
	if got := ids(mn.controllers); !slices.Equal(got, []int32{1, 2}) {
		t.Errorf("controllers %v, want the controller and the combined node", got)
	}
	if got := ids(mn.brokers); !slices.Equal(got, []int32{2, 101}) {
		t.Errorf("brokers %v, want the combined node and the broker", got)
	}
	if got := ids(dedicatedBrokers(mn)); !slices.Equal(got, []int32{101}) {
		t.Errorf("dedicated brokers %v", got)
	}
	if len(mn.nodes) != 4 {
		t.Errorf("%d nodes, want all 4", len(mn.nodes))
	}
}

func TestCheckPlan(t *testing.T) {
	t.Parallel()
	on := docker.NewDetector(docker.ModeOn, "unix:///nonexistent/docker.sock")
	off := docker.NewDetector(docker.ModeOff, "unix:///nonexistent/docker.sock")
	restart := []string{ops.KindHealthGate, KindRestart, ops.KindElectPreferred}
	rebalance := []string{ops.KindHealthGate, ops.KindThrottle, ops.KindReassign, ops.KindWaitReassign, ops.KindClearThrottle}
	decommission := []string{ops.KindCordon, ops.KindVerifyEmpty, KindRemoveNode, ops.KindUnregisterBroker}
	tests := []struct {
		name     string
		provider string
		detector *docker.Detector
		kinds    []string
		wantErr  bool
	}{
		{"Docker cluster, Docker clusters available", store.ProviderDocker, on, restart, false},
		{"Docker cluster, rolling restart", store.ProviderDocker, off, restart, true},
		{"Docker cluster, create nodes", store.ProviderDocker, off, []string{KindSetSettings, KindProvision, KindWaitAgent}, true},
		{"Docker cluster, decommission a broker", store.ProviderDocker, off, decommission, true},
		{"Docker cluster, rebalance", store.ProviderDocker, off, rebalance, false},
		{"Docker cluster without a detector", store.ProviderDocker, nil, restart, true},
		{"agent cluster", store.ProviderAgent, off, restart, false},
		{"external cluster", store.ProviderExternal, nil, decommission, false},
	}
	for _, tt := range tests {
		m := &Manager{DockerClusters: tt.detector}
		err := m.CheckPlan(t.Context(), &store.Cluster{Name: "lab", Provider: tt.provider}, tt.kinds)
		if (err != nil) != tt.wantErr || (err != nil && !errors.Is(err, docker.ErrUnavailable)) {
			t.Errorf("%s: CheckPlan = %v, want an error: %v", tt.name, err, tt.wantErr)
		}
	}
	err := (&Manager{DockerClusters: off}).CheckPlan(t.Context(), &store.Cluster{Provider: store.ProviderDocker}, restart)
	if err == nil || !strings.Contains(err.Error(), "BROKERIUM_DOCKER_PROVIDER=off") {
		t.Errorf("CheckPlan = %v, want the reason", err)
	}
}

func TestRenderAdvertisedHosts(t *testing.T) {
	t.Parallel()
	s := settingsOf(&store.Cluster{})
	tests := []struct {
		host, listener, controller, quorum, address string
	}{
		{"kafka-17.example.com", "PLAINTEXT://kafka-17.example.com:9092", "CONTROLLER://kafka-17.example.com:9093",
			"kafka-17.example.com:9093", "kafka-17.example.com:9092"},
		{"192.0.2.10", "PLAINTEXT://192.0.2.10:9092", "CONTROLLER://192.0.2.10:9093", "192.0.2.10:9093", "192.0.2.10:9092"},
		{"2001:db8::10", "PLAINTEXT://[2001:db8::10]:9092", "CONTROLLER://[2001:db8::10]:9093", "[2001:db8::10]:9093", "[2001:db8::10]:9092"},
	}
	m := &Manager{}
	for _, tt := range tests {
		broker := renderProperties(s, false, RoleBroker, tt.host, 101, "rack-a", "dynamic", quorumBootstrap([]string{tt.host}))
		ctrl := renderProperties(s, false, RoleController, tt.host, 1, "", "dynamic", quorumBootstrap([]string{tt.host}))
		switch {
		case broker["advertised.listeners"] != tt.listener:
			t.Errorf("%s: broker advertises %q, want %q", tt.host, broker["advertised.listeners"], tt.listener)
		case ctrl["advertised.listeners"] != tt.controller:
			t.Errorf("%s: controller advertises %q, want %q", tt.host, ctrl["advertised.listeners"], tt.controller)
		case broker["controller.quorum.bootstrap.servers"] != tt.quorum:
			t.Errorf("%s: quorum %q, want %q", tt.host, broker["controller.quorum.bootstrap.servers"], tt.quorum)
		case m.brokerAddress(s, false, tt.host, 101) != tt.address:
			t.Errorf("%s: broker address %q, want %q", tt.host, m.brokerAddress(s, false, tt.host, 101), tt.address)
		case broker["listeners"] != "PLAINTEXT://0.0.0.0:9092" || broker["broker.rack"] != "rack-a":
			t.Errorf("%s: listeners %q, rack %q", tt.host, broker["listeners"], broker["broker.rack"])
		}
	}
}

func TestHostOf(t *testing.T) {
	t.Parallel()
	n := &store.Node{Name: "lab-b101", AdvertisedHost: "kafka-1.example.com"}
	if hostOf(true, n) != "lab-b101" || hostOf(false, n) != "kafka-1.example.com" {
		t.Errorf("hostOf: docker %q, agent %q", hostOf(true, n), hostOf(false, n))
	}
	if got := hostOf(false, &store.Node{Name: "prod-b101"}); got != "prod-b101" {
		t.Errorf("a node without an advertised host advertises %q, want its name", got)
	}
}

func TestSettingsDefaults(t *testing.T) {
	t.Parallel()
	s := settingsOf(&store.Cluster{KafkaClusterID: "abc"})
	if s.KafkaVersion != "4.3.1" || s.Quorum != "dynamic" || s.ReplicationFactor != 3 || s.MinISR != 2 || s.KafkaClusterID != "abc" {
		t.Errorf("defaults %+v", s)
	}
	round := settingsOf(&store.Cluster{Settings: s.toMap()})
	if round.KafkaVersion != s.KafkaVersion || round.MinISR != s.MinISR {
		t.Errorf("settings do not survive toMap: %+v", round)
	}
	if NodeName("delta", RoleController, 3) != "delta-c3" || NodeName("delta", RoleBroker, 101) != "delta-b101" ||
		NodeName("delta", RoleCombined, 4) != "delta-cb4" {
		t.Error("container names of Docker nodes")
	}
}

func TestInstallCommand(t *testing.T) {
	t.Parallel()
	for _, mirror := range []string{"", "https://artifacts.example.com/kafka"} {
		m := &Manager{Cfg: Config{KafkaMirror: mirror}}
		install := m.installCommand("4.4.0").GetInstall()
		if install.GetVersion() != "4.4.0" || install.GetMirrorUrl() != mirror {
			t.Errorf("with mirror %q the install command is %v", mirror, install)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		a, b string
		want int
	}{
		{"4.3.1", "4.4.0", -1},
		{"4.4.0", "4.3.1", 1},
		{"4.3.1", "4.3.1", 0},
		{"4.10.0", "4.9.9", 1},
		{"3.9.0", "4.0.0", -1},
	}
	for _, tc := range tests {
		if got := compareVersions(tc.a, tc.b); got != tc.want {
			t.Errorf("compareVersions(%s, %s) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestIssuesOf(t *testing.T) {
	t.Parallel()
	for version, affected := range map[string]bool{
		"4.0.2": false, "4.1.0": true, "4.2.1": true, "4.3.0": true, "4.3.1": true, "4.3.2": false, "4.4.0": false,
	} {
		got := issuesOf(version)
		if (len(got) > 0) != affected {
			t.Errorf("issuesOf(%s) = %v, want affected=%v", version, got, affected)
		}
		for _, g := range got {
			if !strings.HasPrefix(g, "KAFKA-20979") {
				t.Errorf("unexpected issue %q", g)
			}
		}
	}
}

func TestJavaMajor(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]int{"21.0.8": 21, "17": 17, "25-ea": 25, "1.8.0_402": 8, "": 0, "garbage": 0} {
		raw, _ := json.Marshal(map[string]string{"javaVersion": version})
		if got := javaMajor(&store.Node{Status: raw}); got != want {
			t.Errorf("javaMajor(%q) = %d, want %d", version, got, want)
		}
	}
	if javaMajor(&store.Node{}) != 0 {
		t.Error("a node without status must report 0")
	}
}

func TestRandomUUID(t *testing.T) {
	t.Parallel()
	// One random value in 32 starts with "-", so 10000 values cover that case.
	for range 10000 {
		id := RandomUUID()
		if strings.HasPrefix(id, "-") {
			t.Fatalf("RandomUUID() = %q starts with a dash", id)
		}
		if b, err := base64.RawURLEncoding.DecodeString(id); err != nil || len(b) != 16 {
			t.Fatalf("RandomUUID() = %q does not decode to 16 bytes: %v", id, err)
		}
	}
}
