// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/brokerium/brokerium/internal/kafkaconfig"
)

func str(s string) *string { return &s }

func synonym(name string, value *string, source kmsg.ConfigSource) kmsg.DescribeConfigsResponseResourceConfigConfigSynonym {
	return kmsg.DescribeConfigsResponseResourceConfigConfigSynonym{Name: name, Value: value, Source: source}
}

func config(name string, value *string, source kmsg.ConfigSource, typ kmsg.ConfigType, readOnly bool,
	synonyms ...kmsg.DescribeConfigsResponseResourceConfigConfigSynonym,
) describedConfig {
	return describedConfig{Name: name, Value: value, Source: source, ConfigType: typ, ReadOnly: readOnly,
		IsSensitive: typ == kmsg.ConfigTypePassword, ConfigSynonyms: synonyms, Documentation: str("About " + name + ".")}
}

const (
	defaultSource = kmsg.ConfigSourceDefaultConfig
	staticSource  = kmsg.ConfigSourceStaticBrokerConfig
)

// The responses of a node of 4.1 or later, trimmed to the cases that matter.
var (
	testBroker = []describedConfig{
		config("num.partitions", str("1"), defaultSource, kmsg.ConfigTypeInt, true,
			synonym("num.partitions", str("1"), defaultSource)),
		// Set by the node; the default is the synonym.
		config("listeners", str("PLAINTEXT://:9092,CONTROLLER://:9093"), staticSource, kmsg.ConfigTypeList, false,
			synonym("listeners", str("PLAINTEXT://:9092,CONTROLLER://:9093"), staticSource),
			synonym("listeners", str("PLAINTEXT://:9092"), defaultSource)),
		// Set by the node and without a default.
		config("node.id", str("1"), staticSource, kmsg.ConfigTypeInt, true, synonym("node.id", str("1"), staticSource)),
		// No default of its own: the synonym of log.retention.hours is dropped.
		config("log.retention.ms", nil, defaultSource, kmsg.ConfigTypeLong, false),
		config("ssl.keystore.password", nil, defaultSource, kmsg.ConfigTypePassword, false),
		// A new cluster records the default of min.insync.replicas as a cluster-wide value.
		config("min.insync.replicas", str("1"), kmsg.ConfigSourceDynamicDefaultBrokerConfig, kmsg.ConfigTypeInt, false,
			synonym("min.insync.replicas", str("1"), kmsg.ConfigSourceDynamicDefaultBrokerConfig),
			synonym("min.insync.replicas", str("1"), defaultSource)),
		// Not in the documentation.
		config("hidden.dynamic.key", str("x"), defaultSource, kmsg.ConfigTypeString, false,
			synonym("hidden.dynamic.key", str("x"), defaultSource)),
		// The documentation calls it dynamic.
		config("misdocumented.key", str("5"), defaultSource, kmsg.ConfigTypeInt, true,
			synonym("misdocumented.key", str("5"), defaultSource)),
	}
	testTopic = []describedConfig{
		config("cleanup.policy", str("delete"), defaultSource, kmsg.ConfigTypeList, false,
			synonym("log.cleanup.policy", str("delete"), defaultSource)),
		config("retention.ms", str("604800000"), defaultSource, kmsg.ConfigTypeLong, false),
		config("min.insync.replicas", str("1"), kmsg.ConfigSourceDynamicDefaultBrokerConfig, kmsg.ConfigTypeInt, false,
			synonym("min.insync.replicas", str("1"), kmsg.ConfigSourceDynamicDefaultBrokerConfig),
			synonym("min.insync.replicas", str("1"), defaultSource)),
	}
	testDocs = docs{
		source: kafkaconfig.Docs{Line: "4.4", Broker: "broker.html", Topic: "topic.html"},
		broker: map[string]docKey{
			"num.partitions":        {typ: "int", def: "1", importance: "medium", updateMode: "read-only"},
			"listeners":             {typ: "list", def: "PLAINTEXT://:9092", importance: "high", updateMode: "per-broker"},
			"node.id":               {typ: "int", importance: "high", updateMode: "read-only"},
			"log.retention.ms":      {typ: "long", def: "null", importance: "high", updateMode: "cluster-wide"},
			"ssl.keystore.password": {typ: "password", def: "null", importance: "high", updateMode: "per-broker"},
			"min.insync.replicas":   {typ: "int", def: "1", importance: "high", updateMode: "cluster-wide"},
			"misdocumented.key":     {typ: "int", def: "5", importance: "low", updateMode: "cluster-wide"},
			// Kafka reports it only when it is set.
			"leader.replication.throttled.rate": {typ: "long", def: "9223372036854775807", importance: "medium", updateMode: "cluster-wide"},
		},
		topic: map[string]docKey{
			"cleanup.policy":      {typ: "list", def: "delete", importance: "medium"},
			"retention.ms":        {typ: "long", def: "604800000 (7 days)", importance: "medium"},
			"min.insync.replicas": {typ: "int", def: "1", importance: "medium"},
			"removed.key":         {typ: "int", def: "1", importance: "low"},
		},
	}
)

func TestBuildCatalog(t *testing.T) {
	t.Parallel()
	c, notes, err := buildCatalog(version{4, 4, 0}, testDocs, testBroker, testTopic)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := kafkaconfig.Marshal(c); err != nil {
		t.Fatalf("the catalog does not validate: %v", err)
	}
	tests := []struct {
		scope      kafkaconfig.Scope
		name       string
		def        *string
		mode       kafkaconfig.UpdateMode
		importance string
	}{
		{kafkaconfig.Broker, "num.partitions", str("1"), kafkaconfig.ReadOnly, "medium"},
		{kafkaconfig.Broker, "listeners", str("PLAINTEXT://:9092"), kafkaconfig.PerBroker, "high"},
		{kafkaconfig.Broker, "node.id", nil, kafkaconfig.ReadOnly, "high"},
		{kafkaconfig.Broker, "log.retention.ms", nil, kafkaconfig.ClusterWide, "high"},
		{kafkaconfig.Broker, "ssl.keystore.password", nil, kafkaconfig.PerBroker, "high"},
		{kafkaconfig.Broker, "min.insync.replicas", str("1"), kafkaconfig.ClusterWide, "high"},
		{kafkaconfig.Broker, "hidden.dynamic.key", str("x"), kafkaconfig.PerBroker, ""},
		{kafkaconfig.Broker, "misdocumented.key", str("5"), kafkaconfig.ReadOnly, "low"},
		{kafkaconfig.Topic, "cleanup.policy", str("delete"), "", "medium"},
		{kafkaconfig.Topic, "retention.ms", str("604800000"), "", "medium"},
		{kafkaconfig.Topic, "min.insync.replicas", str("1"), "", "medium"},
	}
	for _, tt := range tests {
		k, ok := c.Key(tt.scope, tt.name)
		switch {
		case !ok:
			t.Errorf("no %s key %s", tt.scope, tt.name)
		case show(k.Default) != show(tt.def) || k.UpdateMode != tt.mode || k.Importance != tt.importance:
			t.Errorf("%s key %s: default %s, mode %q, importance %q; want %s, %q, %q",
				tt.scope, tt.name, show(k.Default), k.UpdateMode, k.Importance, show(tt.def), tt.mode, tt.importance)
		case k.Documentation != "About "+tt.name+".":
			t.Errorf("%s key %s: documentation %q", tt.scope, tt.name, k.Documentation)
		}
	}
	if k, _ := c.Key(kafkaconfig.Broker, "ssl.keystore.password"); !k.Sensitive || k.Type != "password" {
		t.Errorf("ssl.keystore.password: %+v", k)
	}
	wantNotes := []string{
		"broker key hidden.dynamic.key is not in the documentation",
		`broker key misdocumented.key: the documentation says "cluster-wide", Kafka reports it read-only`,
		"the documentation lists the topic key removed.key",
	}
	for _, want := range wantNotes {
		if !slices.ContainsFunc(notes, func(n string) bool { return strings.Contains(n, want) }) {
			t.Errorf("no note %q in %q", want, notes)
		}
	}
	if len(notes) != len(wantNotes) {
		t.Errorf("notes %q, want %d", notes, len(wantNotes))
	}
}

func TestBuildCatalogRefusesChangedDefaults(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		broker describedConfig
		topic  describedConfig
		want   string
	}{
		{
			name: "static key the node does not set",
			broker: config("log.retention.hours", str("24"), staticSource, kmsg.ConfigTypeInt, true,
				synonym("log.retention.hours", str("24"), staticSource), synonym("log.retention.hours", str("168"), defaultSource)),
			want: "set in server.properties",
		},
		{
			name: "dynamic broker value",
			broker: config("num.io.threads", str("4"), kmsg.ConfigSourceDynamicBrokerConfig, kmsg.ConfigTypeInt, false,
				synonym("num.io.threads", str("4"), kmsg.ConfigSourceDynamicBrokerConfig), synonym("num.io.threads", str("8"), defaultSource)),
			want: "comes from DYNAMIC_BROKER_CONFIG",
		},
		{
			name: "cluster-wide value other than the default",
			broker: config("min.insync.replicas", str("2"), kmsg.ConfigSourceDynamicDefaultBrokerConfig, kmsg.ConfigTypeInt, false,
				synonym("min.insync.replicas", str("2"), kmsg.ConfigSourceDynamicDefaultBrokerConfig), synonym("min.insync.replicas", str("1"), defaultSource)),
			want: "comes from DYNAMIC_DEFAULT_BROKER_CONFIG",
		},
		{
			name:  "topic value",
			topic: config("retention.ms", str("1000"), kmsg.ConfigSourceDynamicTopicConfig, kmsg.ConfigTypeLong, false),
			want:  "topic key retention.ms comes from DYNAMIC_TOPIC_CONFIG",
		},
		{
			name:   "unknown type",
			broker: config("custom.key", str("x"), defaultSource, kmsg.ConfigTypeUnknown, false),
			want:   "key custom.key has no type",
		},
	}
	for _, tt := range tests {
		broker, topic := slices.Clone(testBroker), slices.Clone(testTopic)
		if tt.broker.Name != "" {
			broker = append(broker, tt.broker)
		}
		if tt.topic.Name != "" {
			topic = append(topic, tt.topic)
		}
		if _, _, err := buildCatalog(version{4, 4, 0}, testDocs, broker, topic); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: error %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestUpdateMode(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		readOnly   bool
		doc        string
		documented bool
		want       kafkaconfig.UpdateMode
		note       bool
	}{
		{"documented read-only", true, "read-only", true, kafkaconfig.ReadOnly, false},
		{"documented per-broker", false, "per-broker", true, kafkaconfig.PerBroker, false},
		{"documented cluster-wide", false, "cluster-wide", true, kafkaconfig.ClusterWide, false},
		{"read-only, documented dynamic", true, "cluster-wide", true, kafkaconfig.ReadOnly, true},
		{"dynamic, documented read-only", false, "read-only", true, kafkaconfig.PerBroker, true},
		{"dynamic, unknown mode", false, "per-controller", true, kafkaconfig.PerBroker, true},
		{"undocumented read-only", true, "", false, kafkaconfig.ReadOnly, true},
		{"undocumented dynamic", false, "", false, kafkaconfig.PerBroker, true},
	}
	for _, tt := range tests {
		got, note := updateMode(kafkaconfig.Key{Name: "k", ReadOnly: tt.readOnly}, docKey{updateMode: tt.doc}, tt.documented)
		if got != tt.want || (note != "") != tt.note {
			t.Errorf("%s: %s, note %q; want %s, note %t", tt.name, got, note, tt.want, tt.note)
		}
	}
}

func TestCompareDocs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		key  kafkaconfig.Key
		doc  docKey
		note bool
	}{
		{"same", kafkaconfig.Key{Name: "a", Type: "int", Default: str("1")}, docKey{typ: "int", def: "1"}, false},
		{"readable suffix", kafkaconfig.Key{Name: "a.bytes", Type: "int", Default: str("1073741824")},
			docKey{typ: "int", def: "1073741824 (1 gibibyte)"}, false},
		{"null", kafkaconfig.Key{Name: "a", Type: "string"}, docKey{typ: "string", def: "null"}, false},
		{"no default", kafkaconfig.Key{Name: "a", Type: "int"}, docKey{typ: "int", def: ""}, false},
		{"empty string", kafkaconfig.Key{Name: "a", Type: "list", Default: str("")}, docKey{typ: "list", def: `""`}, false},
		{"other default", kafkaconfig.Key{Name: "a", Type: "int", Default: str("2")}, docKey{typ: "int", def: "1"}, true},
		{"default and none", kafkaconfig.Key{Name: "a", Type: "int", Default: str("")}, docKey{typ: "int", def: ""}, true},
		{"other type", kafkaconfig.Key{Name: "a", Type: "list", Default: str("x")}, docKey{typ: "string", def: "x"}, true},
		{"JVM default", kafkaconfig.Key{Name: "ssl.protocol", Type: "string", Default: str("TLSv1.3")},
			docKey{typ: "string", def: "TLSv1.2"}, false},
	}
	for _, tt := range tests {
		notes := compareDocs(kafkaconfig.Broker, tt.key, tt.doc, true)
		if (len(notes) > 0) != tt.note {
			t.Errorf("%s: notes %q, want a note %t", tt.name, notes, tt.note)
		}
	}
}
