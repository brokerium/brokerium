// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/provider/docker"
	"github.com/brokerium/brokerium/internal/version"
)

func TestMiddleware(t *testing.T) {
	t.Parallel()
	s := &Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	h := s.Handler()
	tests := []struct {
		name    string
		method  string
		path    string
		csrf    bool
		status  int
		errCode string
	}{
		{"liveness", http.MethodGet, "/healthz", false, http.StatusOK, ""},
		{"POST without the CSRF header", http.MethodPost, "/api/v1/auth/logout", false, http.StatusForbidden, "csrf"},
		{"DELETE without the CSRF header", http.MethodDelete, "/api/v1/clusters/x", false, http.StatusForbidden, "csrf"},
		{"POST with the CSRF header", http.MethodPost, "/api/v1/auth/logout", true, http.StatusNoContent, ""},
		{"protected route without a session", http.MethodGet, "/api/v1/auth/me", false, http.StatusUnauthorized, "unauthenticated"},
		{"server info without a session", http.MethodGet, "/api/v1/server", false, http.StatusUnauthorized, "unauthenticated"},
		{"unknown API path", http.MethodGet, "/api/v1/nope", false, http.StatusNotFound, ""},
		{"dashboard fallback", http.MethodGet, "/clusters/abc", false, http.StatusOK, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.csrf {
				req.Header.Set(CSRFHeader, "1")
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body.String())
			}
			if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" ||
				rec.Header().Get("Content-Security-Policy") != contentSecurityPolicy {
				t.Error("security headers missing")
			}
			if tc.errCode != "" {
				var body struct {
					Error struct{ Code string } `json:"error"`
				}
				if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || body.Error.Code != tc.errCode {
					t.Errorf("error code = %q (%v), want %q", body.Error.Code, err, tc.errCode)
				}
			}
		})
	}
}

func TestServerInfo(t *testing.T) {
	prevVersion, prevCommit, prevDate := version.Version, version.Commit, version.Date
	version.Version, version.Commit, version.Date = "v0.3.1", "abc1234", "2026-10-21T10:00:00Z"
	t.Cleanup(func() { version.Version, version.Commit, version.Date = prevVersion, prevCommit, prevDate })
	tests := []struct {
		name     string
		edition  string
		detector *docker.Detector
		want     string
	}{
		{"Docker clusters on", edition.Community, docker.NewDetector(docker.ModeOn, "unix:///nonexistent/docker.sock"),
			`{"version":"v0.3.1","commit":"abc1234","date":"2026-10-21T10:00:00Z","edition":"community",` +
				`"providers":{"docker":{"available":true,"reason":""}}}`},
		{"Docker clusters off", edition.Enterprise, docker.NewDetector(docker.ModeOff, "unix:///nonexistent/docker.sock"),
			`{"version":"v0.3.1","commit":"abc1234","date":"2026-10-21T10:00:00Z","edition":"enterprise",` +
				`"providers":{"docker":{"available":false,"reason":"Docker clusters are turned off on this server (BROKERIUM_DOCKER_PROVIDER=off)"}}}`},
		{"no detector", edition.Community, nil,
			`{"version":"v0.3.1","commit":"abc1234","date":"2026-10-21T10:00:00Z","edition":"community",` +
				`"providers":{"docker":{"available":false,"reason":"Docker clusters are not set up on this server"}}}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useEdition(t, tt.edition, nil)
			rec := httptest.NewRecorder()
			(&Server{DockerClusters: tt.detector}).handleServer(rec, httptest.NewRequest(http.MethodGet, "/api/v1/server", nil))
			if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != tt.want {
				t.Errorf("GET /server = %d %s, want 200 %s", rec.Code, rec.Body, tt.want)
			}
		})
	}
}
