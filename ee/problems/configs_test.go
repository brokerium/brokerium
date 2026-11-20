// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
)

// fakeKafka starts an in-memory cluster of one broker, node 0, with the topics.
func fakeKafka(t *testing.T, topics ...string) *kafka.Client {
	t.Helper()
	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(3, topics...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)
	return &kafka.Client{KGO: cl, Adm: kadm.NewClient(cl)}
}

func fakeView(topics ...kafka.Topic) *cluster.View {
	return view([]kafka.Broker{{ID: 0, Alive: true}}, topics...)
}

func TestAlterTopicConfigs(t *testing.T) {
	t.Parallel()
	cl := fakeKafka(t, "orders")
	orders := onBrokers("orders", "1", []int32{0}, []int32{0}, []int32{0})
	p := planner(fakeView(orders))
	p.Client = cl
	planned, err := planAlterTopicConfigs(t.Context(), p, json.RawMessage(`{"topics": ["orders"], "set": {"min.insync.replicas": "2"}}`))
	if err != nil {
		t.Fatal(err)
	}
	if planned.Doc.Summary != "Change the configs of topic orders: min.insync.replicas=2" || len(planned.Steps) != 1 ||
		planned.Steps[0].Kind != KindAlterTopicConfigs {
		t.Fatalf("plan %+v", planned)
	}
	changes := planned.Doc.Details["changes"].([]configChange)
	if len(changes) != 1 || changes[0].To != "2" || !strings.HasSuffix(changes[0].From, "(default)") {
		t.Errorf("changes %+v", changes)
	}
	if !slices.ContainsFunc(planned.Doc.Warnings, func(w string) bool { return strings.Contains(w, "above the replication factor 1") }) {
		t.Errorf("warnings %v", planned.Doc.Warnings)
	}

	args, _ := json.Marshal(planned.Steps[0].Args)
	if _, err := stepAlterTopicConfigs(t.Context(), &ops.Exec{Client: cl}, args); err != nil {
		t.Fatal(err)
	}
	described, err := cl.Adm.DescribeTopicConfigs(t.Context(), "orders")
	if err != nil {
		t.Fatal(err)
	}
	rc, _ := described.On("orders", nil)
	for _, c := range rc.Configs {
		if c.Key == "min.insync.replicas" && (c.MaybeValue() != "2" || c.Source != kmsg.ConfigSourceDynamicTopicConfig) {
			t.Errorf("min.insync.replicas after the step: %s (%v)", c.MaybeValue(), c.Source)
		}
	}

	for name, params := range map[string]string{
		"no topics":          `{"set": {"min.insync.replicas": "2"}}`,
		"no change":          `{"topics": ["orders"]}`,
		"set and deleted":    `{"topics": ["orders"], "set": {"retention.ms": "1"}, "delete": ["retention.ms"]}`,
		"unknown topic":      `{"topics": ["nope"], "set": {"retention.ms": "1"}}`,
		"unknown parameters": `{"topics": ["orders"], "values": {}}`,
	} {
		if _, err := planAlterTopicConfigs(t.Context(), p, json.RawMessage(params)); err == nil {
			t.Errorf("%s: planned", name)
		}
	}
}

func TestMinISRWarnings(t *testing.T) {
	t.Parallel()
	topic := onBrokers("orders", "1", []int32{101, 102, 103}, []int32{101, 102, 103})
	topic.Partitions[1].ISR = []int32{101}
	tests := []struct {
		value string
		want  []string
	}{
		{"1", nil},
		{"2", []string{"1 partition(s) have fewer than 2 in-sync replicas"}},
		{"3", []string{"equal to the replication factor 3", "1 partition(s) have fewer than 3"}},
		{"4", []string{"above the replication factor 3"}},
		{"", nil},
	}
	for _, tc := range tests {
		got := minISRWarnings([]kafka.Topic{topic}, tc.value)
		if len(got) != len(tc.want) {
			t.Errorf("%q: %v, want %v", tc.value, got, tc.want)
			continue
		}
		for i := range got {
			if !strings.Contains(got[i], tc.want[i]) {
				t.Errorf("%q: %q does not contain %q", tc.value, got[i], tc.want[i])
			}
		}
	}
}

func TestAlign(t *testing.T) {
	t.Parallel()
	cfg := func(value string, source kmsg.ConfigSource) brokerConfig {
		return brokerConfig{Value: value, Source: source}
	}
	static, dynamic, defaultCfg := kmsg.ConfigSourceStaticBrokerConfig, kmsg.ConfigSourceDynamicBrokerConfig, kmsg.ConfigSourceDynamicDefaultBrokerConfig
	key := "log.retention.ms"
	tests := []struct {
		name      string
		values    map[string]string
		configs   map[string]map[string]brokerConfig
		value     string
		overrides []int32
		aligned   bool
		err       string
	}{
		{name: "one broker differs", configs: map[string]map[string]brokerConfig{
			"101": {key: cfg("1000", static)}, "102": {key: cfg("2000", dynamic)}, "103": {key: cfg("1000", static)}},
			value: "1000", overrides: []int32{102}},
		{name: "a tie goes to the lowest broker", configs: map[string]map[string]brokerConfig{
			"101": {key: cfg("2000", static)}, "102": {key: cfg("1000", static)}, "103": {key: cfg("3000", static)}},
			value: "2000", overrides: []int32{}},
		{name: "a chosen value", values: map[string]string{key: "5000"}, configs: map[string]map[string]brokerConfig{
			"101": {key: cfg("1000", static)}, "102": {key: cfg("1000", static)}, "103": {key: cfg("1000", static)}},
			value: "5000", overrides: []int32{}},
		{name: "already aligned", configs: map[string]map[string]brokerConfig{"": {key: cfg("1000", defaultCfg)},
			"101": {key: cfg("1000", defaultCfg)}, "102": {key: cfg("1000", defaultCfg)}, "103": {key: cfg("1000", defaultCfg)}},
			value: "1000", overrides: []int32{}, aligned: true},
		{name: "same values without a cluster-wide value", configs: map[string]map[string]brokerConfig{
			"101": {key: cfg("1000", static)}, "102": {key: cfg("1000", static)}, "103": {key: cfg("1000", static)}},
			value: "1000", overrides: []int32{}},
		{name: "read-only", configs: map[string]map[string]brokerConfig{
			"101": {key: {Value: "1", ReadOnly: true}}, "102": {key: cfg("1", static)}, "103": {key: cfg("1", static)}},
			err: "read-only"},
		{name: "sensitive", configs: map[string]map[string]brokerConfig{
			"101": {key: {Sensitive: true}}, "102": {key: {Sensitive: true}}, "103": {key: {Sensitive: true}}},
			err: "sensitive"},
		{name: "unknown key", configs: map[string]map[string]brokerConfig{"101": {}, "102": {}, "103": {}}, err: "does not report"},
	}
	for _, tc := range tests {
		got, err := align(AlignParams{Keys: []string{key}, Values: tc.values}, []int32{101, 102, 103}, tc.configs)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%s: %v, want %q", tc.name, err, tc.err)
			}
			continue
		}
		if err != nil || len(got) != 1 {
			t.Fatalf("%s: %v, %v", tc.name, got, err)
		}
		if got[0].Value != tc.value || !reflect.DeepEqual(got[0].Overrides, tc.overrides) || got[0].aligned() != tc.aligned {
			t.Errorf("%s: %+v (aligned %v)", tc.name, got[0], got[0].aligned())
		}
	}
}

func TestAlignBrokerConfigs(t *testing.T) {
	t.Parallel()
	cl := fakeKafka(t)
	p := planner(fakeView())
	p.Client = cl
	planned, err := planAlignBrokerConfigs(t.Context(), p, json.RawMessage(`{"keys": ["log.retention.ms"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(planned.Steps) != 1 || planned.Steps[0].Kind != KindSetClusterConfigs {
		t.Fatalf("plan %+v", planned)
	}
	set := planned.Steps[0].Args.(clusterConfigArgs).Set
	if set["log.retention.ms"] == "" || !strings.HasPrefix(planned.Doc.Summary, "Use the cluster-wide value log.retention.ms=") {
		t.Errorf("plan %q, set %v", planned.Doc.Summary, set)
	}
	for name, params := range map[string]string{
		"no keys":            `{"keys": []}`,
		"per-broker key":     `{"keys": ["advertised.listeners"]}`,
		"listener key":       `{"keys": ["listener.name.internal.ssl.keystore.location"]}`,
		"listed twice":       `{"keys": ["log.retention.ms", "log.retention.ms"]}`,
		"value of no key":    `{"keys": ["log.retention.ms"], "values": {"log.segment.bytes": "1"}}`,
		"read-only in kfake": `{"keys": ["sasl.enabled.mechanisms"]}`,
	} {
		if _, err := planAlignBrokerConfigs(t.Context(), p, json.RawMessage(params)); err == nil {
			t.Errorf("%s: planned", name)
		}
	}
}

func TestPerBrokerKey(t *testing.T) {
	t.Parallel()
	for key, want := range map[string]bool{
		"broker.id": true, "node.id": true, "listeners": true, "advertised.listeners": true, "log.dirs": true, "log.dir": true,
		"broker.rack": true, "listener.security.protocol.map": true, "ssl.keystore.location": true,
		"listener.name.external.ssl.truststore.location": true, "log.retention.ms": false, "num.io.threads": false,
		"min.insync.replicas": false,
	} {
		if got := perBrokerKey(key); got != want {
			t.Errorf("perBrokerKey(%s) = %v, want %v", key, got, want)
		}
	}
}
