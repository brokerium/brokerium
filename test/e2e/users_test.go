// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"net/http"
	"testing"
)

// TestUsersRolesAudit checks that roles limit what users can do and that changes are audited.
func TestUsersRolesAudit(t *testing.T) {
	admin := newClient(t)
	beta := admin.clusterID("beta")
	created := map[string]string{}
	for _, role := range []string{"viewer", "operator"} {
		name := uniqueName("e2e-" + role + "-")
		var u struct {
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		}
		admin.must(http.StatusCreated, "POST", "/api/v1/users", map[string]any{"username": name, "password": "e2e-password-1", "role": role}, &u)
		created[role] = name
		defer admin.must(http.StatusNoContent, "DELETE", "/api/v1/users/"+u.User.ID, nil, nil)
	}
	login := func(username string) *client {
		c := &client{t: t, base: admin.base}
		var res struct {
			Token string `json:"token"`
		}
		c.must(http.StatusOK, "POST", "/api/v1/auth/login", map[string]string{"username": username, "password": "e2e-password-1"}, &res)
		c.token = res.Token
		return c
	}
	viewer, operator := login(created["viewer"]), login(created["operator"])

	tests := []struct {
		name   string
		c      *client
		method string
		path   string
		body   any
		status int
	}{
		{"viewer reads clusters", viewer, "GET", "/api/v1/clusters", nil, http.StatusOK},
		{"viewer reads the audit log", viewer, "GET", "/api/v1/audit?limit=5", nil, http.StatusOK},
		{"viewer cannot plan operations", viewer, "POST", "/api/v1/clusters/" + beta + "/operations", map[string]any{"type": "elect-leaders"}, http.StatusForbidden},
		{"viewer cannot create topics", viewer, "POST", "/api/v1/clusters/" + beta + "/topics", map[string]any{"name": "nope"}, http.StatusForbidden},
		{"viewer cannot list users", viewer, "GET", "/api/v1/users", nil, http.StatusForbidden},
		{"operator plans operations", operator, "POST", "/api/v1/clusters/" + beta + "/operations", map[string]any{"type": "elect-leaders"}, http.StatusCreated},
		{"operator cannot manage users", operator, "POST", "/api/v1/users", map[string]any{"username": "nope", "password": "e2e-password-1", "role": "admin"}, http.StatusForbidden},
		{"operator cannot delete clusters", operator, "DELETE", "/api/v1/clusters/" + beta, nil, http.StatusForbidden},
		{"wrong password", &client{t: t, base: admin.base}, "POST", "/api/v1/auth/login", map[string]string{"username": created["viewer"], "password": "wrong-password"}, http.StatusUnauthorized},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.c.do(tc.method, tc.path, tc.body, nil); got != tc.status {
				t.Errorf("%s %s: status %d, want %d", tc.method, tc.path, got, tc.status)
			}
		})
	}

	// The operator's plan is audited under its name; discard it again.
	var audit struct {
		Entries []struct {
			Actor  string `json:"actor"`
			Action string `json:"action"`
		} `json:"entries"`
	}
	admin.must(http.StatusOK, "GET", "/api/v1/audit?limit=20", nil, &audit)
	found := false
	for _, e := range audit.Entries {
		found = found || (e.Actor == created["operator"] && e.Action == "operation.plan")
	}
	if !found {
		t.Errorf("no operation.plan audit entry by %s in %+v", created["operator"], audit.Entries)
	}
	var ops struct {
		Operations []struct {
			ID        string `json:"id"`
			Status    string `json:"status"`
			CreatedBy string `json:"createdBy"`
		} `json:"operations"`
	}
	admin.must(http.StatusOK, "GET", "/api/v1/clusters/"+beta+"/operations?limit=5", nil, &ops)
	for _, op := range ops.Operations {
		if op.CreatedBy == created["operator"] && op.Status == "planned" {
			admin.must(http.StatusNoContent, "DELETE", "/api/v1/operations/"+op.ID, nil, nil)
		}
	}

	// Logging out ends the session.
	viewer.must(http.StatusNoContent, "POST", "/api/v1/auth/logout", nil, nil)
	if got := viewer.do("GET", "/api/v1/auth/me", nil, nil); got != http.StatusUnauthorized {
		t.Errorf("session still valid after logout: status %d", got)
	}
}
