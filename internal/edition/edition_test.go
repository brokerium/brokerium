// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package edition

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeManager struct{ allowed map[string]bool }

func (f fakeManager) Allows(feature string) bool { return f.allowed[feature] }
func (fakeManager) Info() *LicenseInfo           { return nil }
func (fakeManager) Set(context.Context, string, string) (*LicenseInfo, error) {
	return nil, errors.New("not implemented")
}
func (fakeManager) Clear(context.Context, string) error { return errors.New("not implemented") }

func TestCatalog(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, f := range Catalog {
		if f.ID == "" || f.Title == "" || f.Description == "" {
			t.Errorf("incomplete catalog entry %+v", f)
		}
		if seen[f.ID] {
			t.Errorf("duplicate feature id %q", f.ID)
		}
		seen[f.ID] = true
	}
	if f, ok := Lookup(MessageTools); !ok || f.Title != "Message tools" {
		t.Errorf("Lookup(%q) = %+v, %v", MessageTools, f, ok)
	}
	if _, ok := Lookup("nope"); ok {
		t.Error("Lookup found an unknown feature")
	}
}

func TestLicenseRequired(t *testing.T) {
	t.Parallel()
	err := LicenseRequired(MessageTools)
	if !errors.Is(err, ErrLicenseRequired) || !strings.Contains(err.Error(), "Message tools") {
		t.Errorf("LicenseRequired = %v", err)
	}
	if err := LicenseRequired("custom"); !strings.Contains(err.Error(), "custom") {
		t.Errorf("unknown features keep their id: %v", err)
	}
}

func TestExternalKeyError(t *testing.T) {
	t.Parallel()
	for source, variable := range map[string]string{SourceEnvironment: "BROKERIUM_LICENSE;", SourceFile: "BROKERIUM_LICENSE_FILE"} {
		if msg := (&ExternalKeyError{Source: source}).Error(); !strings.Contains(msg, variable) {
			t.Errorf("source %s: %q does not name %s", source, msg, variable)
		}
	}
}

// TestAllows replaces the process-wide manager and therefore does not run in parallel.
func TestAllows(t *testing.T) {
	t.Cleanup(func() { SetManager(nil) })
	if Allows(MessageTools) {
		t.Fatal("allowed without a manager")
	}
	SetManager(fakeManager{allowed: map[string]bool{MessageTools: true}})
	if !Allows(MessageTools) || Allows("other") {
		t.Error("Allows does not follow the manager")
	}
}
