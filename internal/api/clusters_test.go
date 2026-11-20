// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"encoding/json"
	"maps"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/provider/docker"
	"github.com/brokerium/brokerium/internal/store"
)

func TestCreateDockerClusterWithoutDockerClusters(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		detector *docker.Detector
		reason   string
	}{
		{"turned off", docker.NewDetector(docker.ModeOff, "unix:///nonexistent/docker.sock"), "BROKERIUM_DOCKER_PROVIDER=off"},
		{"not set up", nil, "not set up"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/v1/clusters",
				strings.NewReader(`{"name":"lab","provider":"docker","skipTest":true,"bootstrapServers":[]}`))
			(&Server{DockerClusters: tt.detector}).handleCreateCluster(rec, req)
			if rec.Code != http.StatusConflict || errorCode(t, rec) != "docker_unavailable" || !strings.Contains(rec.Body.String(), tt.reason) {
				t.Errorf("POST /clusters = %d %s, want 409 docker_unavailable with %q", rec.Code, rec.Body, tt.reason)
			}
		})
	}
}

func decodeSettings(t *testing.T, raw string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// savedSettings are the settings of a Docker cluster as its creation saves them, with a key of
// the operator.
const savedSettings = `{"kafkaVersion":"4.3.1","quorum":"static","kafkaClusterId":"x5dY0i3TQ2u3kE1nC7vB9w","portBase":21000,
	"racks":["rack-a","rack-b"],"replicationFactor":3,"minIsr":2,"brokerHeap":"-Xmx512m -Xms512m",
	"controllerHeap":"-Xmx384m -Xms384m","image":"","controllerBootstrapServers":["localhost:21001"],"owner":"team-a"}`

func TestUpdateClusterSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		set     map[string]any
		removed []string
		wantErr string
	}{
		{"one key", `{"settings":{"brokerHeap":"-Xmx1g -Xms1g"}}`, map[string]any{"brokerHeap": "-Xmx1g -Xms1g"}, nil, ""},
		{"no settings", `{"name":"lab-2"}`, nil, nil, ""},
		{"null removes keys", `{"settings":{"image":null,"owner":null,"racks":null}}`, nil, []string{"image", "owner", "racks"}, ""},
		{"new key", `{"settings":{"team":"payments"}}`, map[string]any{"team": "payments"}, nil, ""},
		{"replication factor with min ISR", `{"settings":{"replicationFactor":1,"minIsr":1}}`,
			map[string]any{"replicationFactor": 1.0, "minIsr": 1.0}, nil, ""},
		{"no published ports", `{"settings":{"portBase":0}}`, map[string]any{"portBase": 0.0}, nil, ""},
		{"Kafka version", `{"settings":{"kafkaVersion":"4.4.0","quorum":"dynamic"}}`,
			map[string]any{"kafkaVersion": "4.4.0", "quorum": "dynamic"}, nil, ""},
		{"JVM option", `{"settings":{"brokerHeap":"-Xmx1g -XX:+UseZGC"}}`, nil, nil, "settings.brokerHeap: heap option"},
		{"heap as a number", `{"settings":{"controllerHeap":512}}`, nil, nil, "settings.controllerHeap: must be a string"},
		{"short version", `{"settings":{"kafkaVersion":"4.4"}}`, nil, nil, "settings.kafkaVersion: must be a Kafka release"},
		{"unknown quorum", `{"settings":{"quorum":"raft"}}`, nil, nil, "settings.quorum: must be static or dynamic"},
		{"replication factor 0", `{"settings":{"replicationFactor":0}}`, nil, nil, "settings.replicationFactor: must be a whole number"},
		{"fractional min ISR", `{"settings":{"minIsr":1.5}}`, nil, nil, "settings.minIsr: must be a whole number"},
		{"count as text", `{"settings":{"replicationFactor":"3"}}`, nil, nil, "settings.replicationFactor: must be a whole number"},
		{"min ISR above the saved replication factor", `{"settings":{"minIsr":4}}`, nil, nil,
			"settings.minIsr: 4 is more than the replication factor 3"},
		{"replication factor below the saved min ISR", `{"settings":{"replicationFactor":1}}`, nil, nil,
			"settings.minIsr: 2 is more than the replication factor 1"},
		{"rack with a space", `{"settings":{"racks":["rack-a","rack b"]}}`, nil, nil, "settings.racks[1]: rack"},
		{"empty rack", `{"settings":{"racks":[""]}}`, nil, nil, "settings.racks[0]: must be a rack name"},
		{"racks as text", `{"settings":{"racks":"rack-a"}}`, nil, nil, "settings.racks: must be a list"},
		{"port base beyond the ports", `{"settings":{"portBase":65000}}`, nil, nil, "settings.portBase: must be 0 or from 1024 to 64536"},
		{"privileged port base", `{"settings":{"portBase":80}}`, nil, nil, "settings.portBase"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var req ClusterRequest
			if err := json.Unmarshal([]byte(tt.body), &req); err != nil {
				t.Fatal(err)
			}
			c := &store.Cluster{Name: "lab", Provider: store.ProviderDocker, Flavor: store.FlavorApache,
				Security: store.Security{Protocol: "PLAINTEXT"}, Settings: decodeSettings(t, savedSettings)}
			err := req.apply(c)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("apply = %v, want %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := decodeSettings(t, savedSettings)
			maps.Copy(want, tt.set)
			for _, key := range tt.removed {
				delete(want, key)
			}
			if !reflect.DeepEqual(c.Settings, want) {
				t.Errorf("settings %v, want %v", c.Settings, want)
			}
		})
	}
}

func TestCreateClusterSettings(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		body    string
		want    map[string]any
		wantErr string
	}{
		{"as given", `{"name":"lab","provider":"docker","settings":{"portBase":22000,"owner":"team-b","racks":null}}`,
			map[string]any{"portBase": 22000.0, "owner": "team-b"}, ""},
		{"none", `{"name":"lab","provider":"docker"}`, nil, ""},
		{"invalid", `{"name":"lab","provider":"docker","settings":{"minIsr":0}}`, nil, "settings.minIsr"},
	}
	for _, tt := range tests {
		var req ClusterRequest
		if err := json.Unmarshal([]byte(tt.body), &req); err != nil {
			t.Fatal(err)
		}
		c := &store.Cluster{}
		err := req.apply(c)
		if (tt.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("%s: apply = %v, want error %q", tt.name, err, tt.wantErr)
			continue
		}
		if err == nil && !reflect.DeepEqual(c.Settings, tt.want) {
			t.Errorf("%s: settings %v, want %v", tt.name, c.Settings, tt.want)
		}
	}
}
