// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net"
	"strings"
	"time"

	"github.com/brokerium/brokerium/internal/agentcfg"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/provider/docker"
	"github.com/brokerium/brokerium/internal/store"
	agentv1 "github.com/brokerium/brokerium/proto/brokerium/agent/v1"
)

// Step kinds of node lifecycle operations.
const (
	KindSetSettings   = "node-set-settings"
	KindCheckHosts    = "node-check-hosts"
	KindProvision     = "node-provision"
	KindWaitAgent     = "node-wait-agent"
	KindInstall       = "node-install"
	KindConfigure     = "node-configure"
	KindFormat        = "node-format"
	KindStart         = "node-start"
	KindStop          = "node-stop"
	KindWaitBroker    = "node-wait-broker"
	KindWaitObserver  = "node-wait-observer"
	KindAddVoter      = "node-add-voter"
	KindWaitQuorum    = "node-wait-quorum"
	KindWaitInSync    = "node-wait-in-sync"
	KindRemoveNode    = "node-remove"
	KindRestart       = "node-restart"
	KindUpgrade       = "node-upgrade"
	KindSyncBootstrap = "node-sync-bootstrap"
	KindRebalance     = "node-rebalance"
)

// NodeArgs are the arguments of node steps. Steps address agent nodes by Node, the agent node
// id, and Docker nodes by Name, their container name.
type NodeArgs struct {
	Name string `json:"name,omitempty"`
	Node string `json:"node,omitempty"`
	// Host is the advertised host of an agent node at planning time.
	Host               string    `json:"host,omitempty"`
	Role               string    `json:"role,omitempty"`
	NodeID             int32     `json:"nodeId,omitempty"`
	Rack               string    `json:"rack,omitempty"`
	Version            string    `json:"version,omitempty"`
	Mode               string    `json:"mode,omitempty"`
	InitialControllers string    `json:"initialControllers,omitempty"`
	QuorumKind         string    `json:"quorumKind,omitempty"`
	Quorum             string    `json:"quorum,omitempty"`
	ExpectVoters       int       `json:"expectVoters,omitempty"`
	TimeoutSeconds     int       `json:"timeoutSeconds,omitempty"`
	Settings           *Settings `json:"settings,omitempty"`
	BootstrapServers   []string  `json:"bootstrapServers,omitempty"`
}

// HostsArgs are the arguments of the step that checks the chosen hosts of an agent cluster.
type HostsArgs struct {
	KafkaClusterID string     `json:"kafkaClusterId"`
	Hosts          []NodeArgs `json:"hosts"`
}

func (m *Manager) StepKinds() map[string]ops.StepFunc {
	return map[string]ops.StepFunc{
		KindSetSettings:   m.stepSetSettings,
		KindCheckHosts:    m.stepCheckHosts,
		KindProvision:     m.stepProvision,
		KindWaitAgent:     m.stepWaitAgent,
		KindInstall:       m.stepInstall,
		KindConfigure:     m.stepConfigure,
		KindFormat:        m.stepFormat,
		KindStart:         m.stepStart,
		KindStop:          m.stepStop,
		KindWaitBroker:    m.stepWaitBroker,
		KindWaitObserver:  m.stepWaitObserver,
		KindAddVoter:      m.stepAddVoter,
		KindWaitQuorum:    m.stepWaitQuorum,
		KindWaitInSync:    m.stepWaitInSync,
		KindRemoveNode:    m.stepRemoveNode,
		KindRestart:       m.stepRestart,
		KindUpgrade:       m.stepUpgrade,
		KindSyncBootstrap: m.stepSyncBootstrap,
		KindRebalance:     m.stepRebalance,

		KindStrimziScale:    m.stepStrimziScale,
		KindStrimziWaitPool: m.stepStrimziWaitPool,
		KindStrimziRoll:     m.stepStrimziRoll,
		KindStrimziVersion:  m.stepStrimziVersion,
		KindStrimziMetadata: m.stepStrimziMetadata,
	}
}

func args(raw json.RawMessage) (NodeArgs, error) {
	var a NodeArgs
	if len(raw) == 0 {
		return a, nil
	}
	err := json.Unmarshal(raw, &a)
	return a, err
}

func timeoutOf(a NodeArgs, def time.Duration) time.Duration {
	if a.TimeoutSeconds > 0 {
		return time.Duration(a.TimeoutSeconds) * time.Second
	}
	return def
}

// final marks an error of a poll check that waiting cannot resolve; poll returns it at once.
type final struct{ error }

func (f final) Unwrap() error { return f.error }

func poll(ctx context.Context, x *ops.Exec, timeout time.Duration, every time.Duration, what string,
	check func(context.Context) (bool, any, error)) (any, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ok, detail, err := check(ctx)
		if err == nil && ok {
			return detail, nil
		}
		var f final
		if errors.As(err, &f) {
			return detail, f.error
		}
		lastErr = err
		x.Progress(map[string]any{"waitingFor": what, "detail": detail, "error": fmt.Sprint(err)})
		if time.Now().After(deadline) {
			if lastErr != nil {
				return detail, fmt.Errorf("timed out after %s waiting for %s: %w", timeout, what, lastErr)
			}
			return detail, fmt.Errorf("timed out after %s waiting for %s", timeout, what)
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(every):
		}
	}
}

// node returns the agent node of step arguments. Operations planned before agent nodes were
// chosen by id address them by name, like Docker nodes.
func (m *Manager) node(ctx context.Context, a NodeArgs) (*store.Node, error) {
	var n *store.Node
	var err error
	if a.Node != "" {
		n, err = m.Store.GetNode(ctx, a.Node)
	} else {
		n, err = m.Store.GetNodeByName(ctx, a.Name)
	}
	if err != nil {
		return nil, fmt.Errorf("node %s: %w", a.Name, err)
	}
	return n, nil
}

func (m *Manager) send(ctx context.Context, a NodeArgs, cmd *agentv1.Command, timeout time.Duration) (*agentv1.CommandResult, error) {
	n, err := m.node(ctx, a)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return m.Gateway.Send(cctx, n.ID, cmd)
}

// checkHost checks again, when a step runs, that the chosen host of a can run its Kafka node.
// Without needConnected an agent that is not connected passes; the next step waits for it.
func (m *Manager) checkHost(ctx context.Context, clusterID string, a NodeArgs, kafkaClusterID string, needConnected bool) (*store.Node, error) {
	n, err := m.Store.GetNode(ctx, a.Node)
	if errors.Is(err, store.ErrNotFound) {
		return nil, fmt.Errorf("host %s no longer exists", a.Name)
	} else if err != nil {
		return nil, err
	}
	if err := claimable(n, clusterID, a.Role, a.NodeID); err != nil {
		return nil, err
	}
	if needConnected && !m.Gateway.Connected(n.ID) {
		return nil, fmt.Errorf("host %s is not connected", n.Name)
	}
	if a.Host != "" && n.AdvertisedHost != a.Host {
		return nil, fmt.Errorf("the advertised host of %s changed from %s to %s after planning; plan again", n.Name, a.Host, n.AdvertisedHost)
	}
	if err := checkStorage(n, kafkaClusterID, a.NodeID); err != nil {
		return nil, err
	}
	return n, nil
}

func (m *Manager) stepSetSettings(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	c, err := m.Store.GetCluster(ctx, x.Cluster.ID)
	if err != nil {
		return nil, err
	}
	if a.Settings != nil {
		// Keys that Brokerium does not read stay.
		maps.Copy(c.Settings, a.Settings.toMap())
	}
	if len(a.BootstrapServers) > 0 {
		c.BootstrapServers = a.BootstrapServers
	}
	if err := m.Store.UpdateCluster(ctx, c); err != nil {
		return nil, err
	}
	x.Cluster = c
	m.Clusters.Invalidate(c.ID)
	cl, _, err := m.Clusters.Client(ctx, c.ID)
	if err == nil {
		x.Client = cl
	}
	return map[string]any{"updated": true}, nil
}

func (m *Manager) stepCheckHosts(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	var a HostsArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, err
	}
	for _, h := range a.Hosts {
		if _, err := m.checkHost(ctx, x.Cluster.ID, h, a.KafkaClusterID, true); err != nil {
			return nil, err
		}
	}
	return map[string]any{"hosts": len(a.Hosts)}, nil
}

func (m *Manager) stepProvision(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	if x.Cluster.Provider != store.ProviderDocker {
		if a.Node == "" {
			if _, err := m.node(ctx, a); err != nil {
				return nil, fmt.Errorf("install brokerium-agent on the host first (Nodes page, join token for %s): %w", a.Name, err)
			}
			return map[string]any{"node": a.Name, "provisioned": "existing agent"}, nil
		}
		n, err := m.checkHost(ctx, x.Cluster.ID, a, settingsOf(x.Cluster).KafkaClusterID, false)
		if err != nil {
			return nil, err
		}
		return map[string]any{"node": n.ID, "name": n.Name, "advertisedHost": n.AdvertisedHost}, nil
	}
	if m.Docker == nil {
		return nil, errors.New("docker provider is not available on this server")
	}
	if info, err := m.Docker.Inspect(ctx, a.Name); err == nil {
		if !info.Running {
			if err := m.Docker.Start(ctx, a.Name); err != nil {
				return nil, err
			}
		}
		return map[string]any{"container": info.ID, "reused": true}, nil
	}
	s := settingsOf(x.Cluster)
	token, _, err := m.Gateway.NewJoinToken(ctx, store.JoinToken{NodeName: a.Name, ClusterID: &x.Cluster.ID, CreatedBy: "brokerium"},
		2*time.Hour)
	if err != nil {
		return nil, err
	}
	image := s.Image
	if image == "" {
		image = m.Cfg.AgentImage
	}
	spec := docker.ContainerSpec{
		Name: a.Name, Image: image, Hostname: a.Name, Network: m.Cfg.LabNetwork,
		Env: []string{"BROKERIUM_AGENT_SERVER=" + m.Cfg.AgentEndpoint, "BROKERIUM_AGENT_TOKEN=" + token},
		Volumes: map[string]string{a.Name + "-data": defaultDataDir, a.Name + "-agent": "/var/lib/brokerium-agent",
			a.Name + "-kafka": "/opt/brokerium/kafka"},
		Labels: map[string]string{"brokerium.cluster": x.Cluster.Name, "brokerium.node": a.Name, "brokerium.role": a.Role},
		Ports:  containerPorts(s, a.Role, a.NodeID),
	}
	id, err := m.Docker.Run(ctx, spec)
	if err != nil {
		return nil, err
	}
	return map[string]any{"container": id, "image": image}, nil
}

// containerPorts maps the listener ports of a node of a Docker cluster to the ports on localhost:
// the client listener of brokers and combined nodes, the controller listener of controllers and
// combined nodes.
func containerPorts(s Settings, role string, id int32) map[int]int {
	port := s.PortBase + int(id)
	switch role {
	case RoleBroker:
		return map[int]int{port: port}
	case RoleCombined:
		return map[int]int{port: port, 9093: controllerPort(s, role, id)}
	}
	return map[int]int{9093: port}
}

// stepWaitAgent waits until the agent of the node is connected and gives the node its Kafka
// role and id in the cluster.
func (m *Manager) stepWaitAgent(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	if a.Node != "" {
		if _, err := m.node(ctx, a); err != nil {
			return nil, err
		}
	}
	var n *store.Node
	if _, err := poll(ctx, x, timeoutOf(a, 3*time.Minute), 2*time.Second, "agent "+a.Name+" to connect",
		func(ctx context.Context) (bool, any, error) {
			found, err := m.node(ctx, a)
			if err != nil {
				return false, nil, nil
			}
			if !m.Gateway.Connected(found.ID) {
				return false, map[string]any{"enrolled": true}, nil
			}
			n = found
			return true, nil, nil
		}); err != nil {
		return nil, err
	}
	if err := m.Store.ClaimNode(ctx, n.ID, x.Cluster.ID, a.Role, a.NodeID); errors.Is(err, store.ErrConflict) {
		return nil, fmt.Errorf("host %s cannot become %s %d: it has another Kafka role or cluster, or another node of the cluster has id %d",
			n.Name, a.Role, a.NodeID, a.NodeID)
	} else if err != nil {
		return nil, err
	}
	return map[string]any{"node": n.ID, "connected": true}, nil
}

func (m *Manager) stepInstall(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	version := a.Version
	if version == "" {
		version = settingsOf(x.Cluster).KafkaVersion
	}
	res, err := m.send(ctx, a, m.installCommand(version), 30*time.Minute)
	if err != nil {
		return nil, err
	}
	return map[string]any{"version": version, "output": res.GetOutput()}, nil
}

// installCommand asks an agent to install a Kafka release, from the configured mirror first.
func (m *Manager) installCommand(version string) *agentv1.Command {
	return &agentv1.Command{Action: &agentv1.Command_Install{Install: &agentv1.InstallKafka{Version: version, MirrorUrl: m.Cfg.KafkaMirror}}}
}

// quorumFor returns the quorum configuration a node should use now.
func (m *Manager) quorumFor(ctx context.Context, x *ops.Exec, a NodeArgs) (string, string, error) {
	if a.Quorum != "" {
		kind := a.QuorumKind
		if kind == "" {
			kind = "dynamic"
		}
		return kind, a.Quorum, nil
	}
	mn, err := m.managedNodes(ctx, x.Cluster.ID)
	if err != nil {
		return "", "", err
	}
	docker := x.Cluster.Provider == store.ProviderDocker
	var hosts, voters []string
	for _, c := range mn.controllers {
		h := hostOf(docker, c)
		hosts = append(hosts, h)
		voters = append(voters, fmt.Sprintf("%d@%s", *c.KafkaNodeID, net.JoinHostPort(h, "9093")))
	}
	if len(hosts) == 0 {
		return "", "", errors.New("the cluster has no managed controllers")
	}
	if settingsOf(x.Cluster).Quorum == "static" {
		return "static", strings.Join(voters, ","), nil
	}
	return "dynamic", quorumBootstrap(hosts), nil
}

// advertised is the host the listeners of node n advertise: its container name on Docker, else
// the advertised host the plan recorded or the one the node has now.
func advertised(x *ops.Exec, a NodeArgs, n *store.Node) string {
	if x.Cluster.Provider == store.ProviderDocker {
		return a.Name
	}
	return cmp.Or(a.Host, hostOf(false, n))
}

func (m *Manager) stepConfigure(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	n, err := m.node(ctx, a)
	if err != nil {
		return nil, err
	}
	if adopted(n) {
		return map[string]any{"skipped": "adopted node keeps its own server.properties"}, nil
	}
	host := advertised(x, a, n)
	if err := agentcfg.ValidateHost(host); err != nil {
		return nil, fmt.Errorf("advertised host of %s: %w", n.Name, err)
	}
	docker := x.Cluster.Provider == store.ProviderDocker
	dataDir, err := dataDirOf(docker, n)
	if err != nil {
		return nil, err
	}
	s := settingsOf(x.Cluster)
	kind, quorum, err := m.quorumFor(ctx, x, a)
	if err != nil {
		return nil, err
	}
	props := renderProperties(s, docker, a.Role, host, dataDir, a.NodeID, a.Rack, kind, quorum)
	heap := s.BrokerHeap
	if a.Role == RoleController {
		heap = s.ControllerHeap
	}
	if err := agentcfg.Validate(props, heap); err != nil {
		return nil, fmt.Errorf("configuration of %s: %w", a.Name, err)
	}
	if _, err := m.send(ctx, a, &agentv1.Command{Action: &agentv1.Command_WriteConfig{WriteConfig: &agentv1.WriteConfig{
		Properties: props, HeapOpts: heap}}}, time.Minute); err != nil {
		return nil, err
	}
	return map[string]any{"quorumKind": kind, "quorum": quorum, "advertisedHost": host, "dataDir": dataDir}, nil
}

func (m *Manager) stepFormat(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	mode := agentv1.FormatMode_FORMAT_MODE_JOIN
	switch a.Mode {
	case "initial":
		mode = agentv1.FormatMode_FORMAT_MODE_INITIAL
	case "standalone":
		mode = agentv1.FormatMode_FORMAT_MODE_STANDALONE
	case "static":
		mode = agentv1.FormatMode_FORMAT_MODE_UNSPECIFIED
	}
	clusterID := settingsOf(x.Cluster).KafkaClusterID
	if clusterID == "" {
		return nil, errors.New("the cluster has no Kafka cluster id in its settings")
	}
	res, err := m.send(ctx, a, &agentv1.Command{Action: &agentv1.Command_Format{Format: &agentv1.FormatStorage{
		ClusterId: clusterID, Mode: mode, InitialControllers: a.InitialControllers}}}, 3*time.Minute)
	if err != nil {
		return nil, err
	}
	return map[string]any{"output": res.GetOutput()}, nil
}

func (m *Manager) stepStart(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	if _, err := m.hostStatus(ctx, a); err != nil {
		return nil, err
	}
	if _, err := m.start(ctx, a); err != nil {
		return nil, err
	}
	return map[string]any{"started": a.Name}, nil
}

// start starts Kafka on node a with the release of a, else the active one, and returns the
// version the agent starts.
func (m *Manager) start(ctx context.Context, a NodeArgs) (string, error) {
	res, err := m.send(ctx, a, &agentv1.Command{Action: &agentv1.Command_Start{Start: &agentv1.StartKafka{Version: a.Version}}}, time.Minute)
	if err != nil {
		return "", err
	}
	return cmp.Or(a.Version, res.GetStatus().GetActiveVersion()), nil
}

func statusCommand() *agentv1.Command {
	return &agentv1.Command{Action: &agentv1.Command_Status{Status: &agentv1.GetStatus{}}}
}

// hostStatus asks the agent of node a for its status and refuses a host with Kafka processes that
// the agent does not run.
func (m *Manager) hostStatus(ctx context.Context, a NodeArgs) (*agentv1.NodeStatus, error) {
	res, err := m.send(ctx, a, statusCommand(), 30*time.Second)
	if err != nil {
		return nil, err
	}
	return res.GetStatus(), foreignKafka(a.Name, res.GetStatus())
}

// foreignKafka refuses Kafka processes on a host that its agent does not run, such as the
// operator's own unit of an adopted node: they keep the ports, so the Kafka the agent starts
// fails while the old one goes on answering.
func foreignKafka(node string, st *agentv1.NodeStatus) error {
	if len(st.GetForeignKafka()) == 0 {
		return nil
	}
	procs := make([]string, 0, len(st.GetForeignKafka()))
	for _, p := range st.GetForeignKafka() {
		procs = append(procs, fmt.Sprintf("process %d of user %s", p.GetPid(), cmp.Or(p.GetUser(), "unknown")))
	}
	return fmt.Errorf("%s runs Kafka that brokerium-agent does not manage (%s); stop the operator's own Kafka unit first "+
		"(for example sudo systemctl disable --now kafka.service), then run the operation again", node, strings.Join(procs, ", "))
}

// waitNewKafka waits until the agent of node a runs a Kafka process that started after before,
// then checks that it runs version want. Agents before 0.3.2 report no process and skip the
// check.
func (m *Manager) waitNewKafka(ctx context.Context, x *ops.Exec, a NodeArgs, before *agentv1.KafkaProcess, want string) (any, error) {
	if before == nil {
		return map[string]any{"skipped": "the agent does not report its Kafka process"}, nil
	}
	return poll(ctx, x, 2*time.Minute, 2*time.Second, "a new Kafka process on "+a.Name,
		func(ctx context.Context) (bool, any, error) {
			res, err := m.send(ctx, a, statusCommand(), 30*time.Second)
			if err != nil {
				return false, nil, err
			}
			done, detail, err := checkNewKafka(a.Name, before, res.GetStatus(), want)
			if err != nil {
				return false, detail, final{err}
			}
			return done, detail, nil
		})
}

// startTimeSlack is how much the start time of one process may move between two reports: agents
// add its clock ticks since boot to the boot time in whole seconds.
const startTimeSlack = 2 * time.Second

// checkNewKafka compares the Kafka process an agent reports after a start with the one before
// it: done once another process runs, failed when that process runs another version than want or
// a Kafka process the agent does not run is on the host. A process whose classpath names no Kafka
// version passes.
func checkNewKafka(node string, before *agentv1.KafkaProcess, st *agentv1.NodeStatus, want string) (bool, map[string]any, error) {
	p := st.GetKafkaProcess()
	detail := map[string]any{"pid": p.GetPid(), "version": p.GetVersion(), "previousPid": before.GetPid()}
	if err := foreignKafka(node, st); err != nil {
		return false, detail, err
	}
	switch {
	case p.GetPid() == 0:
		detail["lastExit"] = st.GetLastExit()
		return false, detail, nil
	case p.GetPid() == before.GetPid() && p.GetStartTimeMs() <= before.GetStartTimeMs()+startTimeSlack.Milliseconds():
		return false, detail, nil
	}
	if p.GetMixedVersions() {
		detail["mixedVersions"] = true
	}
	if p.GetVersion() != "" && want != "" && p.GetVersion() != want {
		err := fmt.Errorf("%s runs Kafka %s in process %d after the start, not %s", node, p.GetVersion(), p.GetPid(), want)
		if p.GetMixedVersions() {
			err = fmt.Errorf("%w; its classpath holds jars of several Kafka versions, check CLASSPATH in the environment of Kafka", err)
		}
		return false, detail, err
	}
	return true, detail, nil
}

func stopKafka() *agentv1.Command {
	return &agentv1.Command{Action: &agentv1.Command_Stop{Stop: &agentv1.StopKafka{TimeoutSeconds: 120}}}
}

func (m *Manager) stepStop(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	n, err := m.node(ctx, a)
	if err == nil && m.Gateway.Connected(n.ID) {
		if _, err := m.send(ctx, a, stopKafka(), 3*time.Minute); err != nil {
			return nil, err
		}
		return map[string]any{"stopped": a.Name}, nil
	}
	if x.Cluster.Provider == store.ProviderDocker && m.Docker != nil {
		if err := m.Docker.Stop(ctx, a.Name, 2*time.Minute); err != nil && !errors.Is(err, docker.ErrNotFound) {
			return nil, err
		}
		return map[string]any{"stopped": a.Name, "via": "docker"}, nil
	}
	return nil, fmt.Errorf("agent of %s is not connected", a.Name)
}

func (m *Manager) stepRemoveNode(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	docker := x.Cluster.Provider == store.ProviderDocker
	if docker && m.Docker != nil {
		if err := m.Docker.Remove(ctx, a.Name, a.Name+"-data", a.Name+"-agent", a.Name+"-kafka"); err != nil {
			return nil, err
		}
	}
	detail := map[string]any{"removed": a.Name}
	if n, err := m.node(ctx, a); err == nil {
		switch {
		case docker:
			_ = m.Store.DeleteNode(ctx, n.ID)
		case holds(n, x.Cluster.ID, a.NodeID):
			// An agent host keeps running Kafka unless it is stopped; a host that is down is
			// released now and stops Kafka when its agent connects again.
			connected := m.Gateway.Connected(n.ID)
			if connected {
				if _, err := m.send(ctx, a, stopKafka(), 3*time.Minute); err != nil {
					return nil, fmt.Errorf("stopping Kafka on %s: %w", n.Name, err)
				}
			}
			if err := m.Store.ReleaseNode(ctx, n.ID, x.Cluster.ID, a.NodeID, !connected); err != nil {
				return nil, err
			}
			detail["kafkaStopped"] = connected
		}
	}
	if _, err := m.stepSyncBootstrap(ctx, x, nil); err != nil {
		return nil, fmt.Errorf("updating bootstrap servers: %w", err)
	}
	return detail, nil
}

func brokerReady(v *cluster.View, id int32) (bool, any) {
	for _, b := range v.Snapshot.Brokers {
		if b.ID == id {
			return b.Alive && !b.Fenced, map[string]any{"registered": true, "alive": b.Alive, "fenced": b.Fenced}
		}
	}
	return false, map[string]any{"registered": false}
}

func (m *Manager) stepWaitBroker(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	return poll(ctx, x, timeoutOf(a, 4*time.Minute), 3*time.Second, fmt.Sprintf("broker %d to register and unfence", a.NodeID),
		func(ctx context.Context) (bool, any, error) {
			st, err := x.View(ctx)
			if err != nil {
				return false, nil, err
			}
			ok, detail := brokerReady(st.View, a.NodeID)
			return ok, detail, nil
		})
}

func (m *Manager) stepWaitObserver(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	return poll(ctx, x, timeoutOf(a, 4*time.Minute), 3*time.Second, fmt.Sprintf("controller %d to catch up", a.NodeID),
		func(ctx context.Context) (bool, any, error) {
			q, err := x.Client.DescribeQuorum(ctx)
			if err != nil {
				return false, nil, err
			}
			if r, ok := q.Voter(a.NodeID); ok {
				return true, map[string]any{"voter": true, "lag": r.Lag}, nil
			}
			if r, ok := q.Observer(a.NodeID); ok {
				caughtUp := r.Lag <= 100 && time.Since(time.UnixMilli(r.LastCaughtUpTimestamp)) < 10*time.Second
				return caughtUp, map[string]any{"observer": true, "lag": r.Lag, "directoryId": r.DirectoryID}, nil
			}
			return false, map[string]any{"seen": false}, nil
		})
}

func (m *Manager) stepAddVoter(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	q, err := x.Client.DescribeQuorum(ctx)
	if err != nil {
		return nil, err
	}
	if _, ok := q.Voter(a.NodeID); ok {
		return map[string]any{"alreadyVoter": true}, nil
	}
	if len(q.Voters) >= maxVoters {
		return nil, fmt.Errorf("the quorum already has %d voters; Brokerium builds quorums of at most %d", len(q.Voters), maxVoters)
	}
	n, err := m.node(ctx, a)
	if err != nil {
		return nil, err
	}
	res, err := m.send(ctx, a, statusCommand(), 30*time.Second)
	if err != nil {
		return nil, err
	}
	dir := res.GetStatus().GetMeta().GetDirectoryId()
	if dir == "" {
		return nil, errors.New("the node reports no directory.id; is its storage formatted?")
	}
	clusterID := settingsOf(x.Cluster).KafkaClusterID
	host := advertised(x, a, n)
	if err := x.Client.AddRaftVoter(ctx, clusterID, a.NodeID, dir,
		[]kafka.Endpoint{{Name: "CONTROLLER", Host: host, Port: 9093}}); err != nil {
		return nil, err
	}
	return map[string]any{"nodeId": a.NodeID, "directoryId": dir, "host": host}, nil
}

func (m *Manager) stepWaitQuorum(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	return poll(ctx, x, timeoutOf(a, 4*time.Minute), 3*time.Second, fmt.Sprintf("a quorum of %d voters", a.ExpectVoters),
		func(ctx context.Context) (bool, any, error) {
			q, err := x.Client.DescribeQuorum(ctx)
			if err != nil {
				return false, nil, err
			}
			lagging := 0
			for _, v := range q.Voters {
				if v.Lag > cluster.VoterLagThreshold {
					lagging++
				}
			}
			ok := q.LeaderID >= 0 && lagging == 0 && (a.ExpectVoters == 0 || len(q.Voters) == a.ExpectVoters)
			return ok, map[string]any{"leader": q.LeaderID, "voters": len(q.Voters), "lagging": lagging}, nil
		})
}

func (m *Manager) stepWaitInSync(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	return poll(ctx, x, timeoutOf(a, 30*time.Minute), 5*time.Second, "all replicas in sync",
		func(ctx context.Context) (bool, any, error) {
			st, err := x.View(ctx)
			if err != nil {
				return false, nil, err
			}
			h := st.View.Health
			return h.UnderReplicated == 0 && h.OfflinePartitions == 0, map[string]any{"underReplicated": h.UnderReplicated}, nil
		})
}

// waitReady waits until a restarted node is back: a controller in the quorum, a broker
// registered and unfenced with every replica in sync, a combined node both.
func (m *Manager) waitReady(ctx context.Context, x *ops.Exec, a NodeArgs) (any, error) {
	if runsController(a.Role) {
		detail, err := m.stepWaitObserver(ctx, x, mustJSON(NodeArgs{NodeID: a.NodeID, TimeoutSeconds: a.TimeoutSeconds}))
		if err != nil || a.Role == RoleController {
			return detail, err
		}
	}
	if _, err := m.stepWaitBroker(ctx, x, mustJSON(NodeArgs{NodeID: a.NodeID, TimeoutSeconds: a.TimeoutSeconds})); err != nil {
		return nil, err
	}
	return m.stepWaitInSync(ctx, x, mustJSON(NodeArgs{TimeoutSeconds: 1800}))
}

func (m *Manager) stepRestart(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	before, err := m.hostStatus(ctx, a)
	if err != nil {
		return nil, err
	}
	// Re-render the configuration from the cluster settings, which also removes drift.
	if _, err := m.stepConfigure(ctx, x, raw); err != nil {
		return nil, err
	}
	if _, err := m.stepStop(ctx, x, raw); err != nil {
		return nil, err
	}
	version, err := m.start(ctx, a)
	if err != nil {
		return nil, err
	}
	process, err := m.waitNewKafka(ctx, x, a, before.GetKafkaProcess(), version)
	if err != nil {
		return process, err
	}
	ready, err := m.waitReady(ctx, x, a)
	return map[string]any{"node": a.Name, "kafkaProcess": process, "ready": ready}, err
}

func (m *Manager) stepUpgrade(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	before, err := m.hostStatus(ctx, a)
	if err != nil {
		return nil, err
	}
	if _, err := m.stepInstall(ctx, x, raw); err != nil {
		return nil, err
	}
	if _, err := m.stepConfigure(ctx, x, raw); err != nil {
		return nil, err
	}
	if _, err := m.stepStop(ctx, x, raw); err != nil {
		return nil, err
	}
	version, err := m.start(ctx, a)
	if err != nil {
		return nil, err
	}
	process, err := m.waitNewKafka(ctx, x, a, before.GetKafkaProcess(), version)
	if err != nil {
		return process, err
	}
	detail, err := m.waitReady(ctx, x, a)
	if err != nil {
		return detail, err
	}
	return map[string]any{"node": a.Name, "version": a.Version, "kafkaProcess": process}, nil
}

func (m *Manager) stepSyncBootstrap(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	docker := x.Cluster.Provider == store.ProviderDocker
	if !docker && x.Cluster.Provider != store.ProviderAgent {
		return map[string]any{"skipped": "the provider manages its own addresses"}, nil
	}
	mn, err := m.managedNodes(ctx, x.Cluster.ID)
	if err != nil {
		return nil, err
	}
	if mn.hasAdopted() {
		return map[string]any{"skipped": "adopted nodes keep the addresses of the cluster"}, nil
	}
	s := settingsOf(x.Cluster)
	var servers []string
	for _, b := range mn.brokers {
		if b.KafkaNodeID != nil {
			servers = append(servers, m.brokerAddress(s, docker, hostOf(docker, b), *b.KafkaNodeID))
		}
	}
	if len(servers) == 0 {
		return map[string]any{"skipped": "no managed brokers"}, nil
	}
	s.ControllerBootstrapServers = nil
	for _, c := range mn.controllers {
		if c.KafkaNodeID != nil {
			s.ControllerBootstrapServers = append(s.ControllerBootstrapServers, m.controllerAddress(s, docker, c.Role, hostOf(docker, c), *c.KafkaNodeID))
		}
	}
	return m.stepSetSettings(ctx, x, mustJSON(NodeArgs{BootstrapServers: servers, Settings: &s}))
}

func (m *Manager) stepRebalance(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	mode := a.Mode
	if mode == "" {
		mode = "even"
	}
	return ops.InlineRebalance(ctx, x, mode, 0)
}

func mustJSON(v any) json.RawMessage {
	raw, _ := json.Marshal(v)
	return raw
}
