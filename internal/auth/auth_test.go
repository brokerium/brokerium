// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package auth

import (
	"strings"
	"testing"

	"github.com/bvaliev/brokerium/internal/store"
)

func TestPasswordHash(t *testing.T) {
	t.Parallel()
	if _, err := HashPassword("short"); err == nil {
		t.Error("a 5-character password was accepted")
	}
	hash, err := HashPassword("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Errorf("unexpected hash format %q", hash)
	}
	if !VerifyPassword(hash, "correct horse") {
		t.Error("the right password is rejected")
	}
	for _, tc := range []struct{ hash, password string }{
		{hash, "wrong horse"},
		{hash, ""},
		{"", "correct horse"},
		{"$bcrypt$whatever", "correct horse"},
		{"$argon2id$v=19$m=x$salt$key", "correct horse"},
	} {
		if VerifyPassword(tc.hash, tc.password) {
			t.Errorf("VerifyPassword(%q, %q) = true", tc.hash, tc.password)
		}
	}
	other, _ := HashPassword("correct horse")
	if other == hash {
		t.Error("two hashes of the same password are identical; the salt is not random")
	}
}

func TestTokens(t *testing.T) {
	t.Parallel()
	token, hash := NewToken()
	if len(token) < 40 || HashToken(token) != hash {
		t.Fatalf("token %q, hash %q", token, hash)
	}
	if again, _ := NewToken(); again == token {
		t.Error("tokens repeat")
	}
}

func TestRoleAllows(t *testing.T) {
	t.Parallel()
	tests := []struct {
		role, required string
		want           bool
	}{
		{store.RoleAdmin, store.RoleViewer, true},
		{store.RoleAdmin, store.RoleAdmin, true},
		{store.RoleOperator, store.RoleOperator, true},
		{store.RoleOperator, store.RoleAdmin, false},
		{store.RoleViewer, store.RoleOperator, false},
		{"", store.RoleViewer, false},
		{store.RoleAdmin, "", false},
		{"root", store.RoleViewer, false},
	}
	for _, tc := range tests {
		if got := RoleAllows(tc.role, tc.required); got != tc.want {
			t.Errorf("RoleAllows(%q, %q) = %v, want %v", tc.role, tc.required, got, tc.want)
		}
	}
	if ValidRole("root") || !ValidRole(store.RoleViewer) {
		t.Error("ValidRole is wrong")
	}
}
