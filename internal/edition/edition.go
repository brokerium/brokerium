// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package edition names the edition of the build and lists the features of Brokerium
// Enterprise (ADR 0016), so that both editions can show what Enterprise adds. Enterprise builds
// install a license Manager; Community builds have none.
package edition

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Editions.
const (
	Community  = "community"
	Enterprise = "enterprise"
)

// Name is the edition of this build.
var Name = Community

// Feature is a feature of Brokerium Enterprise that a license key can cover.
type Feature struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
}

// MessageTools is the id of the message tools feature.
const MessageTools = "message-tools"

// Catalog lists the features of Brokerium Enterprise.
var Catalog = []Feature{
	{ID: MessageTools, Title: "Message tools", Description: "Delete records and empty topics, search, export and copy messages."},
}

// Lookup returns the catalog entry of a feature.
func Lookup(id string) (Feature, bool) {
	for _, f := range Catalog {
		if f.ID == id {
			return f, true
		}
	}
	return Feature{}, false
}

// States of a license. A feature is allowed in StateValid and StateGrace.
const (
	StateNone    = "none"
	StateInvalid = "invalid"
	StateValid   = "valid"
	StateGrace   = "grace"
	StateExpired = "expired"
)

// Sources of a license key.
const (
	SourceEnvironment = "environment"
	SourceFile        = "file"
	SourceDatabase    = "database"
)

// LicenseInfo describes the license key in use. The claims are empty when the key is invalid.
type LicenseInfo struct {
	ID          string    `json:"id"`
	Customer    string    `json:"customer"`
	Email       string    `json:"email"`
	Features    []string  `json:"features"`
	IssuedAt    time.Time `json:"issuedAt,omitzero"`
	NotBefore   time.Time `json:"notBefore,omitzero"`
	ExpiresAt   time.Time `json:"expiresAt,omitzero"`
	GraceEndsAt time.Time `json:"graceEndsAt,omitzero"`
	State       string    `json:"state"`
	Source      string    `json:"source"`
	Error       string    `json:"error,omitempty"`
}

// Manager checks and stores the license key of an Enterprise build.
type Manager interface {
	// Allows reports whether the license covers a feature and is valid or in its grace period.
	Allows(feature string) bool
	// Info describes the license; nil when there is no key.
	Info() *LicenseInfo
	// Set verifies a key and stores it in the database.
	Set(ctx context.Context, key, actor string) (*LicenseInfo, error)
	// Clear deletes the key stored in the database.
	Clear(ctx context.Context, actor string) error
}

// ErrInvalidKey is returned by Manager.Set for a key that cannot be used.
var ErrInvalidKey = errors.New("invalid license key")

// ExternalKeyError is returned by Manager.Set and Manager.Clear when the key comes from
// BROKERIUM_LICENSE or BROKERIUM_LICENSE_FILE, which take precedence over the database.
type ExternalKeyError struct {
	Source string
}

func (e *ExternalKeyError) Error() string {
	if e.Source == SourceFile {
		return "the license key is read from BROKERIUM_LICENSE_FILE; replace the file and restart the server"
	}
	return "the license key is set by BROKERIUM_LICENSE; change the variable and restart the server"
}

// ErrLicenseRequired is returned for Enterprise features that the license does not cover.
var ErrLicenseRequired = errors.New("license required")

// LicenseRequired returns the error for a feature that the license does not cover.
func LicenseRequired(feature string) error {
	title := feature
	if f, ok := Lookup(feature); ok {
		title = f.Title
	}
	if Name != Enterprise {
		return fmt.Errorf("%w: %s is a feature of Brokerium Enterprise", ErrLicenseRequired, title)
	}
	return fmt.Errorf("%w: %s needs a Brokerium Enterprise license that covers it", ErrLicenseRequired, title)
}

var (
	mu      sync.RWMutex
	manager Manager
)

// SetManager installs the license manager of an Enterprise build.
func SetManager(m Manager) {
	mu.Lock()
	defer mu.Unlock()
	manager = m
}

// CurrentManager returns the license manager; nil in Community builds.
func CurrentManager() Manager {
	mu.RLock()
	defer mu.RUnlock()
	return manager
}

// Allows reports whether the current license covers a feature.
func Allows(feature string) bool {
	m := CurrentManager()
	return m != nil && m.Allows(feature)
}
