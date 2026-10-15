// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestAgentVMs installs brokerium-agent with the real install.sh on systemd hosts and builds a
// cluster on them (provider agent). Prepare with deploy/lab/vm/vm.sh up and run against the
// containerized server:
//
//	BROKERIUM_E2E_VM=1 BROKERIUM_E2E_URL=http://localhost:18080
func TestAgentVMs(t *testing.T) {
	if os.Getenv("BROKERIUM_E2E_VM") != "1" {
		t.Skip("set BROKERIUM_E2E_VM=1 after deploy/lab/vm/vm.sh up")
	}
	c := newClient(t)
	const name, server = "vm-lab", "brokerium-vm-server"
	// Host names are fixed (vm-lab-c1, vm-lab-b101), so an earlier registration is replaced.
	var list struct {
		Clusters []summary `json:"clusters"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/clusters", nil, &list)
	for _, s := range list.Clusters {
		if s.Cluster.Name == name {
			c.must(http.StatusNoContent, "DELETE", "/api/v1/clusters/"+s.Cluster.ID, nil, nil)
		}
	}
	var created struct {
		Cluster struct {
			ID string `json:"id"`
		} `json:"cluster"`
	}
	c.must(http.StatusCreated, "POST", "/api/v1/clusters", map[string]any{"name": name, "provider": "agent", "skipTest": true,
		"bootstrapServers": []string{}}, &created)
	id := created.Cluster.ID

	hosts := []string{name + "-c1", name + "-b101"}
	for _, host := range hosts {
		var tok struct {
			Token string `json:"token"`
		}
		c.must(http.StatusCreated, "POST", "/api/v1/join-tokens", map[string]any{"nodeName": host, "clusterId": id}, &tok)
		script := fmt.Sprintf("curl -fsSL http://%s:8080/api/v1/agent/install.sh | BROKERIUM_AGENT_SERVER=%s:8443 "+
			"BROKERIUM_AGENT_TOKEN=%s BROKERIUM_AGENT_URL=file:///opt/brokerium-agent sh", server, server, tok.Token)
		if out, err := exec.Command("docker", "exec", host, "sh", "-c", script).CombinedOutput(); err != nil {
			t.Fatalf("installing the agent on %s: %v\n%s", host, err, out)
		}
	}
	deadline := time.Now().Add(2 * time.Minute)
	for {
		var nodes struct {
			Nodes []struct {
				Connected bool `json:"connected"`
			} `json:"nodes"`
		}
		c.must(http.StatusOK, "GET", "/api/v1/nodes?clusterId="+id, nil, &nodes)
		connected := 0
		for _, n := range nodes.Nodes {
			if n.Connected {
				connected++
			}
		}
		if connected == len(hosts) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d agents connected", connected, len(hosts))
		}
		time.Sleep(2 * time.Second)
	}

	c.run(id, name, "bootstrap-cluster", map[string]any{"controllers": 1, "brokers": 1, "racks": []string{"r1"}}, 20*time.Minute)
	waitView(t, c, id, 3*time.Minute, "a healthy cluster on the hosts", healthy)
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+id+"/topics", map[string]any{"name": "on-vms", "partitions": 3, "replicationFactor": 1}, nil)
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+id+"/topics/on-vms/messages", map[string]any{"value": "hello"}, nil)

	user, err := exec.Command("docker", "exec", name+"-b101", "sh", "-c", "ps -o user= -C java | sort -u").Output()
	if err != nil || strings.TrimSpace(string(user)) != "kafka" {
		t.Errorf("Kafka runs as %q (%v), want kafka", strings.TrimSpace(string(user)), err)
	}
	if out, err := exec.Command("docker", "exec", name+"-b101", "systemctl", "is-active", "brokerium-kafka.service").Output(); err != nil ||
		strings.TrimSpace(string(out)) != "active" {
		t.Errorf("brokerium-kafka.service is %q (%v)", strings.TrimSpace(string(out)), err)
	}

	// Restarting the agent must not stop Kafka (separate systemd unit).
	if out, err := exec.Command("docker", "exec", name+"-b101", "systemctl", "restart", "brokerium-agent.service").CombinedOutput(); err != nil {
		t.Fatalf("restarting the agent: %v %s", err, out)
	}
	waitView(t, c, id, time.Minute, "a healthy cluster after the agent restart", healthy)

	op := c.run(id, name, "rolling-restart", map[string]any{}, 15*time.Minute)
	t.Logf("rolling restart on the hosts: %s", op.Summary)
	waitView(t, c, id, 3*time.Minute, "a healthy cluster after the rolling restart", healthy)
}
