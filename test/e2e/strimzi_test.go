// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"net/http"
	"os"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestStrimzi drives the Strimzi lab (deploy/lab/strimzi/strimzi.sh up) through Brokerium:
// node pool scaling with rebalancing, a rolling restart, an upgrade and its finalization.
//
//	BROKERIUM_E2E_STRIMZI_KUBECONFIG=.data/kind.kubeconfig
func TestStrimzi(t *testing.T) {
	path := os.Getenv("BROKERIUM_E2E_STRIMZI_KUBECONFIG")
	if path == "" {
		t.Skip("BROKERIUM_E2E_STRIMZI_KUBECONFIG is not set")
	}
	kubeconfig, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	c := newClient(t)
	name := uniqueName("kube-lab-")
	var created struct {
		Cluster struct {
			ID string `json:"id"`
		} `json:"cluster"`
	}
	c.must(http.StatusCreated, "POST", "/api/v1/clusters", map[string]any{"name": name, "provider": "strimzi",
		"bootstrapServers": []string{"localhost:32100"}, "settings": map[string]any{"namespace": "kafka", "kafka": "lab"},
		"secrets": map[string]any{"kubeconfig": string(kubeconfig)}}, &created)
	id := created.Cluster.ID
	defer c.must(http.StatusNoContent, "DELETE", "/api/v1/clusters/"+id, nil, nil)
	waitView(t, c, id, 2*time.Minute, "a healthy Strimzi cluster", healthy)
	produce(t, c, id, "on-kubernetes", 12, 60)

	type pool struct {
		Name     string   `json:"name"`
		Roles    []string `json:"roles"`
		Replicas int32    `json:"replicas"`
		NodeIDs  []int32  `json:"nodeIds"`
	}
	pools := func() map[string]pool {
		var res struct {
			Pools []pool `json:"pools"`
		}
		c.must(http.StatusOK, "GET", "/api/v1/clusters/"+id+"/node-pools", nil, &res)
		out := map[string]pool{}
		for _, p := range res.Pools {
			out[p.Name] = p
		}
		return out
	}
	if p := pools(); p["brokers"].Replicas != 3 || !slices.Equal(p["brokers"].NodeIDs, []int32{100, 101, 102}) {
		t.Fatalf("node pools %+v", p)
	}
	if got := c.do("POST", "/api/v1/clusters/"+id+"/operations",
		map[string]any{"type": "scale-node-pool", "params": map[string]any{"pool": "controllers", "replicas": 3}}, nil); got < 400 {
		t.Error("scaling the controller pool was planned")
	}

	op := c.run(id, name, "scale-node-pool", map[string]any{"pool": "brokers", "replicas": 4}, 20*time.Minute)
	t.Logf("scale up: %s", op.Summary)
	v := waitView(t, c, id, 3*time.Minute, "4 brokers", func(v view) bool { return len(v.Snapshot.Brokers) == 4 && healthy(v) })
	if s := spread(v.Snapshot.Brokers, replicas); s > 2 {
		t.Errorf("replica spread after scaling up = %d", s)
	}

	op = c.run(id, name, "scale-node-pool", map[string]any{"pool": "brokers", "replicas": 3}, 20*time.Minute)
	t.Logf("scale down: %s", op.Summary)
	v = waitView(t, c, id, 3*time.Minute, "3 brokers", func(v view) bool { return len(v.Snapshot.Brokers) == 3 && healthy(v) })
	if slices.Contains(brokerIDs(v), 103) {
		t.Errorf("broker 103 still registered: %v", brokerIDs(v))
	}

	op = c.run(id, name, "rolling-restart", map[string]any{}, 30*time.Minute)
	t.Logf("rolling restart: %s", op.Summary)
	waitView(t, c, id, 3*time.Minute, "a healthy cluster after the restart", healthy)

	before := c.view(id).Health.MetadataVersion
	op = c.run(id, name, "rolling-upgrade", map[string]any{"version": "4.3.1"}, 45*time.Minute)
	t.Logf("upgrade: %s", op.Summary)
	v = waitView(t, c, id, 3*time.Minute, "a healthy cluster on 4.3.1", healthy)
	if v.Health.MetadataVersion != before {
		t.Errorf("metadata.version moved before finalizing: %s -> %s", before, v.Health.MetadataVersion)
	}
	c.run(id, name, "finalize-upgrade", map[string]any{}, 20*time.Minute)
	v = waitView(t, c, id, 2*time.Minute, "metadata.version 4.3", func(v view) bool { return strings.HasPrefix(v.Health.MetadataVersion, "4.3") })
	t.Logf("metadata.version %s -> %s", before, v.Health.MetadataVersion)
}
