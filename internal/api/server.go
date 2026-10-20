// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package api implements the Brokerium REST API used by the dashboard and external clients.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/auth"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/provider/docker"
	"github.com/brokerium/brokerium/internal/secrets"
	"github.com/brokerium/brokerium/internal/store"
	"github.com/brokerium/brokerium/internal/version"
	"github.com/brokerium/brokerium/internal/webui"
)

// SessionCookie is the name of the session cookie.
const SessionCookie = "brokerium_session"

// CSRFHeader must be present on every state-changing request.
const CSRFHeader = "X-Brokerium-Request"

// Extension registers additional routes (operations, nodes, data plane) on the server.
type Extension interface {
	Register(s *Server)
}

// Server holds dependencies of the HTTP API.
type Server struct {
	Store         *store.Store
	Clusters      *cluster.Service
	Box           *secrets.Box
	Log           *slog.Logger
	SecureCookies bool
	SessionTTL    time.Duration
	// DockerClusters tells whether the server offers Docker clusters (ADR 0019).
	DockerClusters *docker.Detector

	mux    *http.ServeMux
	logins *loginLimiter
}

// contentSecurityPolicy allows only the dashboard's own scripts. Mantine sets inline styles.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; font-src 'self' data:; connect-src 'self'; object-src 'none'; base-uri 'self'; " +
	"form-action 'self'; frame-ancestors 'none'"

type ctxKey int

const userKey ctxKey = 1

// UserFrom returns the authenticated user of a request.
func UserFrom(ctx context.Context) *store.User {
	u, _ := ctx.Value(userKey).(*store.User)
	return u
}

// Handler builds the HTTP handler with all routes.
func (s *Server) Handler(exts ...Extension) http.Handler {
	if s.SessionTTL == 0 {
		s.SessionTTL = 12 * time.Hour
	}
	s.logins = newLoginLimiter()
	s.mux = http.NewServeMux()
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	s.mux.HandleFunc("GET /readyz", s.handleReady)
	s.mux.HandleFunc("GET /api/v1/version", s.handleVersion)
	s.Route("GET /api/v1/server", store.RoleViewer, s.handleServer)
	s.mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	s.mux.HandleFunc("POST /api/v1/auth/logout", s.handleLogout)
	s.Route("GET /api/v1/auth/me", store.RoleViewer, s.handleMe)
	s.registerClusters()
	s.registerDataPlane()
	s.registerUsers()
	s.registerLicense()
	for _, e := range exts {
		e.Register(s)
	}
	s.mux.Handle("/api/", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		WriteError(w, http.StatusNotFound, "not_found", "unknown API route")
	}))
	s.mux.Handle("/", webui.Handler())
	return s.middleware(s.mux)
}

// Route registers a handler that requires an authenticated user with at least the given role.
func (s *Server) Route(pattern, role string, h http.HandlerFunc) {
	s.mux.Handle(pattern, s.requireRole(role, h))
}

func (s *Server) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				s.Log.Error("panic in handler", "path", r.URL.Path, "panic", rec, "stack", string(debug.Stack()))
				WriteError(sw, http.StatusInternalServerError, "internal", "internal error")
			}
			if strings.HasPrefix(r.URL.Path, "/api/") {
				s.Log.Debug("request", "method", r.Method, "path", r.URL.Path, "status", sw.status, "duration", time.Since(start))
			}
		}()
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Referrer-Policy", "same-origin")
		w.Header().Set("Content-Security-Policy", contentSecurityPolicy)
		if r.Method != http.MethodGet && r.Method != http.MethodHead && strings.HasPrefix(r.URL.Path, "/api/") &&
			r.Header.Get(CSRFHeader) == "" {
			WriteError(sw, http.StatusForbidden, "csrf", "missing "+CSRFHeader+" header")
			return
		}
		next.ServeHTTP(sw, r)
	})
}

func (s *Server) requireRole(role string, h http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, err := s.authenticate(r)
		if err != nil {
			WriteError(w, http.StatusUnauthorized, "unauthenticated", "login required")
			return
		}
		if !auth.RoleAllows(user.Role, role) {
			WriteError(w, http.StatusForbidden, "forbidden", fmt.Sprintf("requires role %s", role))
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	})
}

func (s *Server) authenticate(r *http.Request) (*store.User, error) {
	token := sessionToken(r)
	if token == "" {
		return nil, errors.New("no session")
	}
	return s.Store.SessionUser(r.Context(), auth.HashToken(token))
}

// sessionToken returns the session token of a request: the bearer token, else the cookie.
func sessionToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		return strings.TrimPrefix(h, "Bearer ")
	}
	if c, err := r.Cookie(SessionCookie); err == nil {
		return c.Value
	}
	return ""
}

// Audit records a change made by the current user.
func (s *Server) Audit(r *http.Request, action, clusterID, target string, detail any, result error) {
	actor := "system"
	if u := UserFrom(r.Context()); u != nil {
		actor = u.Username
	}
	raw, _ := json.Marshal(detail)
	entry := store.AuditEntry{Actor: actor, Action: action, ClusterID: clusterID, Target: target, Detail: raw}
	if result != nil {
		entry.Result = "error: " + result.Error()
	}
	if err := s.Store.AddAudit(context.WithoutCancel(r.Context()), entry); err != nil {
		s.Log.Warn("writing audit entry", "error", err)
	}
}

func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.Pool.Ping(r.Context()); err != nil {
		s.Log.Warn("readiness check failed", "error", err)
		WriteError(w, http.StatusServiceUnavailable, "database", "database unavailable")
		return
	}
	_, _ = w.Write([]byte("ready"))
}

func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]string{"version": version.Version, "commit": version.Commit, "date": version.Date})
}

// ServerInfo describes the build and the edition of the server and the providers it offers.
type ServerInfo struct {
	Version   string          `json:"version"`
	Commit    string          `json:"commit"`
	Date      string          `json:"date"`
	Edition   string          `json:"edition"`
	Providers ServerProviders `json:"providers"`
}

// ServerProviders tells which providers can create clusters on this server.
type ServerProviders struct {
	Docker docker.Availability `json:"docker"`
}

func (s *Server) handleServer(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, ServerInfo{Version: version.Version, Commit: version.Commit, Date: version.Date,
		Edition: edition.Name, Providers: ServerProviders{Docker: s.DockerClusters.Check(r.Context())}})
}

// dockerUnavailable answers 409 docker_unavailable with the reason when the server does not
// offer Docker clusters.
func (s *Server) dockerUnavailable(w http.ResponseWriter, r *http.Request) bool {
	if err := s.DockerClusters.Check(r.Context()).Err(); err != nil {
		WriteError(w, http.StatusConflict, "docker_unavailable", err.Error())
		return true
	}
	return false
}

// WriteJSON writes v as JSON.
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// WriteError writes a JSON error.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	WriteJSON(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// WriteStoreError maps store errors to HTTP responses.
func WriteStoreError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", "not found")
	case errors.Is(err, store.ErrConflict):
		WriteError(w, http.StatusConflict, "conflict", "already exists or conflicts with a running operation")
	default:
		WriteError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

// Decode reads a JSON body of at most 4 MiB into v.
func Decode(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", "invalid JSON: "+err.Error())
		return false
	}
	return true
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
