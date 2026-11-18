// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/ops"
	"github.com/brokerium/brokerium/internal/store"
)

// combinedSnapshot is a cluster with controllers 1 and 2, combined node 3 and brokers 101 and
// 102 in racks r3, r1 and r2; controller 1 leads the quorum.
func combinedSnapshot() *kafka.Snapshot {
	return &kafka.Snapshot{
		Brokers: []kafka.Broker{{ID: 3, Rack: "r3", Alive: true}, {ID: 101, Rack: "r1", Alive: true}, {ID: 102, Rack: "r2", Alive: true}},
		Quorum: &kafka.Quorum{LeaderID: 1,
			Voters:    []kafka.QuorumReplica{{NodeID: 1, DirectoryID: "d1"}, {NodeID: 2, DirectoryID: "d2"}, {NodeID: 3, DirectoryID: "d3"}},
			Observers: []kafka.QuorumReplica{{NodeID: 101}, {NodeID: 102}}},
		Topics: []kafka.Topic{{Name: "t", Partitions: []kafka.Partition{{ID: 0, Leader: 3, Replicas: []int32{3, 101, 102}}}}},
	}
}

// combinedFleet holds the managed nodes of combinedSnapshot and the free host free-1.
func combinedFleet(extra ...*store.Node) fleet {
	return fleetOf(append([]*store.Node{
		host("c-1", "ctl-1", runs("c1", RoleController, 1)),
		host("c-2", "ctl-2", runs("c1", RoleController, 2)),
		host("x-3", "both-3", runs("c1", RoleCombined, 3)),
		host("b-101", "kafka-1", runs("c1", RoleBroker, 101)),
		host("b-102", "kafka-2", runs("c1", RoleBroker, 102)),
		host("f-1", "free-1"),
	}, extra...)...)
}

const combinedWarning = "combined nodes run broker and controller in one process"

func waitedBrokers(steps []ops.StepSpec) []int32 {
	var out []int32
	for _, s := range stepsOf(KindWaitBroker, steps) {
		out = append(out, s.Args.(NodeArgs).NodeID)
	}
	return out
}

func hasWarning(doc ops.PlanDoc, text string) bool {
	return slices.ContainsFunc(doc.Warnings, func(w string) bool { return strings.Contains(w, text) })
}

func TestBootstrapCombinedAgentHosts(t *testing.T) {
	t.Parallel()
	f := fleetOf(host("n1", "kafka-1"), host("n2", "kafka-2", rack("r1")), host("n3", "kafka-3"))
	both := func(node string) hostChoice { return hostChoice{Node: node, Role: RoleCombined} }
	tests := []struct {
		name        string
		params      bootstrapParams
		started     []string
		quorum      string
		servers     []string
		controllers []string
		waited      []int32
		rf          int
	}{
		{"one combined node", bootstrapParams{Nodes: []hostChoice{both("n1")}},
			[]string{"n1/combined/1/"}, "kafka-1.example.com:9093",
			[]string{"kafka-1.example.com:9092"}, []string{"kafka-1.example.com:9093"}, []int32{1}, 1},
		{"a combined node with a rack and a broker", bootstrapParams{Nodes: []hostChoice{{Node: "n1", Role: RoleCombined, Rack: "r0"}, {Node: "n2", Role: RoleBroker}}},
			[]string{"n1/combined/1/r0", "n2/broker/101/r1"}, "kafka-1.example.com:9093",
			[]string{"kafka-1.example.com:9092", "kafka-2.example.com:9092"}, []string{"kafka-1.example.com:9093"}, []int32{1, 101}, 2},
		{"voters start before a broker listed first", bootstrapParams{Nodes: []hostChoice{{Node: "n2", Role: RoleBroker}, both("n1"), {Node: "n3", Role: RoleController}}},
			[]string{"n1/combined/1/", "n3/controller/2/", "n2/broker/101/r1"}, "kafka-1.example.com:9093,kafka-3.example.com:9093",
			[]string{"kafka-2.example.com:9092", "kafka-1.example.com:9092"}, []string{"kafka-1.example.com:9093", "kafka-3.example.com:9093"},
			[]int32{101, 1}, 2},
		{"explicit ids", bootstrapParams{Nodes: []hostChoice{{Node: "n1", Role: RoleCombined, NodeID: id32(7)}, {Node: "n3", Role: RoleController},
			{Node: "n2", Role: RoleBroker, NodeID: id32(1)}}},
			[]string{"n1/combined/7/", "n3/controller/2/", "n2/broker/1/r1"}, "kafka-1.example.com:9093,kafka-3.example.com:9093",
			[]string{"kafka-1.example.com:9092", "kafka-2.example.com:9092"}, []string{"kafka-1.example.com:9093", "kafka-3.example.com:9093"},
			[]int32{7, 1}, 2},
		{"static quorum", bootstrapParams{Quorum: "static", Nodes: []hostChoice{both("n1"), {Node: "n3", Role: RoleController}}},
			[]string{"n1/combined/1/", "n3/controller/2/"}, "1@kafka-1.example.com:9093,2@kafka-3.example.com:9093",
			[]string{"kafka-1.example.com:9092"}, []string{"kafka-1.example.com:9093", "kafka-3.example.com:9093"}, []int32{1}, 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			planned, err := (&Manager{}).bootstrapPlan(plannerFor(&store.Cluster{ID: "c1", Name: "prod", Provider: store.ProviderAgent}, nil), f, tt.params, nil)
			if err != nil {
				t.Fatal(err)
			}
			if got := started(planned.Steps); !slices.Equal(got, tt.started) {
				t.Errorf("started %v, want %v", got, tt.started)
			}
			save := stepsOf(KindSetSettings, planned.Steps)[0].Args.(NodeArgs)
			if !slices.Equal(save.BootstrapServers, tt.servers) || !slices.Equal(save.Settings.ControllerBootstrapServers, tt.controllers) {
				t.Errorf("bootstrap servers %v and controllers %v, want %v and %v", save.BootstrapServers,
					save.Settings.ControllerBootstrapServers, tt.servers, tt.controllers)
			}
			if save.Settings.ReplicationFactor != tt.rf || save.Settings.MinISR != min(2, tt.rf) {
				t.Errorf("replication factor %d, min ISR %d, want %d", save.Settings.ReplicationFactor, save.Settings.MinISR, tt.rf)
			}
			voters := 0
			for _, s := range stepsOf(KindFormat, planned.Steps) {
				a := s.Args.(NodeArgs)
				switch {
				case a.Quorum != tt.quorum:
					t.Errorf("%s: quorum %q, want %q", s.Name, a.Quorum, tt.quorum)
				case runsController(a.Role):
					voters++
					want := "initial"
					if tt.params.Quorum == "static" {
						want = "static"
					}
					if a.Mode != want || strings.Count(a.InitialControllers, "@") != len(tt.controllers) {
						t.Errorf("%s: mode %q, initial controllers %q", s.Name, a.Mode, a.InitialControllers)
					}
				case a.Mode != "join":
					t.Errorf("%s: a dedicated broker formats with mode %q, want join", s.Name, a.Mode)
				}
			}
			if got := waitedBrokers(planned.Steps); !slices.Equal(got, tt.waited) {
				t.Errorf("waits for brokers %v, want %v", got, tt.waited)
			}
			if wait := stepsOf(KindWaitQuorum, planned.Steps)[0].Args.(NodeArgs); wait.ExpectVoters != voters || voters != len(tt.controllers) {
				t.Errorf("waits for %d voters, %d voters formatted, want %d", wait.ExpectVoters, voters, len(tt.controllers))
			}
			if !hasWarning(planned.Doc, combinedWarning) {
				t.Errorf("warnings %v do not explain combined nodes", planned.Doc.Warnings)
			}
			if !strings.Contains(planned.Doc.Summary, " combined node(s) ") || !strings.Contains(planned.Doc.Summary, "kafka-1 (combined ") {
				t.Errorf("summary %q", planned.Doc.Summary)
			}
		})
	}
}

func TestBootstrapCombinedWarnings(t *testing.T) {
	t.Parallel()
	f := fleetOf(host("n1", "kafka-1"), host("n2", "kafka-2", rack("r1")), host("n3", "kafka-3"))
	p := plannerFor(&store.Cluster{ID: "c1", Name: "prod", Provider: store.ProviderAgent}, nil)
	planned, err := (&Manager{}).bootstrapPlan(p, f, bootstrapParams{Nodes: []hostChoice{
		{Node: "n1", Role: RoleCombined}, {Node: "n3", Role: RoleController}, {Node: "n2", Role: RoleBroker}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{combinedWarning, "broker(s) 1 have no rack", "2 voters tolerate as many failures as 1"} {
		if !hasWarning(planned.Doc, want) {
			t.Errorf("warnings %v, want %q", planned.Doc.Warnings, want)
		}
	}
	plain, err := (&Manager{}).bootstrapPlan(p, f, bootstrapParams{Nodes: []hostChoice{
		{Node: "n1", Role: RoleController}, {Node: "n2", Role: RoleBroker}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if hasWarning(plain.Doc, combinedWarning) || strings.Contains(plain.Doc.Summary, "combined") {
		t.Errorf("a cluster without combined nodes: %+v", plain.Doc)
	}
}

func TestBootstrapCombinedDocker(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name                 string
		params               bootstrapParams
		started              []string
		servers, controllers []string
		quorum               string
	}{
		{"combined nodes only", bootstrapParams{Combined: 3},
			[]string{"lab-cb1/combined/1/rack-a", "lab-cb2/combined/2/rack-b", "lab-cb3/combined/3/rack-c"},
			[]string{"localhost:22001", "localhost:22002", "localhost:22003"}, []string{"localhost:22901", "localhost:22902", "localhost:22903"},
			"lab-cb1:9093,lab-cb2:9093,lab-cb3:9093"},
		{"controllers, combined nodes and a broker", bootstrapParams{Controllers: 1, Combined: 2, Brokers: 1, Racks: []string{"r1", "r2"}},
			[]string{"lab-c1/controller/1/", "lab-cb2/combined/2/r1", "lab-cb3/combined/3/r2", "lab-b101/broker/101/r1"},
			[]string{"localhost:22002", "localhost:22003", "localhost:22101"}, []string{"localhost:22001", "localhost:22902", "localhost:22903"},
			"lab-c1:9093,lab-cb2:9093,lab-cb3:9093"},
		{"no counts", bootstrapParams{},
			[]string{"lab-c1/controller/1/", "lab-c2/controller/2/", "lab-c3/controller/3/",
				"lab-b101/broker/101/rack-a", "lab-b102/broker/102/rack-b", "lab-b103/broker/103/rack-c"},
			[]string{"localhost:22101", "localhost:22102", "localhost:22103"}, []string{"localhost:22001", "localhost:22002", "localhost:22003"},
			"lab-c1:9093,lab-c2:9093,lab-c3:9093"},
		{"counts of 0 without combined nodes", bootstrapParams{Controllers: 1, Brokers: 0},
			[]string{"lab-c1/controller/1/", "lab-b101/broker/101/rack-a", "lab-b102/broker/102/rack-b", "lab-b103/broker/103/rack-c"},
			[]string{"localhost:22101", "localhost:22102", "localhost:22103"}, []string{"localhost:22001"}, "lab-c1:9093"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := plannerFor(&store.Cluster{ID: "c1", Name: "lab", Provider: store.ProviderDocker}, nil)
			planned, err := (&Manager{}).bootstrapPlan(p, fleet{}, tt.params, map[int]bool{21000: true})
			if err != nil {
				t.Fatal(err)
			}
			if got := started(planned.Steps); !slices.Equal(got, tt.started) {
				t.Errorf("started %v, want %v", got, tt.started)
			}
			save := planned.Steps[0].Args.(NodeArgs)
			if !slices.Equal(save.BootstrapServers, tt.servers) || !slices.Equal(save.Settings.ControllerBootstrapServers, tt.controllers) {
				t.Errorf("bootstrap servers %v and controllers %v, want %v and %v", save.BootstrapServers,
					save.Settings.ControllerBootstrapServers, tt.servers, tt.controllers)
			}
			if quorum := stepsOf(KindConfigure, planned.Steps)[0].Args.(NodeArgs).Quorum; quorum != tt.quorum {
				t.Errorf("quorum %q, want %q", quorum, tt.quorum)
			}
			if hasWarning(planned.Doc, combinedWarning) != (tt.params.Combined > 0) {
				t.Errorf("warnings %v", planned.Doc.Warnings)
			}
		})
	}
}

func TestBootstrapCombinedDockerRefusals(t *testing.T) {
	t.Parallel()
	tests := []struct {
		params bootstrapParams
		want   string
	}{
		{bootstrapParams{Combined: 6}, "at most 5 together, not 6"},
		{bootstrapParams{Controllers: 3, Combined: 3}, "at most 5 together, not 6"},
		{bootstrapParams{Controllers: 6}, "at most 5 together, not 6"},
		{bootstrapParams{Combined: -1}, "cannot be negative"},
		{bootstrapParams{Combined: 1, Controllers: -1}, "cannot be negative"},
		{bootstrapParams{Combined: 1, Brokers: 10}, "brokers must be at most 9"},
	}
	for _, tt := range tests {
		p := plannerFor(&store.Cluster{ID: "c1", Name: "lab", Provider: store.ProviderDocker}, nil)
		if _, err := (&Manager{}).bootstrapPlan(p, fleet{}, tt.params, nil); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%+v: error %v, want %q", tt.params, err, tt.want)
		}
	}
}

func TestCombinedRestartOrder(t *testing.T) {
	t.Parallel()
	mn := combinedFleet().managed("c1")
	tests := []struct {
		leader int32
		want   []string
	}{
		{1, []string{"c-2", "x-3", "c-1", "b-101", "b-102"}},
		{3, []string{"c-1", "c-2", "x-3", "b-101", "b-102"}},
		{2, []string{"c-1", "x-3", "c-2", "b-101", "b-102"}},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("leader %d", tt.leader), func(t *testing.T) {
			t.Parallel()
			p := plannerFor(agentCluster(), combinedSnapshot())
			p.View.Health.QuorumLeader = tt.leader
			restart, err := restartPlan(p, mn, restartParams{})
			if err != nil {
				t.Fatal(err)
			}
			var order []string
			for i, s := range restart.Steps {
				if s.Kind != KindRestart {
					continue
				}
				a := s.Args.(NodeArgs)
				order = append(order, a.Node)
				if next := restart.Steps[i+1]; next.Kind != ops.KindHealthGate {
					t.Errorf("%s is followed by %s, want a health gate", s.Name, next.Kind)
				}
				if a.Node == "x-3" && (a.Role != RoleCombined || a.NodeID != 3 || a.Rack != "r3") {
					t.Errorf("restart of the combined node %+v, want role combined, id 3 and its broker rack", a)
				}
			}
			if !slices.Equal(order, tt.want) {
				t.Errorf("restart order %v, want %v (voters first, the leader last, then brokers)", order, tt.want)
			}

			upgrade, err := upgradePlan(p, mn, upgradeParams{Version: "4.4.0"})
			if err != nil {
				t.Fatal(err)
			}
			var installs, upgrades []string
			for _, s := range stepsOf(KindInstall, upgrade.Steps) {
				installs = append(installs, s.Args.(NodeArgs).Node)
			}
			for _, s := range stepsOf(KindUpgrade, upgrade.Steps) {
				upgrades = append(upgrades, s.Args.(NodeArgs).Node)
			}
			if !slices.Equal(upgrades, tt.want) || !slices.Equal(installs, tt.want) {
				t.Errorf("upgrade installs on %v and upgrades %v, want %v", installs, upgrades, tt.want)
			}
		})
	}

	only, err := restartPlan(plannerFor(agentCluster(), combinedSnapshot()), mn, restartParams{NodeIDs: []int32{3}})
	if err != nil {
		t.Fatal(err)
	}
	if restarts := stepsOf(KindRestart, only.Steps); len(restarts) != 1 || restarts[0].Args.(NodeArgs).Node != "x-3" {
		t.Errorf("restarting node 3 restarts %+v", restarts)
	}
}

func TestMigrateQuorumWithCombinedNodes(t *testing.T) {
	t.Parallel()
	p := plannerFor(agentCluster(), combinedSnapshot())
	planned, err := migratePlan(p, combinedFleet().managed("c1"))
	if err != nil {
		t.Fatal(err)
	}
	restarts := stepsOf(KindRestart, planned.Steps)
	var order []string
	for _, s := range restarts {
		a := s.Args.(NodeArgs)
		order = append(order, a.Node)
		if a.Quorum != "ctl-1.example.com:9093,ctl-2.example.com:9093,both-3.example.com:9093" {
			t.Errorf("%s: quorum %q, want the controllers and the combined node", s.Name, a.Quorum)
		}
	}
	if want := []string{"c-2", "x-3", "c-1", "b-101", "b-102"}; !slices.Equal(order, want) {
		t.Errorf("restart order %v, want %v", order, want)
	}
	if wait := stepsOf(KindWaitQuorum, planned.Steps)[0].Args.(NodeArgs); wait.ExpectVoters != 3 {
		t.Errorf("waits for %d voters, want 3", wait.ExpectVoters)
	}
	if !strings.Contains(planned.Doc.Summary, "3 voter(s)") || !strings.Contains(planned.Doc.Summary, "restart 5 node(s)") {
		t.Errorf("summary %q", planned.Doc.Summary)
	}
}

func TestCombinedRefusals(t *testing.T) {
	t.Parallel()
	failed := combinedSnapshot()
	failed.Brokers[0].Alive = false
	unmanaged := fleetOf(host("c-1", "ctl-1", runs("c1", RoleController, 1)), host("b-101", "kafka-1", runs("c1", RoleBroker, 101)))
	docker := fleetOf(host("d-3", "lab-cb3", runs("c1", RoleCombined, 3)), host("d-101", "lab-b101", runs("c1", RoleBroker, 101)))
	tests := []struct {
		name    string
		cluster *store.Cluster
		snap    *kafka.Snapshot
		fleet   fleet
		typ     string
		id      int32
		want    string
	}{
		{"replace a serving combined node", agentCluster(), nil, combinedFleet(), "replace-broker", 3,
			"node 3 (both-3) is a combined node, broker and controller in one process; Brokerium does not replace combined nodes"},
		{"replace a failed combined node", agentCluster(), failed, combinedFleet(), "replace-broker", 3, "does not replace combined nodes"},
		{"replace a combined container", dockerCluster(), failed, docker, "replace-broker", 3, "node 3 (lab-cb3) is a combined node"},
		{"drain a combined node", agentCluster(), nil, combinedFleet(), "drain-broker", 3, "does not drain or decommission combined nodes"},
		{"remove a combined node from the quorum", agentCluster(), nil, combinedFleet(), "remove-controller", 3,
			"does not remove combined nodes from the quorum"},
		{"a combined node Brokerium does not run", agentCluster(), nil, unmanaged, "drain-broker", 3, "node 3 is a combined node"},
		{"drain a broker", agentCluster(), nil, combinedFleet(), "drain-broker", 101, ""},
		{"remove a controller", agentCluster(), nil, combinedFleet(), "remove-controller", 1, ""},
		{"cordon a combined node", agentCluster(), nil, combinedFleet(), "cordon-broker", 3, ""},
		{"restart a combined node", agentCluster(), nil, combinedFleet(), "rolling-restart", 3, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			snap := tt.snap
			if snap == nil {
				snap = combinedSnapshot()
			}
			p := plannerFor(tt.cluster, snap)
			err := refuseCombined(p, tt.fleet.managed("c1"), tt.typ, tt.id)
			if tt.want == "" {
				if err != nil {
					t.Fatalf("refused: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.want) || !strings.Contains(err.Error(), combinedDocs) {
				t.Fatalf("error %v, want %q and a link to the docs", err, tt.want)
			}
			if tt.typ != "replace-broker" {
				return
			}
			if _, err := replacePlan(p, tt.fleet, replaceParams{BrokerID: id32(tt.id)}); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Errorf("replace-broker: %v, want %q", err, tt.want)
			}
		})
	}

	failedBroker := combinedSnapshot()
	failedBroker.Brokers[2].Alive = false
	if _, err := replacePlan(plannerFor(agentCluster(), failedBroker), combinedFleet(), replaceParams{BrokerID: id32(102)}); err != nil {
		t.Errorf("replacing dedicated broker 102 next to a combined node: %v", err)
	}
	if err := (&Manager{}).CheckNode(t.Context(), plannerFor(&store.Cluster{ID: "c1", Provider: store.ProviderExternal}, combinedSnapshot()),
		"drain-broker", 3); err != nil {
		t.Errorf("an external cluster: %v", err)
	}
}

func TestAddNodesNextToCombinedNodes(t *testing.T) {
	t.Parallel()
	p := plannerFor(agentCluster(), combinedSnapshot())
	broker, err := addBrokerPlan(p, combinedFleet(), addBrokerParams{Nodes: []brokerChoice{{Node: "f-1"}}})
	if err != nil {
		t.Fatal(err)
	}
	if got := started(broker.Steps); !slices.Equal(got, []string{"f-1/broker/103/"}) {
		t.Errorf("add-broker starts %v, want a dedicated broker 103", got)
	}
	controller, err := addControllerPlan(p, combinedFleet(), addControllerParams{Node: "f-1"})
	if err != nil {
		t.Fatal(err)
	}
	if got := started(controller.Steps); !slices.Equal(got, []string{"f-1/controller/4/"}) {
		t.Errorf("add-controller starts %v, want a dedicated controller 4", got)
	}
	if _, err := addBrokerPlan(p, combinedFleet(), addBrokerParams{Nodes: []brokerChoice{{Node: "x-3"}}}); err == nil ||
		!strings.Contains(err.Error(), "host both-3 already runs Kafka as combined 3 in this cluster") {
		t.Errorf("adding the combined host as a broker: %v", err)
	}

	dockerFleet := fleetOf(host("d-1", "lab-cb1", runs("c1", RoleCombined, 1)), host("d-101", "lab-b101", runs("c1", RoleBroker, 101)))
	snap := &kafka.Snapshot{Brokers: []kafka.Broker{{ID: 1, Alive: true}, {ID: 101, Alive: true}},
		Quorum: &kafka.Quorum{LeaderID: 1, Voters: []kafka.QuorumReplica{{NodeID: 1}}}}
	dp := plannerFor(dockerCluster(), snap)
	planned, err := addControllerPlan(dp, dockerFleet, addControllerParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got := started(planned.Steps); !slices.Equal(got, []string{"lab-c2/controller/2/"}) {
		t.Errorf("Docker add-controller starts %v", got)
	}
	planned, err = addBrokerPlan(dp, dockerFleet, addBrokerParams{})
	if err != nil {
		t.Fatal(err)
	}
	if got := started(planned.Steps); len(got) != 1 || !strings.HasPrefix(got[0], "lab-b102/broker/102/") {
		t.Errorf("Docker add-broker starts %v", got)
	}
}

func TestDestroyCombinedNodes(t *testing.T) {
	t.Parallel()
	mn := fleetOf(host("d-1", "lab-c1", runs("c1", RoleController, 1)), host("d-2", "lab-cb2", runs("c1", RoleCombined, 2)),
		host("d-101", "lab-b101", runs("c1", RoleBroker, 101))).managed("c1")
	planned, err := destroyPlan(plannerFor(dockerCluster(), nil), mn)
	if err != nil {
		t.Fatal(err)
	}
	var deleted []string
	for _, s := range stepsOf(KindRemoveNode, planned.Steps) {
		a := s.Args.(NodeArgs)
		deleted = append(deleted, fmt.Sprintf("%s/%s/%d", a.Name, a.Role, a.NodeID))
	}
	if want := []string{"lab-b101/broker/101", "lab-c1/controller/1", "lab-cb2/combined/2"}; !slices.Equal(deleted, want) {
		t.Errorf("deletes %v, want %v (every node once, brokers first)", deleted, want)
	}
	if !strings.Contains(planned.Doc.Summary, "Delete 3 node(s)") {
		t.Errorf("summary %q", planned.Doc.Summary)
	}
}
