// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package brokerium

import (
	"slices"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/edition"
)

func TestLegalFiles(t *testing.T) {
	t.Parallel()
	want := []string{"LICENSE", "NOTICE", "THIRD_PARTY_NOTICES"}
	if edition.Name == edition.Enterprise {
		want = []string{"LICENSE", "LICENSE-enterprise", "NOTICE", "THIRD_PARTY_NOTICES"}
	}
	files := LegalFiles()
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
		if f.Title == "" || strings.TrimSpace(f.Text) == "" {
			t.Errorf("%s has no title or text", f.Name)
		}
	}
	if !slices.Equal(names, want) {
		t.Fatalf("files = %v, want %v", names, want)
	}
	if !strings.Contains(notice, "Required Notice: Copyright (c) 2026 Bulat Valiev") {
		t.Error("NOTICE lost the Required Notice of the license")
	}
	if !strings.Contains(thirdPartyNotices, "\nGo standard library go") {
		t.Error("THIRD_PARTY_NOTICES does not list the Go standard library")
	}
}
