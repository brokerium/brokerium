// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package nodes implements node lifecycle operations on clusters whose nodes run
// brokerium-agent: creating clusters, adding and replacing brokers, adding controllers,
// migrating quorums, rolling restarts and upgrades.
package nodes

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/brokerium/brokerium/internal/agentcfg"
	"github.com/brokerium/brokerium/internal/agentgw"
	"github.com/brokerium/brokerium/internal/balancer"
	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/provider/docker"
	"github.com/brokerium/brokerium/internal/store"
)

// Config holds provider settings.
type Config struct {
	// AgentEndpoint is the address the agents in the containers of Docker clusters dial.
	AgentEndpoint string
	AgentImage    string
	LabNetwork    string
	// ServerInContainer is set when the server runs in a container on LabNetwork; it reaches the
	// nodes of Docker clusters by container name instead of through ports published on localhost.
	ServerInContainer bool
	// KafkaMirror goes into every Kafka install command; agents try it before the Apache mirrors.
	KafkaMirror string
}

// Manager implements ops.NodeManager.
type Manager struct {
	Store    *store.Store
	Gateway  *agentgw.Gateway
	Docker   *docker.Client
	Clusters *cluster.Service
	Log      *slog.Logger
	Cfg      Config
	// DockerClusters tells whether the server offers Docker clusters (ADR 0019).
	DockerClusters *docker.Detector
}

var versionRe = regexp.MustCompile(`^\d+\.\d+\.\d+$`)

// Types lists node lifecycle operation types.
func (m *Manager) Types() []ops.OpType {
	return []ops.OpType{
		{Type: "bootstrap-cluster", Title: "Create cluster nodes", Scope: "cluster", Requires: []string{"restartNodes"},
			AllowUnreachable: true, Plan: m.planBootstrap,
			Description: "Provision controllers and brokers, format storage for a dynamic quorum and start the cluster."},
		{Type: "add-broker", Title: "Add brokers", Scope: "cluster", Requires: []string{"restartNodes"}, Plan: m.planAddBroker,
			Description: "Provision new brokers, start them and optionally rebalance data onto them."},
		{Type: "replace-broker", Title: "Replace broker", Scope: "broker", Requires: []string{"restartNodes"}, Plan: m.planReplace,
			Description: "Replace a failed broker: same node id on an empty disk, or a new node id with its replicas moved over."},
		{Type: "add-controller", Title: "Add controller", Scope: "controller", Requires: []string{"addControllers"}, Plan: m.planAddController,
			Description: "Provision a controller, let it catch up as an observer, then add it as a voter (KIP-853)."},
		{Type: "migrate-quorum", Title: "Migrate to dynamic quorum", Scope: "cluster", Requires: []string{"migrateQuorum"},
			Irreversible: true, Plan: m.planMigrate,
			Description: "Upgrade kraft.version to 1 and restart every node with controller.quorum.bootstrap.servers (Kafka 4.1+)."},
		{Type: "rolling-restart", Title: "Rolling restart", Scope: "cluster", Requires: []string{"restartNodes"}, Plan: m.planRestart,
			Description: "Restart nodes one at a time with health checks: controllers first (leader last), then brokers."},
		{Type: "rolling-upgrade", Title: "Rolling upgrade", Scope: "cluster", Requires: []string{"upgradeVersion"}, Plan: m.planUpgrade,
			Description: "Install a new Kafka version and restart nodes on it one at a time; finalize separately."},
		{Type: "destroy-cluster", Title: "Delete cluster nodes", Scope: "cluster", Requires: []string{"provisionNodes"},
			Irreversible: true, AllowUnreachable: true, Plan: m.planDestroy,
			Description: "Stop every node and delete its container and volumes, data included. The cluster record stays until you remove it."},
		{Type: "scale-node-pool", Title: "Scale node pool", Scope: "cluster", Requires: []string{"scaleNodePools"}, Plan: m.planScalePool,
			Description: "Change the replicas of a Strimzi broker node pool: rebalance onto new brokers, or empty brokers before they are removed."},
		{Type: "finalize-upgrade", Title: "Finalize upgrade", Scope: "cluster", Requires: []string{"finalizeFeatures"},
			Irreversible: true, Plan: m.planFinalize,
			Description: "Raise metadata.version and other features to the highest level all nodes support."},
	}
}

// StopBrokerStep stops and removes a managed broker node.
func (m *Manager) StopBrokerStep(p *ops.Planner, broker int32) (ops.StepSpec, bool) {
	return m.removeStep(p, RoleBroker, broker)
}

// StopControllerStep stops and removes a managed controller node.
func (m *Manager) StopControllerStep(p *ops.Planner, node int32) (ops.StepSpec, bool) {
	return m.removeStep(p, RoleController, node)
}

func (m *Manager) removeStep(p *ops.Planner, role string, id int32) (ops.StepSpec, bool) {
	if p.Cluster.Provider != store.ProviderDocker && p.Cluster.Provider != store.ProviderAgent {
		return ops.StepSpec{}, false
	}
	mn, err := m.managedNodes(context.Background(), p.Cluster.ID)
	if err != nil {
		return ops.StepSpec{}, false
	}
	for _, n := range mn.nodes {
		if n.Role == role && n.KafkaNodeID != nil && *n.KafkaNodeID == id {
			name := fmt.Sprintf("Stop and remove node %s", n.Name)
			if p.Cluster.Provider == store.ProviderAgent {
				name = fmt.Sprintf("Stop Kafka on %s and release the host", n.Name)
			}
			return ops.StepSpec{Name: name, Kind: KindRemoveNode, Args: nodeArgs(p.Cluster, n)}, true
		}
	}
	return ops.StepSpec{}, false
}

// nodeArgs address a managed node in step arguments: agent nodes by id, Docker nodes by their
// container name.
func nodeArgs(c *store.Cluster, n *store.Node) NodeArgs {
	a := NodeArgs{Name: n.Name, Role: n.Role}
	if n.KafkaNodeID != nil {
		a.NodeID = *n.KafkaNodeID
	}
	if c.Provider != store.ProviderDocker {
		a.Node = n.ID
	}
	return a
}

func parse[T any](raw json.RawMessage) (T, error) {
	var v T
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("invalid params: %w", err)
	}
	return v, nil
}

// startSteps prepares one node and starts Kafka on it: a new container on Docker, the chosen
// host on agent clusters.
func startSteps(a NodeArgs) []ops.StepSpec {
	first := "Provision node " + a.Name
	if a.Node != "" {
		first = "Check host " + a.Name
	}
	return []ops.StepSpec{
		{Name: first, Kind: KindProvision, Args: a},
		{Name: "Wait for agent on " + a.Name, Kind: KindWaitAgent, Args: a},
		{Name: "Install Kafka on " + a.Name, Kind: KindInstall, Args: a},
		{Name: "Configure " + a.Name, Kind: KindConfigure, Args: a},
		{Name: "Format storage on " + a.Name, Kind: KindFormat, Args: a},
		{Name: "Start Kafka on " + a.Name, Kind: KindStart, Args: a},
	}
}

// checkHostsStep checks every chosen host again before the operation changes anything.
func checkHostsStep(kafkaClusterID string, members []member) ops.StepSpec {
	a := HostsArgs{KafkaClusterID: kafkaClusterID}
	names := make([]string, 0, len(members))
	for _, mb := range members {
		a.Hosts = append(a.Hosts, mb.args())
		names = append(names, mb.name)
	}
	return ops.StepSpec{Name: "Check hosts " + strings.Join(names, ", "), Kind: KindCheckHosts, Args: a}
}

type bootstrapParams struct {
	Controllers int          `json:"controllers"`
	Brokers     int          `json:"brokers"`
	Version     string       `json:"version"`
	Racks       []string     `json:"racks"`
	Quorum      string       `json:"quorum"`
	PortBase    int          `json:"portBase"`
	Nodes       []hostChoice `json:"nodes"`
}

// hostChoice is a host chosen for a node of a new agent cluster.
type hostChoice struct {
	Node   string `json:"node"`
	Role   string `json:"role"`
	NodeID *int32 `json:"nodeId"`
	Rack   string `json:"rack"`
}

func (m *Manager) usedPortBases(ctx context.Context, except string) map[int]bool {
	used := map[int]bool{}
	clusters, _ := m.Store.ListClusters(ctx)
	for _, c := range clusters {
		if c.ID != except {
			used[settingsOf(c).PortBase] = true
		}
	}
	return used
}

func (m *Manager) planBootstrap(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := parse[bootstrapParams](raw)
	if err != nil {
		return nil, err
	}
	if p.Cluster.Provider == store.ProviderDocker && m.Docker == nil {
		return nil, errors.New("the Docker provider is not available on this server (no Docker socket)")
	}
	f, err := m.fleet(ctx)
	if err != nil {
		return nil, err
	}
	var used map[int]bool
	if p.Cluster.Provider == store.ProviderDocker {
		used = m.usedPortBases(ctx, p.Cluster.ID)
	}
	return m.bootstrapPlan(p, f, params, used)
}

func (m *Manager) bootstrapPlan(p *ops.Planner, f fleet, params bootstrapParams, usedPortBases map[int]bool) (*ops.Planned, error) {
	if params.Version == "" {
		params.Version = "4.3.1"
	}
	if params.Quorum == "" {
		params.Quorum = "dynamic"
	}
	switch {
	case !versionRe.MatchString(params.Version):
		return nil, fmt.Errorf("invalid version %q", params.Version)
	case params.Quorum != "dynamic" && params.Quorum != "static":
		return nil, errors.New("quorum must be dynamic or static")
	case p.Cluster.Provider != store.ProviderDocker && p.Cluster.Provider != store.ProviderAgent:
		return nil, errors.New("only Docker and agent clusters are created by Brokerium")
	}
	// Agent hosts join the cluster before it exists; only nodes with a Kafka role count.
	if mn := f.managed(p.Cluster.ID); len(mn.controllers)+len(mn.brokers) > 0 {
		return nil, errors.New("the cluster already has Kafka nodes")
	}
	docker := p.Cluster.Provider == store.ProviderDocker
	s := Settings{KafkaVersion: params.Version, Quorum: params.Quorum, KafkaClusterID: RandomUUID()}
	var members []member
	var err error
	if docker {
		members, err = dockerMembers(p.Cluster.Name, &params, usedPortBases)
		s.PortBase, s.Racks = params.PortBase, params.Racks
	} else {
		members, err = chooseHosts(p.Cluster.ID, f, params, s.KafkaClusterID)
	}
	if err != nil {
		return nil, err
	}
	var controllers, brokers []member
	for _, mb := range members {
		if mb.role == RoleController {
			controllers = append(controllers, mb)
		} else {
			brokers = append(brokers, mb)
		}
	}
	rf := min(3, len(brokers))
	s.ReplicationFactor, s.MinISR = rf, min(2, rf)
	s = settingsOf(&store.Cluster{Settings: s.toMap()})

	var hosts, initial, voters []string
	for _, c := range controllers {
		addr := net.JoinHostPort(c.host(), "9093")
		hosts = append(hosts, c.host())
		s.ControllerBootstrapServers = append(s.ControllerBootstrapServers, m.controllerAddress(s, docker, c.host(), c.id))
		initial = append(initial, fmt.Sprintf("%d@%s:%s", c.id, addr, RandomUUID()))
		voters = append(voters, fmt.Sprintf("%d@%s", c.id, addr))
	}
	var bootstrap []string
	for _, b := range brokers {
		bootstrap = append(bootstrap, m.brokerAddress(s, docker, b.host(), b.id))
	}
	var steps []ops.StepSpec
	if !docker {
		steps = append(steps, checkHostsStep(s.KafkaClusterID, members))
	}
	steps = append(steps, ops.StepSpec{Name: "Save cluster settings", Kind: KindSetSettings,
		Args: NodeArgs{Settings: &s, BootstrapServers: bootstrap}})
	quorumKind, quorum, mode := "dynamic", quorumBootstrap(hosts), "initial"
	if params.Quorum == "static" {
		quorumKind, quorum, mode = "static", strings.Join(voters, ","), "static"
	}
	for _, c := range controllers {
		a := c.args()
		a.Mode, a.InitialControllers, a.Version, a.QuorumKind, a.Quorum = mode, strings.Join(initial, ","), params.Version, quorumKind, quorum
		steps = append(steps, startSteps(a)...)
	}
	for _, b := range brokers {
		a := b.args()
		a.Mode, a.Version, a.QuorumKind, a.Quorum = "join", params.Version, quorumKind, quorum
		steps = append(steps, startSteps(a)...)
	}
	for _, b := range brokers {
		steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Wait for broker %d", b.id), Kind: KindWaitBroker,
			Args: NodeArgs{NodeID: b.id}})
	}
	steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Wait for a quorum of %d voters", len(controllers)),
		Kind: KindWaitQuorum, Args: NodeArgs{ExpectVoters: len(controllers)}})

	doc := ops.PlanDoc{Summary: fmt.Sprintf("Create %d controller(s) and %d broker(s) running Kafka %s with a %s quorum",
		len(controllers), len(brokers), params.Version, params.Quorum),
		Details: map[string]any{"kafkaClusterId": s.KafkaClusterID, "bootstrapServers": bootstrap}}
	if docker {
		doc.Details["portBase"] = s.PortBase
	} else {
		doc.Summary += " on " + describe(members)
		doc.Details["hosts"] = hostDetails(members)
		doc.Warnings = append(doc.Warnings, rackWarnings(nil, members)...)
	}
	if params.Quorum == "static" {
		doc.Warnings = append(doc.Warnings, "a static quorum cannot change controllers until it is migrated to a dynamic quorum")
	}
	if len(controllers)%2 == 0 {
		doc.Warnings = append(doc.Warnings, fmt.Sprintf("%d controllers tolerate as many failures as %d", len(controllers), len(controllers)-1))
	}
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}

// dockerMembers names the containers of a new Docker cluster and picks its port base.
func dockerMembers(cluster string, params *bootstrapParams, usedPortBases map[int]bool) ([]member, error) {
	if len(params.Nodes) > 0 {
		return nil, errors.New("the Docker provider creates the nodes of Docker clusters: give controllers and brokers instead of nodes")
	}
	if params.Controllers == 0 {
		params.Controllers = 3
	}
	if params.Brokers == 0 {
		params.Brokers = 3
	}
	if len(params.Racks) == 0 {
		params.Racks = []string{"rack-a", "rack-b", "rack-c"}
	}
	switch {
	case params.Controllers < 1 || params.Controllers > maxVoters:
		return nil, errors.New("controllers must be between 1 and 5")
	case params.Brokers < 1 || params.Brokers > 9:
		return nil, errors.New("brokers must be between 1 and 9")
	}
	if params.PortBase == 0 {
		for base := 21000; base < 60000; base += 1000 {
			if !usedPortBases[base] {
				params.PortBase = base
				break
			}
		}
	}
	var out []member
	for i := 1; i <= params.Controllers; i++ {
		out = append(out, member{role: RoleController, id: int32(i), name: NodeName(cluster, RoleController, int32(i))})
	}
	for i := 0; i < params.Brokers; i++ {
		id := int32(101 + i)
		out = append(out, member{role: RoleBroker, id: id, rack: params.Racks[i%len(params.Racks)],
			name: NodeName(cluster, RoleBroker, id)})
	}
	return out, nil
}

// chooseHosts checks the hosts chosen for a new agent cluster and gives each its node id: the
// explicit one, else the next free id from 1 for controllers and from 101 for brokers, in the
// order of the list.
func chooseHosts(clusterID string, f fleet, params bootstrapParams, kafkaClusterID string) ([]member, error) {
	if params.Controllers != 0 || params.Brokers != 0 || len(params.Racks) > 0 || params.PortBase != 0 {
		return nil, errors.New("controllers, brokers, racks and portBase apply to Docker clusters; choose the hosts of an agent cluster with nodes")
	}
	if len(params.Nodes) == 0 {
		return nil, errors.New(`choose the hosts with nodes: [{"node": "<agent node id>", "role": "controller"}, ...]`)
	}
	used, chosen := ids{}, map[string]bool{}
	voters, brokers := 0, 0
	for i, c := range params.Nodes {
		switch c.Role {
		case RoleController:
			voters++
		case RoleBroker:
			brokers++
		case RoleCombined:
			return nil, fmt.Errorf("nodes[%d]: combined nodes are not supported yet; choose controller or broker", i)
		default:
			return nil, fmt.Errorf("nodes[%d]: role must be controller or broker, not %q", i, c.Role)
		}
		switch {
		case c.Node == "":
			return nil, fmt.Errorf("nodes[%d]: node is required: the id of an agent node", i)
		case chosen[c.Node]:
			return nil, fmt.Errorf("nodes[%d]: host %s is chosen twice", i, f.label(c.Node))
		}
		chosen[c.Node] = true
		if err := agentcfg.ValidateRack(c.Rack); err != nil {
			return nil, fmt.Errorf("nodes[%d]: %w", i, err)
		}
		if c.NodeID != nil {
			if err := used.claim(*c.NodeID); err != nil {
				return nil, fmt.Errorf("nodes[%d]: %w", i, err)
			}
		}
	}
	switch {
	case voters == 0:
		return nil, errors.New("choose at least one controller")
	case voters > maxVoters:
		return nil, fmt.Errorf("choose at most %d controllers, not %d", maxVoters, voters)
	case brokers == 0:
		return nil, errors.New("choose at least one broker")
	}
	members := make([]member, 0, len(params.Nodes))
	for _, c := range params.Nodes {
		var id int32
		switch {
		case c.NodeID != nil:
			id = *c.NodeID
		case c.Role == RoleController:
			id = used.next(1)
		default:
			id = used.next(101)
		}
		n, err := f.pick(c.Node, clusterID, c.Role, id, kafkaClusterID)
		if err != nil {
			return nil, err
		}
		mb := member{role: c.Role, id: id, name: n.Name, node: n}
		if c.Role == RoleBroker {
			mb.rack = cmp.Or(c.Rack, n.Rack)
		}
		members = append(members, mb)
	}
	return members, checkAddresses(members, nil)
}

type addBrokerParams struct {
	Count     int            `json:"count"`
	Rack      string         `json:"rack"`
	Rebalance *bool          `json:"rebalance"`
	Nodes     []brokerChoice `json:"nodes"`
}

// brokerChoice is a host chosen for a new broker of an agent cluster.
type brokerChoice struct {
	Node   string `json:"node"`
	Rack   string `json:"rack"`
	NodeID *int32 `json:"nodeId"`
}

func nextID(mn managed, role string, start int32) int32 {
	id := start
	for _, n := range mn.nodes {
		if n.KafkaNodeID != nil && n.Role == role && *n.KafkaNodeID >= id {
			id = *n.KafkaNodeID + 1
		}
	}
	return id
}

// nextBrokerID is where new broker ids start: after the highest managed or registered broker.
func nextBrokerID(p *ops.Planner, mn managed) int32 {
	id := nextID(mn, RoleBroker, 101)
	for _, b := range p.View.Snapshot.Brokers {
		if b.ID >= id {
			id = b.ID + 1
		}
	}
	return id
}

func (m *Manager) planAddBroker(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := parse[addBrokerParams](raw)
	if err != nil {
		return nil, err
	}
	f, err := m.fleet(ctx)
	if err != nil {
		return nil, err
	}
	return addBrokerPlan(p, f, params)
}

func addBrokerPlan(p *ops.Planner, f fleet, params addBrokerParams) (*ops.Planned, error) {
	mn := f.managed(p.Cluster.ID)
	s := settingsOf(p.Cluster)
	used := usedIDs(p, mn)
	first := nextBrokerID(p, mn)
	docker := p.Cluster.Provider == store.ProviderDocker
	var members []member
	if docker {
		if len(params.Nodes) > 0 {
			return nil, errors.New("the Docker provider creates the brokers of Docker clusters: give count instead of nodes")
		}
		if params.Count <= 0 {
			params.Count = 1
		}
		if params.Count > 6 {
			return nil, errors.New("add at most 6 brokers at once")
		}
		rackCount := map[string]int{}
		for _, b := range p.View.Snapshot.Brokers {
			rackCount[b.Rack]++
		}
		for i := 0; i < params.Count; i++ {
			rack := params.Rack
			if rack == "" && len(s.Racks) > 0 {
				rack = s.Racks[0]
				for _, r := range s.Racks {
					if rackCount[r] < rackCount[rack] {
						rack = r
					}
				}
			}
			rackCount[rack]++
			id := used.next(first)
			members = append(members, member{role: RoleBroker, id: id, rack: rack, name: NodeName(p.Cluster.Name, RoleBroker, id)})
		}
	} else {
		var err error
		if members, err = chooseBrokers(p.Cluster.ID, f, params, used, registeredBrokers(p), first, s.KafkaClusterID); err != nil {
			return nil, err
		}
		if err := checkAddresses(members, mn.nodes); err != nil {
			return nil, err
		}
	}
	steps := []ops.StepSpec{{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}}}
	if !docker {
		steps = append(steps, checkHostsStep(s.KafkaClusterID, members))
	}
	var names []string
	for _, mb := range members {
		a := mb.args()
		a.Mode, a.Version = "join", s.KafkaVersion
		steps = append(steps, startSteps(a)...)
		steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Wait for broker %d", mb.id), Kind: KindWaitBroker, Args: NodeArgs{NodeID: mb.id}})
		name := brokerLabel(mb.id, mb.rack)
		if !docker {
			name += " on " + mb.name
		}
		names = append(names, name)
	}
	steps = append(steps, ops.StepSpec{Name: "Update bootstrap servers", Kind: KindSyncBootstrap, Args: NodeArgs{}})
	rebalance := params.Rebalance == nil || *params.Rebalance
	if rebalance {
		steps = append(steps, ops.StepSpec{Name: "Rebalance onto the new brokers", Kind: KindRebalance, Args: NodeArgs{Mode: "even"}})
	}
	doc := ops.PlanDoc{Summary: fmt.Sprintf("Add %d broker(s): %s; rebalance afterwards: %v", len(members), strings.Join(names, ", "), rebalance)}
	if !docker {
		doc.Details = map[string]any{"hosts": hostDetails(members)}
		doc.Warnings = append(resumeWarnings(members), rackWarnings(p.View.Snapshot.Brokers, members)...)
	}
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}

// chooseBrokers checks the hosts chosen for new brokers of an agent cluster and gives each its
// node id: the one it holds from a failed operation while that broker is not registered, else
// the explicit one, else the next free id from first on.
func chooseBrokers(clusterID string, f fleet, params addBrokerParams, used, registered ids, first int32, kafkaClusterID string) ([]member, error) {
	if params.Count != 0 || params.Rack != "" {
		return nil, errors.New("count and rack apply to Docker clusters; choose the hosts of an agent cluster with nodes")
	}
	if len(params.Nodes) == 0 {
		return nil, errors.New(`choose the hosts with nodes: [{"node": "<agent node id>"}, ...]`)
	}
	chosen, resumed := map[string]bool{}, map[string]int32{}
	for i, c := range params.Nodes {
		switch {
		case c.Node == "":
			return nil, fmt.Errorf("nodes[%d]: node is required: the id of an agent node", i)
		case chosen[c.Node]:
			return nil, fmt.Errorf("nodes[%d]: host %s is chosen twice", i, f.label(c.Node))
		}
		chosen[c.Node] = true
		if err := agentcfg.ValidateRack(c.Rack); err != nil {
			return nil, fmt.Errorf("nodes[%d]: %w", i, err)
		}
		if id, ok := resumable(f.node(c.Node), clusterID, RoleBroker, registered); ok && (c.NodeID == nil || *c.NodeID == id) {
			resumed[c.Node] = id
			continue
		}
		if c.NodeID != nil {
			if err := used.claim(*c.NodeID); err != nil {
				return nil, fmt.Errorf("nodes[%d]: %w", i, err)
			}
		}
	}
	members := make([]member, 0, len(params.Nodes))
	for _, c := range params.Nodes {
		id, resume := resumed[c.Node]
		switch {
		case resume:
		case c.NodeID != nil:
			id = *c.NodeID
		default:
			id = used.next(first)
		}
		n, err := f.pick(c.Node, clusterID, RoleBroker, id, kafkaClusterID)
		if err != nil {
			return nil, err
		}
		members = append(members, member{role: RoleBroker, id: id, rack: cmp.Or(c.Rack, n.Rack), name: n.Name, node: n, resumed: resume})
	}
	return members, nil
}

type replaceParams struct {
	BrokerID *int32 `json:"brokerId"`
	Mode     string `json:"mode"`
	Node     string `json:"node"`
}

func (m *Manager) planReplace(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := parse[replaceParams](raw)
	if err != nil {
		return nil, err
	}
	f, err := m.fleet(ctx)
	if err != nil {
		return nil, err
	}
	return replacePlan(p, f, params)
}

func replacePlan(p *ops.Planner, f fleet, params replaceParams) (*ops.Planned, error) {
	if params.BrokerID == nil {
		return nil, errors.New("brokerId is required")
	}
	if params.Mode == "" {
		params.Mode = "same-id"
	}
	if params.Mode != "same-id" && params.Mode != "new-id" {
		return nil, errors.New("mode must be same-id or new-id")
	}
	old := *params.BrokerID
	var rack string
	alive := false
	found := false
	for _, b := range p.View.Snapshot.Brokers {
		if b.ID == old {
			found, rack, alive = true, b.Rack, b.Alive && !b.Fenced
		}
	}
	if !found {
		return nil, fmt.Errorf("broker %d is not registered", old)
	}
	if alive {
		return nil, fmt.Errorf("broker %d is serving; replace only failed brokers (drain a healthy one instead)", old)
	}
	s := settingsOf(p.Cluster)
	mn := f.managed(p.Cluster.ID)
	var oldNode *store.Node
	for _, n := range mn.brokers {
		if *n.KafkaNodeID == old {
			oldNode = n
		}
	}
	docker := p.Cluster.Provider == store.ProviderDocker
	id := old
	if params.Mode == "new-id" {
		id = usedIDs(p, mn).next(nextBrokerID(p, mn))
	}
	var mb member
	var warnings []string
	var remove *ops.StepSpec
	if docker {
		if params.Node != "" {
			return nil, errors.New("the Docker provider creates the replacement in Docker clusters: give no node")
		}
		oldName := NodeName(p.Cluster.Name, RoleBroker, old)
		if oldNode != nil {
			oldName = oldNode.Name
		}
		remove = &ops.StepSpec{Name: "Remove failed node " + oldName, Kind: KindRemoveNode,
			Args: NodeArgs{Name: oldName, Role: RoleBroker, NodeID: old}}
		name := NodeName(p.Cluster.Name, RoleBroker, id)
		if params.Mode == "same-id" {
			name = oldName
		}
		mb = member{role: RoleBroker, id: id, rack: rack, name: name}
	} else {
		target := params.Node
		switch {
		case target == "" && params.Mode == "new-id":
			return nil, fmt.Errorf("node is required for new-id: the agent node that runs broker %d", id)
		case target == "" && oldNode == nil:
			return nil, fmt.Errorf("broker %d has no host managed by Brokerium; choose one with node", old)
		case target == "":
			target = oldNode.ID
		case params.Mode == "new-id" && oldNode != nil && target == oldNode.ID:
			return nil, fmt.Errorf("host %s ran broker %d; choose another host for a new id, or replace with same-id on it", oldNode.Name, old)
		}
		n, err := f.pick(target, p.Cluster.ID, RoleBroker, id, s.KafkaClusterID)
		if err != nil {
			return nil, err
		}
		mb = member{role: RoleBroker, id: id, rack: cmp.Or(n.Rack, rack), name: n.Name, node: n}
		if n.Rack != "" && rack != "" && n.Rack != rack {
			warnings = append(warnings, fmt.Sprintf("broker %d was in rack %s; host %s is in rack %s, so rebalance afterwards to restore rack-aware placement",
				old, rack, n.Name, n.Rack))
		}
		var others []*store.Node
		for _, o := range mn.nodes {
			if oldNode == nil || o.ID != oldNode.ID {
				others = append(others, o)
			}
		}
		if err := checkAddresses([]member{mb}, others); err != nil {
			return nil, err
		}
		if oldNode != nil {
			remove = &ops.StepSpec{Name: "Remove failed node " + oldNode.Name, Kind: KindRemoveNode, Args: nodeArgs(p.Cluster, oldNode)}
		}
	}
	a := mb.args()
	a.Mode, a.Version = "join", s.KafkaVersion
	on := ""
	if !docker {
		on = " on host " + mb.name
	}
	steps := []ops.StepSpec{{Name: "Check health (under-replicated partitions allowed)", Kind: ops.KindHealthGate,
		Args: ops.HealthGateArgs{AllowUnderReplicated: true}}}
	if !docker {
		steps = append(steps, checkHostsStep(s.KafkaClusterID, []member{mb}))
	}
	if params.Mode == "same-id" {
		if remove != nil {
			steps = append(steps, *remove)
		}
		steps = append(steps, startSteps(a)...)
		steps = append(steps,
			ops.StepSpec{Name: fmt.Sprintf("Wait for broker %d", old), Kind: KindWaitBroker, Args: NodeArgs{NodeID: old}},
			ops.StepSpec{Name: "Wait until all replicas are in sync", Kind: KindWaitInSync, Args: NodeArgs{}},
			ops.StepSpec{Name: "Update bootstrap servers", Kind: KindSyncBootstrap, Args: NodeArgs{}},
		)
		summary := fmt.Sprintf("Replace broker %d in place: new node with the same id and an empty disk; followers re-replicate from the leaders", old)
		if !docker {
			summary = fmt.Sprintf("Start broker %d again with the same id%s; followers re-replicate from the leaders what its disk lacks", old, on)
		}
		return &ops.Planned{Doc: ops.PlanDoc{Summary: summary, Warnings: warnings}, Steps: steps}, nil
	}
	var moves []balancer.Move
	for _, t := range p.View.Snapshot.Topics {
		for _, part := range t.Partitions {
			var to []int32
			changed := false
			for _, r := range part.Replicas {
				if r == old {
					to, changed = append(to, id), true
				} else {
					to = append(to, r)
				}
			}
			if changed {
				moves = append(moves, balancer.Move{Topic: t.Name, Partition: part.ID, From: part.Replicas, To: to,
					SizeBytes: part.SizeBytes, ReplicaMoves: 1})
			}
		}
	}
	steps = append(steps, startSteps(a)...)
	steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Wait for broker %d", id), Kind: KindWaitBroker, Args: NodeArgs{NodeID: id}})
	if len(moves) > 0 {
		steps = append(steps, ops.ReassignSteps(moves, 0, 0, true)...)
	}
	steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Unregister broker %d", old), Kind: ops.KindUnregisterBroker, Args: ops.BrokerArgs{BrokerID: old}})
	if remove != nil {
		steps = append(steps, *remove)
	}
	steps = append(steps, ops.StepSpec{Name: "Update bootstrap servers", Kind: KindSyncBootstrap, Args: NodeArgs{}})
	return &ops.Planned{Doc: ops.PlanDoc{Summary: fmt.Sprintf(
		"Replace broker %d with new broker %s%s: move %d partition(s) over, then unregister %d", old, brokerLabel(id, mb.rack), on, len(moves), old),
		Irreversible: true, Moves: moves, Warnings: warnings}, Steps: steps}, nil
}

// brokerLabel names a broker in plan summaries: its id, and its rack when it has one.
func brokerLabel(id int32, rack string) string {
	if rack == "" {
		return fmt.Sprint(id)
	}
	return fmt.Sprintf("%d (%s)", id, rack)
}

type addControllerParams struct {
	Node   string `json:"node"`
	NodeID *int32 `json:"nodeId"`
}

func (m *Manager) planAddController(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := parse[addControllerParams](raw)
	if err != nil {
		return nil, err
	}
	f, err := m.fleet(ctx)
	if err != nil {
		return nil, err
	}
	return addControllerPlan(p, f, params)
}

func addControllerPlan(p *ops.Planner, f fleet, params addControllerParams) (*ops.Planned, error) {
	q := p.View.Snapshot.Quorum
	if q == nil {
		return nil, errors.New("the quorum cannot be described")
	}
	if len(q.Voters) >= maxVoters {
		return nil, fmt.Errorf("the quorum already has %d voters; Brokerium builds quorums of at most %d", len(q.Voters), maxVoters)
	}
	mn := f.managed(p.Cluster.ID)
	s := settingsOf(p.Cluster)
	used := usedIDs(p, mn)
	first := nextID(mn, RoleController, 1)
	for _, v := range q.Voters {
		if v.NodeID >= first {
			first = v.NodeID + 1
		}
	}
	var mb member
	if p.Cluster.Provider == store.ProviderDocker {
		if params.Node != "" || params.NodeID != nil {
			return nil, errors.New("the Docker provider creates the controllers of Docker clusters: give neither node nor nodeId")
		}
		id := used.next(first)
		if id >= 100 {
			return nil, errors.New("controller ids must stay below 100")
		}
		mb = member{role: RoleController, id: id, name: NodeName(p.Cluster.Name, RoleController, id)}
	} else {
		if params.Node == "" {
			return nil, errors.New("node is required: the agent node that runs the new controller")
		}
		voters := ids{}
		for _, v := range q.Voters {
			voters[v.NodeID] = true
		}
		id, resume := resumable(f.node(params.Node), p.Cluster.ID, RoleController, voters)
		if resume && params.NodeID != nil && *params.NodeID != id {
			resume = false
		}
		switch {
		case resume:
		case params.NodeID != nil:
			if err := used.claim(*params.NodeID); err != nil {
				return nil, err
			}
			id = *params.NodeID
		default:
			id = used.next(first)
		}
		n, err := f.pick(params.Node, p.Cluster.ID, RoleController, id, s.KafkaClusterID)
		if err != nil {
			return nil, err
		}
		mb = member{role: RoleController, id: id, name: n.Name, node: n, resumed: resume}
		if err := checkAddresses([]member{mb}, mn.nodes); err != nil {
			return nil, err
		}
	}
	a := mb.args()
	start := a
	start.Mode, start.Version = "join", s.KafkaVersion
	steps := []ops.StepSpec{{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}}}
	if mb.node != nil {
		steps = append(steps, checkHostsStep(s.KafkaClusterID, []member{mb}))
	}
	steps = append(steps, startSteps(start)...)
	steps = append(steps,
		ops.StepSpec{Name: fmt.Sprintf("Wait for controller %d to catch up as an observer", mb.id), Kind: KindWaitObserver, Args: a},
		ops.StepSpec{Name: fmt.Sprintf("Add controller %d as a voter", mb.id), Kind: KindAddVoter, Args: a},
		ops.StepSpec{Name: fmt.Sprintf("Wait for a quorum of %d voters", len(q.Voters)+1), Kind: KindWaitQuorum, Args: NodeArgs{ExpectVoters: len(q.Voters) + 1}},
	)
	steps = append(steps, ops.StepSpec{Name: "Update bootstrap servers", Kind: KindSyncBootstrap, Args: NodeArgs{}})
	doc := ops.PlanDoc{Summary: fmt.Sprintf("Add controller %d (%s): %d -> %d voters", mb.id, mb.name, len(q.Voters), len(q.Voters)+1)}
	if mb.node != nil {
		doc.Details = map[string]any{"hosts": hostDetails([]member{mb})}
		doc.Warnings = resumeWarnings([]member{mb})
	}
	if (len(q.Voters)+1)%2 == 0 {
		doc.Warnings = append(doc.Warnings, fmt.Sprintf("%d voters tolerate as many failures as %d; add one more for an odd quorum",
			len(q.Voters)+1, len(q.Voters)))
	}
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}

func (m *Manager) planDestroy(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	if _, err := parse[struct{}](raw); err != nil {
		return nil, err
	}
	if p.Cluster.Provider != store.ProviderDocker {
		return nil, errors.New("only Docker clusters can be deleted by Brokerium; decommission agent nodes one by one")
	}
	mn, err := m.managedNodes(ctx, p.Cluster.ID)
	if err != nil {
		return nil, err
	}
	if len(mn.nodes) == 0 {
		return nil, errors.New("the cluster has no nodes")
	}
	var steps []ops.StepSpec
	var names []string
	for _, n := range append(append([]*store.Node(nil), mn.brokers...), mn.controllers...) {
		names = append(names, n.Name)
		var id int32
		if n.KafkaNodeID != nil {
			id = *n.KafkaNodeID
		}
		steps = append(steps, ops.StepSpec{Name: "Delete node " + n.Name, Kind: KindRemoveNode,
			Args: NodeArgs{Name: n.Name, Role: n.Role, NodeID: id}})
	}
	doc := ops.PlanDoc{Irreversible: true,
		Summary:  fmt.Sprintf("Delete %d node(s) of %s with their data: %s", len(names), p.Cluster.Name, strings.Join(names, ", ")),
		Warnings: []string{"all topics and messages of this cluster are lost"}}
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}

// orderedNodes returns managed controllers (quorum leader last) followed by brokers.
func orderedNodes(mn managed, leader int32) []*store.Node {
	ctrls := append([]*store.Node(nil), mn.controllers...)
	sort.SliceStable(ctrls, func(i, j int) bool {
		return *ctrls[i].KafkaNodeID != leader && *ctrls[j].KafkaNodeID == leader
	})
	return append(ctrls, mn.brokers...)
}

// syncSteps update the recorded bootstrap servers once the nodes of an agent cluster were
// configured again, which applies changed advertised hosts.
func syncSteps(c *store.Cluster, mn managed) []ops.StepSpec {
	if c.Provider != store.ProviderAgent || mn.hasAdopted() {
		return nil
	}
	return []ops.StepSpec{{Name: "Update bootstrap servers", Kind: KindSyncBootstrap, Args: NodeArgs{}}}
}

// hasAdopted reports whether a cluster has adopted nodes. Their listeners are not rendered by
// Brokerium, so such clusters keep the addresses they were registered with.
func (mn managed) hasAdopted() bool {
	for _, n := range mn.nodes {
		if adopted(n) {
			return true
		}
	}
	return false
}

func (m *Manager) planMigrate(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	if _, err := parse[struct{}](raw); err != nil {
		return nil, err
	}
	mn, err := m.managedNodes(ctx, p.Cluster.ID)
	if err != nil {
		return nil, err
	}
	return migratePlan(p, mn)
}

func migratePlan(p *ops.Planner, mn managed) (*ops.Planned, error) {
	if p.View.Health.KRaftVersion != 0 {
		return nil, errors.New("the quorum is already dynamic")
	}
	if len(mn.controllers) == 0 {
		return nil, errors.New("no managed controllers; adopt the nodes with brokerium-agent first")
	}
	docker := p.Cluster.Provider == store.ProviderDocker
	var hosts []string
	for _, c := range mn.controllers {
		hosts = append(hosts, hostOf(docker, c))
	}
	s := settingsOf(p.Cluster)
	s.Quorum = "dynamic"
	steps := []ops.StepSpec{
		{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}},
		{Name: "Upgrade kraft.version to 1", Kind: ops.KindUpdateFeature, Args: ops.FeatureArgs{Feature: "kraft.version", Level: 1}},
		{Name: "Save quorum mode", Kind: KindSetSettings, Args: NodeArgs{Settings: &s}},
	}
	for _, n := range orderedNodes(mn, p.View.Health.QuorumLeader) {
		a := nodeArgs(p.Cluster, n)
		a.Rack, a.QuorumKind, a.Quorum = rackOf(p, n), "dynamic", quorumBootstrap(hosts)
		steps = append(steps, ops.StepSpec{Name: "Restart " + n.Name + " with bootstrap servers", Kind: KindRestart, Args: a})
	}
	steps = append(steps, ops.StepSpec{Name: "Wait for the quorum", Kind: KindWaitQuorum, Args: NodeArgs{ExpectVoters: len(mn.controllers)}})
	steps = append(steps, syncSteps(p.Cluster, mn)...)
	return &ops.Planned{Doc: ops.PlanDoc{Summary: fmt.Sprintf(
		"Migrate the static quorum of %d controller(s) to a dynamic quorum and restart %d node(s) one by one", len(mn.controllers), len(mn.nodes)),
		Irreversible: true, Warnings: []string{"kraft.version cannot be downgraded from 1 to 0"}}, Steps: steps}, nil
}

func rackOf(p *ops.Planner, n *store.Node) string {
	if p.View.Snapshot == nil || n.KafkaNodeID == nil {
		return ""
	}
	for _, b := range p.View.Snapshot.Brokers {
		if b.ID == *n.KafkaNodeID {
			return b.Rack
		}
	}
	return ""
}

type restartParams struct {
	NodeIDs []int32 `json:"nodeIds"`
}

func (m *Manager) planRestart(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := parse[restartParams](raw)
	if err != nil {
		return nil, err
	}
	if p.Cluster.Provider == store.ProviderStrimzi {
		return m.planStrimziRestart(ctx, p)
	}
	mn, err := m.managedNodes(ctx, p.Cluster.ID)
	if err != nil {
		return nil, err
	}
	return restartPlan(p, mn, params)
}

func restartPlan(p *ops.Planner, mn managed, params restartParams) (*ops.Planned, error) {
	only := map[int32]bool{}
	for _, id := range params.NodeIDs {
		only[id] = true
	}
	steps := []ops.StepSpec{{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}}}
	count := 0
	for _, n := range orderedNodes(mn, p.View.Health.QuorumLeader) {
		if len(only) > 0 && !only[*n.KafkaNodeID] {
			continue
		}
		a := nodeArgs(p.Cluster, n)
		a.Rack = rackOf(p, n)
		steps = append(steps,
			ops.StepSpec{Name: "Restart " + n.Name, Kind: KindRestart, Args: a},
			ops.StepSpec{Name: "Check health after " + n.Name, Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}})
		count++
	}
	if count == 0 {
		return nil, errors.New("no managed nodes to restart")
	}
	steps = append(steps, ops.StepSpec{Name: "Elect preferred leaders", Kind: ops.KindElectPreferred, Args: ops.ReassignArgs{}})
	steps = append(steps, syncSteps(p.Cluster, mn)...)
	return &ops.Planned{Doc: ops.PlanDoc{Summary: fmt.Sprintf("Restart %d node(s) one at a time, controllers first and the quorum leader last", count)},
		Steps: steps}, nil
}

type upgradeParams struct {
	Version string `json:"version"`
}

func compareVersions(a, b string) int {
	var x, y [3]int
	_, _ = fmt.Sscanf(a, "%d.%d.%d", &x[0], &x[1], &x[2])
	_, _ = fmt.Sscanf(b, "%d.%d.%d", &y[0], &y[1], &y[2])
	for i := range x {
		if x[i] != y[i] {
			if x[i] < y[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

// knownIssue is a Kafka defect that matters for node lifecycle operations, affecting versions
// in [from, fixed). Sources: the Apache Kafka issue tracker and release notes.
type knownIssue struct {
	id, from, fixed, text string
}

var knownIssues = []knownIssue{
	{"KAFKA-20979", "4.1.0", "4.3.2",
		"time index truncation is not flushed on clean shutdown; retention may delete recent segments after a restart (no fix in 4.2.x)"},
}

// issuesOf returns the known issues of a Kafka version.
func issuesOf(version string) []string {
	var out []string
	for _, i := range knownIssues {
		if compareVersions(version, i.from) >= 0 && compareVersions(version, i.fixed) < 0 {
			out = append(out, i.id+": "+i.text)
		}
	}
	return out
}

// minJavaMajor is the oldest Java runtime Kafka 4.x brokers and controllers support.
const minJavaMajor = 17

// adopted reports whether the node's agent runs Kafka with an operator-owned configuration.
func adopted(n *store.Node) bool {
	var st struct {
		Adopted bool `json:"adopted"`
	}
	return json.Unmarshal(n.Status, &st) == nil && st.Adopted
}

// javaMajor returns the Java feature release reported by the node's agent, 0 when unknown.
// Old runtimes report "1.8.0_402", newer ones "21.0.8".
func javaMajor(n *store.Node) int {
	var st struct {
		JavaVersion string `json:"javaVersion"`
	}
	if json.Unmarshal(n.Status, &st) != nil || st.JavaVersion == "" {
		return 0
	}
	parts := strings.SplitN(st.JavaVersion, ".", 3)
	major, err := strconv.Atoi(strings.TrimSuffix(parts[0], "-ea"))
	if err != nil {
		return 0
	}
	if major == 1 && len(parts) > 1 {
		major, _ = strconv.Atoi(parts[1])
	}
	return major
}

func (m *Manager) planUpgrade(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := parse[upgradeParams](raw)
	if err != nil {
		return nil, err
	}
	if !versionRe.MatchString(params.Version) {
		return nil, fmt.Errorf("invalid version %q", params.Version)
	}
	if p.Cluster.Provider == store.ProviderStrimzi {
		return m.planStrimziUpgrade(ctx, p, params.Version)
	}
	mn, err := m.managedNodes(ctx, p.Cluster.ID)
	if err != nil {
		return nil, err
	}
	return upgradePlan(p, mn, params)
}

func upgradePlan(p *ops.Planner, mn managed, params upgradeParams) (*ops.Planned, error) {
	s := settingsOf(p.Cluster)
	if compareVersions(params.Version, s.KafkaVersion) <= 0 {
		return nil, fmt.Errorf("target %s is not newer than the current version %s", params.Version, s.KafkaVersion)
	}
	if compareVersions(params.Version, "4.0.0") >= 0 && compareVersions(s.KafkaVersion, "3.3.0") < 0 {
		return nil, errors.New("kafka 4.x requires upgrading from 3.3 or later (upgrade to 3.9 first)")
	}
	nodes := orderedNodes(mn, p.View.Health.QuorumLeader)
	if len(nodes) == 0 {
		return nil, errors.New("no managed nodes")
	}
	doc := ops.PlanDoc{Summary: fmt.Sprintf("Upgrade %d node(s) from Kafka %s to %s one at a time (controllers first, quorum leader last)",
		len(nodes), s.KafkaVersion, params.Version), Warnings: []string{}, Details: map[string]any{"from": s.KafkaVersion, "to": params.Version}}
	for _, bug := range issuesOf(params.Version) {
		doc.Warnings = append(doc.Warnings, "target "+params.Version+" has "+bug+"; prefer 4.3.2 or later")
	}
	for _, bug := range issuesOf(s.KafkaVersion) {
		doc.Warnings = append(doc.Warnings, "the current version "+s.KafkaVersion+" has "+bug+"; the restarts of this upgrade are exposed to it")
	}
	for _, n := range nodes {
		if major := javaMajor(n); major > 0 && major < minJavaMajor {
			return nil, fmt.Errorf("%s runs Java %d; Kafka 4.x needs Java %d or later", n.Name, major, minJavaMajor)
		}
	}
	doc.Warnings = append(doc.Warnings, "metadata.version stays at the old level until you run finalize-upgrade; you can still roll back binaries before that")
	steps := []ops.StepSpec{{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}}}
	for _, n := range nodes {
		a := nodeArgs(p.Cluster, n)
		steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Download Kafka %s on %s", params.Version, n.Name), Kind: KindInstall,
			Args: NodeArgs{Name: a.Name, Node: a.Node, Version: params.Version}})
	}
	for _, n := range nodes {
		a := nodeArgs(p.Cluster, n)
		a.Version, a.Rack = params.Version, rackOf(p, n)
		steps = append(steps,
			ops.StepSpec{Name: fmt.Sprintf("Upgrade %s to %s", n.Name, params.Version), Kind: KindUpgrade, Args: a},
			ops.StepSpec{Name: "Check health after " + n.Name, Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}})
	}
	s.KafkaVersion = params.Version
	steps = append(steps,
		ops.StepSpec{Name: "Save the new version", Kind: KindSetSettings, Args: NodeArgs{Settings: &s}},
		ops.StepSpec{Name: "Elect preferred leaders", Kind: ops.KindElectPreferred, Args: ops.ReassignArgs{}},
	)
	steps = append(steps, syncSteps(p.Cluster, mn)...)
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}
