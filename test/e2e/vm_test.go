// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

//go:build e2e

package e2e

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const vmServer = "brokerium-vm-server"

// vmHosts are the systemd hosts deploy/lab/vm/vm.sh starts by default. The cluster runs on the
// first two, a combined node and a broker; the third joins as a broker and is decommissioned
// again.
var vmHosts = []string{"vm-host-1", "vm-host-2", "vm-host-3"}

type agentHost struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	ClusterID      *string `json:"clusterId"`
	Role           string  `json:"role"`
	AdvertisedHost string  `json:"advertisedHost"`
	Rack           string  `json:"rack"`
	Connected      bool    `json:"connected"`
}

// TestAgentVMs installs brokerium-agent with the real install.sh on systemd hosts and builds a
// cluster on them (provider agent), choosing the hosts explicitly. The host that leaves the
// cluster is deleted at the end and its agent removed with the uninstall script. Prepare with
// deploy/lab/vm/vm.sh up and run against the containerized server:
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
	// The hosts are fixed, so an earlier registration is replaced.
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

	// Join tokens without a node name: the nodes are named after the host names.
	current := fmt.Sprintf("curl -fsSL http://%s:8080/api/v1/agent/install.sh | %%s BROKERIUM_AGENT_URL=file:///opt/brokerium-agent sh", vmServer)
	previous := os.Getenv("BROKERIUM_E2E_VM_PREVIOUS")
	for _, host := range vmHosts {
		if previous == "" {
			installAgent(t, c, map[string]any{"clusterId": id}, host, current)
			continue
		}
		for _, f := range []string{"install.sh", "brokerium-agent"} {
			if out, err := exec.Command("docker", "cp", filepath.Join(previous, f), host+":/opt/previous-"+f).CombinedOutput(); err != nil {
				t.Fatalf("copying the previous %s to %s: %v %s", f, host, err, out)
			}
		}
		installAgent(t, c, map[string]any{"clusterId": id}, host, "%s BROKERIUM_AGENT_URL=file:///opt/previous-brokerium-agent sh /opt/previous-install.sh")
	}
	hosts := waitAgents(t, c, id, vmHosts)
	combined, broker, third := hosts["vm-host-1"], hosts["vm-host-2"], hosts["vm-host-3"]
	cluster := vmHosts[:2]
	if combined.AdvertisedHost != "vm-host-1" || combined.Role != "" {
		t.Errorf("a new host advertises %q with role %q, want its host name and no role", combined.AdvertisedHost, combined.Role)
	}

	// The rack of a host is the default of the brokers it runs.
	var patched agentHost
	c.must(http.StatusOK, "PATCH", "/api/v1/nodes/"+broker.ID, map[string]any{"rack": "r1"}, &patched)
	if patched.Rack != "r1" || patched.Name != "vm-host-2" || !patched.Connected {
		t.Errorf("patched node %+v", patched)
	}
	if got := c.do("PATCH", "/api/v1/nodes/"+broker.ID, map[string]any{"name": combined.Name}, nil); got != http.StatusConflict {
		t.Errorf("renaming a node to a taken name: status %d, want 409", got)
	}
	if got := c.do("POST", "/api/v1/clusters/"+id+"/operations", map[string]any{"type": "bootstrap-cluster",
		"params": map[string]any{"nodes": []map[string]any{{"node": combined.ID, "role": "controller"}}}}, nil); got != http.StatusBadRequest {
		t.Errorf("a cluster without a broker was planned (status %d)", got)
	}

	// vm-host-1 runs broker and controller 1 in one process, vm-host-2 broker 101.
	op := c.run(id, name, "bootstrap-cluster", map[string]any{"nodes": []map[string]any{
		{"node": combined.ID, "role": "combined", "rack": "r0"}, {"node": broker.ID, "role": "broker"},
	}}, 20*time.Minute)
	t.Logf("bootstrap on the hosts: %s", op.Summary)
	v := waitView(t, c, id, 3*time.Minute, "a healthy cluster on the hosts", healthy)
	racks := map[int32]string{}
	for _, b := range v.Snapshot.Brokers {
		racks[b.ID] = b.Rack
	}
	if len(racks) != 2 || racks[1] != "r0" || racks[101] != "r1" {
		t.Errorf("brokers %+v, want the combined node 1 in rack r0 and broker 101 in rack r1", v.Snapshot.Brokers)
	}
	if got := voterIDs(v); !slices.Equal(got, []int32{1}) {
		t.Errorf("voters %v, want the combined node 1", got)
	}
	var registered struct {
		Cluster struct {
			BootstrapServers []string `json:"bootstrapServers"`
			Settings         struct {
				ControllerBootstrapServers []string `json:"controllerBootstrapServers"`
			} `json:"settings"`
		} `json:"cluster"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/clusters/"+id, nil, &registered)
	if !slices.Equal(registered.Cluster.BootstrapServers, []string{"vm-host-1:9092", "vm-host-2:9092"}) ||
		!slices.Equal(registered.Cluster.Settings.ControllerBootstrapServers, []string{"vm-host-1:9093"}) {
		t.Errorf("bootstrap servers %v and controller servers %v, want the advertised hosts of the brokers and the combined node",
			registered.Cluster.BootstrapServers, registered.Cluster.Settings.ControllerBootstrapServers)
	}
	if roles := hostOutput(t, "vm-host-1", "sed", "-n", "s/^process.roles=//p", "/var/lib/brokerium-agent/server.properties"); roles != "broker,controller" {
		t.Errorf("vm-host-1 runs process.roles=%q, want broker,controller", roles)
	}
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+id+"/topics", map[string]any{"name": "on-vms", "partitions": 3, "replicationFactor": 2}, nil)
	c.must(http.StatusCreated, "POST", "/api/v1/clusters/"+id+"/topics/on-vms/messages", map[string]any{"value": "hello"}, nil)

	if previous != "" {
		pids := map[string]string{}
		for _, host := range vmHosts {
			pids[host] = hostOutput(t, host, "systemctl", "show", "-p", "MainPID", "--value", "brokerium-kafka.service")
		}
		for _, host := range vmHosts {
			installAgent(t, c, map[string]any{"nodeId": hosts[host].ID}, host, current)
		}
		waitAgents(t, c, id, vmHosts)
		for _, host := range vmHosts {
			if pid := hostOutput(t, host, "systemctl", "show", "-p", "MainPID", "--value", "brokerium-kafka.service"); pid != pids[host] {
				t.Errorf("converting %s restarted Kafka (pid %s, was %s)", host, pid, pids[host])
			}
		}
		waitView(t, c, id, time.Minute, "a healthy cluster after converting the hosts", healthy)
	}

	// The third host joins as broker 102 and leaves again: decommissioning stops Kafka on it and
	// releases the host.
	c.run(id, name, "add-broker", map[string]any{"nodes": []map[string]any{{"node": third.ID, "rack": "r2"}}}, 15*time.Minute)
	waitView(t, c, id, 3*time.Minute, "a healthy cluster with broker 102", func(v view) bool {
		return slices.Equal(brokerIDs(v), []int32{1, 101, 102}) && healthy(v)
	})
	c.must(http.StatusOK, "GET", "/api/v1/clusters/"+id, nil, &registered)
	if !slices.Equal(registered.Cluster.BootstrapServers, []string{"vm-host-1:9092", "vm-host-2:9092", "vm-host-3:9092"}) {
		t.Errorf("bootstrap servers with broker 102: %v", registered.Cluster.BootstrapServers)
	}
	c.run(id, name, "drain-broker", map[string]any{"brokerId": 102, "decommission": true}, 15*time.Minute)
	waitView(t, c, id, 3*time.Minute, "a healthy cluster without broker 102", func(v view) bool {
		return slices.Equal(brokerIDs(v), []int32{1, 101}) && healthy(v)
	})
	var all struct {
		Nodes []agentHost `json:"nodes"`
	}
	c.must(http.StatusOK, "GET", "/api/v1/nodes", nil, &all)
	for _, n := range all.Nodes {
		if n.ID == third.ID && (n.Role != "" || n.ClusterID != nil || !n.Connected) {
			t.Errorf("the decommissioned host %+v keeps its role or cluster", n)
		}
	}
	if out := hostOutput(t, "vm-host-3", "systemctl", "is-active", "brokerium-kafka.service"); out == "active" {
		t.Error("Kafka still runs on the decommissioned host")
	}
	c.must(http.StatusOK, "GET", "/api/v1/clusters/"+id, nil, &registered)
	if !slices.Equal(registered.Cluster.BootstrapServers, []string{"vm-host-1:9092", "vm-host-2:9092"}) {
		t.Errorf("bootstrap servers after the decommission: %v", registered.Cluster.BootstrapServers)
	}
	if status, body := c.send("POST", "/api/v1/clusters/"+id+"/operations", map[string]any{"type": "drain-broker",
		"params": map[string]any{"brokerId": 1, "decommission": true}}); status != http.StatusBadRequest ||
		!strings.Contains(string(body), "node 1 (vm-host-1) is a combined node") {
		t.Errorf("decommissioning the combined node: %d %s, want 400 naming vm-host-1", status, body)
	}

	for _, host := range vmHosts {
		if user := hostOutput(t, host, "sh", "-c", "ps -o user:32= -C brokerium-agent | sort -u"); user != "brokerium-agent" {
			t.Errorf("the agent on %s runs as %q, want brokerium-agent", host, user)
		}
		if user := hostOutput(t, host, "systemctl", "show", "-p", "User", "--value", "brokerium-agent.service"); user != "brokerium-agent" {
			t.Errorf("brokerium-agent.service on %s has User=%q", host, user)
		}
	}
	if user := hostOutput(t, "vm-host-2", "sh", "-c", "ps -o user= -C java | sort -u"); user != "kafka" {
		t.Errorf("Kafka runs as %q, want kafka", user)
	}
	if out := hostOutput(t, "vm-host-2", "systemctl", "is-active", "brokerium-kafka.service"); out != "active" {
		t.Errorf("brokerium-kafka.service is %q", out)
	}

	// Restarting the agent must not stop Kafka (separate systemd unit).
	if out, err := exec.Command("docker", "exec", "vm-host-2", "systemctl", "restart", "brokerium-agent.service").CombinedOutput(); err != nil {
		t.Fatalf("restarting the agent: %v %s", err, out)
	}
	waitView(t, c, id, time.Minute, "a healthy cluster after the agent restart", healthy)

	// A host whose agent lost its identity enrolls again with a join token for its node: it keeps
	// the node with its name and Kafka role, and Kafka keeps running.
	pid := hostOutput(t, "vm-host-2", "systemctl", "show", "-p", "MainPID", "--value", "brokerium-kafka.service")
	hostOutput(t, "vm-host-2", "rm", "-f", "/var/lib/brokerium-agent/node.json")
	installAgent(t, c, map[string]any{"nodeId": broker.ID}, "vm-host-2", current)
	again := waitAgents(t, c, id, cluster)
	if n := again["vm-host-2"]; n.ID != broker.ID || n.Role != "broker" {
		t.Errorf("after enrolling again vm-host-2 is node %s with role %q, want node %s with role broker", n.ID, n.Role, broker.ID)
	}
	if n := again["vm-host-1"]; n.Role != "combined" {
		t.Errorf("vm-host-1 has role %q, want combined", n.Role)
	}
	if got := hostOutput(t, "vm-host-2", "systemctl", "show", "-p", "MainPID", "--value", "brokerium-kafka.service"); got != pid {
		t.Errorf("enrolling again restarted Kafka (pid %s, was %s)", got, pid)
	}
	waitView(t, c, id, time.Minute, "a healthy cluster after enrolling a host again", healthy)

	// The agent restarts Kafka through sudo; Kafka then runs from the static unit of the installer.
	op = c.run(id, name, "rolling-restart", map[string]any{}, 15*time.Minute)
	t.Logf("rolling restart on the hosts: %s", op.Summary)
	var restarted []string
	for _, s := range op.Steps {
		if s.Kind == "node-restart" {
			restarted = append(restarted, s.Name)
		}
	}
	if !slices.Equal(restarted, []string{"Restart vm-host-1", "Restart vm-host-2"}) {
		t.Errorf("restarts %v, want the combined node first", restarted)
	}
	waitView(t, c, id, 3*time.Minute, "a healthy cluster after the rolling restart", healthy)
	for _, host := range cluster {
		pid := hostOutput(t, host, "systemctl", "show", "-p", "MainPID", "--value", "brokerium-kafka.service")
		if umask := hostOutput(t, host, "sh", "-c", "sed -n 's/^Umask:\\t//p' /proc/"+pid+"/status"); umask != "0007" {
			t.Errorf("Kafka on %s runs with umask %q, want 0007", host, umask)
		}
	}

	// The decommissioned host leaves Brokerium: deleting its node stops its agent for good, and the
	// command of the answer removes the agent, Kafka and its data from the host.
	if got := c.do("DELETE", "/api/v1/nodes/"+broker.ID, nil, nil); got != http.StatusConflict {
		t.Errorf("deleting the node of broker 101: status %d, want 409", got)
	}
	var deleted struct {
		UninstallCommand string `json:"uninstallCommand"`
	}
	c.must(http.StatusOK, "DELETE", "/api/v1/nodes/"+third.ID, nil, &deleted)
	deadline := time.Now().Add(time.Minute)
	for hostOutput(t, "vm-host-3", "systemctl", "show", "-p", "ExecMainStatus", "--value", "brokerium-agent.service") != "6" {
		if time.Now().After(deadline) {
			t.Fatal("the agent of the deleted node did not exit with status 6")
		}
		time.Sleep(time.Second)
	}
	// The unit restarts the agent after 5 seconds unless it exits with status 6.
	time.Sleep(10 * time.Second)
	if state := hostOutput(t, "vm-host-3", "systemctl", "is-active", "brokerium-agent.service"); state != "failed" {
		t.Errorf("the agent of the deleted node is %s, want failed: systemd must not start it again", state)
	}
	if out, err := exec.Command("docker", "exec", "vm-host-3", "sh", "-c",
		deleted.UninstallCommand+" --stop-kafka --remove-kafka --delete-data").CombinedOutput(); err != nil {
		t.Fatalf("removing the agent from vm-host-3: %v\n%s", err, out)
	}
	for _, path := range []string{"/etc/systemd/system/brokerium-agent.service", "/etc/systemd/system/brokerium-kafka.service",
		"/usr/local/bin/brokerium-agent", "/etc/sudoers.d/brokerium-agent", "/etc/brokerium-agent.env", "/var/lib/brokerium-agent",
		"/opt/brokerium/kafka", "/var/lib/kafka/data"} {
		if exec.Command("docker", "exec", "vm-host-3", "test", "-e", path).Run() == nil {
			t.Errorf("the uninstall script left %s on vm-host-3", path)
		}
	}
	if exec.Command("docker", "exec", "vm-host-3", "id", "brokerium-agent").Run() == nil {
		t.Error("the uninstall script left the account brokerium-agent on vm-host-3")
	}
}

// installAgent creates a join token and runs an install command on a host; command has a %s for
// the server and the token.
func installAgent(t *testing.T, c *client, token map[string]any, host, command string) {
	t.Helper()
	var tok struct {
		Token string `json:"token"`
	}
	c.must(http.StatusCreated, "POST", "/api/v1/join-tokens", token, &tok)
	script := fmt.Sprintf(command, fmt.Sprintf("BROKERIUM_AGENT_SERVER=%s:8443 BROKERIUM_AGENT_TOKEN=%s", vmServer, tok.Token))
	if out, err := exec.Command("docker", "exec", host, "sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("installing the agent on %s: %v\n%s", host, err, out)
	}
}

// waitAgents waits until the cluster has exactly the nodes named after the hosts and their agents
// are connected; it returns the nodes by name.
func waitAgents(t *testing.T, c *client, clusterID string, hosts []string) map[string]agentHost {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	for {
		var res struct {
			Nodes []agentHost `json:"nodes"`
		}
		c.must(http.StatusOK, "GET", "/api/v1/nodes?clusterId="+clusterID, nil, &res)
		byName := map[string]agentHost{}
		connected := 0
		for _, n := range res.Nodes {
			byName[n.Name] = n
			if n.Connected && slices.Contains(hosts, n.Name) {
				connected++
			}
		}
		if connected == len(hosts) && len(res.Nodes) == len(hosts) {
			return byName
		}
		if time.Now().After(deadline) {
			t.Fatalf("%d of %d agents connected; nodes of the cluster: %+v", connected, len(hosts), res.Nodes)
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
