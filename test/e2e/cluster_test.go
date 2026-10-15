// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// dockerCluster creates a Docker lab cluster through the API (scenario 1) and deletes its
// nodes and registration when the test ends, unless BROKERIUM_E2E_KEEP=1.
func dockerCluster(t *testing.T, c *client, name string, params map[string]any) string {
	t.Helper()
	var v struct {
		Cluster struct {
			ID string `json:"id"`
		} `json:"cluster"`
	}
	c.must(http.StatusCreated, "POST", "/api/v1/clusters",
		map[string]any{"name": name, "provider": "docker", "skipTest": true, "bootstrapServers": []string{}}, &v)
	id := v.Cluster.ID
	t.Cleanup(func() {
		if os.Getenv("BROKERIUM_E2E_KEEP") == "1" {
			t.Logf("keeping cluster %s", name)
			return
		}
		var nodes struct {
			Nodes []any `json:"nodes"`
		}
		c.must(http.StatusOK, "GET", "/api/v1/nodes?clusterId="+id, nil, &nodes)
		if len(nodes.Nodes) > 0 {
			c.run(id, name, "destroy-cluster", map[string]any{}, 5*time.Minute)
		}
		c.must(http.StatusNoContent, "DELETE", "/api/v1/clusters/"+id, nil, nil)
	})
	c.run(id, name, "bootstrap-cluster", params, 6*time.Minute)
	return id
}

// produce creates a topic and writes records through the API.
func produce(t *testing.T, c *client, clusterID, topic string, partitions, records int) {
	t.Helper()
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+clusterID+"/topics",
		map[string]any{"name": topic, "partitions": partitions, "replicationFactor": 3}, nil)
	value := strings.Repeat("x", 2048)
	for i := 0; i < records; i++ {
		c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+clusterID+"/topics/"+topic+"/messages",
			map[string]any{"key": fmt.Sprintf("k%d", i), "value": value}, nil)
	}
}

func spread(brokers []broker, metric func(broker) int) int {
	if len(brokers) == 0 {
		return 0
	}
	lo, hi := metric(brokers[0]), metric(brokers[0])
	for _, b := range brokers[1:] {
		lo, hi = min(lo, metric(b)), max(hi, metric(b))
	}
	return hi - lo
}

func replicas(b broker) int { return b.Replicas }
func leaders(b broker) int  { return b.Leaders }

// waitView polls the cluster until cond holds.
func waitView(t *testing.T, c *client, id string, timeout time.Duration, what string, cond func(view) bool) view {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		v := c.view(id)
		if cond(v) {
			return v
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(3 * time.Second)
	}
}

func assertNoThrottles(t *testing.T, c *client, id string, v view) {
	t.Helper()
	var detail struct {
		Topic struct {
			Configs map[string]string `json:"configs"`
		} `json:"topic"`
	}
	for _, tp := range v.Snapshot.Topics {
		c.must(http.StatusOK, "GET", "/api/v1/clusters/"+id+"/topics/"+tp.Name, nil, &detail)
		for _, key := range []string{"leader.replication.throttled.replicas", "follower.replication.throttled.replicas"} {
			if val := detail.Topic.Configs[key]; val != "" {
				t.Errorf("topic %s still has %s=%s", tp.Name, key, val)
			}
		}
	}
	for _, b := range v.Snapshot.Brokers {
		var cfg struct {
			Configs []struct {
				Name   string `json:"name"`
				Value  string `json:"value"`
				Source string `json:"source"`
			} `json:"configs"`
		}
		c.must(http.StatusOK, "GET", fmt.Sprintf("/api/v1/clusters/%s/brokers/%d/configs", id, b.ID), nil, &cfg)
		for _, e := range cfg.Configs {
			if strings.HasSuffix(e.Name, "replication.throttled.rate") && strings.Contains(e.Source, "DYNAMIC") {
				t.Errorf("broker %d still has %s=%s (%s)", b.ID, e.Name, e.Value, e.Source)
			}
		}
	}
}

// TestScaleOutAndBalance covers scenario 1 (create a cluster from the API and load it),
// scenario 2 (add two brokers and rebalance with throttling) and scenario 6 (skew the
// cluster and balance it evenly in one operation).
func TestScaleOutAndBalance(t *testing.T) {
	heavy(t)
	c := newClient(t)
	name := uniqueName("e2e-scale-")
	// One rack per broker, so rack awareness does not force an uneven spread (lab finding 1).
	id := dockerCluster(t, c, name, map[string]any{"controllers": 1, "brokers": 3,
		"racks": []string{"r1", "r2", "r3", "r4", "r5"}})
	v := waitView(t, c, id, 2*time.Minute, "3 live brokers", func(v view) bool {
		return v.Health.Status == "ok" && len(v.Snapshot.Brokers) == 3
	})
	t.Logf("scenario 1: %s is up with %d brokers", name, len(v.Snapshot.Brokers))
	produce(t, c, id, "orders", 30, 200)

	// Scenario 2: 3 -> 5 brokers, then the throttled rebalance that add-broker runs.
	op := c.run(id, name, "add-broker", map[string]any{"count": 2}, 10*time.Minute)
	t.Logf("scenario 2: %s", op.Summary)
	v = waitView(t, c, id, 2*time.Minute, "5 live brokers, no under-replicated partitions", func(v view) bool {
		alive := 0
		for _, b := range v.Snapshot.Brokers {
			if b.Alive && !b.Fenced {
				alive++
			}
		}
		return alive == 5 && v.Health.UnderReplicated == 0
	})
	if s := spread(v.Snapshot.Brokers, replicas); s > 2 {
		t.Errorf("replica spread after scale-out = %d, want <= 2: %+v", s, v.Snapshot.Brokers)
	}
	if s := spread(v.Snapshot.Brokers, leaders); s > 2 {
		t.Errorf("leader spread after scale-out = %d, want <= 2: %+v", s, v.Snapshot.Brokers)
	}
	for _, b := range v.Snapshot.Brokers {
		if b.Replicas == 0 {
			t.Errorf("broker %d got no replicas", b.ID)
		}
	}
	assertNoThrottles(t, c, id, v)

	// Scenario 6: pile every replica of "orders" onto the first three brokers.
	var moves []map[string]any
	for _, tp := range v.Snapshot.Topics {
		if tp.Name != "orders" {
			continue
		}
		for _, p := range tp.Partitions {
			first := []int32{101, 102, 103}
			k := int(p.ID) % 3
			moves = append(moves, map[string]any{"topic": tp.Name, "partition": p.ID,
				"replicas": []int32{first[k], first[(k+1)%3], first[(k+2)%3]}})
		}
	}
	c.run(id, name, "move-partitions", map[string]any{"moves": moves}, 10*time.Minute)
	skewed := waitView(t, c, id, time.Minute, "the skew to show", func(v view) bool {
		return spread(v.Snapshot.Brokers, replicas) >= 20
	})
	t.Logf("scenario 6: skewed replica spread %d", spread(skewed.Snapshot.Brokers, replicas))

	plan := c.plan(id, "rebalance", map[string]any{"mode": "even"})
	if len(plan.Plan.Moves) == 0 {
		t.Fatal("the balancer proposes no moves on a skewed cluster")
	}
	c.must(http.StatusOK, "POST", "/api/v1/operations/"+plan.ID+"/confirm", map[string]any{}, nil)
	op = c.wait(plan.ID, 10*time.Minute)
	t.Logf("scenario 6: %s", op.Summary)
	v = waitView(t, c, id, 2*time.Minute, "an even spread", func(v view) bool {
		return spread(v.Snapshot.Brokers, replicas) <= 2 && v.Health.UnderReplicated == 0
	})
	if s := spread(v.Snapshot.Brokers, leaders); s > 2 {
		t.Errorf("leader spread after balancing = %d", s)
	}
	assertNoThrottles(t, c, id, v)
}
