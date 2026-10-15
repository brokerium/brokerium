// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"fmt"
	"os/exec"
	"slices"
	"testing"
	"time"
)

// failHost simulates a lost host: the container and all its volumes disappear.
func failHost(t *testing.T, node string) {
	t.Helper()
	if out, err := exec.Command("docker", "rm", "-f", node).CombinedOutput(); err != nil {
		t.Fatalf("docker rm %s: %v: %s", node, err, out)
	}
	for _, v := range []string{"-data", "-agent", "-kafka"} {
		_ = exec.Command("docker", "volume", "rm", "-f", node+v).Run()
	}
}

func brokerIDs(v view) []int32 {
	var ids []int32
	for _, b := range v.Snapshot.Brokers {
		ids = append(ids, b.ID)
	}
	slices.Sort(ids)
	return ids
}

func voterIDs(v view) []int32 {
	var ids []int32
	for _, x := range v.Snapshot.Quorum.Voters {
		ids = append(ids, x.NodeID)
	}
	slices.Sort(ids)
	return ids
}

func healthy(v view) bool {
	for _, b := range v.Snapshot.Brokers {
		if !b.Alive || b.Fenced {
			return false
		}
	}
	return v.Health.Status == "ok" && v.Health.UnderReplicated == 0
}

// TestNodeLifecycle covers scenarios 3 (controllers 3 -> 5 -> 3), 4 (replace a broken broker
// with the same id and with a new id), 5 (decommission a broker) and a rolling restart.
func TestNodeLifecycle(t *testing.T) {
	heavy(t)
	c := newClient(t)
	name := uniqueName("e2e-nodes-")
	id := dockerCluster(t, c, name, map[string]any{"controllers": 3, "brokers": 4,
		"racks": []string{"r1", "r2", "r3", "r4", "r5", "r6"}})
	waitView(t, c, id, 2*time.Minute, "a healthy cluster", healthy)
	produce(t, c, id, "events", 12, 100)

	// Scenario 3: grow the quorum to five voters and shrink it back, one voter at a time.
	for want := 4; want <= 5; want++ {
		c.run(id, name, "add-controller", map[string]any{}, 5*time.Minute)
		v := waitView(t, c, id, time.Minute, fmt.Sprintf("%d voters", want), func(v view) bool { return len(v.Snapshot.Quorum.Voters) == want })
		t.Logf("scenario 3: voters %v", voterIDs(v))
	}
	for _, node := range []int32{5, 4} {
		c.run(id, name, "remove-controller", map[string]any{"nodeId": node}, 5*time.Minute)
	}
	v := waitView(t, c, id, time.Minute, "3 voters", func(v view) bool { return len(v.Snapshot.Quorum.Voters) == 3 })
	if got := voterIDs(v); !slices.Equal(got, []int32{1, 2, 3}) {
		t.Errorf("voters after shrinking = %v", got)
	}

	// Scenario 4a: broker 103 loses its disk; replace it with the same id.
	failHost(t, name+"-b103")
	waitView(t, c, id, 2*time.Minute, "broker 103 offline", func(v view) bool {
		for _, b := range v.Snapshot.Brokers {
			if b.ID == 103 {
				return !b.Alive || b.Fenced
			}
		}
		return false
	})
	c.run(id, name, "replace-broker", map[string]any{"brokerId": 103, "mode": "same-id"}, 10*time.Minute)
	v = waitView(t, c, id, 3*time.Minute, "a healthy cluster after the same-id replacement", healthy)
	t.Logf("scenario 4a: brokers %v", brokerIDs(v))

	// Scenario 4b: broker 102 is lost; replace it with a new node id.
	failHost(t, name+"-b102")
	waitView(t, c, id, 2*time.Minute, "broker 102 offline", func(v view) bool {
		for _, b := range v.Snapshot.Brokers {
			if b.ID == 102 {
				return !b.Alive || b.Fenced
			}
		}
		return false
	})
	c.run(id, name, "replace-broker", map[string]any{"brokerId": 102, "mode": "new-id"}, 10*time.Minute)
	v = waitView(t, c, id, 3*time.Minute, "a healthy cluster after the new-id replacement", healthy)
	ids := brokerIDs(v)
	if slices.Contains(ids, 102) || len(ids) != 4 {
		t.Errorf("brokers after the new-id replacement = %v, want 4 brokers without 102", ids)
	}
	t.Logf("scenario 4b: brokers %v", ids)

	// Scenario 5: decommission broker 104.
	c.run(id, name, "drain-broker", map[string]any{"brokerId": 104, "decommission": true}, 10*time.Minute)
	v = waitView(t, c, id, 3*time.Minute, "broker 104 gone", func(v view) bool {
		return !slices.Contains(brokerIDs(v), 104) && healthy(v)
	})
	t.Logf("scenario 5: brokers %v", brokerIDs(v))

	// Rolling restart of every node, controllers first.
	op := c.run(id, name, "rolling-restart", map[string]any{}, 15*time.Minute)
	t.Logf("rolling restart: %s", op.Summary)
	waitView(t, c, id, 2*time.Minute, "a healthy cluster after the rolling restart", healthy)
}

// TestMigrateStaticQuorum is scenario 10: a static quorum moves to a dynamic one, after which
// controllers can be added.
func TestMigrateStaticQuorum(t *testing.T) {
	heavy(t)
	c := newClient(t)
	name := uniqueName("e2e-static-")
	id := dockerCluster(t, c, name, map[string]any{"controllers": 3, "brokers": 1, "quorum": "static",
		"racks": []string{"r1"}})
	v := waitView(t, c, id, 2*time.Minute, "a healthy static cluster", healthy)
	if v.Health.KRaftVersion != 0 {
		t.Fatalf("kraft.version = %d, want 0 for a static quorum", v.Health.KRaftVersion)
	}
	if got := c.do("POST", "/api/v1/clusters/"+id+"/operations", map[string]any{"type": "add-controller", "params": map[string]any{}}, nil); got < 400 {
		t.Errorf("add-controller on a static quorum was planned (status %d)", got)
	}
	c.run(id, name, "migrate-quorum", map[string]any{}, 15*time.Minute)
	v = waitView(t, c, id, 2*time.Minute, "kraft.version 1", func(v view) bool { return v.Health.KRaftVersion == 1 && healthy(v) })
	t.Logf("scenario 10: kraft.version %d, voters %v", v.Health.KRaftVersion, voterIDs(v))
	c.run(id, name, "add-controller", map[string]any{}, 5*time.Minute)
	waitView(t, c, id, time.Minute, "4 voters", func(v view) bool { return len(v.Snapshot.Quorum.Voters) == 4 })
}
