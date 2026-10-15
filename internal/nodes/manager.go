// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package nodes implements node lifecycle operations on clusters whose nodes run
// brokerium-agent: creating clusters, adding and replacing brokers, adding controllers,
// migrating quorums, rolling restarts and upgrades.
package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/bvaliev/brokerium/internal/agentgw"
	"github.com/bvaliev/brokerium/internal/balancer"
	"github.com/bvaliev/brokerium/internal/cluster"
	"github.com/bvaliev/brokerium/internal/ops"
	"github.com/bvaliev/brokerium/internal/provider/docker"
	"github.com/bvaliev/brokerium/internal/store"
)

// Config holds provider settings.
type Config struct {
	AgentEndpoint string
	AgentImage    string
	LabNetwork    string
}

// Manager implements ops.NodeManager.
type Manager struct {
	Store    *store.Store
	Gateway  *agentgw.Gateway
	Docker   *docker.Client
	Clusters *cluster.Service
	Log      *slog.Logger
	Cfg      Config
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
			return ops.StepSpec{Name: fmt.Sprintf("Stop and remove node %s", n.Name), Kind: KindRemoveNode,
				Args: NodeArgs{Name: n.Name, Role: role, NodeID: id}}, true
		}
	}
	return ops.StepSpec{}, false
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

// startSteps provisions and starts one node.
func startSteps(role, name string, id int32, rack, mode, initial, version string) []ops.StepSpec {
	a := NodeArgs{Name: name, Role: role, NodeID: id, Rack: rack, Mode: mode, InitialControllers: initial, Version: version}
	steps := []ops.StepSpec{
		{Name: "Provision node " + name, Kind: KindProvision, Args: a},
		{Name: "Wait for agent on " + name, Kind: KindWaitAgent, Args: a},
		{Name: "Install Kafka on " + name, Kind: KindInstall, Args: a},
		{Name: "Configure " + name, Kind: KindConfigure, Args: a},
		{Name: "Format storage on " + name, Kind: KindFormat, Args: a},
		{Name: "Start Kafka on " + name, Kind: KindStart, Args: a},
	}
	return steps
}

type bootstrapParams struct {
	Controllers int      `json:"controllers"`
	Brokers     int      `json:"brokers"`
	Version     string   `json:"version"`
	Racks       []string `json:"racks"`
	Quorum      string   `json:"quorum"`
	PortBase    int      `json:"portBase"`
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
	if params.Controllers == 0 {
		params.Controllers = 3
	}
	if params.Brokers == 0 {
		params.Brokers = 3
	}
	if params.Version == "" {
		params.Version = "4.3.1"
	}
	if params.Quorum == "" {
		params.Quorum = "dynamic"
	}
	if len(params.Racks) == 0 {
		params.Racks = []string{"rack-a", "rack-b", "rack-c"}
	}
	switch {
	case !versionRe.MatchString(params.Version):
		return nil, fmt.Errorf("invalid version %q", params.Version)
	case params.Controllers < 1 || params.Controllers > 5:
		return nil, errors.New("controllers must be between 1 and 5")
	case params.Brokers < 1 || params.Brokers > 9:
		return nil, errors.New("brokers must be between 1 and 9")
	case params.Quorum != "dynamic" && params.Quorum != "static":
		return nil, errors.New("quorum must be dynamic or static")
	}
	switch {
	case p.Cluster.Provider != store.ProviderDocker && p.Cluster.Provider != store.ProviderAgent:
		return nil, errors.New("only Docker and agent clusters are created by Brokerium")
	case p.Cluster.Provider == store.ProviderDocker && m.Docker == nil:
		return nil, errors.New("the Docker provider is not available on this server (no Docker socket)")
	}
	// Agent hosts join the cluster before it exists; only nodes with a Kafka role count.
	if mn, err := m.managedNodes(ctx, p.Cluster.ID); err == nil && len(mn.controllers)+len(mn.brokers) > 0 {
		return nil, errors.New("the cluster already has Kafka nodes")
	}
	if params.PortBase == 0 {
		used := m.usedPortBases(ctx, p.Cluster.ID)
		for base := 21000; base < 60000; base += 1000 {
			if !used[base] {
				params.PortBase = base
				break
			}
		}
	}
	rf := min(3, params.Brokers)
	s := Settings{KafkaVersion: params.Version, Quorum: params.Quorum, KafkaClusterID: RandomUUID(),
		PortBase: params.PortBase, Racks: params.Racks, ReplicationFactor: rf, MinISR: min(2, rf)}
	s = settingsOf(&store.Cluster{Settings: s.toMap()})

	docker := p.Cluster.Provider == store.ProviderDocker
	var controllers []string
	var initial []string
	var voters []string
	for i := 1; i <= params.Controllers; i++ {
		name := NodeName(p.Cluster.Name, RoleController, int32(i))
		controllers = append(controllers, name)
		s.ControllerBootstrapServers = append(s.ControllerBootstrapServers, controllerAddress(s, docker, name, int32(i)))
		initial = append(initial, fmt.Sprintf("%d@%s:9093:%s", i, name, RandomUUID()))
		voters = append(voters, fmt.Sprintf("%d@%s:9093", i, name))
	}
	var bootstrap []string
	for i := 0; i < params.Brokers; i++ {
		id := int32(101 + i)
		bootstrap = append(bootstrap, brokerAddress(s, docker, NodeName(p.Cluster.Name, RoleBroker, id), id))
	}
	steps := []ops.StepSpec{{Name: "Save cluster settings", Kind: KindSetSettings,
		Args: NodeArgs{Settings: &s, BootstrapServers: bootstrap}}}
	quorumKind, quorum := "dynamic", quorumBootstrap(controllers)
	if params.Quorum == "static" {
		quorumKind, quorum = "static", strings.Join(voters, ",")
	}
	for i, name := range controllers {
		mode := "initial"
		if params.Quorum == "static" {
			mode = "static"
		}
		for _, st := range startSteps(RoleController, name, int32(i+1), "", mode, strings.Join(initial, ","), params.Version) {
			a := st.Args.(NodeArgs)
			a.QuorumKind, a.Quorum = quorumKind, quorum
			st.Args = a
			steps = append(steps, st)
		}
	}
	for i := 0; i < params.Brokers; i++ {
		id := int32(101 + i)
		name := NodeName(p.Cluster.Name, RoleBroker, id)
		rack := params.Racks[i%len(params.Racks)]
		for _, st := range startSteps(RoleBroker, name, id, rack, "join", "", params.Version) {
			a := st.Args.(NodeArgs)
			a.QuorumKind, a.Quorum = quorumKind, quorum
			st.Args = a
			steps = append(steps, st)
		}
	}
	for i := 0; i < params.Brokers; i++ {
		steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Wait for broker %d", 101+i), Kind: KindWaitBroker,
			Args: NodeArgs{NodeID: int32(101 + i)}})
	}
	steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Wait for a quorum of %d voters", params.Controllers),
		Kind: KindWaitQuorum, Args: NodeArgs{ExpectVoters: params.Controllers}})
	doc := ops.PlanDoc{Summary: fmt.Sprintf("Create %d controller(s) and %d broker(s) running Kafka %s with a %s quorum",
		params.Controllers, params.Brokers, params.Version, params.Quorum),
		Details: map[string]any{"kafkaClusterId": s.KafkaClusterID, "portBase": s.PortBase, "bootstrapServers": bootstrap}}
	if params.Quorum == "static" {
		doc.Warnings = append(doc.Warnings, "a static quorum cannot change controllers until it is migrated to a dynamic quorum")
	}
	if params.Controllers%2 == 0 {
		doc.Warnings = append(doc.Warnings, fmt.Sprintf("%d controllers tolerate as many failures as %d", params.Controllers, params.Controllers-1))
	}
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}

type addBrokerParams struct {
	Count     int    `json:"count"`
	Rack      string `json:"rack"`
	Rebalance *bool  `json:"rebalance"`
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

func (m *Manager) planAddBroker(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := parse[addBrokerParams](raw)
	if err != nil {
		return nil, err
	}
	if params.Count <= 0 {
		params.Count = 1
	}
	if params.Count > 6 {
		return nil, errors.New("add at most 6 brokers at once")
	}
	mn, err := m.managedNodes(ctx, p.Cluster.ID)
	if err != nil {
		return nil, err
	}
	s := settingsOf(p.Cluster)
	id := nextID(mn, RoleBroker, 101)
	for _, b := range p.View.Snapshot.Brokers {
		if b.ID >= id {
			id = b.ID + 1
		}
	}
	rackCount := map[string]int{}
	for _, b := range p.View.Snapshot.Brokers {
		rackCount[b.Rack]++
	}
	var steps []ops.StepSpec
	var names []string
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
		name := NodeName(p.Cluster.Name, RoleBroker, id)
		names = append(names, fmt.Sprintf("%d (%s)", id, rack))
		steps = append(steps, startSteps(RoleBroker, name, id, rack, "join", "", s.KafkaVersion)...)
		steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Wait for broker %d", id), Kind: KindWaitBroker, Args: NodeArgs{NodeID: id}})
		id++
	}
	steps = append(steps, ops.StepSpec{Name: "Update bootstrap servers", Kind: KindSyncBootstrap, Args: NodeArgs{}})
	rebalance := params.Rebalance == nil || *params.Rebalance
	if rebalance {
		steps = append(steps, ops.StepSpec{Name: "Rebalance onto the new brokers", Kind: KindRebalance, Args: NodeArgs{Mode: "even"}})
	}
	doc := ops.PlanDoc{Summary: fmt.Sprintf("Add %d broker(s): %s; rebalance afterwards: %v", params.Count, strings.Join(names, ", "), rebalance)}
	return &ops.Planned{Doc: doc, Steps: append([]ops.StepSpec{{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}}}, steps...)}, nil
}

type replaceParams struct {
	BrokerID *int32 `json:"brokerId"`
	Mode     string `json:"mode"`
}

func (m *Manager) planReplace(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	params, err := parse[replaceParams](raw)
	if err != nil {
		return nil, err
	}
	if params.BrokerID == nil {
		return nil, errors.New("brokerId is required")
	}
	if params.Mode == "" {
		params.Mode = "same-id"
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
	mn, err := m.managedNodes(ctx, p.Cluster.ID)
	if err != nil {
		return nil, err
	}
	oldName := NodeName(p.Cluster.Name, RoleBroker, old)
	for _, n := range mn.brokers {
		if *n.KafkaNodeID == old {
			oldName = n.Name
		}
	}
	gate := ops.StepSpec{Name: "Check health (under-replicated partitions allowed)", Kind: ops.KindHealthGate,
		Args: ops.HealthGateArgs{AllowUnderReplicated: true}}
	switch params.Mode {
	case "same-id":
		steps := []ops.StepSpec{gate, {Name: "Remove failed node " + oldName, Kind: KindRemoveNode, Args: NodeArgs{Name: oldName, Role: RoleBroker, NodeID: old}}}
		steps = append(steps, startSteps(RoleBroker, oldName, old, rack, "join", "", s.KafkaVersion)...)
		steps = append(steps,
			ops.StepSpec{Name: fmt.Sprintf("Wait for broker %d", old), Kind: KindWaitBroker, Args: NodeArgs{NodeID: old}},
			ops.StepSpec{Name: "Wait until all replicas are in sync", Kind: KindWaitInSync, Args: NodeArgs{}},
			ops.StepSpec{Name: "Update bootstrap servers", Kind: KindSyncBootstrap, Args: NodeArgs{}},
		)
		return &ops.Planned{Doc: ops.PlanDoc{Summary: fmt.Sprintf(
			"Replace broker %d in place: new node with the same id and an empty disk; followers re-replicate from the leaders", old)},
			Steps: steps}, nil
	case "new-id":
		id := nextID(mn, RoleBroker, 101)
		for _, b := range p.View.Snapshot.Brokers {
			if b.ID >= id {
				id = b.ID + 1
			}
		}
		name := NodeName(p.Cluster.Name, RoleBroker, id)
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
		steps := append([]ops.StepSpec{gate}, startSteps(RoleBroker, name, id, rack, "join", "", s.KafkaVersion)...)
		steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Wait for broker %d", id), Kind: KindWaitBroker, Args: NodeArgs{NodeID: id}})
		if len(moves) > 0 {
			steps = append(steps, ops.ReassignSteps(moves, 0, 0, true)...)
		}
		steps = append(steps,
			ops.StepSpec{Name: fmt.Sprintf("Unregister broker %d", old), Kind: ops.KindUnregisterBroker, Args: ops.BrokerArgs{BrokerID: old}},
			ops.StepSpec{Name: "Remove failed node " + oldName, Kind: KindRemoveNode, Args: NodeArgs{Name: oldName, Role: RoleBroker, NodeID: old}},
			ops.StepSpec{Name: "Update bootstrap servers", Kind: KindSyncBootstrap, Args: NodeArgs{}},
		)
		return &ops.Planned{Doc: ops.PlanDoc{Summary: fmt.Sprintf(
			"Replace broker %d with new broker %d (%s): move %d partition(s) over, then unregister %d", old, id, rack, len(moves), old),
			Irreversible: true, Moves: moves}, Steps: steps}, nil
	default:
		return nil, errors.New("mode must be same-id or new-id")
	}
}

func (m *Manager) planAddController(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	if _, err := parse[struct{}](raw); err != nil {
		return nil, err
	}
	q := p.View.Snapshot.Quorum
	if q == nil {
		return nil, errors.New("the quorum cannot be described")
	}
	mn, err := m.managedNodes(ctx, p.Cluster.ID)
	if err != nil {
		return nil, err
	}
	id := nextID(mn, RoleController, 1)
	for _, v := range q.Voters {
		if v.NodeID >= id {
			id = v.NodeID + 1
		}
	}
	if id >= 100 {
		return nil, errors.New("controller ids must stay below 100")
	}
	s := settingsOf(p.Cluster)
	name := NodeName(p.Cluster.Name, RoleController, id)
	steps := []ops.StepSpec{{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}}}
	steps = append(steps, startSteps(RoleController, name, id, "", "join", "", s.KafkaVersion)...)
	a := NodeArgs{Name: name, Role: RoleController, NodeID: id}
	steps = append(steps,
		ops.StepSpec{Name: fmt.Sprintf("Wait for controller %d to catch up as an observer", id), Kind: KindWaitObserver, Args: a},
		ops.StepSpec{Name: fmt.Sprintf("Add controller %d as a voter", id), Kind: KindAddVoter, Args: a},
		ops.StepSpec{Name: fmt.Sprintf("Wait for a quorum of %d voters", len(q.Voters)+1), Kind: KindWaitQuorum, Args: NodeArgs{ExpectVoters: len(q.Voters) + 1}},
	)
	steps = append(steps, ops.StepSpec{Name: "Update bootstrap servers", Kind: KindSyncBootstrap, Args: NodeArgs{}})
	doc := ops.PlanDoc{Summary: fmt.Sprintf("Add controller %d (%s): %d -> %d voters", id, name, len(q.Voters), len(q.Voters)+1)}
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

func (m *Manager) planMigrate(ctx context.Context, p *ops.Planner, raw json.RawMessage) (*ops.Planned, error) {
	if _, err := parse[struct{}](raw); err != nil {
		return nil, err
	}
	if p.View.Health.KRaftVersion != 0 {
		return nil, errors.New("the quorum is already dynamic")
	}
	mn, err := m.managedNodes(ctx, p.Cluster.ID)
	if err != nil {
		return nil, err
	}
	if len(mn.controllers) == 0 {
		return nil, errors.New("no managed controllers; adopt the nodes with brokerium-agent first")
	}
	var names []string
	for _, c := range mn.controllers {
		names = append(names, c.Name)
	}
	s := settingsOf(p.Cluster)
	s.Quorum = "dynamic"
	steps := []ops.StepSpec{
		{Name: "Check cluster health", Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}},
		{Name: "Upgrade kraft.version to 1", Kind: ops.KindUpdateFeature, Args: ops.FeatureArgs{Feature: "kraft.version", Level: 1}},
		{Name: "Save quorum mode", Kind: KindSetSettings, Args: NodeArgs{Settings: &s}},
	}
	for _, n := range orderedNodes(mn, p.View.Health.QuorumLeader) {
		a := NodeArgs{Name: n.Name, Role: n.Role, NodeID: *n.KafkaNodeID, Rack: rackOf(p, n), QuorumKind: "dynamic", Quorum: quorumBootstrap(names)}
		steps = append(steps, ops.StepSpec{Name: "Restart " + n.Name + " with bootstrap servers", Kind: KindRestart, Args: a})
	}
	steps = append(steps, ops.StepSpec{Name: "Wait for the quorum", Kind: KindWaitQuorum, Args: NodeArgs{ExpectVoters: len(mn.controllers)}})
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
		steps = append(steps,
			ops.StepSpec{Name: "Restart " + n.Name, Kind: KindRestart, Args: NodeArgs{Name: n.Name, Role: n.Role, NodeID: *n.KafkaNodeID, Rack: rackOf(p, n)}},
			ops.StepSpec{Name: "Check health after " + n.Name, Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}})
		count++
	}
	if count == 0 {
		return nil, errors.New("no managed nodes to restart")
	}
	steps = append(steps, ops.StepSpec{Name: "Elect preferred leaders", Kind: ops.KindElectPreferred, Args: ops.ReassignArgs{}})
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
	s := settingsOf(p.Cluster)
	if compareVersions(params.Version, s.KafkaVersion) <= 0 {
		return nil, fmt.Errorf("target %s is not newer than the current version %s", params.Version, s.KafkaVersion)
	}
	if compareVersions(params.Version, "4.0.0") >= 0 && compareVersions(s.KafkaVersion, "3.3.0") < 0 {
		return nil, errors.New("kafka 4.x requires upgrading from 3.3 or later (upgrade to 3.9 first)")
	}
	mn, err := m.managedNodes(ctx, p.Cluster.ID)
	if err != nil {
		return nil, err
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
		steps = append(steps, ops.StepSpec{Name: fmt.Sprintf("Download Kafka %s on %s", params.Version, n.Name), Kind: KindInstall,
			Args: NodeArgs{Name: n.Name, Version: params.Version}})
	}
	for _, n := range nodes {
		steps = append(steps,
			ops.StepSpec{Name: fmt.Sprintf("Upgrade %s to %s", n.Name, params.Version), Kind: KindUpgrade,
				Args: NodeArgs{Name: n.Name, Role: n.Role, NodeID: *n.KafkaNodeID, Version: params.Version, Rack: rackOf(p, n)}},
			ops.StepSpec{Name: "Check health after " + n.Name, Kind: ops.KindHealthGate, Args: ops.HealthGateArgs{}})
	}
	s.KafkaVersion = params.Version
	steps = append(steps,
		ops.StepSpec{Name: "Save the new version", Kind: KindSetSettings, Args: NodeArgs{Settings: &s}},
		ops.StepSpec{Name: "Elect preferred leaders", Kind: ops.KindElectPreferred, Args: ops.ReassignArgs{}},
	)
	return &ops.Planned{Doc: doc, Steps: steps}, nil
}
