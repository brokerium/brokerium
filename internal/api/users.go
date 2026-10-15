// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/bvaliev/brokerium/internal/auth"
	"github.com/bvaliev/brokerium/internal/store"
)

func (s *Server) registerUsers() {
	s.Route("GET /api/v1/users", store.RoleAdmin, s.handleListUsers)
	s.Route("POST /api/v1/users", store.RoleAdmin, s.handleCreateUser)
	s.Route("PATCH /api/v1/users/{id}", store.RoleAdmin, s.handleUpdateUser)
	s.Route("DELETE /api/v1/users/{id}", store.RoleAdmin, s.handleDeleteUser)
	s.Route("GET /api/v1/audit", store.RoleViewer, s.handleAudit)
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !Decode(w, r, &req) {
		return
	}
	u, err := s.Store.GetUserByUsername(r.Context(), req.Username)
	if err != nil || u.Disabled || !auth.VerifyPassword(u.PasswordHash, req.Password) {
		time.Sleep(300 * time.Millisecond)
		WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid username or password")
		return
	}
	token, hash := auth.NewToken()
	expires := time.Now().Add(s.SessionTTL)
	if err := s.Store.CreateSession(r.Context(), hash, u.ID, expires); err != nil {
		WriteStoreError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: token, Path: "/", HttpOnly: true,
		Secure: s.SecureCookies, SameSite: http.SameSiteLaxMode, Expires: expires})
	_ = s.Store.DeleteExpiredSessions(r.Context())
	WriteJSON(w, http.StatusOK, map[string]any{"user": u, "token": token, "expiresAt": expires})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookie); err == nil {
		_ = s.Store.DeleteSession(r.Context(), auth.HashToken(c.Value))
	}
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		_ = s.Store.DeleteSession(r.Context(), auth.HashToken(strings.TrimPrefix(h, "Bearer ")))
	}
	http.SetCookie(w, &http.Cookie{Name: SessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]any{"user": UserFrom(r.Context())})
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.Store.ListUsers(r.Context())
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	if users == nil {
		users = []*store.User{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"users": users})
}

type userRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	Disabled *bool  `json:"disabled"`
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req userRequest
	if !Decode(w, r, &req) {
		return
	}
	if req.Username == "" || !auth.ValidRole(req.Role) {
		WriteError(w, http.StatusBadRequest, "bad_request", "username and a valid role (admin, operator, viewer) are required")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
		return
	}
	u := &store.User{Username: req.Username, PasswordHash: hash, Role: req.Role}
	err = s.Store.CreateUser(r.Context(), u)
	s.Audit(r, "user.create", "", u.Username, map[string]string{"role": u.Role}, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	WriteJSON(w, http.StatusCreated, map[string]any{"user": u})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.Store.GetUser(r.Context(), r.PathValue("id"))
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	var req userRequest
	if !Decode(w, r, &req) {
		return
	}
	if req.Role != "" {
		if !auth.ValidRole(req.Role) {
			WriteError(w, http.StatusBadRequest, "bad_request", "invalid role")
			return
		}
		u.Role = req.Role
	}
	if req.Disabled != nil {
		u.Disabled = *req.Disabled
	}
	if req.Password != "" {
		if u.PasswordHash, err = auth.HashPassword(req.Password); err != nil {
			WriteError(w, http.StatusBadRequest, "bad_request", err.Error())
			return
		}
	}
	if me := UserFrom(r.Context()); me.ID == u.ID && (u.Role != store.RoleAdmin || u.Disabled) {
		WriteError(w, http.StatusBadRequest, "bad_request", "you cannot demote or disable yourself")
		return
	}
	err = s.Store.UpdateUser(r.Context(), u)
	s.Audit(r, "user.update", "", u.Username, map[string]any{"role": u.Role, "disabled": u.Disabled, "passwordChanged": req.Password != ""}, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"user": u})
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if UserFrom(r.Context()).ID == id {
		WriteError(w, http.StatusBadRequest, "bad_request", "you cannot delete yourself")
		return
	}
	u, err := s.Store.GetUser(r.Context(), id)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	err = s.Store.DeleteUser(r.Context(), id)
	s.Audit(r, "user.delete", "", u.Username, nil, err)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	entries, err := s.Store.ListAudit(r.Context(), r.URL.Query().Get("clusterId"), limit)
	if err != nil {
		WriteStoreError(w, err)
		return
	}
	if entries == nil {
		entries = []*store.AuditEntry{}
	}
	WriteJSON(w, http.StatusOK, map[string]any{"entries": entries})
}

// EnsureAdmin creates the initial administrator when no users exist. It returns the generated
// password when none was configured.
func EnsureAdmin(ctx context.Context, st *store.Store, username, password string) (string, error) {
	n, err := st.CountUsers(ctx)
	if err != nil || n > 0 {
		return "", err
	}
	generated := ""
	if password == "" {
		token, _ := auth.NewToken()
		password = token[:20]
		generated = password
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return "", err
	}
	err = st.CreateUser(ctx, &store.User{Username: username, PasswordHash: hash, Role: store.RoleAdmin})
	if errors.Is(err, store.ErrConflict) {
		return "", nil
	}
	return generated, err
}
