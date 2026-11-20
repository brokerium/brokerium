// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package agentgw

import (
	"encoding/json"
	"reflect"
	"testing"

	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

func TestStatusJSON(t *testing.T) {
	t.Parallel()
	decode := func(raw json.RawMessage) map[string]any {
		var m map[string]any
		if err := json.Unmarshal(raw, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	st := decode(StatusJSON(&agentv1.NodeStatus{Running: true, Pid: 4242, KafkaDataDir: "/data/kafka",
		KafkaProcess: &agentv1.KafkaProcess{Pid: 4242, StartTimeMs: 1763625650000, Version: "4.4.0", User: "kafka"},
		ForeignKafka: []*agentv1.KafkaProcess{{Pid: 77, Version: "4.3.1", MixedVersions: true, User: "root"}}}))
	if st["kafkaDataDir"] != "/data/kafka" || st["pid"] != 4242.0 {
		t.Errorf("status %v", st)
	}
	wantProcess := map[string]any{"pid": 4242.0, "startTimeMs": 1763625650000.0, "version": "4.4.0", "mixedVersions": false, "user": "kafka"}
	if !reflect.DeepEqual(st["kafkaProcess"], wantProcess) {
		t.Errorf("kafkaProcess %v, want %v", st["kafkaProcess"], wantProcess)
	}
	wantForeign := []any{map[string]any{"pid": 77.0, "startTimeMs": 0.0, "version": "4.3.1", "mixedVersions": true, "user": "root"}}
	if !reflect.DeepEqual(st["foreignKafka"], wantForeign) {
		t.Errorf("foreignKafka %v, want %v", st["foreignKafka"], wantForeign)
	}

	old := decode(StatusJSON(&agentv1.NodeStatus{Running: true, Pid: 17}))
	if _, ok := old["kafkaProcess"]; ok {
		t.Errorf("an agent before 0.3.2 reports no Kafka process, got %v", old)
	}
	if _, ok := old["foreignKafka"]; ok {
		t.Errorf("an agent before 0.3.2 reports no foreign processes, got %v", old)
	}
}
