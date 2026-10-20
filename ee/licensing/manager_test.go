// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package licensing

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/store"
)

type memoryStore struct {
	mu  sync.Mutex
	key *store.LicenseKey
	err error
}

func (s *memoryStore) GetLicenseKey(context.Context) (*store.LicenseKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch {
	case s.err != nil:
		return nil, s.err
	case s.key == nil:
		return nil, store.ErrNotFound
	}
	k := *s.key
	return &k, nil
}

func (s *memoryStore) SetLicenseKey(_ context.Context, key, actor string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = &store.LicenseKey{Key: key, UpdatedAt: time.Now(), UpdatedBy: actor}
	return nil
}

func (s *memoryStore) DeleteLicenseKey(context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.key = nil
	return nil
}

func (s *memoryStore) stored() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.key == nil {
		return ""
	}
	return s.key.Key
}

func testManager(st KeyStore, now string) *Manager {
	clock := at(now)
	return &Manager{Store: st, Log: slog.New(slog.DiscardHandler), Now: func() time.Time { return clock }, keys: vectorKeys}
}

func TestManagerSources(t *testing.T) {
	t.Parallel()
	file := filepath.Join(t.TempDir(), "license")
	if err := os.WriteFile(file, []byte(vectorText+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		text   string
		file   string
		stored string
		source string
		state  string
		err    string
	}{
		{name: "no key"},
		{name: "database", stored: vectorText, source: edition.SourceDatabase, state: edition.StateValid},
		{name: "environment", text: vectorText, source: edition.SourceEnvironment, state: edition.StateValid},
		{name: "environment before the file and the database", text: vectorText, file: file, stored: "brk-lic-1.x.y",
			source: edition.SourceEnvironment, state: edition.StateValid},
		{name: "file", file: file, source: edition.SourceFile, state: edition.StateValid},
		{name: "missing file", file: filepath.Join(t.TempDir(), "missing"), source: edition.SourceFile,
			state: edition.StateInvalid, err: "reading BROKERIUM_LICENSE_FILE"},
		{name: "invalid key", text: "brk-lic-1.x.y", source: edition.SourceEnvironment, state: edition.StateInvalid,
			err: "not base64url"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := &memoryStore{}
			if tc.stored != "" {
				st.key = &store.LicenseKey{Key: tc.stored}
			}
			m := testManager(st, "2027-06-01T00:00:00Z")
			m.Text, m.File = tc.text, tc.file
			if err := m.Load(t.Context()); err != nil {
				t.Fatal(err)
			}
			info := m.Info()
			if tc.source == "" {
				if info != nil || m.Allows(edition.MessageTools) {
					t.Fatalf("Info = %+v without a key", info)
				}
				return
			}
			if info == nil || info.Source != tc.source || info.State != tc.state || !strings.Contains(info.Error, tc.err) {
				t.Fatalf("Info = %+v, want source %s, state %s, error %q", info, tc.source, tc.state, tc.err)
			}
			if allowed := tc.state == edition.StateValid; m.Allows(edition.MessageTools) != allowed {
				t.Errorf("Allows(%s) = %v, want %v", edition.MessageTools, !allowed, allowed)
			}
			if tc.state == edition.StateValid && (info.Customer != "Example GmbH" || info.ID != "lic_test0000000000" ||
				!info.GraceEndsAt.Equal(at("2027-11-03T00:00:00Z")) || len(info.Features) != 1) {
				t.Errorf("Info = %+v", info)
			}
		})
	}
}

func TestManagerStoreError(t *testing.T) {
	t.Parallel()
	m := testManager(&memoryStore{err: errors.New("database is down")}, "2027-06-01T00:00:00Z")
	if err := m.Load(t.Context()); err == nil || !strings.Contains(err.Error(), "database is down") {
		t.Fatalf("Load = %v", err)
	}
}

func TestManagerEvaluate(t *testing.T) {
	t.Parallel()
	m := testManager(&memoryStore{key: &store.LicenseKey{Key: vectorText}}, "2026-10-19T12:00:00Z")
	if err := m.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	steps := []struct {
		now     string
		state   string
		allowed bool
	}{
		{"2026-10-19T12:00:00Z", edition.StateInvalid, false},
		{"2026-10-20T00:00:00Z", edition.StateValid, true},
		{"2027-10-25T00:00:00Z", edition.StateGrace, true},
		{"2027-11-03T00:00:00Z", edition.StateExpired, false},
	}
	for _, s := range steps {
		now := at(s.now)
		m.Now = func() time.Time { return now }
		m.Evaluate()
		info := m.Info()
		if info.State != s.state || m.Allows(edition.MessageTools) != s.allowed {
			t.Fatalf("at %s: state %s, allowed %v; want %s, %v", s.now, info.State, m.Allows(edition.MessageTools), s.state, s.allowed)
		}
		if s.state == edition.StateInvalid && !strings.Contains(info.Error, "not valid before 2026-10-20T00:00:00Z") {
			t.Errorf("error of a key that is not valid yet: %q", info.Error)
		}
	}
}

func TestManagerRun(t *testing.T) {
	t.Parallel()
	m := testManager(&memoryStore{key: &store.LicenseKey{Key: vectorText}}, "2027-06-01T00:00:00Z")
	if err := m.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	now := at("2027-06-01T00:00:00Z")
	m.Now = func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		return now
	}
	mu.Lock()
	now = at("2028-01-01T00:00:00Z")
	mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		m.Run(ctx, time.Millisecond)
		close(done)
	}()
	deadline := time.Now().Add(5 * time.Second)
	for m.Info().State != edition.StateExpired {
		if time.Now().After(deadline) {
			t.Fatal("Run did not evaluate the state")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	<-done
}

func TestManagerSet(t *testing.T) {
	t.Parallel()
	expired := mustSign(t, func(c *Claims) {
		c.ID = "lic_old"
		c.NotBefore, c.ExpiresAt = at("2025-01-01T00:00:00Z"), at("2026-01-01T00:00:00Z")
	})
	inGrace := mustSign(t, func(c *Claims) {
		c.ID = "lic_grace"
		c.NotBefore, c.ExpiresAt = at("2026-06-01T00:00:00Z"), at("2027-05-25T00:00:00Z")
	})
	future := mustSign(t, func(c *Claims) {
		c.ID = "lic_future"
		c.NotBefore, c.ExpiresAt = at("2028-01-01T00:00:00Z"), at("2029-01-01T00:00:00Z")
	})
	_, signature, _ := strings.Cut(strings.TrimPrefix(vectorText, Prefix), ".")
	tampered := Prefix + base64.RawURLEncoding.EncodeToString([]byte(strings.Replace(vectorPayload, "Example GmbH", "Example Corp", 1))) +
		"." + signature
	tests := []struct {
		name  string
		key   string
		state string
		err   string
	}{
		{"valid key", vectorText, edition.StateValid, ""},
		{"key with line breaks", vectorText[:60] + "\n" + vectorText[60:] + "\n", edition.StateValid, ""},
		{"key in its grace period", inGrace, edition.StateGrace, ""},
		{"expired key", expired, "", "expired at 2026-01-01T00:00:00Z"},
		{"key that is not valid yet", future, "", "not valid before 2028-01-01T00:00:00Z"},
		{"tampered key", tampered, "", "signature does not match"},
		{"not a key", "hello", "", "does not start with"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			st := &memoryStore{}
			m := testManager(st, "2027-06-01T00:00:00Z")
			if err := m.Load(t.Context()); err != nil {
				t.Fatal(err)
			}
			info, err := m.Set(t.Context(), tc.key, "alice")
			if tc.err != "" {
				if !errors.Is(err, edition.ErrInvalidKey) || !strings.Contains(err.Error(), tc.err) {
					t.Fatalf("Set error = %v, want %q", err, tc.err)
				}
				if st.stored() != "" || m.Info() != nil {
					t.Fatal("a refused key was stored")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if info.State != tc.state || info.Source != edition.SourceDatabase || !m.Allows(edition.MessageTools) {
				t.Fatalf("Set = %+v", info)
			}
			if got := st.stored(); got != normalize(tc.key) || st.key.UpdatedBy != "alice" {
				t.Errorf("stored %q by %q", got, st.key.UpdatedBy)
			}
			if err := m.Clear(t.Context(), "alice"); err != nil {
				t.Fatal(err)
			}
			if st.stored() != "" || m.Info() != nil || m.Allows(edition.MessageTools) {
				t.Error("Clear kept the key")
			}
		})
	}
}

func TestManagerExternalKey(t *testing.T) {
	t.Parallel()
	for source, configure := range map[string]func(*Manager){
		edition.SourceEnvironment: func(m *Manager) { m.Text = vectorText },
		edition.SourceFile:        func(m *Manager) { m.File = filepath.Join(t.TempDir(), "missing") },
	} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			st := &memoryStore{}
			m := testManager(st, "2027-06-01T00:00:00Z")
			configure(m)
			if err := m.Load(t.Context()); err != nil {
				t.Fatal(err)
			}
			var external *edition.ExternalKeyError
			if _, err := m.Set(t.Context(), vectorText, "alice"); !errors.As(err, &external) || external.Source != source {
				t.Errorf("Set = %v, want an external key error for %s", err, source)
			}
			if err := m.Clear(t.Context(), "alice"); !errors.As(err, &external) {
				t.Errorf("Clear = %v, want an external key error", err)
			}
			if st.stored() != "" {
				t.Error("the database key changed")
			}
		})
	}
}

func TestManagerWildcard(t *testing.T) {
	t.Parallel()
	m := testManager(&memoryStore{}, "2027-06-01T00:00:00Z")
	m.Text = mustSign(t, func(c *Claims) { c.Features = []string{AllFeatures} })
	if err := m.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	for _, feature := range []string{edition.MessageTools, "sso"} {
		if !m.Allows(feature) {
			t.Errorf("a key for all features does not allow %s", feature)
		}
	}
}
