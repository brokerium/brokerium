// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
)

var serverPlatforms = []string{"darwin/amd64", "darwin/arm64", "linux/amd64", "linux/arm64"}

// releases are the binaries that .github/workflows/release.yml and the images build: the package,
// its build tags and its platforms.
var releases = []struct {
	pkg, tags string
	platforms []string
}{
	{"./cmd/brokerium-server", "", serverPlatforms},
	{"./cmd/brokerium-server", "enterprise", serverPlatforms},
	{"./cmd/brokerium-agent", "", []string{"linux/amd64", "linux/arm64"}},
}

type goPackage struct {
	Dir      string
	Standard bool
	Module   *goModule
}

type goModule struct {
	Path    string
	Version string
	Dir     string
	Main    bool
	Replace *goModule
}

// goModules returns the Go standard library and the modules of the packages linked into the
// released binaries.
func goModules() ([]component, error) {
	type module struct {
		path, version, dir string
		pkgDirs            map[string]bool
	}
	modules := map[string]*module{}
	for _, r := range releases {
		for _, platform := range r.platforms {
			pkgs, err := listDeps(r.pkg, r.tags, platform)
			if err != nil {
				return nil, err
			}
			for _, p := range pkgs {
				m := p.Module
				if p.Standard || m == nil || m.Main {
					continue
				}
				if m.Replace != nil {
					m = m.Replace
				}
				key := m.Path + "@" + m.Version
				if modules[key] == nil {
					modules[key] = &module{path: m.Path, version: m.Version, dir: m.Dir, pkgDirs: map[string]bool{}}
				}
				modules[key].pkgDirs[p.Dir] = true
			}
		}
	}
	var components []component
	for _, m := range modules {
		c, err := goComponent(m.path, m.version, m.dir, slices.Collect(maps.Keys(m.pkgDirs)))
		if err != nil {
			return nil, err
		}
		components = append(components, c)
	}
	sortComponents(components)
	std, err := goStandardLibrary()
	if err != nil {
		return nil, err
	}
	return append([]component{std}, components...), nil
}

// listDeps lists the packages that the build of pkg with tags for platform links.
func listDeps(pkg, tags, platform string) ([]goPackage, error) {
	goos, goarch, _ := strings.Cut(platform, "/")
	cmd := exec.Command("go", "list", "-deps", "-tags="+tags, "-json=Dir,Standard,Module", pkg)
	cmd.Env = append(os.Environ(), "GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0")
	out, err := output(cmd)
	if err != nil {
		return nil, err
	}
	var pkgs []goPackage
	for dec := json.NewDecoder(bytes.NewReader(out)); dec.More(); {
		var p goPackage
		if err := dec.Decode(&p); err != nil {
			return nil, fmt.Errorf("decoding the output of go list: %w", err)
		}
		pkgs = append(pkgs, p)
	}
	return pkgs, nil
}

// goComponent reads the license and notice files of a module: those of its root and of every
// directory from the root down to a linked package, which covers code that the module took from
// elsewhere together with its license.
func goComponent(name, version, root string, pkgDirs []string) (component, error) {
	dirs := map[string]bool{root: true}
	for _, dir := range pkgDirs {
		for ; strings.HasPrefix(dir, root+string(filepath.Separator)); dir = filepath.Dir(dir) {
			dirs[dir] = true
		}
	}
	c := component{name: name, version: version}
	for _, dir := range slices.Sorted(maps.Keys(dirs)) {
		if err := c.addFiles(root, dir, false); err != nil {
			return c, fmt.Errorf("%s %s: %w", name, version, err)
		}
	}
	if len(c.licenses) == 0 {
		return c, fmt.Errorf("%s %s has no license file", name, version)
	}
	c.license = expression(licenseIDs(c.licenses))
	return c, nil
}

// goStandardLibrary describes the standard library and runtime of the Go toolchain, which every
// binary links.
func goStandardLibrary() (component, error) {
	out, err := output(exec.Command("go", "env", "GOROOT", "GOVERSION"))
	if err != nil {
		return component{}, err
	}
	root, version, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	c := component{name: "Go standard library", version: version}
	if err := c.addFiles(root, root, true); err != nil {
		return c, fmt.Errorf("reading the license of the Go standard library: %w", err)
	}
	c.license = expression(licenseIDs(c.licenses))
	return c, nil
}
