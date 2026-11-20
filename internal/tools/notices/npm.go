// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// standardTexts are the texts of common licenses, for packages that declare a license without
// including its text.
//
//go:embed licenses/*.txt
var standardTexts embed.FS

// pnpmPackage is a package in the output of pnpm licenses list --json, which groups packages by
// license.
type pnpmPackage struct {
	Name    string   `json:"name"`
	License string   `json:"license"`
	Paths   []string `json:"paths"`
}

type packageJSON struct {
	Name    string          `json:"name"`
	Version string          `json:"version"`
	Author  json.RawMessage `json:"author"`
	OS      []string        `json:"os"`
	CPU     []string        `json:"cpu"`
}

// npmPackages returns the production dependencies of the dashboard in dir, as installed by pnpm.
func npmPackages(dir string) ([]component, error) {
	cmd := exec.Command("pnpm", "licenses", "list", "--prod", "--json")
	cmd.Dir = dir
	out, err := output(cmd)
	if err != nil {
		return nil, err
	}
	var byLicense map[string][]pnpmPackage
	if err := json.Unmarshal(out, &byLicense); err != nil {
		return nil, fmt.Errorf("decoding the output of pnpm licenses list: %w", err)
	}
	var components []component
	for _, pkgs := range byLicense {
		for _, p := range pkgs {
			for _, path := range p.Paths {
				c, ok, err := npmComponent(p.License, path)
				if err != nil {
					return nil, fmt.Errorf("%s: %w", p.Name, err)
				}
				if ok {
					components = append(components, c)
				}
			}
		}
	}
	sortComponents(components)
	return components, nil
}

// npmComponent reads the package installed in dir. It skips packages for particular platforms:
// pnpm installs them only on matching machines, and they hold native programs rather than code of
// the dashboard.
func npmComponent(license, dir string) (component, bool, error) {
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return component{}, false, err
	}
	var manifest packageJSON
	if err := json.Unmarshal(b, &manifest); err != nil {
		return component{}, false, fmt.Errorf("decoding package.json: %w", err)
	}
	if len(manifest.OS) > 0 || len(manifest.CPU) > 0 {
		return component{}, false, nil
	}
	c := component{name: manifest.Name, version: manifest.Version, license: license}
	if err := c.addFiles(dir, dir, false); err != nil {
		return c, false, err
	}
	if len(c.licenses) == 0 {
		if err := c.addMissingLicenses(dir, author(manifest.Author)); err != nil {
			return c, false, err
		}
	}
	return c, true, nil
}

// addMissingLicenses completes a package without license files in its root: the standard text of
// each license it declares, then the license files of code it bundles in subdirectories.
func (c *component) addMissingLicenses(dir, author string) error {
	ids, err := declaredLicenses(c.license)
	if err != nil {
		return fmt.Errorf("%s %s: %w", c.name, c.version, err)
	}
	for _, id := range ids {
		body, err := standardTexts.ReadFile("licenses/" + id + ".txt")
		if err != nil {
			return fmt.Errorf("%s %s includes no license file, and internal/tools/notices/licenses has no text of %s",
				c.name, c.version, id)
		}
		c.licenses = append(c.licenses, text{name: id + " (standard text)", body: normalize(body)})
	}
	c.note = "The package includes no license file of its own. Its package.json declares the license " + c.license
	if author != "" {
		c.note += " and the author " + author
	}
	if len(ids) == 1 {
		c.note += "; the standard text of " + ids[0] + " follows."
	} else {
		c.note += "; the standard texts of " + strings.Join(ids, " and ") + " follow."
	}
	return filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() == "node_modules":
			return filepath.SkipDir
		case d.IsDir() && path != dir:
			return c.addFiles(dir, path, true)
		}
		return nil
	})
}

// declaredLicenses returns the licenses of an SPDX expression; of an OR expression, those of its
// first alternative.
func declaredLicenses(expr string) ([]string, error) {
	var ids []string
	exception := false
	for _, token := range strings.Fields(strings.NewReplacer("(", " ", ")", " ").Replace(expr)) {
		if strings.EqualFold(token, "OR") {
			break
		}
		switch {
		case exception:
			exception = false
		case strings.EqualFold(token, "WITH"):
			exception = true
		case !strings.EqualFold(token, "AND"):
			ids = append(ids, strings.TrimSuffix(token, "+"))
		}
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("the license %q names no license", expr)
	}
	return ids, nil
}

// author returns the author of package.json, a string or an object with a name and an email.
func author(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var person struct{ Name, Email string }
	if json.Unmarshal(raw, &person) != nil || person.Name == "" {
		return ""
	}
	if person.Email != "" {
		return person.Name + " <" + person.Email + ">"
	}
	return person.Name
}
