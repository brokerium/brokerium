// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

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
	if got := brokerAddress(s, false, "kafka-1.example.net", 101); got != "kafka-1.example.net:9092" {
		t.Errorf("agent broker address %s", got)
	}
	if got := brokerAddress(s, true, "lab-b101", 101); got != "localhost:21101" {
		t.Errorf("docker broker address %s", got)
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
	if NodeName("delta", RoleController, 3) != "delta-c3" || NodeName("delta", RoleBroker, 101) != "delta-b101" {
		t.Error("NodeName")
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
