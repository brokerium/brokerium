// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
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
			if rec.Header().Get("X-Frame-Options") != "DENY" || rec.Header().Get("X-Content-Type-Options") != "nosniff" {
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
