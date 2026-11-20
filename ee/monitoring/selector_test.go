// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package monitoring

import (
	"errors"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/store"
)

func TestNormalizeSelector(t *testing.T) {
	t.Parallel()
	valid := map[string]string{
		``:            ``,
		`job="kafka"`: `job="kafka"`,
		` { job = "kafka" , env=~"prod|stage" } `: `job="kafka",env=~"prod|stage"`,
		`job='kafka-eu',`:                         `job="kafka-eu"`,
		"instance=~`kafka-\\d+:9141`":             `instance=~"kafka-\\d+:9141"`,
		`cluster!="test",zone!~"a|b"`:             `cluster!="test",zone!~"a|b"`,
		`team="a \"quoted\" name"`:                `team="a \"quoted\" name"`,
		`team='it\'s',x="\u00e9"`:                 `team="it's",x="é"`,
		`_private="1"`:                            `_private="1"`,
	}
	for in, want := range valid {
		got, err := normalizeSelector(in)
		if err != nil || got != want {
			t.Errorf("normalizeSelector(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	invalid := map[string]string{
		`kafka_server_replicamanager_leadercount`: "expected =",
		`__name__="up"`:                   "__name__",
		`job=kafka`:                       "expected a quoted string",
		`job="kafka`:                      "unterminated",
		`job=="kafka"`:                    "expected a quoted string",
		`job="a"} or vector(1) or {x="b"`: "expected a comma",
		`job="a" env="b"`:                 "expected a comma",
		`instance=~"kafka-(1"`:            "regular expression",
		`1job="x"`:                        "expected a label name",
		`job="\q"`:                        "invalid escape",
		`job="x",,`:                       "expected a label name",
		`job="` + strings.Repeat("a", maxSelector) + `"`: "longer than",
	}
	for in, want := range invalid {
		if got, err := normalizeSelector(in); !errors.Is(err, ErrBadSelector) || !strings.Contains(err.Error(), want) {
			t.Errorf("normalizeSelector(%q) = %q, %v; want an error containing %q", cut(in), got, err, want)
		}
	}
}

func TestDefaultSelector(t *testing.T) {
	t.Parallel()
	if got := defaultSelector(nil, 9141); got != "" {
		t.Errorf("without hosts: %q", got)
	}
	got := defaultSelector([]string{"kafka-1.example.com", "10.0.0.7", "2001:db8::1"}, 9141)
	want := `instance=~"kafka-1\\.example\\.com:9141|10\\.0\\.0\\.7:9141|\\[2001:db8::1\\]:9141"`
	if got != want {
		t.Fatalf("defaultSelector = %s, want %s", got, want)
	}
	if norm, err := normalizeSelector(got); err != nil || norm != got {
		t.Errorf("the default selector does not normalize to itself: %q, %v", norm, err)
	}
	re := regexp.MustCompile(`^(?:kafka-1\.example\.com:9141|10\.0\.0\.7:9141|\[2001:db8::1\]:9141)$`)
	for _, instance := range []string{"kafka-1.example.com:9141", "10.0.0.7:9141", "[2001:db8::1]:9141"} {
		if !re.MatchString(instance) {
			t.Errorf("%s is not matched", instance)
		}
	}
	if re.MatchString("kafka-1Xexample.com:9141") {
		t.Error("the dots of host names must be escaped")
	}
}

func TestClusterNodes(t *testing.T) {
	t.Parallel()
	n := clusterNodes(alphaView())
	if want := []string{"controller-1.example.com", "kafka-1.example.com", "kafka-2.example.com", "kafka-3.example.com"}; !slices.Equal(n.hosts, want) {
		t.Errorf("hosts = %v, want %v", n.hosts, want)
	}
	for instance, want := range map[string]string{
		"kafka-2.example.com:9141":      "broker 102",
		"controller-1.example.com:9141": "controller 1",
		"kafka-9.example.com:9141":      "kafka-9.example.com:9141",
		"kafka-1.example.com":           "broker 101",
	} {
		if got := n.instanceName(instance); got != want {
			t.Errorf("instanceName(%s) = %q, want %q", instance, got, want)
		}
	}

	combined := &cluster.View{Snapshot: &kafka.Snapshot{
		Brokers: []kafka.Broker{{ID: 1, Host: "node-1"}, {ID: 2, Host: "lab"}, {ID: 3, Host: "lab"}},
		Quorum:  &kafka.Quorum{Voters: []kafka.QuorumReplica{{NodeID: 1, Endpoints: []kafka.Endpoint{{Host: "node-1"}}}}},
	}}
	n = clusterNodes(combined)
	if n.instanceName("node-1:9141") != "node 1" || n.instanceName("lab:9141") != "lab:9141" {
		t.Errorf("combined node and shared host: %v", n.names)
	}

	fresh := &cluster.View{Cluster: &store.Cluster{BootstrapServers: []string{"seed-2:9092", "seed-1:9092", "not-a-host-port"}}}
	n = clusterNodes(fresh)
	if !slices.Equal(n.hosts, []string{"seed-1", "seed-2"}) || len(n.names) != 0 {
		t.Errorf("before the first snapshot: hosts %v, names %v", n.hosts, n.names)
	}
}
