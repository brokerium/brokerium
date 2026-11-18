// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/provider/docker"
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
