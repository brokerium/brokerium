// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package licensing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/brokerium/brokerium/internal/edition"
	"github.com/brokerium/brokerium/internal/store"
)

// KeyStore keeps the license key added through the dashboard.
type KeyStore interface {
	GetLicenseKey(ctx context.Context) (*store.LicenseKey, error)
	SetLicenseKey(ctx context.Context, key, actor string) error
	DeleteLicenseKey(ctx context.Context) error
}

// Manager is the license manager of Enterprise builds. It uses the key in Text
// (BROKERIUM_LICENSE), else the key in File (BROKERIUM_LICENSE_FILE), else the key stored in
// the database. The state is evaluated by Load, Set and Evaluate, not on every check.
type Manager struct {
	Text  string
	File  string
	Store KeyStore
	Log   *slog.Logger
	// Now is the clock of the state evaluation; nil means time.Now.
	Now func() time.Time

	// keys replaces the public keys of the build in tests.
	keys map[string]string

	mu  sync.Mutex // serializes the changes of cur
	cur atomic.Pointer[loaded]
}

var _ edition.Manager = (*Manager)(nil)

// loaded is the key in use.
type loaded struct {
	source string
	// claims are nil when the key does not verify.
	claims *Claims
	state  string
	// err tells why the key cannot be used.
	err error
}

func (l *loaded) stateName() string {
	if l == nil {
		return edition.StateNone
	}
	return l.state
}

// Load reads and evaluates the license key at startup.
func (m *Manager) Load(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	switch m.externalSource() {
	case edition.SourceEnvironment:
		if m.File != "" {
			m.Log.Warn("BROKERIUM_LICENSE_FILE is ignored because BROKERIUM_LICENSE is set")
		}
		m.use(m.verify(m.Text, edition.SourceEnvironment))
	case edition.SourceFile:
		raw, err := os.ReadFile(m.File) //nolint:gosec // the path comes from BROKERIUM_LICENSE_FILE
		if err != nil {
			m.use(&loaded{source: edition.SourceFile, state: edition.StateInvalid, err: fmt.Errorf("reading BROKERIUM_LICENSE_FILE: %w", err)})
			return nil
		}
		m.use(m.verify(string(raw), edition.SourceFile))
	default:
		k, err := m.Store.GetLicenseKey(ctx)
		switch {
		case errors.Is(err, store.ErrNotFound):
			m.Log.Info("no license key: Enterprise features stay locked until one is added")
			m.use(nil)
		case err != nil:
			return fmt.Errorf("reading the stored license key: %w", err)
		default:
			m.use(m.verify(k.Key, edition.SourceDatabase))
		}
	}
	return nil
}

// Run evaluates the state every interval until ctx ends.
func (m *Manager) Run(ctx context.Context, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			m.Evaluate()
		}
	}
}

// Evaluate recomputes the state of the license in use at the current time.
func (m *Manager) Evaluate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if l := m.cur.Load(); l != nil && l.claims != nil {
		m.use(m.evaluate(l.source, l.claims))
	}
}

// Allows reports whether the license is valid or in its grace period and covers the feature.
func (m *Manager) Allows(feature string) bool {
	l := m.cur.Load()
	return l != nil && l.claims != nil && (l.state == edition.StateValid || l.state == edition.StateGrace) &&
		l.claims.Covers(feature)
}

// Info describes the license in use; nil when there is no key.
func (m *Manager) Info() *edition.LicenseInfo {
	l := m.cur.Load()
	if l == nil {
		return nil
	}
	info := &edition.LicenseInfo{Features: []string{}, State: l.state, Source: l.source}
	if l.err != nil {
		info.Error = l.err.Error()
	}
	if c := l.claims; c != nil {
		info.ID, info.Customer, info.Email = c.ID, c.Customer, c.Email
		info.Features = append(info.Features, c.Features...)
		info.IssuedAt, info.NotBefore, info.ExpiresAt, info.GraceEndsAt = c.IssuedAt, c.NotBefore, c.ExpiresAt, c.GraceEndsAt()
	}
	return info
}

// Set verifies a key and stores it in the database. Keys that expired or are not valid yet
// are refused, so they cannot replace a working key.
func (m *Manager) Set(ctx context.Context, text, actor string) (*edition.LicenseInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if source := m.externalSource(); source != "" {
		return nil, &edition.ExternalKeyError{Source: source}
	}
	text = normalize(text)
	c, err := parse(text, m.publicKeys())
	if err != nil {
		return nil, fmt.Errorf("%w: %w", edition.ErrInvalidKey, err)
	}
	l := m.evaluate(edition.SourceDatabase, c)
	switch l.state {
	case edition.StateExpired:
		return nil, fmt.Errorf("%w: the key expired at %s and its grace period ended at %s", edition.ErrInvalidKey,
			c.ExpiresAt.Format(time.RFC3339), c.GraceEndsAt().Format(time.RFC3339))
	case edition.StateInvalid:
		return nil, fmt.Errorf("%w: %w", edition.ErrInvalidKey, l.err)
	}
	if err := m.Store.SetLicenseKey(ctx, text, actor); err != nil {
		return nil, fmt.Errorf("storing the license key: %w", err)
	}
	m.Log.Info("license key stored", "license", c.ID, "customer", c.Customer, "by", actor)
	m.use(l)
	return m.Info(), nil
}

// Clear deletes the key stored in the database.
func (m *Manager) Clear(ctx context.Context, actor string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if source := m.externalSource(); source != "" {
		return &edition.ExternalKeyError{Source: source}
	}
	if err := m.Store.DeleteLicenseKey(ctx); err != nil {
		return fmt.Errorf("deleting the license key: %w", err)
	}
	m.Log.Info("license key removed", "by", actor)
	m.use(nil)
	return nil
}

func (m *Manager) externalSource() string {
	switch {
	case m.Text != "":
		return edition.SourceEnvironment
	case m.File != "":
		return edition.SourceFile
	default:
		return ""
	}
}

func (m *Manager) verify(text, source string) *loaded {
	c, err := parse(text, m.publicKeys())
	if err != nil {
		return &loaded{source: source, state: edition.StateInvalid, err: err}
	}
	return m.evaluate(source, c)
}

func (m *Manager) evaluate(source string, c *Claims) *loaded {
	l := &loaded{source: source, claims: c, state: c.State(m.now())}
	if l.state == edition.StateInvalid {
		l.err = fmt.Errorf("the key is not valid before %s", c.NotBefore.Format(time.RFC3339))
	}
	return l
}

// use makes l the key in use and logs when the state changes.
func (m *Manager) use(l *loaded) {
	from, to := m.cur.Swap(l).stateName(), l.stateName()
	if from == to {
		return
	}
	level := slog.LevelWarn
	attrs := []any{"from", from, "to", to}
	if l != nil {
		if l.state == edition.StateValid {
			level = slog.LevelInfo
		}
		attrs = append(attrs, "source", l.source)
		if c := l.claims; c != nil {
			attrs = append(attrs, "license", c.ID, "customer", c.Customer, "expiresAt", c.ExpiresAt, "graceEndsAt", c.GraceEndsAt())
		}
		if l.err != nil {
			attrs = append(attrs, "error", l.err)
		}
	}
	m.Log.Log(context.Background(), level, "license state changed", attrs...)
}

func (m *Manager) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now()
}

func (m *Manager) publicKeys() map[string]string {
	if m.keys != nil {
		return m.keys
	}
	return publicKeys
}
