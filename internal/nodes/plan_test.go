// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"cmp"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/cluster"
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/store"
)

const testKafkaID = "x5dY0i3TQ2u3kE1nC7vB9w"

func agentCluster() *store.Cluster {
	return &store.Cluster{ID: "c1", Name: "prod", Provider: store.ProviderAgent,
		Settings: map[string]any{"kafkaClusterId": testKafkaID, "kafkaVersion": "4.3.1"}}
}

func dockerCluster() *store.Cluster {
	return &store.Cluster{ID: "c1", Name: "lab", Provider: store.ProviderDocker,
		Settings: map[string]any{"kafkaClusterId": testKafkaID, "kafkaVersion": "4.3.1", "portBase": 21000,
			"racks": []any{"rack-a", "rack-b", "rack-c"}}}
}

// host is a connected agent node without a cluster whose advertised host is <name>.example.com.
func host(id, name string, opts ...func(*store.Node)) *store.Node {
	n := &store.Node{ID: id, Name: name, Hostname: name, AdvertisedHost: name + ".example.com", Status: json.RawMessage(`{}`)}
	for _, o := range opts {
		o(n)
	}
	return n
}

func runs(clusterID, role string, id int32) func(*store.Node) {
	return func(n *store.Node) { n.ClusterID, n.Role, n.KafkaNodeID = &clusterID, role, &id }
}

func joined(clusterID string) func(*store.Node) {
	return func(n *store.Node) { n.ClusterID = &clusterID }
}
func rack(r string) func(*store.Node)       { return func(n *store.Node) { n.Rack = r } }
func advertises(h string) func(*store.Node) { return func(n *store.Node) { n.AdvertisedHost = h } }
func status(s string) func(*store.Node)     { return func(n *store.Node) { n.Status = json.RawMessage(s) } }

func fleetOf(nodes ...*store.Node) fleet {
	f := fleet{nodes: nodes, connected: map[string]bool{}}
	for _, n := range nodes {
		f.connected[n.ID] = true
	}
	return f
}

func (f fleet) offline(id string) fleet {
	f.connected[id] = false
	return f
}

func plannerFor(c *store.Cluster, snap *kafka.Snapshot) *ops.Planner {
	return &ops.Planner{Cluster: c, View: &cluster.View{Cluster: c, Snapshot: snap, Health: cluster.Health{QuorumLeader: 1}}}
}

// runningSnapshot is a cluster with voters 1-3 and brokers 101-103 in racks r1-r3; broker 103 is
// fenced, and topic t has two partitions on all brokers.
func runningSnapshot() *kafka.Snapshot {
	return &kafka.Snapshot{
		Brokers: []kafka.Broker{{ID: 101, Rack: "r1", Alive: true}, {ID: 102, Rack: "r2", Alive: true}, {ID: 103, Rack: "r3", Fenced: true}},
		Quorum: &kafka.Quorum{LeaderID: 1,
			Voters:    []kafka.QuorumReplica{{NodeID: 1}, {NodeID: 2}, {NodeID: 3}},
			Observers: []kafka.QuorumReplica{{NodeID: 101}, {NodeID: 102}, {NodeID: 103}}},
		Topics: []kafka.Topic{{Name: "t", Partitions: []kafka.Partition{
			{ID: 0, Leader: 101, Replicas: []int32{101, 102, 103}},
			{ID: 1, Leader: 102, Replicas: []int32{102, 103, 101}}}}},
	}
}

// runningFleet holds the managed nodes of runningSnapshot and free hosts free-1 to free-3.
func runningFleet(extra ...*store.Node) fleet {
	nodes := []*store.Node{
		host("c-1", "ctl-1", runs("c1", RoleController, 1)),
		host("c-2", "ctl-2", runs("c1", RoleController, 2)),
		host("c-3", "ctl-3", runs("c1", RoleController, 3)),
		host("b-101", "kafka-1", runs("c1", RoleBroker, 101)),
		host("b-102", "kafka-2", runs("c1", RoleBroker, 102)),
		host("b-103", "kafka-3", runs("c1", RoleBroker, 103)),
		host("f-1", "free-1", rack("r3")),
		host("f-2", "free-2"),
		host("f-3", "free-3", rack("r1")),
	}
	return fleetOf(append(nodes, extra...)...)
}

func id32(v int32) *int32 { return &v }

func stepsOf(kind string, steps []ops.StepSpec) []ops.StepSpec {
	var out []ops.StepSpec
	for _, s := range steps {
		if s.Kind == kind {
			out = append(out, s)
		}
	}
	return out
}

// started lists host, role, Kafka id and rack of every node a plan starts, in order.
func started(steps []ops.StepSpec) []string {
	var out []string
	for _, s := range stepsOf(KindProvision, steps) {
		a := s.Args.(NodeArgs)
		out = append(out, fmt.Sprintf("%s/%s/%d/%s", cmp.Or(a.Node, a.Name), a.Role, a.NodeID, a.Rack))
	}
	return out
}

func TestBootstrapAgentHosts(t *testing.T) {
	t.Parallel()
	free := func(extra ...*store.Node) fleet {
		return fleetOf(append([]*store.Node{
			host("n1", "kafka-1"), host("n2", "kafka-2", rack("r1")), host("n3", "kafka-3"), host("n4", "kafka-4"),
		}, extra...)...)
	}
	tests := []struct {
		name    string
		fleet   fleet
		params  bootstrapParams
		started []string
		quorum  string
		servers []string
	}{
		{"one controller and one broker", free(),
			bootstrapParams{Nodes: []hostChoice{{Node: "n1", Role: RoleController}, {Node: "n2", Role: RoleBroker}}},
			[]string{"n1/controller/1/", "n2/broker/101/r1"}, "kafka-1.example.com:9093", []string{"kafka-2.example.com:9092"}},
		{"explicit ids and racks in the given order", free(),
			bootstrapParams{Nodes: []hostChoice{
				{Node: "n1", Role: RoleController, NodeID: id32(2)}, {Node: "n3", Role: RoleController},
				{Node: "n2", Role: RoleBroker, Rack: "r9"}, {Node: "n4", Role: RoleBroker, NodeID: id32(101)},
			}},
			[]string{"n1/controller/2/", "n3/controller/1/", "n2/broker/102/r9", "n4/broker/101/"},
			"kafka-1.example.com:9093,kafka-3.example.com:9093", []string{"kafka-2.example.com:9092", "kafka-4.example.com:9092"}},
		{"controller id 0 and a broker below 101", free(),
			bootstrapParams{Nodes: []hostChoice{{Node: "n1", Role: RoleController, NodeID: id32(0)}, {Node: "n2", Role: RoleBroker, NodeID: id32(1)}}},
			[]string{"n1/controller/0/", "n2/broker/1/r1"}, "kafka-1.example.com:9093", []string{"kafka-2.example.com:9092"}},
		{"static quorum", free(),
			bootstrapParams{Quorum: "static", Nodes: []hostChoice{{Node: "n1", Role: RoleController, NodeID: id32(3)}, {Node: "n2", Role: RoleBroker}}},
			[]string{"n1/controller/3/", "n2/broker/101/r1"}, "3@kafka-1.example.com:9093", []string{"kafka-2.example.com:9092"}},
		{"hosts that joined with a token of the cluster",
			fleetOf(host("n1", "kafka-1", joined("c1")), host("n2", "kafka-2", joined("c1"))),
			bootstrapParams{Nodes: []hostChoice{{Node: "n1", Role: RoleController}, {Node: "n2", Role: RoleBroker}}},
			[]string{"n1/controller/1/", "n2/broker/101/"}, "kafka-1.example.com:9093", []string{"kafka-2.example.com:9092"}},
		{"IP addresses",
			fleetOf(host("n1", "kafka-1", advertises("2001:db8::1")), host("n2", "kafka-2", advertises("192.0.2.7"))),
			bootstrapParams{Nodes: []hostChoice{{Node: "n1", Role: RoleController}, {Node: "n2", Role: RoleBroker}}},
			[]string{"n1/controller/1/", "n2/broker/101/"}, "[2001:db8::1]:9093", []string{"192.0.2.7:9092"}},
		{"a host whose storage is not formatted",
			fleetOf(host("n1", "kafka-1"), host("n2", "kafka-2", status(`{"meta":{"clusterId":"other","nodeId":7,"formatted":false}}`))),
			bootstrapParams{Nodes: []hostChoice{{Node: "n1", Role: RoleController}, {Node: "n2", Role: RoleBroker}}},
			[]string{"n1/controller/1/", "n2/broker/101/"}, "kafka-1.example.com:9093", []string{"kafka-2.example.com:9092"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			planned, err := (&Manager{}).bootstrapPlan(plannerFor(&store.Cluster{ID: "c1", Name: "prod", Provider: store.ProviderAgent}, nil), tt.fleet, tt.params, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := started(planned.Steps); !slices.Equal(got, tt.started) {
				t.Errorf("started %v, want %v", got, tt.started)
			}
			check := planned.Steps[0].Args.(HostsArgs)
			if planned.Steps[0].Kind != KindCheckHosts || len(check.Hosts) != len(tt.started) {
				t.Errorf("first step %s %+v, want a check of every chosen host", planned.Steps[0].Kind, check)
			}
			save := planned.Steps[1].Args.(NodeArgs)
			if !slices.Equal(save.BootstrapServers, tt.servers) {
				t.Errorf("bootstrap servers %v, want %v", save.BootstrapServers, tt.servers)
			}
			if save.Settings.KafkaClusterID != check.KafkaClusterID || save.Settings.PortBase != 0 || save.Settings.Racks != nil {
				t.Errorf("settings %+v", save.Settings)
			}
			for _, s := range stepsOf(KindConfigure, planned.Steps) {
				a := s.Args.(NodeArgs)
				if a.Quorum != tt.quorum || a.Node == "" || a.Host == "" {
					t.Errorf("%s: node %q host %q quorum %q, want quorum %q", s.Name, a.Node, a.Host, a.Quorum, tt.quorum)
				}
			}
			if tt.params.Quorum != "static" {
				initial := stepsOf(KindFormat, planned.Steps)[0].Args.(NodeArgs).InitialControllers
				if !strings.Contains(initial, "@"+strings.Split(tt.quorum, ",")[0]+":") {
					t.Errorf("initial controllers %q do not use the advertised host", initial)
				}
			}
			if !strings.Contains(planned.Doc.Summary, " on kafka-1 (controller ") {
				t.Errorf("summary %q does not name the hosts", planned.Doc.Summary)
			}
		})
	}
}

func TestBootstrapAgentRefusals(t *testing.T) {
	t.Parallel()
	nodes := []*store.Node{
		host("n1", "kafka-1"), host("n2", "kafka-2"), host("n3", "kafka-3"), host("n4", "kafka-4"),
		host("n5", "kafka-5"), host("n6", "kafka-6"),
		host("busy", "busy-1", runs("c2", RoleBroker, 101)),
		host("taken", "taken-1", joined("c2")),
		host("adopted", "adopted-1", status(`{"adopted":true}`)),
		host("blank", "blank-1", advertises("")),
		host("bad", "bad-1", advertises("bad_name")),
		host("dirty", "dirty-1", status(`{"meta":{"clusterId":"old","nodeId":2,"formatted":true}}`)),
		host("twin", "twin-1", advertises("kafka-1.example.com")),
		host("down", "down-1"),
	}
	two := func(first, second hostChoice) bootstrapParams {
		return bootstrapParams{Nodes: []hostChoice{first, second}}
	}
	ctl := func(node string) hostChoice { return hostChoice{Node: node, Role: RoleController} }
	brk := func(node string) hostChoice { return hostChoice{Node: node, Role: RoleBroker} }
	tests := []struct {
		name    string
		cluster *store.Cluster
		extra   *store.Node
		params  bootstrapParams
		want    string
	}{
		{"no hosts", nil, nil, bootstrapParams{}, "choose the hosts with nodes"},
		{"counts of a Docker cluster", nil, nil, bootstrapParams{Controllers: 3, Nodes: []hostChoice{ctl("n1"), brk("n2")}}, "apply to Docker clusters"},
		{"combined count of a Docker cluster", nil, nil, bootstrapParams{Combined: 1, Nodes: []hostChoice{ctl("n1"), brk("n2")}}, "apply to Docker clusters"},
		{"racks of a Docker cluster", nil, nil, bootstrapParams{Racks: []string{"a"}, Nodes: []hostChoice{ctl("n1"), brk("n2")}}, "apply to Docker clusters"},
		{"unknown role", nil, nil, two(hostChoice{Node: "n1", Role: "voter"}, brk("n2")), `role must be controller, broker or combined, not "voter"`},
		{"no node id", nil, nil, two(ctl(""), brk("n2")), "nodes[0]: node is required"},
		{"the same host twice", nil, nil, two(ctl("n1"), brk("n1")), "host kafka-1 is chosen twice"},
		{"unknown host", nil, nil, two(ctl("nope"), brk("n2")), `unknown host "nope"`},
		{"host not connected", nil, nil, two(ctl("down"), brk("n2")), "host down-1 is not connected"},
		{"host with a Kafka role", nil, nil, two(ctl("n1"), brk("busy")), "host busy-1 already runs Kafka as broker 101 of another cluster"},
		{"host of another cluster", nil, nil, two(ctl("n1"), brk("taken")), "host taken-1 belongs to another cluster"},
		{"adopted host", nil, nil, two(ctl("adopted"), brk("n2")), "host adopted-1 runs Kafka with its own configuration"},
		{"no advertised host", nil, nil, two(ctl("blank"), brk("n2")), "host blank-1 has no advertised host"},
		{"advertised host that is no DNS name", nil, nil, two(ctl("bad"), brk("n2")), `host "bad_name" is neither a DNS name nor an IP address`},
		{"data of another cluster", nil, nil, two(ctl("dirty"), brk("n2")), "host dirty-1 holds the data of node 2 of Kafka cluster old"},
		{"two hosts with one address", nil, nil, two(ctl("n1"), brk("twin")), "hosts kafka-1 and twin-1 both advertise kafka-1.example.com"},
		{"no controller", nil, nil, two(brk("n1"), brk("n2")), "choose at least one controller or combined node"},
		{"no broker", nil, nil, two(ctl("n1"), ctl("n2")), "choose at least one broker or combined node"},
		{"six controllers", nil, nil, bootstrapParams{Nodes: []hostChoice{ctl("n1"), ctl("n2"), ctl("n3"), ctl("n4"), ctl("n5"), ctl("n6"), brk("dirty")}},
			"choose at most 5 controllers and combined nodes together, not 6"},
		{"five controllers and a combined node", nil, nil, bootstrapParams{Nodes: []hostChoice{ctl("n1"), ctl("n2"), ctl("n3"), ctl("n4"), ctl("n5"),
			{Node: "n6", Role: RoleCombined}}}, "choose at most 5 controllers and combined nodes together, not 6"},
		{"a combined node on a host of another cluster", nil, nil, two(hostChoice{Node: "busy", Role: RoleCombined}, brk("n2")),
			"host busy-1 already runs Kafka as broker 101 of another cluster"},
		{"an id given twice", nil, nil, two(hostChoice{Node: "n1", Role: RoleController, NodeID: id32(1)}, hostChoice{Node: "n2", Role: RoleBroker, NodeID: id32(1)}),
			"nodes[1]: node id 1 is already used"},
		{"a negative id", nil, nil, two(hostChoice{Node: "n1", Role: RoleController, NodeID: id32(-1)}, brk("n2")), "node ids are at least 0"},
		{"an invalid rack", nil, nil, two(ctl("n1"), hostChoice{Node: "n2", Role: RoleBroker, Rack: "rack a"}), `rack "rack a"`},
		{"a cluster with Kafka nodes", nil, host("old", "old-1", runs("c1", RoleBroker, 101)), two(ctl("n1"), brk("n2")), "the cluster already has Kafka nodes"},
		{"a Strimzi cluster", &store.Cluster{ID: "c1", Provider: store.ProviderStrimzi}, nil, two(ctl("n1"), brk("n2")), "only Docker and agent clusters"},
		{"an invalid version", nil, nil, bootstrapParams{Version: "4.3", Nodes: []hostChoice{ctl("n1"), brk("n2")}}, `invalid version "4.3"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			all := slices.Clone(nodes)
			if tt.extra != nil {
				all = append(all, tt.extra)
			}
			f := fleetOf(all...).offline("down")
			c := tt.cluster
			if c == nil {
				c = &store.Cluster{ID: "c1", Name: "prod", Provider: store.ProviderAgent}
			}
			_, err := (&Manager{}).bootstrapPlan(plannerFor(c, nil), f, tt.params, nil)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestBootstrapDocker(t *testing.T) {
	t.Parallel()
	p := plannerFor(&store.Cluster{ID: "c1", Name: "lab", Provider: store.ProviderDocker}, nil)
	planned, err := (&Manager{}).bootstrapPlan(p, fleet{}, bootstrapParams{Controllers: 1, Brokers: 2}, map[int]bool{21000: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"lab-c1/controller/1/", "lab-b101/broker/101/rack-a", "lab-b102/broker/102/rack-b"}
	if got := started(planned.Steps); !slices.Equal(got, want) {
		t.Errorf("started %v, want %v", got, want)
	}
	save := planned.Steps[0].Args.(NodeArgs)
	if planned.Steps[0].Kind != KindSetSettings || save.Settings.PortBase != 22000 ||
		!slices.Equal(save.BootstrapServers, []string{"localhost:22101", "localhost:22102"}) {
		t.Errorf("first step %s: %+v", planned.Steps[0].Kind, save)
	}
	if first := planned.Steps[1]; first.Name != "Provision node lab-c1" || first.Args.(NodeArgs).Node != "" {
		t.Errorf("Docker nodes are provisioned by container name, got %q %+v", first.Name, first.Args)
	}
	if quorum := stepsOf(KindConfigure, planned.Steps)[0].Args.(NodeArgs).Quorum; quorum != "lab-c1:9093" {
		t.Errorf("quorum %q", quorum)
	}
	if _, err := (&Manager{}).bootstrapPlan(p, fleet{}, bootstrapParams{Nodes: []hostChoice{{Node: "n1", Role: RoleBroker}}}, nil); err == nil ||
		!strings.Contains(err.Error(), "give controllers, combined and brokers instead of nodes") {
		t.Errorf("nodes on a Docker cluster: %v", err)
	}
}

func TestKafkaVersionDefaults(t *testing.T) {
	t.Parallel()
	installed := func(steps []ops.StepSpec) []string {
		var out []string
		for _, s := range stepsOf(KindInstall, steps) {
			out = append(out, s.Args.(NodeArgs).Version)
		}
		return out
	}
	p := plannerFor(&store.Cluster{ID: "c1", Name: "lab", Provider: store.ProviderDocker}, nil)
	for _, tt := range []struct {
		version, want string
	}{
		{"", DefaultKafkaVersion},
		{"4.3.1", "4.3.1"},
	} {
		planned, err := (&Manager{}).bootstrapPlan(p, fleet{}, bootstrapParams{Controllers: 1, Brokers: 1, Version: tt.version}, nil)
		if err != nil {
			t.Fatal(err)
		}
		saved := planned.Steps[0].Args.(NodeArgs).Settings.KafkaVersion
		if got := installed(planned.Steps); saved != tt.want || !slices.Equal(got, []string{tt.want, tt.want}) {
			t.Errorf("version %q: saved %s, installed %v, want %s", tt.version, saved, got, tt.want)
		}
	}

	// A cluster whose settings record no version keeps the release its nodes were created with.
	unrecorded := agentCluster()
	delete(unrecorded.Settings, "kafkaVersion")
	planned, err := addBrokerPlan(plannerFor(unrecorded, runningSnapshot()), runningFleet(), addBrokerParams{Nodes: []brokerChoice{{Node: "f-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := installed(planned.Steps); !slices.Equal(got, []string{"4.3.1"}) {
		t.Errorf("add-broker on a cluster without a recorded version installs %v, want 4.3.1", got)
	}
}

func TestAddBrokerAgentHosts(t *testing.T) {
	t.Parallel()
	p := plannerFor(agentCluster(), runningSnapshot())
	planned, err := addBrokerPlan(p, runningFleet(), addBrokerParams{Nodes: []brokerChoice{
		{Node: "f-1"}, {Node: "f-2", Rack: "r2"}, {Node: "f-3", NodeID: id32(104)},
	}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"f-1/broker/105/r3", "f-2/broker/106/r2", "f-3/broker/104/r1"}
	if got := started(planned.Steps); !slices.Equal(got, want) {
		t.Errorf("started %v, want %v", got, want)
	}
	kinds := []string{planned.Steps[0].Kind, planned.Steps[1].Kind}
	if !slices.Equal(kinds, []string{ops.KindHealthGate, KindCheckHosts}) {
		t.Errorf("first steps %v", kinds)
	}
	if check := planned.Steps[1].Args.(HostsArgs); check.KafkaClusterID != testKafkaID || len(check.Hosts) != 3 {
		t.Errorf("check %+v", check)
	}
	if len(stepsOf(KindSyncBootstrap, planned.Steps)) != 1 || len(stepsOf(KindRebalance, planned.Steps)) != 1 {
		t.Error("add-broker must update the bootstrap servers and rebalance")
	}
	if len(planned.Doc.Warnings) != 0 || !strings.Contains(planned.Doc.Summary, "105 (r3) on free-1") {
		t.Errorf("doc %+v", planned.Doc)
	}

	rackless, err := addBrokerPlan(p, runningFleet(), addBrokerParams{Nodes: []brokerChoice{{Node: "f-2"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(rackless.Doc.Warnings) != 1 || !strings.Contains(rackless.Doc.Warnings[0], "broker(s) 104 have no rack") {
		t.Errorf("warnings %v", rackless.Doc.Warnings)
	}
}

func TestAddBrokerRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cluster *store.Cluster
		extra   *store.Node
		params  addBrokerParams
		want    string
	}{
		{"no hosts", nil, nil, addBrokerParams{}, "choose the hosts with nodes"},
		{"a count", nil, nil, addBrokerParams{Count: 2, Nodes: []brokerChoice{{Node: "f-1"}}}, "count and rack apply to Docker clusters"},
		{"a rack for all", nil, nil, addBrokerParams{Rack: "r1", Nodes: []brokerChoice{{Node: "f-1"}}}, "count and rack apply to Docker clusters"},
		{"no node id", nil, nil, addBrokerParams{Nodes: []brokerChoice{{}}}, "nodes[0]: node is required"},
		{"the same host twice", nil, nil, addBrokerParams{Nodes: []brokerChoice{{Node: "f-1"}, {Node: "f-1"}}}, "host free-1 is chosen twice"},
		{"a host of the cluster", nil, nil, addBrokerParams{Nodes: []brokerChoice{{Node: "b-101"}}},
			"host kafka-1 already runs Kafka as broker 101 in this cluster"},
		{"an id in use", nil, nil, addBrokerParams{Nodes: []brokerChoice{{Node: "f-1", NodeID: id32(102)}}}, "node id 102 is already used"},
		{"a controller id", nil, nil, addBrokerParams{Nodes: []brokerChoice{{Node: "f-1", NodeID: id32(2)}}}, "node id 2 is already used"},
		{"an id given twice", nil, nil, addBrokerParams{Nodes: []brokerChoice{{Node: "f-1", NodeID: id32(150)}, {Node: "f-2", NodeID: id32(150)}}},
			"nodes[1]: node id 150 is already used"},
		{"the address of a node", nil, host("twin", "twin-1", advertises("KAFKA-2.example.com")), addBrokerParams{Nodes: []brokerChoice{{Node: "twin"}}},
			"hosts kafka-2 and twin-1 both advertise KAFKA-2.example.com"},
		{"hosts on Docker", dockerCluster(), nil, addBrokerParams{Nodes: []brokerChoice{{Node: "f-1"}}}, "give count instead of nodes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c := tt.cluster
			if c == nil {
				c = agentCluster()
			}
			var extra []*store.Node
			if tt.extra != nil {
				extra = append(extra, tt.extra)
			}
			_, err := addBrokerPlan(plannerFor(c, runningSnapshot()), runningFleet(extra...), tt.params)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestAddBrokerDocker(t *testing.T) {
	t.Parallel()
	f := fleetOf(host("d1", "lab-b101", runs("c1", RoleBroker, 101)), host("d2", "lab-b102", runs("c1", RoleBroker, 102)))
	planned, err := addBrokerPlan(plannerFor(dockerCluster(), runningSnapshot()), f, addBrokerParams{Count: 2})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := started(planned.Steps), []string{"lab-b104/broker/104/rack-a", "lab-b105/broker/105/rack-b"}; !slices.Equal(got, want) {
		t.Errorf("started %v, want %v", got, want)
	}
	if len(stepsOf(KindCheckHosts, planned.Steps)) != 0 {
		t.Error("Docker containers need no host check")
	}
}

func TestAddControllerAgentHost(t *testing.T) {
	t.Parallel()
	p := plannerFor(agentCluster(), runningSnapshot())
	for _, tt := range []struct {
		params addControllerParams
		id     int32
	}{
		{addControllerParams{Node: "f-2"}, 4},
		{addControllerParams{Node: "f-2", NodeID: id32(9)}, 9},
	} {
		planned, err := addControllerPlan(p, runningFleet(), tt.params)
		if err != nil {
			t.Fatal(err)
		}
		if got, want := started(planned.Steps), []string{fmt.Sprintf("f-2/controller/%d/", tt.id)}; !slices.Equal(got, want) {
			t.Errorf("started %v, want %v", got, want)
		}
		voter := stepsOf(KindAddVoter, planned.Steps)[0].Args.(NodeArgs)
		if voter.Node != "f-2" || voter.Host != "free-2.example.com" || voter.NodeID != tt.id {
			t.Errorf("add voter %+v", voter)
		}
		if !strings.Contains(planned.Doc.Summary, "free-2): 3 -> 4 voters") {
			t.Errorf("summary %q", planned.Doc.Summary)
		}
	}
}

func TestAddControllerRefusals(t *testing.T) {
	t.Parallel()
	five := runningSnapshot()
	five.Quorum.Voters = append(five.Quorum.Voters, kafka.QuorumReplica{NodeID: 4}, kafka.QuorumReplica{NodeID: 5})
	noQuorum := runningSnapshot()
	noQuorum.Quorum = nil
	tests := []struct {
		name    string
		cluster *store.Cluster
		snap    *kafka.Snapshot
		params  addControllerParams
		want    string
	}{
		{"no host", nil, nil, addControllerParams{}, "node is required"},
		{"five voters", nil, five, addControllerParams{Node: "f-1"}, "the quorum already has 5 voters"},
		{"no quorum", nil, noQuorum, addControllerParams{Node: "f-1"}, "the quorum cannot be described"},
		{"a broker id", nil, nil, addControllerParams{Node: "f-1", NodeID: id32(101)}, "node id 101 is already used"},
		{"a host of the cluster", nil, nil, addControllerParams{Node: "c-2"}, "host ctl-2 already runs Kafka as controller 2 in this cluster"},
		{"a host on Docker", dockerCluster(), nil, addControllerParams{Node: "f-1"}, "give neither node nor nodeId"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, snap := tt.cluster, tt.snap
			if c == nil {
				c = agentCluster()
			}
			if snap == nil {
				snap = runningSnapshot()
			}
			_, err := addControllerPlan(plannerFor(c, snap), runningFleet(), tt.params)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestReplaceAgentBroker(t *testing.T) {
	t.Parallel()
	p := plannerFor(agentCluster(), runningSnapshot())
	tests := []struct {
		name    string
		params  replaceParams
		started []string
		removed bool
		moves   int
		warn    string
	}{
		{"same id on its host", replaceParams{BrokerID: id32(103)}, []string{"b-103/broker/103/r3"}, true, 0, ""},
		{"same id on another host", replaceParams{BrokerID: id32(103), Node: "f-3"}, []string{"f-3/broker/103/r1"}, true, 0,
			"broker 103 was in rack r3; host free-3 is in rack r1"},
		{"new id on another host", replaceParams{BrokerID: id32(103), Mode: "new-id", Node: "f-2"}, []string{"f-2/broker/104/r3"}, true, 2, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			planned, err := replacePlan(p, runningFleet(), tt.params)
			if err != nil {
				t.Fatal(err)
			}
			if got := started(planned.Steps); !slices.Equal(got, tt.started) {
				t.Errorf("started %v, want %v", got, tt.started)
			}
			removes := stepsOf(KindRemoveNode, planned.Steps)
			if tt.removed && (len(removes) != 1 || removes[0].Args.(NodeArgs).Node != "b-103") {
				t.Errorf("remove steps %+v, want the old host b-103", removes)
			}
			if len(planned.Doc.Moves) != tt.moves {
				t.Errorf("%d moves, want %d", len(planned.Doc.Moves), tt.moves)
			}
			if (tt.warn == "") != (len(planned.Doc.Warnings) == 0) || (tt.warn != "" && !strings.Contains(planned.Doc.Warnings[0], tt.warn)) {
				t.Errorf("warnings %v, want %q", planned.Doc.Warnings, tt.warn)
			}
			if tt.params.Mode == "" && slices.IndexFunc(planned.Steps, func(s ops.StepSpec) bool { return s.Kind == KindRemoveNode }) >
				slices.IndexFunc(planned.Steps, func(s ops.StepSpec) bool { return s.Kind == KindProvision }) {
				t.Error("same-id must release the old host before the new one takes the id")
			}
			if i := slices.IndexFunc(planned.Steps, func(s ops.StepSpec) bool { return s.Kind == KindCheckHosts }); i != 1 {
				t.Errorf("the chosen host is checked in step %d, want step 1, before anything changes", i)
			}
		})
	}
}

func TestReplaceRefusals(t *testing.T) {
	t.Parallel()
	unmanaged := runningSnapshot()
	unmanaged.Brokers = append(unmanaged.Brokers, kafka.Broker{ID: 120, Fenced: true})
	tests := []struct {
		name    string
		cluster *store.Cluster
		snap    *kafka.Snapshot
		fleet   fleet
		params  replaceParams
		want    string
	}{
		{"no broker id", nil, nil, runningFleet(), replaceParams{}, "brokerId is required"},
		{"an unknown mode", nil, nil, runningFleet(), replaceParams{BrokerID: id32(103), Mode: "swap"}, "mode must be same-id or new-id"},
		{"an unknown broker", nil, nil, runningFleet(), replaceParams{BrokerID: id32(140)}, "broker 140 is not registered"},
		{"a serving broker", nil, nil, runningFleet(), replaceParams{BrokerID: id32(101)}, "broker 101 is serving"},
		{"new id without a host", nil, nil, runningFleet(), replaceParams{BrokerID: id32(103), Mode: "new-id"}, "node is required for new-id"},
		{"new id on the old host", nil, nil, runningFleet(), replaceParams{BrokerID: id32(103), Mode: "new-id", Node: "b-103"},
			"host kafka-3 ran broker 103; choose another host"},
		{"a broker without a managed host", nil, unmanaged, runningFleet(), replaceParams{BrokerID: id32(120)},
			"broker 120 has no host managed by Brokerium"},
		{"its host is down", nil, nil, runningFleet().offline("b-103"), replaceParams{BrokerID: id32(103)}, "host kafka-3 is not connected"},
		{"a host with a Kafka role", nil, nil, runningFleet(), replaceParams{BrokerID: id32(103), Node: "b-102"},
			"host kafka-2 already runs Kafka as broker 102 in this cluster"},
		{"a host on Docker", dockerCluster(), nil, runningFleet(), replaceParams{BrokerID: id32(103), Node: "f-1"}, "give no node"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, snap := tt.cluster, tt.snap
			if c == nil {
				c = agentCluster()
			}
			if snap == nil {
				snap = runningSnapshot()
			}
			_, err := replacePlan(plannerFor(c, snap), tt.fleet, tt.params)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error %v, want %q", err, tt.want)
			}
		})
	}
}

func TestReplaceDocker(t *testing.T) {
	t.Parallel()
	f := fleetOf(host("d3", "lab-b103", runs("c1", RoleBroker, 103)))
	planned, err := replacePlan(plannerFor(dockerCluster(), runningSnapshot()), f, replaceParams{BrokerID: id32(103)})
	if err != nil {
		t.Fatal(err)
	}
	if got := started(planned.Steps); !slices.Equal(got, []string{"lab-b103/broker/103/r3"}) {
		t.Errorf("started %v", got)
	}
	if remove := stepsOf(KindRemoveNode, planned.Steps)[0].Args.(NodeArgs); remove.Name != "lab-b103" || remove.Node != "" {
		t.Errorf("remove %+v", remove)
	}
}

// TestHostsThatJoinedTheCluster chooses hosts that joined the cluster with its join token and run
// no Kafka node yet: they are free for the cluster, and only nodes that run Kafka hold addresses.
func TestHostsThatJoinedTheCluster(t *testing.T) {
	t.Parallel()
	p := plannerFor(agentCluster(), runningSnapshot())
	f := runningFleet(host("j-1", "joined-1", joined("c1")), host("j-2", "joined-2", joined("c1"), advertises("joined-1.example.com")))
	tests := []struct {
		name    string
		plan    func() (*ops.Planned, error)
		started string
	}{
		{"add-broker", func() (*ops.Planned, error) {
			return addBrokerPlan(p, f, addBrokerParams{Nodes: []brokerChoice{{Node: "j-1"}}})
		}, "j-1/broker/104/"},
		{"add-controller", func() (*ops.Planned, error) {
			return addControllerPlan(p, f, addControllerParams{Node: "j-1"})
		}, "j-1/controller/4/"},
		{"replace-broker", func() (*ops.Planned, error) {
			return replacePlan(p, f, replaceParams{BrokerID: id32(103), Node: "j-1"})
		}, "j-1/broker/103/r3"},
	}
	for _, tt := range tests {
		planned, err := tt.plan()
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got := started(planned.Steps); !slices.Equal(got, []string{tt.started}) {
			t.Errorf("%s: started %v, want %s", tt.name, got, tt.started)
		}
		check := planned.Steps[1]
		if check.Kind != KindCheckHosts || check.Args.(HostsArgs).Hosts[0].Node != "j-1" {
			t.Errorf("%s: second step %s %+v, want the check of host j-1", tt.name, check.Kind, check.Args)
		}
	}
	if _, err := addBrokerPlan(p, f, addBrokerParams{Nodes: []brokerChoice{{Node: "j-1"}, {Node: "j-2"}}}); err == nil ||
		!strings.Contains(err.Error(), "hosts joined-1 and joined-2 both advertise joined-1.example.com") {
		t.Errorf("two chosen hosts with one address: %v", err)
	}
}

// TestResumeUnfinishedNodes plans again on hosts that kept their role from an add-broker or
// add-controller that failed before the node joined: the plan continues that node.
func TestResumeUnfinishedNodes(t *testing.T) {
	t.Parallel()
	p := plannerFor(agentCluster(), runningSnapshot())
	f := runningFleet(host("s-1", "stuck-1", runs("c1", RoleBroker, 104)), host("s-2", "stuck-2", runs("c1", RoleController, 4)))
	tests := []struct {
		name    string
		plan    func() (*ops.Planned, error)
		started string
		want    string
	}{
		{"the broker again", func() (*ops.Planned, error) {
			return addBrokerPlan(p, f, addBrokerParams{Nodes: []brokerChoice{{Node: "s-1"}}})
		}, "s-1/broker/104/", ""},
		{"the broker with its id", func() (*ops.Planned, error) {
			return addBrokerPlan(p, f, addBrokerParams{Nodes: []brokerChoice{{Node: "s-1", NodeID: id32(104)}}})
		}, "s-1/broker/104/", ""},
		{"the broker with another id", func() (*ops.Planned, error) {
			return addBrokerPlan(p, f, addBrokerParams{Nodes: []brokerChoice{{Node: "s-1", NodeID: id32(110)}}})
		}, "", "host stuck-1 already runs Kafka as broker 104 in this cluster"},
		{"the broker as a controller", func() (*ops.Planned, error) {
			return addControllerPlan(p, f, addControllerParams{Node: "s-1"})
		}, "", "host stuck-1 already runs Kafka as broker 104 in this cluster"},
		{"the controller again", func() (*ops.Planned, error) {
			return addControllerPlan(p, f, addControllerParams{Node: "s-2"})
		}, "s-2/controller/4/", ""},
		{"a registered broker", func() (*ops.Planned, error) {
			return addBrokerPlan(p, f, addBrokerParams{Nodes: []brokerChoice{{Node: "b-102"}}})
		}, "", "host kafka-2 already runs Kafka as broker 102 in this cluster"},
		{"a voter", func() (*ops.Planned, error) {
			return addControllerPlan(p, f, addControllerParams{Node: "c-1"})
		}, "", "host ctl-1 already runs Kafka as controller 1 in this cluster"},
	}
	for _, tt := range tests {
		planned, err := tt.plan()
		if tt.want != "" {
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
			}
			continue
		}
		if err != nil {
			t.Errorf("%s: %v", tt.name, err)
			continue
		}
		if got := started(planned.Steps); !slices.Equal(got, []string{tt.started}) {
			t.Errorf("%s: started %v, want %s", tt.name, got, tt.started)
		}
		if len(planned.Doc.Warnings) == 0 || !strings.Contains(planned.Doc.Warnings[0], "from an operation that did not finish") {
			t.Errorf("%s: warnings %v", tt.name, planned.Doc.Warnings)
		}
	}
}

func TestRestartAndUpgradeAddressHostsByID(t *testing.T) {
	t.Parallel()
	mn := runningFleet().managed("c1")
	p := plannerFor(agentCluster(), runningSnapshot())
	restart, err := restartPlan(p, mn, restartParams{})
	if err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, s := range stepsOf(KindRestart, restart.Steps) {
		a := s.Args.(NodeArgs)
		order = append(order, a.Node)
		if a.Name == "" || a.NodeID == 0 {
			t.Errorf("%s: %+v", s.Name, a)
		}
	}
	if want := []string{"c-2", "c-3", "c-1", "b-101", "b-102", "b-103"}; !slices.Equal(order, want) {
		t.Errorf("restart order %v, want %v (controllers first, quorum leader last)", order, want)
	}
	if last := restart.Steps[len(restart.Steps)-1]; last.Kind != KindSyncBootstrap {
		t.Errorf("last step %s, want the bootstrap servers updated", last.Kind)
	}
	upgrade, err := upgradePlan(p, mn, upgradeParams{Version: "4.4.0"})
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range append(stepsOf(KindInstall, upgrade.Steps), stepsOf(KindUpgrade, upgrade.Steps)...) {
		if s.Args.(NodeArgs).Node == "" {
			t.Errorf("%s addresses its host by name", s.Name)
		}
	}

	adopted := runningFleet()
	adopted.nodes[3].Status = json.RawMessage(`{"adopted":true}`)
	plan, err := restartPlan(p, adopted.managed("c1"), restartParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(stepsOf(KindSyncBootstrap, plan.Steps)) != 0 {
		t.Error("a cluster with adopted nodes keeps its bootstrap servers")
	}
	docker, err := restartPlan(plannerFor(dockerCluster(), runningSnapshot()), mn, restartParams{})
	if err != nil {
		t.Fatal(err)
	}
	if a := stepsOf(KindRestart, docker.Steps)[0].Args.(NodeArgs); a.Node != "" || len(stepsOf(KindSyncBootstrap, docker.Steps)) != 0 {
		t.Errorf("Docker restarts address containers by name and keep the addresses: %+v", a)
	}
}

func TestMigrateQuorumUsesAdvertisedHosts(t *testing.T) {
	t.Parallel()
	p := plannerFor(agentCluster(), runningSnapshot())
	planned, err := migratePlan(p, runningFleet().managed("c1"))
	if err != nil {
		t.Fatal(err)
	}
	a := stepsOf(KindRestart, planned.Steps)[0].Args.(NodeArgs)
	if a.Quorum != "ctl-1.example.com:9093,ctl-2.example.com:9093,ctl-3.example.com:9093" || a.Node == "" {
		t.Errorf("restart %+v", a)
	}
	p.View.Health.KRaftVersion = 1
	if _, err := migratePlan(p, runningFleet().managed("c1")); err == nil {
		t.Error("a dynamic quorum was migrated again")
	}
}

func TestClaimable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		node *store.Node
		want string
	}{
		{"free host", host("n", "n"), ""},
		{"joined the cluster", host("n", "n", joined("c1")), ""},
		{"runs this node", host("n", "n", runs("c1", RoleBroker, 101)), ""},
		{"runs another node of the cluster", host("n", "n", runs("c1", RoleBroker, 102)), "already runs Kafka as broker 102 in this cluster"},
		{"runs another role", host("n", "n", runs("c1", RoleController, 101)), "already runs Kafka as controller 101 in this cluster"},
		{"runs a node of another cluster", host("n", "n", runs("c2", RoleBroker, 101)), "already runs Kafka as broker 101 of another cluster"},
		{"joined another cluster", host("n", "n", joined("c2")), "belongs to another cluster"},
		{"adopted", host("n", "n", status(`{"adopted":true}`)), "runs Kafka with its own configuration"},
	}
	for _, tt := range tests {
		err := claimable(tt.node, "c1", RoleBroker, 101)
		if (tt.want == "") != (err == nil) || (err != nil && !strings.Contains(err.Error(), tt.want)) {
			t.Errorf("%s: %v, want %q", tt.name, err, tt.want)
		}
	}
}

func TestCheckStorage(t *testing.T) {
	t.Parallel()
	tests := []struct {
		status string
		ok     bool
	}{
		{`{}`, true},
		{`{"meta":{"formatted":false}}`, true},
		{`{"meta":{"clusterId":"` + testKafkaID + `","nodeId":101,"formatted":true}}`, true},
		{`{"meta":{"clusterId":"` + testKafkaID + `","nodeId":104,"formatted":true}}`, false},
		{`{"meta":{"clusterId":"other","nodeId":101,"formatted":true}}`, false},
	}
	for _, tt := range tests {
		if err := checkStorage(host("n", "n", status(tt.status)), testKafkaID, 101); (err == nil) != tt.ok {
			t.Errorf("status %s: %v, want ok=%v", tt.status, err, tt.ok)
		}
	}
}

func TestIDs(t *testing.T) {
	t.Parallel()
	used := ids{1: true, 101: true}
	if got := []int32{used.next(1), used.next(1), used.next(101)}; !slices.Equal(got, []int32{2, 3, 102}) {
		t.Errorf("next ids %v", got)
	}
	if err := used.claim(0); err != nil {
		t.Error(err)
	}
	for _, id := range []int32{0, 2, -1} {
		if used.claim(id) == nil {
			t.Errorf("claim(%d) succeeded", id)
		}
	}
}

func TestBrokerLabel(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id   int32
		rack string
		want string
	}{
		{102, "", "102"},
		{105, "r3", "105 (r3)"},
	}
	for _, tt := range tests {
		if got := brokerLabel(tt.id, tt.rack); got != tt.want {
			t.Errorf("brokerLabel(%d, %q) = %q, want %q", tt.id, tt.rack, got, tt.want)
		}
	}
}
