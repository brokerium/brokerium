// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const vmServer = "brokerium-vm-server"

// TestAgentVMs installs brokerium-agent with the real install.sh on systemd hosts and builds a
// cluster on them (provider agent). Prepare with deploy/lab/vm/vm.sh up and run against the
// containerized server:
//
//	BROKERIUM_E2E_VM=1 BROKERIUM_E2E_URL=http://localhost:18080
//
// With BROKERIUM_E2E_VM_PREVIOUS set to a directory with the install.sh and the linux
// brokerium-agent binary of an earlier release, the cluster is built on hosts installed with
// those, and the current install.sh then converts the hosts while Kafka keeps running.
func TestAgentVMs(t *testing.T) {
	if os.Getenv("BROKERIUM_E2E_VM") != "1" {
		t.Skip("set BROKERIUM_E2E_VM=1 after deploy/lab/vm/vm.sh up")
	}
	c := newClient(t)
	const name = "vm-lab"
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
	current := fmt.Sprintf("curl -fsSL http://%s:8080/api/v1/agent/install.sh | %%s BROKERIUM_AGENT_URL=file:///opt/brokerium-agent sh", vmServer)
	previous := os.Getenv("BROKERIUM_E2E_VM_PREVIOUS")
	for _, host := range hosts {
		if previous == "" {
			installAgent(t, c, id, host, current)
			continue
		}
		for _, f := range []string{"install.sh", "brokerium-agent"} {
			if out, err := exec.Command("docker", "cp", filepath.Join(previous, f), host+":/opt/previous-"+f).CombinedOutput(); err != nil {
				t.Fatalf("copying the previous %s to %s: %v %s", f, host, err, out)
			}
		}
		installAgent(t, c, id, host, "%s BROKERIUM_AGENT_URL=file:///opt/previous-brokerium-agent sh /opt/previous-install.sh")
	}
	waitAgents(t, c, id, len(hosts))

	c.run(id, name, "bootstrap-cluster", map[string]any{"controllers": 1, "brokers": 1, "racks": []string{"r1"}}, 20*time.Minute)
	waitView(t, c, id, 3*time.Minute, "a healthy cluster on the hosts", healthy)
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+id+"/topics", map[string]any{"name": "on-vms", "partitions": 3, "replicationFactor": 1}, nil)
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+id+"/topics/on-vms/messages", map[string]any{"value": "hello"}, nil)

	if previous != "" {
		pids := map[string]string{}
		for _, host := range hosts {
			pids[host] = hostOutput(t, host, "systemctl", "show", "-p", "MainPID", "--value", "brokerium-kafka.service")
		}
		for _, host := range hosts {
			installAgent(t, c, id, host, current)
		}
		waitAgents(t, c, id, len(hosts))
		for _, host := range hosts {
			if pid := hostOutput(t, host, "systemctl", "show", "-p", "MainPID", "--value", "brokerium-kafka.service"); pid != pids[host] {
				t.Errorf("converting %s restarted Kafka (pid %s, was %s)", host, pid, pids[host])
			}
		}
		waitView(t, c, id, time.Minute, "a healthy cluster after converting the hosts", healthy)
	}

	for _, host := range hosts {
		if user := hostOutput(t, host, "sh", "-c", "ps -o user:32= -C brokerium-agent | sort -u"); user != "brokerium-agent" {
			t.Errorf("the agent on %s runs as %q, want brokerium-agent", host, user)
		}
		if user := hostOutput(t, host, "systemctl", "show", "-p", "User", "--value", "brokerium-agent.service"); user != "brokerium-agent" {
			t.Errorf("brokerium-agent.service on %s has User=%q", host, user)
		}
	}
	if user := hostOutput(t, name+"-b101", "sh", "-c", "ps -o user= -C java | sort -u"); user != "kafka" {
		t.Errorf("Kafka runs as %q, want kafka", user)
	}
	if out := hostOutput(t, name+"-b101", "systemctl", "is-active", "brokerium-kafka.service"); out != "active" {
		t.Errorf("brokerium-kafka.service is %q", out)
	}

	// Restarting the agent must not stop Kafka (separate systemd unit).
	if out, err := exec.Command("docker", "exec", name+"-b101", "systemctl", "restart", "brokerium-agent.service").CombinedOutput(); err != nil {
		t.Fatalf("restarting the agent: %v %s", err, out)
	}
	waitView(t, c, id, time.Minute, "a healthy cluster after the agent restart", healthy)

	// The agent restarts Kafka through sudo; Kafka then runs from the static unit of the installer.
	op := c.run(id, name, "rolling-restart", map[string]any{}, 15*time.Minute)
	t.Logf("rolling restart on the hosts: %s", op.Summary)
	waitView(t, c, id, 3*time.Minute, "a healthy cluster after the rolling restart", healthy)
	for _, host := range hosts {
		pid := hostOutput(t, host, "systemctl", "show", "-p", "MainPID", "--value", "brokerium-kafka.service")
		if umask := hostOutput(t, host, "sh", "-c", "sed -n 's/^Umask:\\t//p' /proc/"+pid+"/status"); umask != "0007" {
			t.Errorf("Kafka on %s runs with umask %q, want 0007", host, umask)
		}
	}
}

// installAgent runs an install command on a host; command has a %s for the server and the token.
func installAgent(t *testing.T, c *client, clusterID, host, command string) {
	t.Helper()
	var tok struct {
		Token string `json:"token"`
	}
	c.must(http.StatusCreated, "POST", "/api/v1/join-tokens", map[string]any{"nodeName": host, "clusterId": clusterID}, &tok)
	script := fmt.Sprintf(command, fmt.Sprintf("BROKERIUM_AGENT_SERVER=%s:8443 BROKERIUM_AGENT_TOKEN=%s", vmServer, tok.Token))
	if out, err := exec.Command("docker", "exec", host, "sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("installing the agent on %s: %v\n%s", host, err, out)
	}
}

// waitAgents waits until n agents of the cluster are connected.
func waitAgents(t *testing.T, c *client, clusterID string, n int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		var nodes struct {
			Nodes []struct {
				Connected bool `json:"connected"`
			} `json:"nodes"`
		}
		c.must(http.StatusOK, "GET", "/api/v1/nodes?clusterId="+clusterID, nil, &nodes)
		connected := 0
		for _, node := range nodes.Nodes {
			if node.Connected {
				connected++
			}
		}
		if connected == n {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d agents connected", connected, n)
		}
		time.Sleep(2 * time.Second)
	}
}

// hostOutput runs a command on a lab host and returns its trimmed output.
func hostOutput(t *testing.T, host string, args ...string) string {
	t.Helper()
	out, err := exec.Command("docker", append([]string{"exec", host}, args...)...).Output()
	if err != nil {
		t.Logf("%s on %s: %v", strings.Join(args, " "), host, err)
	}
	return strings.TrimSpace(string(out))
}
