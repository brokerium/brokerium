// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func standardText(t *testing.T, id string) string {
	t.Helper()
	b, err := standardTexts.ReadFile("licenses/" + id + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// writeFiles creates files under root, with directories as needed.
func writeFiles(t *testing.T, root string, files map[string]string) {
	t.Helper()
	for name, body := range files {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func names(texts []text) []string {
	var out []string
	for _, t := range texts {
		out = append(out, t.name)
	}
	return out
}

func TestKindOf(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		want fileKind
	}{
		{"LICENSE", licenseFile},
		{"LICENSE.txt", licenseFile},
		{"LICENCE.md", licenseFile},
		{"license", licenseFile},
		{"license-mit", licenseFile},
		{"LICENSE-APACHE-2.0", licenseFile},
		{"LICENSE.BSD", licenseFile},
		{"COPYING", licenseFile},
		{"PATENTS", licenseFile},
		{"NOTICE", noticeFile},
		{"NOTICE.txt", noticeFile},
		{"notice.md", noticeFile},
		{"notice_response.go", otherFile},
		{"license.js", otherFile},
		{"license.d.ts", otherFile},
		{"licenses", otherFile},
		{"README.md", otherFile},
		{"CopyrightNotice.txt", otherFile},
	}
	for _, tc := range tests {
		if got := kindOf(tc.name); got != tc.want {
			t.Errorf("kindOf(%q) = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestNormalize(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, in, want string
	}{
		{"Windows line ends", "a\r\nb\r\n", "a\nb"},
		{"byte order mark", "\uFEFFCopyright", "Copyright"},
		{"trailing spaces and blank lines", "\n\n  Apache License  \n\tVersion 2.0\t\n\n", "  Apache License\n\tVersion 2.0"},
		{"invalid UTF-8", "caf\xe9", "caf\uFFFD"},
	}
	for _, tc := range tests {
		if got := normalize([]byte(tc.in)); got != tc.want {
			t.Errorf("%s: normalize(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
		}
	}
}

func TestDeclaredLicenses(t *testing.T) {
	t.Parallel()
	tests := []struct {
		expr    string
		want    []string
		wantErr bool
	}{
		{expr: "MIT", want: []string{"MIT"}},
		{expr: "MIT AND ISC", want: []string{"MIT", "ISC"}},
		{expr: "(MIT OR CC0-1.0)", want: []string{"MIT"}},
		{expr: "Apache-2.0 WITH LLVM-exception", want: []string{"Apache-2.0"}},
		{expr: "GPL-2.0+", want: []string{"GPL-2.0"}},
		{expr: "", wantErr: true},
	}
	for _, tc := range tests {
		got, err := declaredLicenses(tc.expr)
		if (err != nil) != tc.wantErr || !slices.Equal(got, tc.want) {
			t.Errorf("declaredLicenses(%q) = %v, %v; want %v, error %v", tc.expr, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestGoComponent(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeFiles(t, root, map[string]string{
		"LICENSE":                        standardText(t, "MIT"),
		"NOTICE.txt":                     "Example\nCopyright 2026 Example Authors\n",
		"notice.go":                      "package example\n",
		"internal/vendored/LICENSE":      standardText(t, "BSD-3-Clause"),
		"internal/vendored/hash/hash.go": "package hash\n",
		"unused/LICENSE":                 standardText(t, "ISC"),
	})
	c, err := goComponent("example.com/m", "v1.2.3", root, []string{root, filepath.Join(root, "internal", "vendored", "hash")})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := names(c.licenses), []string{"LICENSE", "internal/vendored/LICENSE"}; !slices.Equal(got, want) {
		t.Errorf("licenses = %v, want %v", got, want)
	}
	if got, want := names(c.notices), []string{"NOTICE.txt"}; !slices.Equal(got, want) {
		t.Errorf("notices = %v, want %v", got, want)
	}
	if c.license != "BSD-3-Clause AND MIT" {
		t.Errorf("license = %q", c.license)
	}

	if _, err := goComponent("example.com/empty", "v1.0.0", t.TempDir(), nil); err == nil {
		t.Error("a module without license files was accepted")
	}
}

func TestNPMComponent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		license   string
		files     map[string]string
		skipped   bool
		wantErr   bool
		licenses  []string
		notices   []string
		noteParts []string
	}{
		{
			name:    "license and notice files",
			license: "MIT",
			files: map[string]string{
				"package.json": `{"name":"a","version":"1.0.0"}`,
				"LICENSE":      standardText(t, "MIT"),
				"NOTICE":       "a\nCopyright A\n",
			},
			licenses: []string{"LICENSE"},
			notices:  []string{"NOTICE"},
		},
		{
			name:    "no license file but bundled code",
			license: "MIT AND ISC",
			files: map[string]string{
				"package.json":           `{"name":"b","version":"2.0.0","author":{"name":"B Authors","email":"b@example.com"}}`,
				"vendor/x/LICENSE":       standardText(t, "ISC"),
				"vendor/x/index.js":      "",
				"node_modules/y/LICENSE": standardText(t, "MIT"),
			},
			licenses:  []string{"MIT (standard text)", "ISC (standard text)", "vendor/x/LICENSE"},
			noteParts: []string{"MIT AND ISC", "B Authors <b@example.com>", "standard texts of MIT and ISC follow"},
		},
		{
			name:    "platform package",
			license: "MIT",
			files: map[string]string{
				"package.json": `{"name":"c","version":"3.0.0","os":["darwin"],"cpu":["arm64"]}`,
			},
			skipped: true,
		},
		{
			name:    "no license file and no standard text",
			license: "Apache-2.0",
			files: map[string]string{
				"package.json": `{"name":"d","version":"4.0.0"}`,
			},
			wantErr: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			writeFiles(t, dir, tc.files)
			c, ok, err := npmComponent(tc.license, dir)
			if tc.wantErr {
				if err == nil {
					t.Fatal("no error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if ok == tc.skipped {
				t.Fatalf("ok = %v", ok)
			}
			if tc.skipped {
				return
			}
			if got := names(c.licenses); !slices.Equal(got, tc.licenses) {
				t.Errorf("licenses = %v, want %v", got, tc.licenses)
			}
			if got := names(c.notices); !slices.Equal(got, tc.notices) {
				t.Errorf("notices = %v, want %v", got, tc.notices)
			}
			for _, part := range tc.noteParts {
				if !strings.Contains(c.note, part) {
					t.Errorf("note %q does not mention %q", c.note, part)
				}
			}
			if len(tc.noteParts) == 0 && c.note != "" {
				t.Errorf("note = %q", c.note)
			}
		})
	}
}

func TestStandardTextsAreRecognized(t *testing.T) {
	t.Parallel()
	entries, err := standardTexts.ReadDir("licenses")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		id := strings.TrimSuffix(e.Name(), ".txt")
		if got := expression(licenseIDs([]text{{name: e.Name(), body: standardText(t, id)}})); got != id {
			t.Errorf("licenses/%s is recognized as %s", e.Name(), got)
		}
	}
}

func TestWithComponentNotices(t *testing.T) {
	t.Parallel()
	own := "Brokerium\nRequired Notice: Copyright (c) 2026 Bulat Valiev\n"
	components := []component{
		{name: "example.com/a", notices: []text{{name: "NOTICE", body: "A\nCopyright A"}}},
		{name: "b", notices: []text{{name: "NOTICE.txt", body: "B"}}},
		{name: "b", notices: []text{{name: "NOTICE.txt", body: "B"}}},
		{name: "c"},
	}
	first := withComponentNotices(own, components)
	want := own + "\n" + noticesHeading + "\n" + strings.Repeat("-", len(noticesHeading)) + "\n\n" + noticesIntro +
		"\n-- example.com/a: NOTICE --\n\nA\nCopyright A\n" +
		"\n-- b: NOTICE.txt --\n\nB\n"
	if first != want {
		t.Fatalf("NOTICE =\n%s\nwant\n%s", first, want)
	}
	if again := withComponentNotices(first, components); again != first {
		t.Errorf("regenerating changes NOTICE:\n%s", again)
	}
	if got := withComponentNotices(first, components[3:]); strings.Contains(got, "Copyright A") || !strings.HasPrefix(got, own) {
		t.Errorf("the section is not replaced:\n%s", got)
	}
}

func TestThirdPartyNotices(t *testing.T) {
	t.Parallel()
	modules := []component{{
		name: "example.com/m", version: "v1.0.0", license: "MIT",
		licenses: []text{{name: "LICENSE", body: "MIT text"}},
		notices:  []text{{name: "NOTICE", body: "Notice text"}},
	}}
	packages := []component{{
		name: "p", version: "2.0.0", license: "ISC", note: "A note.",
		licenses: []text{{name: "ISC (standard text)", body: "ISC text"}},
	}}
	got := thirdPartyNotices(modules, packages)
	order := []string{
		header,
		"\nGo modules\n  example.com/m v1.0.0: MIT\n",
		"\nnpm packages of the dashboard\n  p 2.0.0: ISC\n",
		"\n" + rule + "\nexample.com/m v1.0.0\nLicense: MIT\n" + rule + "\n",
		"\n-- LICENSE --\n\nMIT text\n",
		"\n-- NOTICE --\n\nNotice text\n",
		"\n" + rule + "\np 2.0.0\nLicense: ISC\n" + rule + "\n\nA note.\n",
		"\n-- ISC (standard text) --\n\nISC text\n",
	}
	rest := got
	for _, part := range order {
		i := strings.Index(rest, part)
		if i < 0 {
			t.Fatalf("missing or out of order: %q\nin\n%s", part, got)
		}
		rest = rest[i+len(part):]
	}
	if rest != "" {
		t.Errorf("unexpected tail %q", rest)
	}
}
