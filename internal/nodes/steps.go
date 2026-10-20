// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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

// NodeArgs are the arguments of node steps.
type NodeArgs struct {
	Name               string    `json:"name,omitempty"`
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

func (m *Manager) StepKinds() map[string]ops.StepFunc {
	return map[string]ops.StepFunc{
		KindSetSettings:   m.stepSetSettings,
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

func poll(ctx context.Context, x *ops.Exec, timeout time.Duration, every time.Duration, what string,
	check func(context.Context) (bool, any, error)) (any, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		ok, detail, err := check(ctx)
		if err == nil && ok {
			return detail, nil
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

func (m *Manager) nodeByName(ctx context.Context, name string) (*store.Node, error) {
	n, err := m.Store.GetNodeByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("node %s: %w", name, err)
	}
	return n, nil
}

func (m *Manager) send(ctx context.Context, name string, cmd *agentv1.Command, timeout time.Duration) (*agentv1.CommandResult, error) {
	n, err := m.nodeByName(ctx, name)
	if err != nil {
		return nil, err
	}
	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return m.Gateway.Send(cctx, n.ID, cmd)
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
		c.Settings = a.Settings.toMap()
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

func (m *Manager) stepProvision(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	if x.Cluster.Provider != store.ProviderDocker {
		if _, err := m.nodeByName(ctx, a.Name); err != nil {
			return nil, fmt.Errorf("install brokerium-agent on the host first (Nodes page, join token for %s): %w", a.Name, err)
		}
		return map[string]any{"node": a.Name, "provisioned": "existing agent"}, nil
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
	token, _, err := m.Gateway.NewJoinToken(ctx, a.Name, &x.Cluster.ID, 2*time.Hour, "brokerium")
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
		Volumes: map[string]string{a.Name + "-data": "/var/lib/kafka/data", a.Name + "-agent": "/var/lib/brokerium-agent",
			a.Name + "-kafka": "/opt/brokerium/kafka"},
		Labels: map[string]string{"brokerium.cluster": x.Cluster.Name, "brokerium.node": a.Name, "brokerium.role": a.Role},
	}
	port := s.PortBase + int(a.NodeID)
	if a.Role == RoleBroker {
		spec.Ports = map[int]int{port: port}
	} else {
		spec.Ports = map[int]int{9093: port}
	}
	id, err := m.Docker.Run(ctx, spec)
	if err != nil {
		return nil, err
	}
	return map[string]any{"container": id, "image": image}, nil
}

func (m *Manager) stepWaitAgent(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	return poll(ctx, x, timeoutOf(a, 3*time.Minute), 2*time.Second, "agent "+a.Name+" to connect",
		func(ctx context.Context) (bool, any, error) {
			n, err := m.Store.GetNodeByName(ctx, a.Name)
			if err != nil {
				return false, nil, nil
			}
			if !m.Gateway.Connected(n.ID) {
				return false, map[string]any{"enrolled": true}, nil
			}
			id := a.NodeID
			if err := m.Store.AssignNode(ctx, n.ID, &x.Cluster.ID, a.Role, &id); err != nil {
				return false, nil, err
			}
			return true, map[string]any{"node": n.ID, "connected": true}, nil
		})
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
	res, err := m.send(ctx, a.Name, m.installCommand(version), 30*time.Minute)
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
	var names []string
	for _, c := range mn.controllers {
		names = append(names, c.Name)
	}
	if len(names) == 0 {
		return "", "", errors.New("the cluster has no managed controllers")
	}
	if settingsOf(x.Cluster).Quorum == "static" {
		var voters []string
		for _, c := range mn.controllers {
			voters = append(voters, fmt.Sprintf("%d@%s:9093", *c.KafkaNodeID, c.Name))
		}
		return "static", strings.Join(voters, ","), nil
	}
	return "dynamic", quorumBootstrap(names), nil
}

func (m *Manager) stepConfigure(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	if n, err := m.Store.GetNodeByName(ctx, a.Name); err == nil && adopted(n) {
		return map[string]any{"skipped": "adopted node keeps its own server.properties"}, nil
	}
	s := settingsOf(x.Cluster)
	kind, quorum, err := m.quorumFor(ctx, x, a)
	if err != nil {
		return nil, err
	}
	props := renderProperties(s, x.Cluster.Provider == store.ProviderDocker, a.Role, a.Name, a.NodeID, a.Rack, kind, quorum)
	heap := s.BrokerHeap
	if a.Role == RoleController {
		heap = s.ControllerHeap
	}
	if err := agentcfg.Validate(props, heap); err != nil {
		return nil, fmt.Errorf("configuration of %s: %w", a.Name, err)
	}
	if _, err := m.send(ctx, a.Name, &agentv1.Command{Action: &agentv1.Command_WriteConfig{WriteConfig: &agentv1.WriteConfig{
		Properties: props, HeapOpts: heap}}}, time.Minute); err != nil {
		return nil, err
	}
	return map[string]any{"quorumKind": kind, "quorum": quorum}, nil
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
	res, err := m.send(ctx, a.Name, &agentv1.Command{Action: &agentv1.Command_Format{Format: &agentv1.FormatStorage{
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
	if _, err := m.send(ctx, a.Name, &agentv1.Command{Action: &agentv1.Command_Start{Start: &agentv1.StartKafka{Version: a.Version}}}, time.Minute); err != nil {
		return nil, err
	}
	return map[string]any{"started": a.Name}, nil
}

func (m *Manager) stepStop(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
	if err != nil {
		return nil, err
	}
	n, err := m.Store.GetNodeByName(ctx, a.Name)
	if err == nil && m.Gateway.Connected(n.ID) {
		if _, err := m.send(ctx, a.Name, &agentv1.Command{Action: &agentv1.Command_Stop{Stop: &agentv1.StopKafka{TimeoutSeconds: 120}}}, 3*time.Minute); err != nil {
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
	if x.Cluster.Provider == store.ProviderDocker && m.Docker != nil {
		if err := m.Docker.Remove(ctx, a.Name, a.Name+"-data", a.Name+"-agent", a.Name+"-kafka"); err != nil {
			return nil, err
		}
	}
	if n, err := m.Store.GetNodeByName(ctx, a.Name); err == nil {
		if x.Cluster.Provider == store.ProviderDocker {
			_ = m.Store.DeleteNode(ctx, n.ID)
		} else {
			_ = m.Store.AssignNode(ctx, n.ID, nil, "", nil)
		}
	}
	if _, err := m.stepSyncBootstrap(ctx, x, nil); err != nil {
		return nil, fmt.Errorf("updating bootstrap servers: %w", err)
	}
	return map[string]any{"removed": a.Name}, nil
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
	res, err := m.send(ctx, a.Name, &agentv1.Command{Action: &agentv1.Command_Status{Status: &agentv1.GetStatus{}}}, 30*time.Second)
	if err != nil {
		return nil, err
	}
	dir := res.GetStatus().GetMeta().GetDirectoryId()
	if dir == "" {
		return nil, errors.New("the node reports no directory.id; is its storage formatted?")
	}
	clusterID := settingsOf(x.Cluster).KafkaClusterID
	if err := x.Client.AddRaftVoter(ctx, clusterID, a.NodeID, dir,
		[]kafka.Endpoint{{Name: "CONTROLLER", Host: a.Name, Port: 9093}}); err != nil {
		return nil, err
	}
	return map[string]any{"nodeId": a.NodeID, "directoryId": dir}, nil
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

func (m *Manager) waitReady(ctx context.Context, x *ops.Exec, a NodeArgs) (any, error) {
	if a.Role == RoleController {
		return m.stepWaitObserver(ctx, x, mustJSON(NodeArgs{NodeID: a.NodeID, TimeoutSeconds: a.TimeoutSeconds}))
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
	// Re-render the configuration from the cluster settings, which also removes drift.
	if _, err := m.stepConfigure(ctx, x, raw); err != nil {
		return nil, err
	}
	if _, err := m.stepStop(ctx, x, raw); err != nil {
		return nil, err
	}
	if _, err := m.stepStart(ctx, x, raw); err != nil {
		return nil, err
	}
	return m.waitReady(ctx, x, a)
}

func (m *Manager) stepUpgrade(ctx context.Context, x *ops.Exec, raw json.RawMessage) (any, error) {
	a, err := args(raw)
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
	if _, err := m.stepStart(ctx, x, raw); err != nil {
		return nil, err
	}
	detail, err := m.waitReady(ctx, x, a)
	if err != nil {
		return detail, err
	}
	return map[string]any{"node": a.Name, "version": a.Version}, nil
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
	s := settingsOf(x.Cluster)
	var servers []string
	for _, b := range mn.brokers {
		if b.KafkaNodeID != nil {
			servers = append(servers, brokerAddress(s, docker, b.Name, *b.KafkaNodeID))
		}
	}
	if len(servers) == 0 {
		return map[string]any{"skipped": "no managed brokers"}, nil
	}
	s.ControllerBootstrapServers = nil
	for _, c := range mn.controllers {
		if c.KafkaNodeID != nil {
			s.ControllerBootstrapServers = append(s.ControllerBootstrapServers, controllerAddress(s, docker, c.Name, *c.KafkaNodeID))
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
