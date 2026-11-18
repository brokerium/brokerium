// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/brokerium/brokerium/internal/agentcfg"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/store"
)

// maxVoters is the largest controller quorum Brokerium builds.
const maxVoters = 5

// fleet is what planners know about agent nodes: every node and which agents are connected.
type fleet struct {
	nodes     []*store.Node
	connected map[string]bool
}

func (m *Manager) fleet(ctx context.Context) (fleet, error) {
	nodes, err := m.Store.ListNodes(ctx, "")
	if err != nil {
		return fleet{}, err
	}
	f := fleet{nodes: nodes, connected: map[string]bool{}}
	for _, n := range nodes {
		f.connected[n.ID] = m.Gateway != nil && m.Gateway.Connected(n.ID)
	}
	return f, nil
}

func (f fleet) node(id string) *store.Node {
	for _, n := range f.nodes {
		if n.ID == id {
			return n
		}
	}
	return nil
}

// label names a chosen host in messages: its node name, or the id when it is unknown.
func (f fleet) label(id string) string {
	if n := f.node(id); n != nil {
		return n.Name
	}
	return fmt.Sprintf("%q", id)
}

// managed returns the nodes of a cluster.
func (f fleet) managed(clusterID string) managed {
	var nodes []*store.Node
	for _, n := range f.nodes {
		if n.ClusterID != nil && *n.ClusterID == clusterID {
			nodes = append(nodes, n)
		}
	}
	return classify(nodes)
}

// pick returns the host with agent node id `id` for Kafka node kafkaID with the role in a
// cluster, or why it cannot run it.
func (f fleet) pick(id, clusterID, role string, kafkaID int32, kafkaClusterID string) (*store.Node, error) {
	if id == "" {
		return nil, errors.New("node is required: the id of an agent node")
	}
	n := f.node(id)
	if n == nil {
		return nil, fmt.Errorf("unknown host %q: no agent node has this id", id)
	}
	if err := claimable(n, clusterID, role, kafkaID); err != nil {
		return nil, err
	}
	if !f.connected[n.ID] {
		return nil, fmt.Errorf("host %s is not connected", n.Name)
	}
	if n.AdvertisedHost == "" {
		return nil, fmt.Errorf("host %s has no advertised host; set one on the Nodes page", n.Name)
	}
	if err := agentcfg.ValidateHost(n.AdvertisedHost); err != nil {
		return nil, fmt.Errorf("host %s: %w; set its advertised host on the Nodes page", n.Name, err)
	}
	if err := checkStorage(n, kafkaClusterID, kafkaID); err != nil {
		return nil, err
	}
	return n, nil
}

// claimable reports why host n cannot run Kafka node id with the role in a cluster. A host that
// already runs exactly this node passes, so that steps can run again.
func claimable(n *store.Node, clusterID, role string, id int32) error {
	inCluster := n.ClusterID != nil && *n.ClusterID == clusterID
	switch {
	case inCluster && n.Role == role && n.KafkaNodeID != nil && *n.KafkaNodeID == id:
		return nil
	case n.Role != "":
		where := "of another cluster"
		if inCluster {
			where = "in this cluster"
		}
		return fmt.Errorf("host %s already runs Kafka as %s %s", n.Name, assignment(n), where)
	case n.ClusterID != nil && !inCluster:
		return fmt.Errorf("host %s belongs to another cluster", n.Name)
	case adopted(n):
		return fmt.Errorf("host %s runs Kafka with its own configuration (adopted)", n.Name)
	}
	return nil
}

// holds reports whether node n runs Kafka node id of a cluster.
func holds(n *store.Node, clusterID string, id int32) bool {
	return n.ClusterID != nil && *n.ClusterID == clusterID && n.KafkaNodeID != nil && *n.KafkaNodeID == id
}

// resumable returns the Kafka node id that host n holds with the role in a cluster while the node
// is not among the joined ones: an earlier operation gave the host its role and failed before the
// node joined. Choosing the host again resumes that node.
func resumable(n *store.Node, clusterID, role string, joined ids) (int32, bool) {
	if n == nil || n.Role != role || n.KafkaNodeID == nil || !holds(n, clusterID, *n.KafkaNodeID) || joined[*n.KafkaNodeID] {
		return 0, false
	}
	return *n.KafkaNodeID, true
}

func assignment(n *store.Node) string {
	if n.KafkaNodeID == nil {
		return n.Role
	}
	return fmt.Sprintf("%s %d", n.Role, *n.KafkaNodeID)
}

// checkStorage refuses a host whose log directory is formatted for another Kafka cluster or
// node id: Kafka would not start with it, and formatting skips formatted directories.
func checkStorage(n *store.Node, kafkaClusterID string, id int32) error {
	var st struct {
		Meta struct {
			ClusterID string `json:"clusterId"`
			NodeID    int32  `json:"nodeId"`
			Formatted bool   `json:"formatted"`
		} `json:"meta"`
	}
	if json.Unmarshal(n.Status, &st) != nil || !st.Meta.Formatted || (st.Meta.ClusterID == kafkaClusterID && st.Meta.NodeID == id) {
		return nil
	}
	return fmt.Errorf("host %s holds the data of node %d of Kafka cluster %s; clear its Kafka data directory first",
		n.Name, st.Meta.NodeID, st.Meta.ClusterID)
}

// hostOf is the address node n advertises: the container name of a Docker node, else its
// advertised host, or its name when it has none.
func hostOf(docker bool, n *store.Node) string {
	if docker || n.AdvertisedHost == "" {
		return n.Name
	}
	return n.AdvertisedHost
}

// member is a Kafka node that a plan starts: its role, id and rack and the container or agent
// node that runs it.
type member struct {
	role string
	id   int32
	rack string
	// name is the container of a Docker node, else the name of the agent node.
	name string
	// node is the agent node; nil for Docker containers.
	node *store.Node
	// resumed is set when the host already holds the node from an operation that failed.
	resumed bool
}

func (mb member) host() string {
	if mb.node == nil {
		return mb.name
	}
	return mb.node.AdvertisedHost
}

// args address the member in step arguments: agent nodes by id, containers by name.
func (mb member) args() NodeArgs {
	a := NodeArgs{Name: mb.name, Role: mb.role, NodeID: mb.id, Rack: mb.rack}
	if mb.node != nil {
		a.Node, a.Host = mb.node.ID, mb.node.AdvertisedHost
	}
	return a
}

func (mb member) String() string {
	s := fmt.Sprintf("%s %d", mb.role, mb.id)
	if mb.rack != "" {
		s += ", rack " + mb.rack
	}
	return mb.name + " (" + s + ")"
}

func describe(members []member) string {
	parts := make([]string, 0, len(members))
	for _, mb := range members {
		parts = append(parts, mb.String())
	}
	return strings.Join(parts, ", ")
}

// hostDetails lists the chosen hosts for the plan document.
func hostDetails(members []member) []map[string]any {
	out := make([]map[string]any, 0, len(members))
	for _, mb := range members {
		out = append(out, map[string]any{"node": mb.node.ID, "name": mb.name, "advertisedHost": mb.host(),
			"role": mb.role, "nodeId": mb.id, "rack": mb.rack})
	}
	return out
}

// checkAddresses refuses hosts that advertise the same address as each other or as a node that
// runs Kafka in the cluster. Hosts that only joined the cluster run nothing yet.
func checkAddresses(members []member, existing []*store.Node) error {
	chosen := map[string]bool{}
	for _, mb := range members {
		if mb.node != nil {
			chosen[mb.node.ID] = true
		}
	}
	seen := map[string]string{}
	for _, n := range existing {
		if n.Role != "" && !chosen[n.ID] {
			seen[strings.ToLower(hostOf(false, n))] = n.Name
		}
	}
	for _, mb := range members {
		key := strings.ToLower(mb.host())
		if other, ok := seen[key]; ok {
			return fmt.Errorf("hosts %s and %s both advertise %s", other, mb.name, mb.host())
		}
		seen[key] = mb.name
	}
	return nil
}

// resumeWarnings name the hosts that continue a node an earlier operation did not finish.
func resumeWarnings(members []member) []string {
	var out []string
	for _, mb := range members {
		if mb.resumed {
			out = append(out, fmt.Sprintf("host %s holds %s %d from an operation that did not finish; this plan continues it", mb.name, mb.role, mb.id))
		}
	}
	return out
}

// rackWarnings warns when only some brokers have a rack: rebalancing then ignores racks.
func rackWarnings(existing []kafka.Broker, added []member) []string {
	var with, without []string
	for _, b := range existing {
		if b.Rack == "" {
			without = append(without, fmt.Sprint(b.ID))
		} else {
			with = append(with, fmt.Sprint(b.ID))
		}
	}
	for _, mb := range added {
		if !runsBroker(mb.role) {
			continue
		}
		if mb.rack == "" {
			without = append(without, fmt.Sprint(mb.id))
		} else {
			with = append(with, fmt.Sprint(mb.id))
		}
	}
	if len(with) == 0 || len(without) == 0 {
		return nil
	}
	return []string{fmt.Sprintf("broker(s) %s have no rack while others have one; rebalancing ignores racks until every broker has one",
		strings.Join(without, ", "))}
}

// ids is a set of Kafka node ids that are in use or given out.
type ids map[int32]bool

// registeredBrokers are the ids of the brokers registered in the cluster.
func registeredBrokers(p *ops.Planner) ids {
	out := ids{}
	if p.View != nil && p.View.Snapshot != nil {
		for _, b := range p.View.Snapshot.Brokers {
			out[b.ID] = true
		}
	}
	return out
}

// usedIDs are the node ids a cluster knows: registered brokers, voters and observers of the
// quorum and its managed nodes.
func usedIDs(p *ops.Planner, mn managed) ids {
	used := ids{}
	if p.View != nil && p.View.Snapshot != nil {
		for _, b := range p.View.Snapshot.Brokers {
			used[b.ID] = true
		}
		if q := p.View.Snapshot.Quorum; q != nil {
			for _, r := range append(append([]kafka.QuorumReplica(nil), q.Voters...), q.Observers...) {
				used[r.NodeID] = true
			}
		}
	}
	for _, n := range mn.nodes {
		if n.KafkaNodeID != nil {
			used[*n.KafkaNodeID] = true
		}
	}
	return used
}

// claim gives out an explicit node id.
func (s ids) claim(id int32) error {
	switch {
	case id < 0:
		return fmt.Errorf("node id %d: node ids are at least 0", id)
	case s[id]:
		return fmt.Errorf("node id %d is already used", id)
	}
	s[id] = true
	return nil
}

// next gives out the lowest free node id from `from` on.
func (s ids) next(from int32) int32 {
	id := from
	for s[id] {
		id++
	}
	s[id] = true
	return id
}
