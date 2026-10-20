// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/edition"
)

type fakeLicense struct {
	info    *edition.LicenseInfo
	allowed map[string]bool
}

func (f fakeLicense) Allows(feature string) bool { return f.allowed[feature] }
func (f fakeLicense) Info() *edition.LicenseInfo { return f.info }
func (fakeLicense) Set(context.Context, string, string) (*edition.LicenseInfo, error) {
	return nil, errors.New("not implemented")
}
func (fakeLicense) Clear(context.Context, string) error { return errors.New("not implemented") }

// useEdition sets the process-wide edition and license manager for one test; tests that call it
// do not run in parallel.
func useEdition(t *testing.T, name string, m edition.Manager) {
	t.Helper()
	prevName, prevManager := edition.Name, edition.CurrentManager()
	edition.Name = name
	edition.SetManager(m)
	t.Cleanup(func() {
		edition.Name = prevName
		edition.SetManager(prevManager)
	})
}

func getLicense(t *testing.T, s *Server) (LicenseView, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	s.handleLicense(rec, httptest.NewRequest(http.MethodGet, "/api/v1/license", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /license: status %d", rec.Code)
	}
	var v LicenseView
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatal(err)
	}
	return v, rec.Body.String()
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct{ Code string } `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
	return body.Error.Code
}

func TestLicenseRoutesNeedASession(t *testing.T) {
	t.Parallel()
	h := (&Server{Log: slog.New(slog.NewTextHandler(io.Discard, nil))}).Handler()
	for _, method := range []string{http.MethodGet, http.MethodPut, http.MethodDelete} {
		req := httptest.NewRequest(method, "/api/v1/license", strings.NewReader(`{"key":"x"}`))
		req.Header.Set(CSRFHeader, "1")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s /api/v1/license without a session: status %d, want 401", method, rec.Code)
		}
	}
}

func TestLicenseCommunity(t *testing.T) {
	useEdition(t, edition.Community, nil)
	s := &Server{}
	v, raw := getLicense(t, s)
	if v.Edition != edition.Community || v.License != nil || len(v.Features) != len(edition.Catalog) {
		t.Fatalf("GET /license = %s", raw)
	}
	for _, f := range v.Features {
		if f.Licensed || f.Title == "" {
			t.Errorf("feature %+v", f)
		}
	}
	if !strings.Contains(raw, `"license":null`) {
		t.Errorf("license is not null: %s", raw)
	}
	for method, handle := range map[string]http.HandlerFunc{http.MethodPut: s.handleSetLicense, http.MethodDelete: s.handleDeleteLicense} {
		rec := httptest.NewRecorder()
		handle(rec, httptest.NewRequest(method, "/api/v1/license", strings.NewReader(`{"key":"brk-lic-1.x.y"}`)))
		if rec.Code != http.StatusConflict || errorCode(t, rec) != "edition_required" {
			t.Errorf("%s /license: status %d %s, want 409 edition_required", method, rec.Code, rec.Body)
		}
	}
}

func TestLicenseEnterprise(t *testing.T) {
	expires := time.Date(2027, 10, 20, 0, 0, 0, 0, time.UTC)
	useEdition(t, edition.Enterprise, fakeLicense{
		allowed: map[string]bool{edition.MessageTools: true},
		info: &edition.LicenseInfo{ID: "lic_1", Customer: "Example GmbH", Features: []string{edition.MessageTools},
			ExpiresAt: expires, GraceEndsAt: expires.AddDate(0, 0, 14), State: edition.StateValid, Source: edition.SourceDatabase},
	})
	v, raw := getLicense(t, &Server{})
	if v.Edition != edition.Enterprise || v.License == nil || v.License.Customer != "Example GmbH" ||
		!v.License.ExpiresAt.Equal(expires) {
		t.Fatalf("GET /license = %s", raw)
	}
	for _, f := range v.Features {
		if f.Licensed != (f.ID == edition.MessageTools) {
			t.Errorf("feature %+v", f)
		}
	}
	for _, want := range []string{`"id":"message-tools"`, `"licensed":true`, `"expiresAt":"2027-10-20T00:00:00Z"`, `"state":"valid"`} {
		if !strings.Contains(raw, want) {
			t.Errorf("%s does not contain %s", raw, want)
		}
	}
	if strings.Contains(raw, "issuedAt") || strings.Contains(raw, `"error"`) {
		t.Errorf("unknown times and empty errors are not omitted: %s", raw)
	}
}

func TestWriteLicenseError(t *testing.T) {
	t.Parallel()
	tests := []struct {
		err    error
		status int
		code   string
	}{
		{&edition.ExternalKeyError{Source: edition.SourceEnvironment}, http.StatusConflict, "license_managed_externally"},
		{fmt.Errorf("%w: the signature does not match", edition.ErrInvalidKey), http.StatusBadRequest, "invalid_license"},
		{errors.New("database is down"), http.StatusInternalServerError, "internal"},
	}
	for _, tc := range tests {
		rec := httptest.NewRecorder()
		writeLicenseError(rec, tc.err)
		if rec.Code != tc.status || errorCode(t, rec) != tc.code {
			t.Errorf("%v: status %d %s, want %d %s", tc.err, rec.Code, rec.Body, tc.status, tc.code)
		}
	}
}
