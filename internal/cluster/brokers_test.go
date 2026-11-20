// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package cluster

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/store"
)

func entries(settings map[string]string) []kafka.ConfigEntry {
	out := []kafka.ConfigEntry{}
	for name, value := range settings {
		out = append(out, kafka.ConfigEntry{Name: name, Value: value, Source: "STATIC_BROKER_CONFIG"})
	}
	return out
}

func TestAgentKafkaVersion(t *testing.T) {
	t.Parallel()
	for status, want := range map[string]string{
		`{"activeVersion":"4.3.1"}`: "4.3.1",
		`{"activeVersion":"4.4.0","kafkaProcess":{"pid":4711,"version":"4.3.1","mixedVersions":false}}`: "4.3.1",
		`{"activeVersion":"4.4.0","kafkaProcess":{"pid":0,"version":""}}`:                               "4.4.0",
		`{"activeVersion":"4.4.0","kafkaProcess":{"pid":4711,"version":"4.3.1","mixedVersions":true}}`:  "4.4.0",
		`{"activeVersion":"","kafkaProcess":{"pid":4711,"version":"7.5.0-ccs"}}`:                        "",
		`{"activeVersion":"4.3"}`: "",
		`{}`:                      "",
	} {
		if got := AgentKafkaVersion(&store.Node{Status: json.RawMessage(status)}); got != want {
			t.Errorf("AgentKafkaVersion(%s) = %q, want %q", status, got, want)
		}
	}
}

func TestCompleteBrokers(t *testing.T) {
	t.Parallel()
	zookeeper := map[string]string{"zookeeper.connect": "zk:2181/kafka", "process.roles": "",
		"zookeeper.metadata.migration.enable": "false", "inter.broker.protocol.version": "3.5-IV2"}
	migrating := map[string]string{"zookeeper.connect": "zk:2181/kafka", "process.roles": "",
		"zookeeper.metadata.migration.enable": "true", "inter.broker.protocol.version": "3.9-IV0"}
	kraft := map[string]string{"process.roles": "broker", "inter.broker.protocol.version": "4.0-IV3"}
	id := func(v int32) *int32 { return &v }
	status := func(version string) json.RawMessage {
		raw, _ := json.Marshal(map[string]any{"running": true, "activeVersion": "4.4.0",
			"kafkaProcess": map[string]any{"pid": 4711, "version": version}})
		return raw
	}
	tests := []struct {
		name    string
		broker  kafka.Broker
		prev    *kafka.Broker
		configs map[string]string
		nodes   []*store.Node
		want    kafka.Broker
		mode    string
	}{
		{"ZooKeeper broker", kafka.Broker{ID: 101, Mode: kafka.ModeZooKeeper, Version: "3.5"}, nil, zookeeper, nil,
			kafka.Broker{ID: 101, Mode: kafka.ModeZooKeeper, Version: "3.5", ZooKeeperConnect: "zk:2181/kafka",
				InterBrokerProtocolVersion: "3.5-IV2"}, kafka.ModeZooKeeper},
		{"ZooKeeper broker with the migration flag", kafka.Broker{ID: 101, Mode: kafka.ModeZooKeeper, Version: "3.9"}, nil, migrating, nil,
			kafka.Broker{ID: 101, Mode: kafka.ModeMigration, Version: "3.9", ZooKeeperConnect: "zk:2181/kafka",
				InterBrokerProtocolVersion: "3.9-IV0"}, kafka.ModeMigration},
		{"KRaft broker: no inter-broker protocol", kafka.Broker{ID: 101, Mode: kafka.ModeKRaft, Version: "4.0"}, nil, kraft, nil,
			kafka.Broker{ID: 101, Mode: kafka.ModeKRaft, Version: "4.0", ProcessRoles: "broker"}, kafka.ModeKRaft},
		{"the agent reports the exact version", kafka.Broker{ID: 101, Mode: kafka.ModeKRaft, Version: "4.3"}, nil, nil,
			[]*store.Node{{Role: "broker", KafkaNodeID: id(101), Status: status("4.3.1")},
				{Role: "controller", KafkaNodeID: id(1), Status: status("4.2.0")}},
			kafka.Broker{ID: 101, Mode: kafka.ModeKRaft, Version: "4.3.1", VersionExact: true}, kafka.ModeKRaft},
		{"no ApiVersions: the previous snapshot", kafka.Broker{ID: 101}, &kafka.Broker{ID: 101, Mode: kafka.ModeKRaft, Version: "4.3"}, nil, nil,
			kafka.Broker{ID: 101, Mode: kafka.ModeKRaft, Version: "4.3"}, kafka.ModeKRaft},
		{"no ApiVersions: the configuration tells the mode", kafka.Broker{ID: 101}, nil, zookeeper, nil,
			kafka.Broker{ID: 101, Mode: kafka.ModeZooKeeper, ZooKeeperConnect: "zk:2181/kafka", InterBrokerProtocolVersion: "3.5-IV2"},
			kafka.ModeZooKeeper},
		{"nothing known", kafka.Broker{ID: 101}, nil, nil, nil, kafka.Broker{ID: 101}, ""},
	}
	for _, tc := range tests {
		snap := &kafka.Snapshot{Brokers: []kafka.Broker{tc.broker}}
		var prev *kafka.Snapshot
		if tc.prev != nil {
			prev = &kafka.Snapshot{Brokers: []kafka.Broker{*tc.prev}}
		}
		var configs map[int32][]kafka.ConfigEntry
		if tc.configs != nil {
			configs = map[int32][]kafka.ConfigEntry{101: entries(tc.configs)}
		}
		completeBrokers(snap, prev, configs, tc.nodes)
		if got := snap.Brokers[0]; !reflect.DeepEqual(got, tc.want) || snap.Mode != tc.mode {
			t.Errorf("%s:\n got %+v, mode %q\nwant %+v, mode %q", tc.name, got, snap.Mode, tc.want, tc.mode)
		}
	}
}
