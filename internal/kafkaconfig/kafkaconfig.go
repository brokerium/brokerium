// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package kafkaconfig is the catalog of Kafka configuration keys (ADR 0025): for every
// supported minor release of Kafka, the broker and topic keys with their type, default,
// documentation, importance, sensitivity and update mode.
//
// The catalogs are data, one file per minor release in catalog/<line>.json, embedded in the
// server. The command internal/tools/catalog (make catalog) generates them from Kafka itself:
// it starts each release in Docker, reads DescribeConfigs of a broker and of a new topic, and
// takes the update modes and the importance of the keys from the configuration pages of the
// Kafka documentation of that release. Adding a release means generating one more file.
package kafkaconfig

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strconv"
	"strings"
	"sync"
)

//go:embed catalog/*.json
var files embed.FS

// Scope is the kind of resource a key configures.
type Scope string

// Scopes of keys.
const (
	Broker Scope = "broker"
	Topic  Scope = "topic"
)

// UpdateMode says how a broker key changes, in the terms of the Kafka documentation.
type UpdateMode string

// Update modes of broker keys.
const (
	// ReadOnly keys change in server.properties and take effect when the broker restarts.
	ReadOnly UpdateMode = "read-only"
	// PerBroker keys change at runtime for one broker at a time.
	PerBroker UpdateMode = "per-broker"
	// ClusterWide keys change at runtime as the default of the cluster, or for one broker.
	ClusterWide UpdateMode = "cluster-wide"
)

// Key is a configuration key of one Kafka release.
type Key struct {
	Name string `json:"name"`
	// Type is the type Kafka reports: boolean, string, int, short, long, double, list, class
	// or password.
	Type string `json:"type"`
	// Default is the default value as Kafka renders it, nil when the key has no default. A
	// topic key defaults to the default of the broker key it inherits from.
	Default       *string `json:"default"`
	Documentation string  `json:"documentation"`
	// Importance is high, medium or low, as the Kafka documentation lists the key; empty when
	// the documentation does not list it.
	Importance string `json:"importance,omitempty"`
	// Sensitive keys are passwords and other secrets; Kafka never returns their values.
	Sensitive bool `json:"sensitive"`
	// ReadOnly keys cannot change at runtime. Topic keys never are.
	ReadOnly bool `json:"readOnly"`
	// UpdateMode is set for broker keys only.
	UpdateMode UpdateMode `json:"updateMode,omitempty"`
}

// Catalog lists the configuration keys of one minor release of Kafka. The catalogs that For
// returns are shared: callers must not modify them.
type Catalog struct {
	// Line is the minor release, such as "4.3".
	Line string `json:"line"`
	// Version is the release the catalog was generated from, such as "4.3.2".
	Version string `json:"version"`
	// Docs are the pages of the Kafka documentation the update modes and importance come from.
	Docs Docs `json:"docs"`
	// Broker and Topic are the keys of each scope, sorted by name.
	Broker []Key `json:"broker"`
	Topic  []Key `json:"topic"`
}

// Docs names the configuration pages of the Kafka documentation a catalog was generated with.
type Docs struct {
	// Line is the release line of the documentation. It differs from the line of the catalog
	// when the documentation of that line has no configuration pages.
	Line   string `json:"line"`
	Broker string `json:"broker"`
	Topic  string `json:"topic"`
}

// Keys lists the keys of a scope sorted by name. The slice belongs to the catalog.
func (c *Catalog) Keys(scope Scope) []Key {
	switch scope {
	case Broker:
		return c.Broker
	case Topic:
		return c.Topic
	}
	return nil
}

// Key returns a key of a scope.
func (c *Catalog) Key(scope Scope, name string) (Key, bool) {
	keys := c.Keys(scope)
	i, ok := slices.BinarySearchFunc(keys, name, func(k Key, name string) int { return strings.Compare(k.Name, name) })
	if !ok {
		return Key{}, false
	}
	return keys[i], true
}

// For returns the catalog of a Kafka release such as "4.3.1" or "4.3": the catalog of its minor
// release with exact set, or else the catalog of the closest older release. It returns nil for
// releases older than every catalog and for versions it cannot read.
func For(version string) (c *Catalog, exact bool) {
	want, ok := parseVersion(version)
	if !ok {
		return nil, false
	}
	e, exact := closestOlder(embedded(), want)
	if e == nil {
		return nil, false
	}
	return e.load(), exact
}

// closestOlder finds the entry of a release in entries sorted oldest first, or else the entry of
// the closest older release.
func closestOlder(entries []entry, want release) (e *entry, exact bool) {
	for i := len(entries) - 1; i >= 0; i-- {
		if c := entries[i].release.compare(want); c <= 0 {
			return &entries[i], c == 0
		}
	}
	return nil, false
}

// Lines lists the minor releases that have a catalog, oldest first.
func Lines() []string {
	all := embedded()
	out := make([]string, len(all))
	for i, e := range all {
		out[i] = e.release.String()
	}
	return out
}

// Marshal renders a catalog in the form of the embedded files: keys sorted by name, indented,
// without HTML escaping, so that regenerated files differ only where Kafka changed.
func Marshal(c *Catalog) ([]byte, error) {
	out := *c
	byName := func(a, b Key) int { return strings.Compare(a.Name, b.Name) }
	out.Broker = slices.SortedFunc(slices.Values(c.Broker), byName)
	out.Topic = slices.SortedFunc(slices.Values(c.Topic), byName)
	if err := out.validate(); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, fmt.Errorf("encoding the catalog of %s: %w", c.Line, err)
	}
	return buf.Bytes(), nil
}

// Parse reads a catalog in the form Marshal writes and checks it.
func Parse(data []byte) (*Catalog, error) {
	var c Catalog
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("decoding the catalog: %w", err)
	}
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Catalog) validate() error {
	r, ok := parseVersion(c.Line)
	if !ok || r.String() != c.Line {
		return fmt.Errorf("catalog line %q is not a minor release", c.Line)
	}
	if _, ok := parseVersion(c.Version); !ok || !strings.HasPrefix(c.Version, c.Line+".") {
		return fmt.Errorf("catalog %s: version %q is not a release of the line", c.Line, c.Version)
	}
	if c.Docs.Line == "" || c.Docs.Broker == "" {
		return fmt.Errorf("catalog %s: the documentation it was generated with is missing", c.Line)
	}
	var errs []error
	for _, scope := range []Scope{Broker, Topic} {
		keys := c.Keys(scope)
		if len(keys) == 0 {
			errs = append(errs, fmt.Errorf("no %s keys", scope))
		}
		for i, k := range keys {
			if i > 0 && keys[i-1].Name >= k.Name {
				errs = append(errs, fmt.Errorf("%s key %q is out of order or repeated", scope, k.Name))
			}
			if k.Name == "" || k.Type == "" {
				errs = append(errs, fmt.Errorf("%s key %q has no name or type", scope, k.Name))
			}
			switch {
			case scope == Topic && (k.UpdateMode != "" || k.ReadOnly):
				errs = append(errs, fmt.Errorf("topic key %q is read-only or has an update mode", k.Name))
			case scope == Broker && !slices.Contains([]UpdateMode{ReadOnly, PerBroker, ClusterWide}, k.UpdateMode):
				errs = append(errs, fmt.Errorf("broker key %q has the update mode %q", k.Name, k.UpdateMode))
			case scope == Broker && k.ReadOnly != (k.UpdateMode == ReadOnly):
				errs = append(errs, fmt.Errorf("broker key %q is read-only %t with the update mode %s", k.Name, k.ReadOnly, k.UpdateMode))
			}
		}
	}
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("catalog %s: %w", c.Line, err)
	}
	return nil
}

// entry is an embedded catalog, parsed on first use.
type entry struct {
	release release
	load    func() *Catalog
}

// embedded lists the embedded catalogs, oldest first. The files are part of the build and
// checked by the tests, so a file that does not parse is a defect of the build: it panics.
var embedded = sync.OnceValue(func() []entry {
	names, err := fs.Glob(files, "catalog/*.json")
	if err != nil {
		panic(err)
	}
	out := make([]entry, 0, len(names))
	for _, name := range names {
		r, ok := parseVersion(strings.TrimSuffix(path.Base(name), ".json"))
		if !ok {
			panic(fmt.Sprintf("kafkaconfig: %s is not named after a minor release", name))
		}
		out = append(out, entry{release: r, load: sync.OnceValue(func() *Catalog {
			c, err := load(name, r)
			if err != nil {
				panic(fmt.Sprintf("kafkaconfig: %s: %v", name, err))
			}
			return c
		})})
	}
	slices.SortFunc(out, func(a, b entry) int { return a.release.compare(b.release) })
	return out
})

func load(name string, r release) (*Catalog, error) {
	data, err := files.ReadFile(name)
	if err != nil {
		return nil, err
	}
	c, err := Parse(data)
	if err != nil {
		return nil, err
	}
	if c.Line != r.String() {
		return nil, fmt.Errorf("the file holds the catalog of %s", c.Line)
	}
	return c, nil
}

// release is a minor release of Kafka.
type release struct{ major, minor int }

func (r release) compare(o release) int {
	if r.major != o.major {
		return r.major - o.major
	}
	return r.minor - o.minor
}

func (r release) String() string { return strconv.Itoa(r.major) + "." + strconv.Itoa(r.minor) }

// parseVersion reads the minor release of "4.3", "4.3.1" or "4.3.1-rc1".
func parseVersion(v string) (release, bool) {
	major, rest, ok := strings.Cut(v, ".")
	minor := rest
	if i := strings.IndexFunc(rest, func(r rune) bool { return r < '0' || r > '9' }); i >= 0 {
		if rest[i] != '.' && rest[i] != '-' {
			return release{}, false
		}
		minor = rest[:i]
	}
	ma, ok1 := number(major)
	mi, ok2 := number(minor)
	return release{ma, mi}, ok && ok1 && ok2
}

func number(s string) (int, bool) {
	if s == "" || strings.Trim(s, "0123456789") != "" {
		return 0, false
	}
	n, err := strconv.Atoi(s)
	return n, err == nil
}
