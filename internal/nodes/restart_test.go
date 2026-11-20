// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/store"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

func TestCheckNewKafka(t *testing.T) {
	t.Parallel()
	before := &agentv1.KafkaProcess{Pid: 100, StartTimeMs: 1_000, Version: "4.3.1"}
	proc := func(pid, start int64, version string) *agentv1.KafkaProcess {
		return &agentv1.KafkaProcess{Pid: pid, StartTimeMs: start, Version: version, User: "kafka"}
	}
	tests := []struct {
		name    string
		status  *agentv1.NodeStatus
		want    string
		done    bool
		wantErr string
	}{
		{"the old process still runs", &agentv1.NodeStatus{KafkaProcess: proc(100, 1_000, "4.3.1")}, "4.4.0", false, ""},
		{"the old process after a clock adjustment", &agentv1.NodeStatus{KafkaProcess: proc(100, 2_000, "4.3.1")}, "4.4.0", false, ""},
		{"no Kafka process yet", &agentv1.NodeStatus{KafkaProcess: proc(0, 0, ""), LastExit: "exit status 1"}, "4.4.0", false, ""},
		{"a new process of the target", &agentv1.NodeStatus{KafkaProcess: proc(200, 5_000, "4.4.0")}, "4.4.0", true, ""},
		{"the old pid started again", &agentv1.NodeStatus{KafkaProcess: proc(100, 60_000, "4.4.0")}, "4.4.0", true, ""},
		{"a restart on the active release", &agentv1.NodeStatus{KafkaProcess: proc(200, 5_000, "4.3.1")}, "4.3.1", true, ""},
		{"a classpath without versions", &agentv1.NodeStatus{KafkaProcess: proc(200, 5_000, "")}, "4.4.0", true, ""},
		{"the old release after the upgrade", &agentv1.NodeStatus{KafkaProcess: proc(200, 5_000, "4.3.1")}, "4.4.0", false,
			"kafka-1 runs Kafka 4.3.1 in process 200 after the start, not 4.4.0"},
		{"jars of several versions", &agentv1.NodeStatus{KafkaProcess: &agentv1.KafkaProcess{Pid: 200, StartTimeMs: 5_000, Version: "4.3.1",
			MixedVersions: true}}, "4.4.0", false, "check CLASSPATH in the environment of Kafka"},
		{"Kafka of the operator", &agentv1.NodeStatus{KafkaProcess: proc(200, 5_000, "4.4.0"),
			ForeignKafka: []*agentv1.KafkaProcess{proc(77, 10, "4.3.1")}}, "4.4.0", false,
			"kafka-1 runs Kafka that brokerium-agent does not manage (process 77 of user kafka); stop the operator's own Kafka unit first"},
	}
	for _, tt := range tests {
		done, detail, err := checkNewKafka("kafka-1", before, tt.status, tt.want)
		if done != tt.done || (tt.wantErr == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.wantErr)) {
			t.Errorf("%s: checkNewKafka = %v, %v, %v; want done %v, error %q", tt.name, done, detail, err, tt.done, tt.wantErr)
		}
	}
	_, detail, _ := checkNewKafka("kafka-1", before, &agentv1.NodeStatus{KafkaProcess: proc(0, 0, ""), LastExit: "exit status 1"}, "4.4.0")
	if detail["lastExit"] != "exit status 1" || detail["previousPid"] != int64(100) {
		t.Errorf("detail while no process runs: %v", detail)
	}
}

func TestWaitNewKafkaWithAnOlderAgent(t *testing.T) {
	t.Parallel()
	detail, err := (&Manager{}).waitNewKafka(t.Context(), nil, NodeArgs{Name: "kafka-1"}, nil, "4.4.0")
	if err != nil || !strings.Contains(detail.(map[string]any)["skipped"].(string), "does not report") {
		t.Errorf("waitNewKafka = %v, %v; want the check skipped", detail, err)
	}
}

func TestPollStopsOnFinalErrors(t *testing.T) {
	t.Parallel()
	calls := 0
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	detail, err := poll(ctx, nil, time.Minute, time.Millisecond, "a new process", func(context.Context) (bool, any, error) {
		calls++
		return false, "seen", final{errors.New("wrong version")}
	})
	if err == nil || err.Error() != "wrong version" || detail != "seen" || calls != 1 {
		t.Errorf("poll = %v, %v after %d checks; want the final error at once", detail, err, calls)
	}
}

func TestForeignKafkaWarnings(t *testing.T) {
	t.Parallel()
	operator := status(`{"foreignKafka":[{"pid":77,"startTimeMs":10,"version":"4.3.1","user":"kafka"}]}`)
	warned := func(warnings []string, node string) bool {
		for _, w := range warnings {
			if strings.HasPrefix(w, node+" runs Kafka process 77 of user kafka, which brokerium-agent does not manage") {
				return true
			}
		}
		return false
	}
	p := plannerFor(agentCluster(), runningSnapshot())

	f := runningFleet()
	operator(f.nodes[3])
	restart, err := restartPlan(p, f.managed("c1"), restartParams{})
	if err != nil {
		t.Fatal(err)
	}
	upgrade, err := upgradePlan(p, f.managed("c1"), upgradeParams{Version: "4.4.0"})
	if err != nil {
		t.Fatal(err)
	}
	add, err := addBrokerPlan(p, runningFleet(host("f-4", "free-4", operator)), addBrokerParams{Nodes: []brokerChoice{{Node: "f-4"}}})
	if err != nil {
		t.Fatal(err)
	}
	if !warned(restart.Doc.Warnings, "kafka-1") || !warned(upgrade.Doc.Warnings, "kafka-1") || !warned(add.Doc.Warnings, "free-4") {
		t.Errorf("warnings of restart %q, upgrade %q, add-broker %q", restart.Doc.Warnings, upgrade.Doc.Warnings, add.Doc.Warnings)
	}
	if got := foreignWarnings([]*store.Node{host("n", "kafka-9")}); len(got) != 0 {
		t.Errorf("a host without foreign Kafka: %v", got)
	}
}
