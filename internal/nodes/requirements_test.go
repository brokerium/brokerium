// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"log/slog"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/ops"
)

// TestDashboardRequirements keeps OPERATION_REQUIREMENTS of the dashboard, which decides which
// buttons are enabled before the server answered, equal to the operation types of the server.
func TestDashboardRequirements(t *testing.T) {
	t.Parallel()
	src, err := os.ReadFile("../../web/src/lib/capabilities.ts")
	if err != nil {
		t.Fatal(err)
	}
	table := string(src)
	start := strings.Index(table, "OPERATION_REQUIREMENTS")
	if start < 0 {
		t.Fatal("OPERATION_REQUIREMENTS not found")
	}
	table = table[start:]
	table = table[:strings.Index(table, "};")]
	dashboard := map[string][]string{}
	entry := regexp.MustCompile(`'?([a-z-]+)'?: \[([^\]]*)\]`)
	capability := regexp.MustCompile(`'([A-Za-z]+)'`)
	for _, m := range entry.FindAllStringSubmatch(table, -1) {
		caps := []string{}
		for _, c := range capability.FindAllStringSubmatch(m[2], -1) {
			caps = append(caps, c[1])
		}
		slices.Sort(caps)
		dashboard[m[1]] = caps
	}

	e := ops.New(nil, nil, nil, slog.New(slog.DiscardHandler))
	e.Extend(&Manager{})
	server := map[string][]string{}
	for _, ot := range e.Types() {
		caps := slices.Clone(ot.Requires)
		if caps == nil {
			caps = []string{}
		}
		slices.Sort(caps)
		server[ot.Type] = caps
	}
	for op, caps := range server {
		got, ok := dashboard[op]
		switch {
		case !ok:
			t.Errorf("the dashboard does not list operation %s", op)
		case !slices.Equal(got, caps):
			t.Errorf("%s: the dashboard requires %v, the server %v", op, got, caps)
		}
	}
	for op := range dashboard {
		if _, ok := server[op]; !ok {
			t.Errorf("the dashboard lists %s, which the server does not offer", op)
		}
	}
}
