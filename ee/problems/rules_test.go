// SPDX-License-Identifier: LicenseRef-Brokerium-Commercial

//go:build enterprise

package problems

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/store"
)

var testNow = time.Date(2026, 11, 20, 12, 0, 0, 0, time.UTC)

// brokers returns n serving brokers 101, 102, ... on racks r1, r2, r3, r1, ...
func brokers(n int) []kafka.Broker {
	out := make([]kafka.Broker, n)
	for i := range out {
		out[i] = kafka.Broker{ID: int32(101 + i), Rack: "r" + strconv.Itoa(i%3+1), Alive: true}
	}
	return out
}

// topic returns a topic with replicas spread over brokers 101 to 103; minISR "" leaves
// min.insync.replicas unknown.
func topic(name string, rf, partitions int, minISR string) kafka.Topic {
	t := kafka.Topic{Name: name, ReplicationFactor: rf, MinISR: 1, Configs: map[string]string{}}
	if minISR != "" {
		t.Configs["min.insync.replicas"] = minISR
		t.MinISR, _ = strconv.Atoi(minISR)
	}
	for p := range partitions {
		var replicas []int32
		for j := range rf {
			replicas = append(replicas, int32(101+(p+j)%3))
		}
		t.Partitions = append(t.Partitions, kafka.Partition{ID: int32(p), Leader: replicas[0], Replicas: replicas,
			ISR: append([]int32(nil), replicas...), SizeBytes: 1 << 20})
	}
	return t
}

func view(bs []kafka.Broker, topics ...kafka.Topic) *cluster.View {
	s := &kafka.Snapshot{Brokers: bs, Topics: topics}
	s.FillEmpty()
	c := &store.Cluster{ID: "c1", Name: "lab", Provider: store.ProviderExternal, Flavor: store.FlavorApache}
	return &cluster.View{Cluster: c, Snapshot: s, Health: cluster.Health{Issues: []cluster.Issue{}},
		Capabilities: cluster.CapabilitiesFor(c, false), UpdatedAt: testNow}
}

func findings(v *cluster.View, nodes ...*store.Node) []Finding {
	return Evaluate(&Input{View: v, Nodes: nodes, Now: testNow})
}

func codesOf(fs []Finding) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.Code)
	}
	return out
}

func fixOps(f Finding) []string {
	out := []string{}
	for _, x := range f.Fixes {
		out = append(out, x.Operation)
	}
	return out
}

func TestTopicReplication(t *testing.T) {
	t.Parallel()
	confluent := view(brokers(3), topic("orders", 1, 3, "2"))
	confluent.Capabilities = cluster.CapabilitiesFor(&store.Cluster{Flavor: store.FlavorConfluentCloud}, false)
	tests := []struct {
		name     string
		view     *cluster.View
		code     string
		severity string
		fixes    []string
		// target is the replication factor of the first fix, if it raises it.
		target int
	}{
		{"replication factor below min.insync.replicas", view(brokers(3), topic("orders", 1, 3, "2")),
			"topic-rf-below-min-isr", SeverityCritical, []string{OpChangeReplicationFactor, OpAlterTopicConfigs}, 3},
		{"two replicas and min.insync.replicas 3 on three brokers", view(brokers(3), topic("orders", 2, 3, "3")),
			"topic-rf-below-min-isr", SeverityCritical, []string{OpChangeReplicationFactor, OpAlterTopicConfigs}, 3},
		{"internal topic with one replica", view(brokers(3), topic("__consumer_offsets", 1, 3, "1")),
			"internal-topic-rf-low", SeverityWarning, []string{OpChangeReplicationFactor}, 3},
		{"internal topic with two replicas", view(brokers(4), topic("__transaction_state", 2, 3, "2")),
			"internal-topic-rf-low", SeverityWarning, []string{OpChangeReplicationFactor}, 3},
		{"single replica", view(brokers(3), topic("orders", 1, 3, "1")),
			"topic-rf-one", SeverityWarning, []string{OpChangeReplicationFactor}, 3},
		{"single replica, min.insync.replicas unknown", view(brokers(2), topic("orders", 1, 3, "")),
			"topic-rf-one", SeverityWarning, []string{OpChangeReplicationFactor}, 2},
		{"single replica on a single broker", view(brokers(1), topic("orders", 1, 3, "1")), "", "", nil, 0},
		{"replication factor equal to min.insync.replicas", view(brokers(3), topic("orders", 2, 3, "2")),
			"topic-rf-equals-min-isr", SeverityWarning, []string{OpChangeReplicationFactor, OpAlterTopicConfigs}, 3},
		{"three replicas and min.insync.replicas 3 on three brokers", view(brokers(3), topic("orders", 3, 3, "3")),
			"topic-rf-equals-min-isr", SeverityWarning, []string{OpAlterTopicConfigs}, 0},
		{"min.insync.replicas 1 with three replicas", view(brokers(3), topic("orders", 3, 3, "1")),
			"topic-min-isr-one", SeverityInfo, []string{OpAlterTopicConfigs}, 0},
		{"min.insync.replicas 1 with two replicas", view(brokers(3), topic("orders", 2, 3, "1")),
			"topic-min-isr-one", SeverityInfo, []string{OpChangeReplicationFactor}, 3},
		{"healthy topic", view(brokers(3), topic("orders", 3, 3, "2")), "", "", nil, 0},
		{"two replicas, min.insync.replicas unknown", view(brokers(3), topic("orders", 2, 3, "")), "", "", nil, 0},
		{"no reassignments on Confluent Cloud", confluent, "topic-rf-below-min-isr", SeverityCritical, []string{OpAlterTopicConfigs}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := findings(tc.view)
			if tc.code == "" {
				if len(fs) != 0 {
					t.Fatalf("findings %v, want none", codesOf(fs))
				}
				return
			}
			if len(fs) != 1 || fs[0].Code != tc.code || fs[0].Severity != tc.severity {
				t.Fatalf("findings %v, want one %s (%s)", codesOf(fs), tc.code, tc.severity)
			}
			f := fs[0]
			if got := fixOps(f); !reflect.DeepEqual(got, tc.fixes) {
				t.Errorf("fixes %v, want %v", got, tc.fixes)
			}
			if f.Object.Kind != ObjectTopic || f.Object.ID != tc.view.Snapshot.Topics[0].Name || f.Title == "" ||
				f.Explanation == "" || f.Recommendation == "" || len(f.Evidence) == 0 {
				t.Errorf("incomplete finding %+v", f)
			}
			if tc.target > 0 {
				params := f.Fixes[0].Params.(ReplicationParams)
				if params.ReplicationFactor != tc.target || !reflect.DeepEqual(params.Topics, []string{f.Object.ID}) {
					t.Errorf("first fix %+v, want replication factor %d", params, tc.target)
				}
			}
		})
	}
}

func TestMinInsyncReplicasFix(t *testing.T) {
	t.Parallel()
	f := findings(view(brokers(3), topic("orders", 1, 3, "2")))[0]
	params := f.Fixes[1].Params.(TopicConfigParams)
	if params.Set["min.insync.replicas"] != "1" || f.Fixes[1].Label != "Set min.insync.replicas to 1" {
		t.Errorf("alternative fix %+v", f.Fixes[1])
	}
	f = findings(view(brokers(3), topic("orders", 3, 3, "1")))[0]
	if params := f.Fixes[0].Params.(TopicConfigParams); params.Set["min.insync.replicas"] != "2" {
		t.Errorf("fix of min.insync.replicas 1: %+v", f.Fixes[0])
	}
}

func TestConsumerOffsetsPartitions(t *testing.T) {
	t.Parallel()
	if got := codesOf(findings(view(brokers(4), topic("__consumer_offsets", 3, 3, "2")))); !reflect.DeepEqual(got, []string{"consumer-offsets-partitions"}) {
		t.Errorf("3 partitions on 4 brokers: %v", got)
	}
	if got := codesOf(findings(view(brokers(3), topic("__consumer_offsets", 3, 50, "2")))); len(got) != 0 {
		t.Errorf("50 partitions on 3 brokers: %v", got)
	}
}

func TestPlacementRules(t *testing.T) {
	t.Parallel()
	withReplicas := func(counts ...int) []kafka.Broker {
		bs := brokers(len(counts))
		for i, n := range counts {
			bs[i].Replicas = n
		}
		return bs
	}
	withData := func(sizes ...int64) []kafka.Broker {
		bs := brokers(len(sizes))
		for i, n := range sizes {
			bs[i].DataBytes = n
			bs[i].Replicas = 10
		}
		return bs
	}
	reassigning := view(withReplicas(10, 10, 0))
	reassigning.Snapshot.Reassignments = []kafka.Reassignment{{Topic: "orders", Partition: 0}}
	cordoned := withReplicas(10, 10, 0)
	cordoned[2].Cordoned = true
	noLogDirs := view(withData(5<<30, 1<<30, 1<<30))
	noLogDirs.Snapshot.Skipped = []string{kafka.PartLogDirs}
	tests := []struct {
		name  string
		view  *cluster.View
		codes []string
	}{
		{"broker without replicas", view(withReplicas(10, 10, 0)), []string{"broker-without-replicas"}},
		{"while partitions are reassigned", reassigning, []string{}},
		{"too few replicas for every broker", view(withReplicas(1, 0, 0)), []string{}},
		{"cordoned broker without replicas", view(cordoned), []string{}},
		{"replica skew", view(withReplicas(40, 20, 20)), []string{"replica-skew"}},
		{"small replica differences", view(withReplicas(4, 2, 2)), []string{}},
		{"data skew", view(withData(5<<30, 1<<30, 1<<30)), []string{"data-skew"}},
		{"small brokers", view(withData(100<<20, 10<<20, 10<<20)), []string{}},
		{"without log dirs", noLogDirs, []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := findings(tc.view)
			if got := codesOf(fs); !reflect.DeepEqual(got, tc.codes) {
				t.Fatalf("findings %v, want %v", got, tc.codes)
			}
			for _, f := range fs {
				if got := fixOps(f); !reflect.DeepEqual(got, []string{"rebalance"}) {
					t.Errorf("%s: fixes %v", f.Code, got)
				}
			}
		})
	}
	f := findings(view(withReplicas(10, 10, 0)))[0]
	if f.Object != (Object{Kind: ObjectBroker, ID: "103"}) {
		t.Errorf("object %+v", f.Object)
	}
	f = findings(view(withReplicas(40, 20, 20)))[0]
	if f.Object.Kind != ObjectCluster || !strings.Contains(f.Explanation, "broker 101") {
		t.Errorf("replica skew %+v", f)
	}
}

func TestAgentsNotSeen(t *testing.T) {
	t.Parallel()
	c1, other := "c1", "c2"
	at := func(d time.Duration) *time.Time { v := testNow.Add(-d); return &v }
	id := int32(101)
	nodes := []*store.Node{
		{ID: "n1", Name: "kafka-1", ClusterID: &c1, Role: "broker", KafkaNodeID: &id, LastSeenAt: at(49 * time.Hour)},
		{ID: "n2", Name: "kafka-2", ClusterID: &c1, Role: "broker", LastSeenAt: at(time.Hour)},
		{ID: "n3", Name: "kafka-3", ClusterID: &c1, Role: "controller", CreatedAt: testNow.Add(-72 * time.Hour)},
		{ID: "n4", Name: "kafka-4", ClusterID: &other, LastSeenAt: at(72 * time.Hour)},
	}
	fs := findings(view(brokers(1)), nodes...)
	if len(fs) != 2 {
		t.Fatalf("findings %v, want 2", codesOf(fs))
	}
	for i, want := range []string{"n1", "n3"} {
		if f := fs[i]; f.Code != "agent-not-seen" || f.Severity != SeverityWarning || f.Object.ID != want || f.Object.Kind != ObjectNode {
			t.Errorf("finding %d: %+v", i, f)
		}
	}
	if !strings.Contains(fs[0].Explanation, "2 day(s) 1 hour(s)") || fs[1].Evidence[0].Value != "never" {
		t.Errorf("explanations %q / evidence %+v", fs[0].Explanation, fs[1].Evidence)
	}
}

func TestHealthIssues(t *testing.T) {
	t.Parallel()
	v := view(brokers(3))
	v.Health.Issues = []cluster.Issue{
		{Severity: cluster.HealthCritical, Code: "under_min_isr", Message: "2 partition(s) are below min.insync.replicas"},
		{Severity: cluster.HealthWarning, Code: "agent_certificate_expiring", Message: "node a expires"},
		{Severity: cluster.HealthWarning, Code: "agent_certificate_expiring", Message: "node b expires"},
		{Severity: "info", Code: "something_new", Message: "a new check"},
	}
	fs := findings(v)
	want := []string{"health-under_min_isr", "health-agent_certificate_expiring", "health-something_new"}
	if got := codesOf(fs); !reflect.DeepEqual(got, want) {
		t.Fatalf("findings %v, want %v", got, want)
	}
	if fs[0].Severity != SeverityCritical || fs[0].Title != "Partitions below min.insync.replicas" ||
		fs[0].Explanation != "2 partition(s) are below min.insync.replicas." || fs[0].Object.Kind != ObjectCluster {
		t.Errorf("health finding %+v", fs[0])
	}
	if fs[1].Explanation != "Node a expires. Node b expires." || fs[2].Severity != SeverityInfo || fs[2].Recommendation == "" {
		t.Errorf("merged or unknown issues: %+v / %+v", fs[1], fs[2])
	}
}

func TestEvaluateOrderAndFingerprints(t *testing.T) {
	t.Parallel()
	v := view(brokers(3), topic("orders", 3, 3, "1"), topic("payments", 1, 3, "2"), topic("clicks", 1, 3, "1"))
	v.Health.Issues = []cluster.Issue{{Severity: cluster.HealthCritical, Code: "under_min_isr", Message: "x"}}
	fs := findings(v)
	want := []string{"health-under_min_isr", "topic-rf-below-min-isr", "topic-rf-one", "topic-min-isr-one"}
	if got := codesOf(fs); !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v, want %v", got, want)
	}
	seen := map[string]bool{}
	for _, f := range fs {
		if seen[f.Fingerprint] || len(f.Fingerprint) != 16 {
			t.Errorf("fingerprint %q of %s", f.Fingerprint, f.Code)
		}
		seen[f.Fingerprint] = true
	}
	if got := Fingerprint("topic-rf-one", Object{Kind: ObjectTopic, ID: "orders"}); got != "b2fe6a9ceb47f240" {
		t.Errorf("the fingerprint changed to %s; mutes and acknowledgements of existing findings would be lost", got)
	}
	if empty := findings(&cluster.View{Cluster: v.Cluster, Health: cluster.Health{}}); len(empty) != 0 {
		t.Errorf("a view without a snapshot: %v", codesOf(empty))
	}
}
