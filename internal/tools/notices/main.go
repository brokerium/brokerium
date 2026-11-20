// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Command notices writes THIRD_PARTY_NOTICES: the name, version and license of every third-party
// Go module linked into the released binaries and of every production npm package of the
// dashboard, with the full text of their license and notice files. It also reproduces the notice
// files at the end of NOTICE. Run it from the repository root once the dependencies of the
// dashboard are installed, which `make notices` does:
//
//	make notices
package main

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"log"
	"maps"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/google/licensecheck"
)

func main() {
	modules, err := goModules()
	if err != nil {
		log.Fatal(err)
	}
	packages, err := npmPackages("web")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("THIRD_PARTY_NOTICES", []byte(thirdPartyNotices(modules, packages)), 0o644); err != nil {
		log.Fatal(err)
	}
	notice, err := os.ReadFile("NOTICE")
	if err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile("NOTICE", []byte(withComponentNotices(string(notice), slices.Concat(modules, packages))), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("THIRD_PARTY_NOTICES and NOTICE: %d Go modules, %d npm packages\n", len(modules), len(packages))
}

// component is a third-party Go module or npm package.
type component struct {
	name, version string
	// license is an SPDX expression.
	license string
	// note explains the standard license texts of a package without license files of its own.
	note     string
	licenses []text
	notices  []text
}

// text is a license or notice file of a component, named by its path in the component.
type text struct {
	name, body string
}

type fileKind int

const (
	otherFile fileKind = iota
	licenseFile
	noticeFile
)

var (
	licenseName = regexp.MustCompile(`^(licen[cs]e|copying|patents)([-_.][a-z0-9.-]+)?$`)
	noticeName  = regexp.MustCompile(`^notice(\.(txt|md|markdown|rst))?$`)
	codeExt     = []string{".go", ".js", ".mjs", ".cjs", ".ts", ".mts", ".cts", ".tsx", ".jsx", ".json", ".map", ".css", ".html"}
)

// kindOf tells license files (LICENSE, LICENCE.md, license-mit, COPYING, PATENTS) and notice
// files (NOTICE, NOTICE.txt) from other files, such as code named after licenses.
func kindOf(name string) fileKind {
	name = strings.ToLower(name)
	switch {
	case slices.Contains(codeExt, path.Ext(name)):
		return otherFile
	case licenseName.MatchString(name):
		return licenseFile
	case noticeName.MatchString(name):
		return noticeFile
	}
	return otherFile
}

// addFiles adds the license and notice files in dir, named by their path relative to root.
// With licensesOnly it skips notice files.
func (c *component) addFiles(root, dir string, licensesOnly bool) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		kind := kindOf(e.Name())
		if kind == otherFile || kind == noticeFile && licensesOnly {
			continue
		}
		file := filepath.Join(dir, e.Name())
		if info, err := os.Stat(file); err != nil || !info.Mode().IsRegular() {
			continue
		}
		b, err := os.ReadFile(file)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return err
		}
		t := text{name: filepath.ToSlash(rel), body: normalize(b)}
		if kind == noticeFile {
			c.notices = append(c.notices, t)
		} else {
			c.licenses = append(c.licenses, t)
		}
	}
	return nil
}

// normalize makes a text valid UTF-8 with Unix line ends, without trailing spaces and blank lines
// around it.
func normalize(b []byte) string {
	s := strings.TrimPrefix(strings.ToValidUTF8(string(b), "\uFFFD"), "\uFEFF")
	s = strings.NewReplacer("\r\n", "\n", "\r", "\n").Replace(s)
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// licenseIDs returns the licenses that licensecheck recognizes in license files. Patent grants
// such as the PATENTS files of Go are not licenses of their own.
func licenseIDs(files []text) map[string]bool {
	ids := map[string]bool{}
	for _, f := range files {
		if strings.HasPrefix(strings.ToLower(path.Base(f.name)), "patents") {
			continue
		}
		for _, m := range licensecheck.Scan([]byte(f.body)).Match {
			ids[m.ID] = true
		}
	}
	return ids
}

// expression joins licenses into an SPDX expression; NOASSERTION when none is recognized.
func expression(ids map[string]bool) string {
	if len(ids) == 0 {
		return "NOASSERTION"
	}
	return strings.Join(slices.Sorted(maps.Keys(ids)), " AND ")
}

func sortComponents(cs []component) {
	slices.SortFunc(cs, func(a, b component) int {
		return cmp.Or(cmp.Compare(a.name, b.name), cmp.Compare(a.version, b.version))
	})
}

// output runs cmd and returns its standard output; errors carry its standard error.
func output(cmd *exec.Cmd) ([]byte, error) {
	out, err := cmd.Output()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return nil, fmt.Errorf("%s: %w: %s", strings.Join(cmd.Args, " "), err, bytes.TrimSpace(exit.Stderr))
	}
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(cmd.Args, " "), err)
	}
	return out, nil
}

const rule = "================================================================================"

const header = `THIRD-PARTY NOTICES

Brokerium includes the third-party software listed below: the Go modules linked
into brokerium-server, brokerium-server-enterprise and brokerium-agent, and the
npm packages of the dashboard that brokerium-server embeds. Each entry names the
component, its version and its license and reproduces its license and notice
files.

make notices generates this file from the dependencies of the repository.
`

// thirdPartyNotices renders THIRD_PARTY_NOTICES: an index of the components, then each component
// with its license and notice files.
func thirdPartyNotices(modules, packages []component) string {
	var b strings.Builder
	b.WriteString(header)
	for _, group := range []struct {
		title      string
		components []component
	}{{"Go modules", modules}, {"npm packages of the dashboard", packages}} {
		fmt.Fprintf(&b, "\n%s\n", group.title)
		for _, c := range group.components {
			fmt.Fprintf(&b, "  %s %s: %s\n", c.name, c.version, c.license)
		}
	}
	for _, c := range slices.Concat(modules, packages) {
		fmt.Fprintf(&b, "\n%s\n%s %s\nLicense: %s\n%s\n", rule, c.name, c.version, c.license, rule)
		if c.note != "" {
			fmt.Fprintf(&b, "\n%s\n", c.note)
		}
		for _, t := range slices.Concat(c.licenses, c.notices) {
			fmt.Fprintf(&b, "\n-- %s --\n\n%s\n", t.name, t.body)
		}
	}
	return b.String()
}

const noticesHeading = "Notices of third-party components"

const noticesIntro = `Brokerium includes third-party components. THIRD_PARTY_NOTICES, distributed
with this file, lists each of them with its version and license and reproduces
its license files. The notice files of these components follow.
`

// withComponentNotices replaces the section of NOTICE that reproduces the notice files of the
// components, or appends it; the text above the section stays as it is.
func withComponentNotices(notice string, components []component) string {
	if i := strings.Index(notice, "\n"+noticesHeading+"\n"); i >= 0 {
		notice = notice[:i]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\n%s\n%s\n\n%s", strings.TrimRight(notice, "\n"), noticesHeading,
		strings.Repeat("-", len(noticesHeading)), noticesIntro)
	seen := map[string]bool{}
	for _, c := range components {
		for _, n := range c.notices {
			key := c.name + "\x00" + n.name + "\x00" + n.body
			if seen[key] {
				continue
			}
			seen[key] = true
			fmt.Fprintf(&b, "\n-- %s: %s --\n\n%s\n", c.name, n.name, n.body)
		}
	}
	return b.String()
}
