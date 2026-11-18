// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/kafka"
)

// TestCombinedNodes runs a Docker cluster with controllers 1 and 2, combined node 3 (broker and
// controller in one process) and brokers 101 and 102: records survive a rolling restart that
// restarts the combined node with the controllers, Brokerium refuses to replace, drain or remove
// the combined node, and deleting the cluster nodes removes it.
func TestCombinedNodes(t *testing.T) {
	heavy(t)
	c := newClient(t)
	name := uniqueName("e2e-combined-")
	id := dockerCluster(t, c, name, map[string]any{"controllers": 2, "combined": 1, "brokers": 2,
		"racks": []string{"r1", "r2", "r3"}})
	v := waitView(t, c, id, 2*time.Minute, "a healthy cluster", healthy)
	if got := voterIDs(v); !slices.Equal(got, []int32{1, 2, 3}) {
		t.Errorf("voters %v, want 1, 2 and the combined node 3", got)
	}
	if got := brokerIDs(v); !slices.Equal(got, []int32{3, 101, 102}) {
		t.Errorf("brokers %v, want the combined node 3, 101 and 102", got)
	}
	combined := name + "-cb3"

	var history struct {
		Operations []operation `json:"operations"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/clusters/"+id+"/operations", nil, &history)
	warned := false
	for _, op := range history.Operations {
		for _, w := range op.Plan.Warnings {
			warned = warned || (op.Type == "bootstrap-cluster" && strings.Contains(w, "combined nodes run broker and controller"))
		}
	}
	if !warned {
		t.Error("the plan of the cluster does not warn about the combined node")
	}
	var nodes struct {
		Nodes []struct {
			Name        string `json:"name"`
			Role        string `json:"role"`
			KafkaNodeID int32  `json:"kafkaNodeId"`
		} `json:"nodes"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/nodes?clusterId="+id, nil, &nodes)
	roles := map[string]string{}
	for _, n := range nodes.Nodes {
		roles[n.Name] = fmt.Sprintf("%s %d", n.Role, n.KafkaNodeID)
	}
	if roles[combined] != "combined 3" || len(roles) != 5 {
		t.Errorf("nodes %v, want %s as combined node 3 among 5 nodes", roles, combined)
	}

	// The combined node is in the bootstrap servers and, with its controller listener, in the
	// controller servers, from which the quorum can be read without any broker.
	var registered struct {
		Cluster struct {
			BootstrapServers []string `json:"bootstrapServers"`
			Settings         struct {
				PortBase                   int      `json:"portBase"`
				ControllerBootstrapServers []string `json:"controllerBootstrapServers"`
			} `json:"settings"`
		} `json:"cluster"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/clusters/"+id, nil, &registered)
	servers, controllers := registered.Cluster.BootstrapServers, registered.Cluster.Settings.ControllerBootstrapServers
	t.Logf("bootstrap servers %v, controller servers %v", servers, controllers)
	if len(servers) != 3 || len(controllers) != 3 {
		t.Fatalf("bootstrap servers %v and controller servers %v, want three of each", servers, controllers)
	}
	if strings.HasPrefix(servers[0], "localhost:") {
		base := registered.Cluster.Settings.PortBase
		local := func(ports ...int) []string {
			var out []string
			for _, p := range ports {
				out = append(out, fmt.Sprintf("localhost:%d", base+p))
			}
			return out
		}
		if !slices.Equal(servers, local(3, 101, 102)) || !slices.Equal(controllers, local(1, 2, 903)) {
			t.Errorf("bootstrap servers %v and controller servers %v, want %v and %v", servers, controllers, local(3, 101, 102), local(1, 2, 903))
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		own, err := kafka.NewControllerClient(kafka.Profile{BootstrapServers: controllers[2:]})
		if err != nil {
			t.Fatal(err)
		}
		_, _, found, err := own.DescribeControllers(ctx)
		own.Close()
		if err != nil || len(found) != 3 {
			t.Errorf("the controller listener of the combined node lists controllers %+v: %v", found, err)
		}
		all, err := kafka.NewControllerClient(kafka.Profile{BootstrapServers: controllers})
		if err != nil {
			t.Fatal(err)
		}
		q, err := all.DescribeQuorum(ctx)
		all.Close()
		switch {
		case err != nil:
			t.Errorf("reading the quorum from the controller servers: %v", err)
		case len(q.Voters) != 3 || q.LeaderID < 0:
			t.Errorf("the quorum read from the controller servers: %+v", q)
		}
	}

	produce(t, c, id, "events", 6, 60)

	// Each refusal names the combined node.
	for _, tt := range []struct {
		typ    string
		params map[string]any
		want   string
	}{
		{"replace-broker", map[string]any{"brokerId": 3, "mode": "same-id"}, "does not replace combined nodes"},
		{"replace-broker", map[string]any{"brokerId": 3, "mode": "new-id"}, "does not replace combined nodes"},
		{"drain-broker", map[string]any{"brokerId": 3}, "does not drain or decommission combined nodes"},
		{"drain-broker", map[string]any{"brokerId": 3, "decommission": true}, "does not drain or decommission combined nodes"},
		{"remove-controller", map[string]any{"nodeId": 3}, "does not remove combined nodes from the quorum"},
	} {
		status, body := c.send("POST", "/api/v1/clusters/"+id+"/operations", map[string]any{"type": tt.typ, "params": tt.params})
		if status != http.StatusBadRequest || !strings.Contains(string(body), "node 3 ("+combined+") is a combined node") ||
			!strings.Contains(string(body), tt.want) {
			t.Errorf("%s %v: %d %s, want 400 with %q", tt.typ, tt.params, status, body, tt.want)
		}
	}
	cordon := c.plan(id, "cordon-broker", map[string]any{"brokerId": 3, "cordon": true})
	c.must(http.StatusNoContent, "DELETE", "/api/v1/operations/"+cordon.ID, nil, nil)

	// The combined node restarts with the controllers, the quorum leader last, then the brokers.
	leader := c.view(id).Health.QuorumLeader
	op := c.run(id, name, "rolling-restart", map[string]any{}, 15*time.Minute)
	var restarted []string
	for _, s := range op.Steps {
		if s.Kind == "node-restart" {
			restarted = append(restarted, strings.TrimPrefix(s.Name, "Restart "))
		}
	}
	container := map[int32]string{1: name + "-c1", 2: name + "-c2", 3: combined}
	if len(restarted) != 5 || !slices.Contains(restarted[:3], combined) || restarted[2] != container[leader] ||
		!slices.Equal(restarted[3:], []string{name + "-b101", name + "-b102"}) {
		t.Errorf("restart order %v, want the controllers and %s with leader %d last, then the brokers", restarted, combined, leader)
	}
	waitView(t, c, id, 3*time.Minute, "a healthy cluster after the rolling restart", healthy)
	var browsed struct {
		Messages []any `json:"messages"`
	}
	total := 0
	for p := 0; p < 6; p++ {
		c.must(http.StatusOK, "GET", fmt.Sprintf("/api/v1/clusters/%s/topics/events/messages?from=earliest&limit=100&partition=%d", id, p), nil, &browsed)
		total += len(browsed.Messages)
	}
	if total != 60 {
		t.Errorf("read %d of the 60 records after the rolling restart", total)
	}

	op = c.run(id, name, "destroy-cluster", map[string]any{}, 5*time.Minute)
	if !strings.Contains(op.Summary, "Delete 5 node(s)") {
		t.Errorf("destroy summary %q", op.Summary)
	}
	c.must(http.StatusOK, "GET", "/api/v1/nodes?clusterId="+id, nil, &nodes)
	if len(nodes.Nodes) != 0 {
		t.Errorf("nodes left after deleting the cluster nodes: %+v", nodes.Nodes)
	}
	if out, err := exec.Command("docker", "ps", "-a", "--filter", "name=^"+name+"-", "--format", "{{.Names}}").Output(); err != nil ||
		strings.TrimSpace(string(out)) != "" {
		t.Errorf("containers left: %q %v", out, err)
	}
}
