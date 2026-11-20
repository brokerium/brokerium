// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package kafkaconfig

import (
	"bytes"
	"strings"
	"testing"
)

// supported are the minor releases Brokerium supports (ADR 0022); each needs a catalog.
var supported = []string{"3.5", "3.6", "3.7", "3.8", "3.9", "4.0", "4.1", "4.2", "4.3", "4.4"}

func TestEveryLineLoads(t *testing.T) {
	t.Parallel()
	if lines := Lines(); len(lines) == 0 || lines[0] != "3.5" {
		t.Fatalf("Lines() = %v, want the oldest line 3.5 first", lines)
	}
	for _, e := range embedded() {
		line := e.release.String()
		if _, err := load("catalog/"+line+".json", e.release); err != nil {
			t.Errorf("%s: %v", line, err)
		}
	}
	for _, line := range supported {
		c, exact := For(line)
		if c == nil || !exact || c.Line != line {
			t.Errorf("For(%q) = %v, %t; want the catalog of the line", line, c, exact)
			continue
		}
		if len(c.Broker) < 200 || len(c.Topic) < 20 {
			t.Errorf("%s: %d broker and %d topic keys, fewer than any release has", line, len(c.Broker), len(c.Topic))
		}
	}
}

// TestFilesAreCanonical keeps the files in the form Marshal writes, so that regenerated files
// differ only where Kafka changed.
func TestFilesAreCanonical(t *testing.T) {
	t.Parallel()
	for _, line := range Lines() {
		name := "catalog/" + line + ".json"
		data, err := files.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		c, err := Parse(data)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out, err := Marshal(c)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if !bytes.Equal(out, data) {
			t.Errorf("%s is not in the form Marshal writes; regenerate it with make catalog", name)
		}
	}
}

func str(s string) *string { return &s }

func TestBrokerKeys(t *testing.T) {
	t.Parallel()
	// Each row holds for the lines from "from" up to and without "to".
	tests := []struct {
		name      string
		from, to  string
		mode      UpdateMode
		def       *string
		typ       string
		sensitive bool
	}{
		{name: "log.segment.bytes", mode: ClusterWide, def: str("1073741824"), typ: "int"},
		{name: "num.io.threads", mode: ClusterWide, def: str("8"), typ: "int"},
		{name: "num.network.threads", mode: ClusterWide, def: str("3"), typ: "int"},
		{name: "min.insync.replicas", mode: ClusterWide, def: str("1"), typ: "int"},
		{name: "log.retention.ms", mode: ClusterWide, def: nil, typ: "long"},
		{name: "listeners", to: "4.2", mode: PerBroker, def: str("PLAINTEXT://:9092"), typ: "string"},
		{name: "listeners", from: "4.2", mode: PerBroker, def: str("PLAINTEXT://:9092"), typ: "list"},
		{name: "advertised.listeners", to: "4.0", mode: PerBroker, def: nil, typ: "string"},
		{name: "advertised.listeners", from: "4.0", to: "4.2", mode: ReadOnly, def: nil, typ: "string"},
		{name: "advertised.listeners", from: "4.2", mode: ReadOnly, def: nil, typ: "list"},
		{name: "auto.create.topics.enable", mode: ReadOnly, def: str("true"), typ: "boolean"},
		{name: "log.retention.hours", mode: ReadOnly, def: str("168"), typ: "int"},
		{name: "socket.send.buffer.bytes", mode: ReadOnly, def: str("102400"), typ: "int"},
		{name: "socket.receive.buffer.bytes", mode: ReadOnly, def: str("102400"), typ: "int"},
		{name: "num.partitions", mode: ReadOnly, def: str("1"), typ: "int"},
		{name: "default.replication.factor", mode: ReadOnly, def: str("1"), typ: "int"},
		{name: "group.initial.rebalance.delay.ms", mode: ReadOnly, def: str("3000"), typ: "int"},
		{name: "offsets.topic.replication.factor", mode: ReadOnly, def: str("3"), typ: "short"},
		{name: "transaction.state.log.min.isr", mode: ReadOnly, def: str("2"), typ: "int"},
		{name: "num.recovery.threads.per.data.dir", to: "4.0", mode: ClusterWide, def: str("1"), typ: "int"},
		{name: "num.recovery.threads.per.data.dir", from: "4.0", mode: ClusterWide, def: str("2"), typ: "int"},
		{name: "process.roles", to: "4.0", mode: ReadOnly, def: str(""), typ: "list"},
		{name: "process.roles", from: "4.0", mode: ReadOnly, def: nil, typ: "list"},
		{name: "ssl.keystore.password", mode: PerBroker, def: nil, typ: "password", sensitive: true},
	}
	for _, line := range supported {
		c, _ := For(line)
		for _, tt := range tests {
			if !between(line, tt.from, tt.to) {
				continue
			}
			k, ok := c.Key(Broker, tt.name)
			switch {
			case !ok:
				t.Errorf("%s: no broker key %s", line, tt.name)
			case k.UpdateMode != tt.mode || k.ReadOnly != (tt.mode == ReadOnly):
				t.Errorf("%s: %s has the update mode %s, read-only %t; want %s", line, tt.name, k.UpdateMode, k.ReadOnly, tt.mode)
			case show(k.Default) != show(tt.def) || k.Type != tt.typ || k.Sensitive != tt.sensitive:
				t.Errorf("%s: %s is %s with default %s, sensitive %t; want %s with %s, sensitive %t",
					line, tt.name, k.Type, show(k.Default), k.Sensitive, tt.typ, show(tt.def), tt.sensitive)
			case k.Documentation == "" || k.Importance == "":
				t.Errorf("%s: %s has no documentation or importance", line, tt.name)
			}
		}
	}
}

func TestTopicKeys(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		def  string
		typ  string
	}{
		{"cleanup.policy", "delete", "list"},
		{"retention.ms", "604800000", "long"},
		{"retention.bytes", "-1", "long"},
		{"min.insync.replicas", "1", "int"},
		{"segment.bytes", "1073741824", "int"},
		{"segment.ms", "604800000", "long"},
		{"max.message.bytes", "1048588", "int"},
	}
	for _, line := range supported {
		c, _ := For(line)
		for _, tt := range tests {
			k, ok := c.Key(Topic, tt.name)
			switch {
			case !ok:
				t.Errorf("%s: no topic key %s", line, tt.name)
			case show(k.Default) != show(&tt.def) || k.Type != tt.typ:
				t.Errorf("%s: topic key %s is %s with default %s; want %s with %q", line, tt.name, k.Type, show(k.Default), tt.typ, tt.def)
			case k.ReadOnly || k.UpdateMode != "" || k.Documentation == "":
				t.Errorf("%s: topic key %s: %+v", line, tt.name, k)
			}
		}
	}
}

// TestRemovedKeys checks keys that a release dropped.
func TestRemovedKeys(t *testing.T) {
	t.Parallel()
	for _, line := range supported {
		c, _ := For(line)
		_, zk := c.Key(Broker, "zookeeper.connect")
		_, format := c.Key(Topic, "message.format.version")
		if old := between(line, "", "4.0"); zk != old || format != old {
			t.Errorf("%s: zookeeper.connect %t, message.format.version %t; want %t (removed in 4.0)", line, zk, format, old)
		}
	}
}

func TestKey(t *testing.T) {
	t.Parallel()
	c, _ := For("4.4.0")
	if k, ok := c.Key(Broker, "num.partitions"); !ok || k.Name != "num.partitions" {
		t.Errorf("Key(broker, num.partitions) = %+v, %t", k, ok)
	}
	for _, tt := range []struct {
		scope Scope
		name  string
	}{{Topic, "num.partitions"}, {Broker, "no.such.key"}, {Broker, ""}, {"group", "num.partitions"}} {
		if k, ok := c.Key(tt.scope, tt.name); ok {
			t.Errorf("Key(%s, %q) = %+v", tt.scope, tt.name, k)
		}
	}
	if keys := c.Keys("group"); keys != nil {
		t.Errorf("Keys(group) = %d keys", len(keys))
	}
}

func TestFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		version string
		line    string
		exact   bool
	}{
		{"4.3.2", "4.3", true},
		{"4.3", "4.3", true},
		{"4.3.1-rc1", "4.3", true},
		{"3.5.0", "3.5", true},
		{"3.9.2", "3.9", true},
		{"4.0.0", "4.0", true},
		{"4.10.0", "4.4", false},
		{"5.0.0", "4.4", false},
		{"3.4.1", "", false},
		{"2.8.2", "", false},
		{"", "", false},
		{"4", "", false},
		{"4.4 or later", "", false},
		{"v4.3.1", "", false},
		{"+4.3", "", false},
	}
	for _, tt := range tests {
		c, exact := For(tt.version)
		line := ""
		if c != nil {
			line = c.Line
		}
		if line != tt.line || exact != tt.exact {
			t.Errorf("For(%q) = %q, %t; want %q, %t", tt.version, line, exact, tt.line, tt.exact)
		}
	}
}

func TestClosestOlder(t *testing.T) {
	t.Parallel()
	entries := []entry{{release: release{3, 5}}, {release: release{3, 7}}, {release: release{4, 0}}}
	tests := []struct {
		want  release
		found string
		exact bool
	}{
		{release{3, 4}, "", false},
		{release{3, 5}, "3.5", true},
		{release{3, 6}, "3.5", false},
		{release{3, 7}, "3.7", true},
		{release{3, 9}, "3.7", false},
		{release{4, 0}, "4.0", true},
		{release{4, 1}, "4.0", false},
	}
	for _, tt := range tests {
		e, exact := closestOlder(entries, tt.want)
		found := ""
		if e != nil {
			found = e.release.String()
		}
		if found != tt.found || exact != tt.exact {
			t.Errorf("closestOlder(%s) = %q, %t; want %q, %t", tt.want, found, exact, tt.found, tt.exact)
		}
	}
}

func TestParseRejects(t *testing.T) {
	t.Parallel()
	valid := func() *Catalog {
		return &Catalog{
			Line: "4.4", Version: "4.4.0",
			Docs: Docs{Line: "4.4", Broker: "https://kafka.apache.org/44/generated/kafka_config.html"},
			Broker: []Key{
				{Name: "a.key", Type: "int", Default: str("1"), UpdateMode: ClusterWide},
				{Name: "b.key", Type: "string", ReadOnly: true, UpdateMode: ReadOnly},
			},
			Topic: []Key{{Name: "c.key", Type: "long", Default: str("-1")}},
		}
	}
	data, err := Marshal(valid())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(data); err != nil {
		t.Fatalf("Parse of a valid catalog: %v", err)
	}
	tests := []struct {
		name   string
		change func(c *Catalog)
		want   string
	}{
		{"line", func(c *Catalog) { c.Line = "4.4.0" }, "not a minor release"},
		{"version", func(c *Catalog) { c.Version = "4.3.2" }, "not a release of the line"},
		{"docs", func(c *Catalog) { c.Docs = Docs{} }, "documentation"},
		{"repeated", func(c *Catalog) { c.Broker[1].Name = "a.key" }, "out of order or repeated"},
		{"mode", func(c *Catalog) { c.Broker[0].UpdateMode = "per-controller" }, "update mode"},
		{"read-only", func(c *Catalog) { c.Broker[0].ReadOnly = true }, "read-only true"},
		{"topic mode", func(c *Catalog) { c.Topic[0].UpdateMode = ClusterWide }, "topic key"},
		{"type", func(c *Catalog) { c.Topic[0].Type = "" }, "no name or type"},
		{"no topic keys", func(c *Catalog) { c.Topic = nil }, "no topic keys"},
	}
	for _, tt := range tests {
		c := valid()
		tt.change(c)
		if _, err := Marshal(c); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: Marshal error %v, want %q", tt.name, err, tt.want)
		}
	}
	if _, err := Parse(bytes.Replace(data, []byte(`"line"`), []byte(`"lines"`), 1)); err == nil {
		t.Error("Parse accepted an unknown field")
	}
}

// between reports whether a line is within [from, to); empty bounds are open.
func between(line, from, to string) bool {
	l, _ := parseVersion(line)
	if f, ok := parseVersion(from); ok && l.compare(f) < 0 {
		return false
	}
	if u, ok := parseVersion(to); ok && l.compare(u) >= 0 {
		return false
	}
	return true
}

func show(s *string) string {
	if s == nil {
		return "none"
	}
	return `"` + *s + `"`
}
