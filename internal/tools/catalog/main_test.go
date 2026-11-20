// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/kafkaconfig"
)

// TestCatalogsMatchReleases keeps the committed catalogs generated from the releases of the
// generator: after a change of releases, run make catalog.
func TestCatalogsMatchReleases(t *testing.T) {
	t.Parallel()
	for _, r := range releases {
		c, exact := kafkaconfig.For(r)
		if c == nil || !exact || c.Version != r {
			t.Errorf("the catalog of %s is missing or generated from another release; run make catalog VERSIONS=%s", r, r)
		}
	}
	if lines := kafkaconfig.Lines(); len(lines) != len(releases) {
		t.Errorf("catalogs of %v for the releases %v", lines, releases)
	}
}

func TestParseReleases(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in   string
		want []version
		err  string
	}{
		{in: "4.4.0", want: []version{{4, 4, 0}}},
		{in: " 3.5.2, 3.6.2  4.4.0 ", want: []version{{3, 5, 2}, {3, 6, 2}, {4, 4, 0}}},
		{in: "4.4", err: "not a Kafka release"},
		{in: "4.4.0-rc1", err: "not a Kafka release"},
		{in: "4.3.1 4.3.2", err: "more than one release of 4.3"},
		{in: " ", err: "no releases"},
	}
	for _, tt := range tests {
		got, err := parseReleases(tt.in)
		switch {
		case tt.err != "" && (err == nil || !strings.Contains(err.Error(), tt.err)):
			t.Errorf("parseReleases(%q) error %v, want %q", tt.in, err, tt.err)
		case tt.err == "" && (err != nil || !slices.Equal(got, tt.want)):
			t.Errorf("parseReleases(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	if _, err := parseReleases(strings.Join(releases, " ")); err != nil {
		t.Errorf("the default releases: %v", err)
	}
}

func TestHasImage(t *testing.T) {
	t.Parallel()
	for v, want := range map[version]bool{{3, 5, 2}: false, {3, 6, 2}: false, {3, 7, 0}: true, {4, 4, 0}: true} {
		if got := v.hasImage(); got != want {
			t.Errorf("%s: hasImage %t, want %t", v, got, want)
		}
	}
	if (line{4, 4}).docsPath() != "44" || (line{3, 5}).docsPath() != "35" {
		t.Error("docsPath")
	}
}

func TestParseSum(t *testing.T) {
	t.Parallel()
	sum := strings.Repeat("0123456789abcdef", 8)
	grouped := strings.ToUpper(sum)
	var groups []string
	for i := 0; i < len(grouped); i += 8 {
		groups = append(groups, grouped[i:i+8])
	}
	tests := map[string]string{
		"apache":    "kafka_2.13-3.5.2.tgz: " + strings.Join(groups[:8], " ") + "\n                      " + strings.Join(groups[8:], " ") + "\n",
		"sha512sum": sum + "  kafka_2.13-3.5.2.tgz\n",
		"bsd":       "SHA512 (kafka_2.13-3.5.2.tgz) = " + sum + "\n",
		"none":      "<html>Not found</html>",
	}
	for name, body := range tests {
		want := sum
		if name == "none" {
			want = ""
		}
		if got := parseSum(body); got != want {
			t.Errorf("%s: parseSum = %q, want %q", name, got, want)
		}
	}
}
