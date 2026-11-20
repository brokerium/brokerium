// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/config"
)

// testCA returns a self-signed CA certificate in PEM.
func testCA(t *testing.T) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "test CA"}, IsCA: true,
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func TestCheckEnvironment(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		p    config.Prometheus
		want string
	}{
		{"nothing", config.Prometheus{}, ""},
		{"URL only", config.Prometheus{URL: "http://prometheus:9090"}, ""},
		{"basic", config.Prometheus{URL: "https://prometheus.example.com/prom", Username: "u", PasswordFile: "/p"}, ""},
		{"bearer", config.Prometheus{URL: "https://prometheus.example.com", TokenFile: "/t", CAFile: "/ca"}, ""},
		{"credentials without URL", config.Prometheus{Username: "u"}, "need BROKERIUM_PROMETHEUS_URL"},
		{"bad URL", config.Prometheus{URL: "prometheus:9090"}, "BROKERIUM_PROMETHEUS_URL"},
		{"URL with a query", config.Prometheus{URL: "http://prometheus:9090/?x=1"}, "query"},
		{"both authentications", config.Prometheus{URL: "http://p", TokenFile: "/t", Username: "u"}, "cannot be combined"},
		{"password without a username", config.Prometheus{URL: "http://p", PasswordFile: "/p"}, "needs BROKERIUM_PROMETHEUS_USERNAME"},
	}
	for _, tt := range tests {
		err := checkEnvironment(tt.p)
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestEnvironmentSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	ca := testCA(t)
	p := config.Prometheus{URL: "https://prometheus.example.com", Username: "brokerium", PasswordFile: write("password", "s3cret\n"),
		CAFile: write("ca.crt", ca)}
	s, err := environmentSource(p)
	if err != nil {
		t.Fatal(err)
	}
	if s.Auth != AuthBasic || s.Username != "brokerium" || s.Password != "s3cret" || s.CAPEM != strings.TrimSpace(ca) ||
		s.Origin != OriginEnvironment || s.Timeout != defaultTimeout {
		t.Errorf("basic source = %+v", s)
	}
	if s, err = environmentSource(config.Prometheus{URL: "http://p", TokenFile: write("token", " tok \n")}); err != nil ||
		s.Auth != AuthBearer || s.Token != "tok" {
		t.Errorf("bearer source = %+v, %v", s, err)
	}
	if s, err = environmentSource(config.Prometheus{URL: "http://p"}); err != nil || s.Auth != AuthNone {
		t.Errorf("source without authentication = %+v, %v", s, err)
	}
	if s, err = environmentSource(config.Prometheus{}); s != nil || err != nil {
		t.Errorf("without a URL = %+v, %v", s, err)
	}
	s, err = environmentSource(config.Prometheus{URL: "http://p", TokenFile: filepath.Join(dir, "missing")})
	if err == nil || !strings.Contains(err.Error(), "BROKERIUM_PROMETHEUS_TOKEN_FILE") || s == nil || s.URL != "http://p" {
		t.Errorf("unreadable token file = %+v, %v", s, err)
	}
	if _, err = environmentSource(config.Prometheus{URL: "http://p", CAFile: write("bad.crt", "nope")}); !errors.Is(err, ErrBadSource) {
		t.Errorf("CA file without a certificate = %v", err)
	}
}

func TestSourceRequest(t *testing.T) {
	t.Parallel()
	ptr := func(s string) *string { return &s }
	basic := &Source{URL: "http://old", Auth: AuthBasic, Username: "old", Password: "kept", Timeout: defaultTimeout}
	bearer := &Source{URL: "http://old", Auth: AuthBearer, Token: "old-token", Timeout: defaultTimeout}
	ca := testCA(t)
	tests := []struct {
		name    string
		req     SourceRequest
		prev    *Source
		want    Source
		wantErr string
	}{
		{name: "no authentication", req: SourceRequest{URL: " http://prometheus:9090 ", Username: "ignored", TimeoutSeconds: 10},
			want: Source{URL: "http://prometheus:9090", Auth: AuthNone, Timeout: 10 * time.Second}},
		{name: "basic keeps the password", req: SourceRequest{URL: "http://p", Auth: AuthBasic, Username: "new"}, prev: basic,
			want: Source{URL: "http://p", Auth: AuthBasic, Username: "new", Password: "kept", Timeout: defaultTimeout}},
		{name: "basic with a new password", req: SourceRequest{URL: "http://p", Auth: AuthBasic, Username: "u", Password: ptr("new")},
			prev: basic, want: Source{URL: "http://p", Auth: AuthBasic, Username: "u", Password: "new", Timeout: defaultTimeout}},
		{name: "basic clears the password", req: SourceRequest{URL: "http://p", Auth: AuthBasic, Username: "u", Password: ptr("")},
			prev: basic, want: Source{URL: "http://p", Auth: AuthBasic, Username: "u", Timeout: defaultTimeout}},
		{name: "basic after bearer", req: SourceRequest{URL: "http://p", Auth: AuthBasic, Username: "u"}, prev: bearer,
			want: Source{URL: "http://p", Auth: AuthBasic, Username: "u", Timeout: defaultTimeout}},
		{name: "bearer keeps the token", req: SourceRequest{URL: "http://p", Auth: AuthBearer, CAPEM: ca}, prev: bearer,
			want: Source{URL: "http://p", Auth: AuthBearer, Token: "old-token", CAPEM: strings.TrimSpace(ca), Timeout: defaultTimeout}},
		{name: "bearer after basic needs a token", req: SourceRequest{URL: "http://p", Auth: AuthBearer}, prev: basic,
			wantErr: "needs a token"},
		{name: "basic needs a username", req: SourceRequest{URL: "http://p", Auth: AuthBasic}, wantErr: "needs a username"},
		{name: "unknown authentication", req: SourceRequest{URL: "http://p", Auth: "digest"}, wantErr: "none, basic or bearer"},
		{name: "missing URL", req: SourceRequest{}, wantErr: "http:// or https://"},
		{name: "credentials in the URL", req: SourceRequest{URL: "https://u:p@prometheus"}, wantErr: "not into the URL"},
		{name: "fragment", req: SourceRequest{URL: "https://prometheus#x"}, wantErr: "query or a fragment"},
		{name: "timeout", req: SourceRequest{URL: "http://p", TimeoutSeconds: 301}, wantErr: "between 1 and 300"},
		{name: "bad CA", req: SourceRequest{URL: "http://p", CAPEM: "-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----"},
			wantErr: "does not parse"},
	}
	for _, tt := range tests {
		got, err := tt.req.apply(tt.prev)
		if tt.wantErr != "" {
			if !errors.Is(err, ErrBadSource) || !strings.Contains(err.Error(), tt.wantErr) {
				t.Errorf("%s: error %v, want one containing %q", tt.name, err, tt.wantErr)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		tt.want.Origin = OriginDatabase
		if *got != tt.want {
			t.Errorf("%s: %+v, want %+v", tt.name, *got, tt.want)
		}
	}
}

func TestStoredSource(t *testing.T) {
	t.Parallel()
	box := testBox(t)
	src := &Source{URL: "https://prometheus.example.com", Auth: AuthBearer, Token: "tok", InsecureSkipVerify: true,
		Timeout: 45 * time.Second}
	st, err := src.setting(box, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(st.Value), "tok") || len(st.SecretsSealed) == 0 || st.Key != sourceKey || st.UpdatedBy != "alice" {
		t.Errorf("setting = %s, sealed %d bytes", st.Value, len(st.SecretsSealed))
	}
	st.UpdatedAt = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)
	got, err := storedSourceOf(st, box)
	if err != nil {
		t.Fatal(err)
	}
	want := *src
	want.Origin, want.UpdatedAt, want.UpdatedBy = OriginDatabase, st.UpdatedAt, "alice"
	if *got != want {
		t.Errorf("round trip = %+v, want %+v", *got, want)
	}
	if fp := got.fingerprint(); fp == "" || fp == (&Source{URL: src.URL}).fingerprint() {
		t.Errorf("fingerprint %q does not follow the credentials", fp)
	}
	none, err := (&Source{URL: "http://p", Auth: AuthNone, Timeout: defaultTimeout}).setting(box, "bob")
	if err != nil || none.SecretsSealed != nil {
		t.Errorf("a source without credentials sealed %v, %v", none.SecretsSealed, err)
	}
	v := viewOf(got)
	if !v.Configured || !v.TokenSet || v.PasswordSet || v.TimeoutSeconds != 45 || v.UpdatedAt == nil || v.Origin != OriginDatabase {
		t.Errorf("view = %+v", v)
	}
	if v := viewOf(nil); v.Configured || v.Origin != OriginNone || v.Auth != AuthNone || v.TimeoutSeconds != 30 {
		t.Errorf("view of no source = %+v", v)
	}
}
