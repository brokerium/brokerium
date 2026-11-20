// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"

	"github.com/brokerium/brokerium/internal/agentcfg"
	"github.com/brokerium/brokerium/internal/store"
)

// Roles of managed nodes. Combined nodes run broker and controller in one process.
const (
	RoleBroker     = "broker"
	RoleController = "controller"
	RoleCombined   = "combined"
)

// runsController reports whether nodes of the role are voters of the quorum: controllers and
// combined nodes.
func runsController(role string) bool { return role == RoleController || role == RoleCombined }

// runsBroker reports whether nodes of the role host partitions: brokers and combined nodes.
func runsBroker(role string) bool { return role == RoleBroker || role == RoleCombined }

// DefaultKafkaVersion is the Kafka release of new clusters when the plan names none.
const DefaultKafkaVersion = "4.4.0"

// unrecordedKafkaVersion is the Kafka version of clusters whose settings record none, for
// example clusters with adopted nodes. It stays what Brokerium assumed for them before, so that
// new nodes of such a cluster get the release its nodes run.
const unrecordedKafkaVersion = "4.3.1"

// defaultDataDir is the Kafka data directory of Docker nodes and of agents that do not report
// theirs.
const defaultDataDir = "/var/lib/kafka/data"

// Settings of a cluster whose nodes Brokerium manages, stored in clusters.settings.
type Settings struct {
	KafkaVersion      string   `json:"kafkaVersion"`
	Quorum            string   `json:"quorum"`
	KafkaClusterID    string   `json:"kafkaClusterId"`
	PortBase          int      `json:"portBase"`
	Racks             []string `json:"racks"`
	ReplicationFactor int      `json:"replicationFactor"`
	MinISR            int      `json:"minIsr"`
	BrokerHeap        string   `json:"brokerHeap"`
	ControllerHeap    string   `json:"controllerHeap"`
	Image             string   `json:"image"`
	// ControllerBootstrapServers lets the server read the quorum when no broker answers.
	ControllerBootstrapServers []string `json:"controllerBootstrapServers,omitempty"`
}

func settingsOf(c *store.Cluster) Settings {
	s := Settings{}
	raw, _ := json.Marshal(c.Settings)
	_ = json.Unmarshal(raw, &s)
	if s.KafkaVersion == "" {
		s.KafkaVersion = unrecordedKafkaVersion
	}
	if s.Quorum == "" {
		s.Quorum = "dynamic"
	}
	if s.ReplicationFactor == 0 {
		s.ReplicationFactor = 3
	}
	if s.MinISR == 0 {
		s.MinISR = min(2, s.ReplicationFactor)
	}
	if s.BrokerHeap == "" {
		s.BrokerHeap = "-Xmx512m -Xms512m"
	}
	if s.ControllerHeap == "" {
		s.ControllerHeap = "-Xmx384m -Xms384m"
	}
	if s.KafkaClusterID == "" {
		s.KafkaClusterID = c.KafkaClusterID
	}
	return s
}

func (s Settings) toMap() map[string]any {
	raw, _ := json.Marshal(s)
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	return m
}

// Port bases of Docker clusters: their nodes publish ports from the base up to base + 999.
const (
	minPortBase = 1024
	maxPortBase = 65535 - 999
)

// ValidateSettings checks the keys of cluster settings that Brokerium reads and keeps the others
// as they are. Saved settings hold portBase 0 for clusters without published ports and racks
// null for none.
func ValidateSettings(settings map[string]any) error {
	for _, key := range []string{"brokerHeap", "controllerHeap"} {
		v, ok := settings[key]
		if !ok {
			continue
		}
		heap, isString := v.(string)
		if !isString {
			return fmt.Errorf("settings.%s: must be a string such as \"-Xms1g -Xmx1g\"", key)
		}
		if err := agentcfg.ValidateHeap(heap); err != nil {
			return fmt.Errorf("settings.%s: %w", key, err)
		}
	}
	if v, ok := settings["kafkaVersion"]; ok {
		if version, _ := v.(string); !versionRe.MatchString(version) {
			return fmt.Errorf("settings.kafkaVersion: must be a Kafka release such as %s", DefaultKafkaVersion)
		}
	}
	if v, ok := settings["quorum"]; ok && v != "static" && v != "dynamic" {
		return errors.New("settings.quorum: must be static or dynamic")
	}
	rf, err := countSetting(settings, "replicationFactor")
	if err != nil {
		return err
	}
	minISR, err := countSetting(settings, "minIsr")
	if err != nil {
		return err
	}
	if rf == 0 {
		rf = settingsOf(&store.Cluster{}).ReplicationFactor
	}
	if minISR > rf {
		return fmt.Errorf("settings.minIsr: %d is more than the replication factor %d", minISR, rf)
	}
	if v, ok := settings["racks"]; ok && v != nil {
		racks, isList := v.([]any)
		if !isList {
			return errors.New("settings.racks: must be a list of rack names")
		}
		for i, r := range racks {
			rack, isString := r.(string)
			if !isString || rack == "" {
				return fmt.Errorf("settings.racks[%d]: must be a rack name", i)
			}
			if err := agentcfg.ValidateRack(rack); err != nil {
				return fmt.Errorf("settings.racks[%d]: %w", i, err)
			}
		}
	}
	if v, ok := settings["portBase"]; ok {
		if base, isInt := wholeNumber(v); !isInt || (base != 0 && (base < minPortBase || base > maxPortBase)) {
			return fmt.Errorf("settings.portBase: must be 0 or from %d to %d", minPortBase, maxPortBase)
		}
	}
	return nil
}

// countSetting reads a setting that counts replicas; it is 0 when the key is absent.
func countSetting(settings map[string]any, key string) (int, error) {
	v, ok := settings[key]
	if !ok {
		return 0, nil
	}
	n, isInt := wholeNumber(v)
	if !isInt || n < 1 {
		return 0, fmt.Errorf("settings.%s: must be a whole number of at least 1", key)
	}
	return n, nil
}

// wholeNumber reads an integer from a decoded JSON value.
func wholeNumber(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		if n != math.Trunc(n) || math.Abs(n) > math.MaxInt32 {
			return 0, false
		}
		return int(n), true
	case int:
		return n, true
	}
	return 0, false
}

// NodeName is the container name of a node of a Docker cluster: <cluster>-c<id> for a
// controller, <cluster>-cb<id> for a combined node and <cluster>-b<id> for a broker.
func NodeName(cluster, role string, id int32) string {
	switch role {
	case RoleController:
		return fmt.Sprintf("%s-c%d", cluster, id)
	case RoleCombined:
		return fmt.Sprintf("%s-cb%d", cluster, id)
	}
	return fmt.Sprintf("%s-b%d", cluster, id)
}

// RandomUUID returns a Kafka-style UUID string. Like Kafka's Uuid.randomUuid, it never starts
// with "-", which command-line parsers read as an option.
func RandomUUID() string {
	b := make([]byte, 16)
	for {
		_, _ = rand.Read(b)
		if s := base64.RawURLEncoding.EncodeToString(b); !strings.HasPrefix(s, "-") {
			return s
		}
	}
}

// managed are the nodes of a cluster. controllers are the voters, controller and combined nodes,
// and brokers the broker and combined nodes, so a combined node is in both.
type managed struct {
	nodes       []*store.Node
	controllers []*store.Node
	brokers     []*store.Node
}

func (m *Manager) managedNodes(ctx context.Context, clusterID string) (managed, error) {
	nodes, err := m.Store.ListNodes(ctx, clusterID)
	if err != nil {
		return managed{}, err
	}
	return classify(nodes), nil
}

// classify sorts the nodes of a cluster into controllers and brokers, each ordered by node id.
func classify(nodes []*store.Node) managed {
	out := managed{nodes: nodes}
	for _, n := range nodes {
		if n.KafkaNodeID == nil {
			continue
		}
		if runsController(n.Role) {
			out.controllers = append(out.controllers, n)
		}
		if runsBroker(n.Role) {
			out.brokers = append(out.brokers, n)
		}
	}
	byID := func(ns []*store.Node) {
		sort.Slice(ns, func(i, j int) bool { return *ns[i].KafkaNodeID < *ns[j].KafkaNodeID })
	}
	byID(out.controllers)
	byID(out.brokers)
	return out
}

// quorumBootstrap lists controller endpoints for controller.quorum.bootstrap.servers.
func quorumBootstrap(hosts []string) string {
	var parts []string
	for _, h := range hosts {
		parts = append(parts, net.JoinHostPort(h, "9093"))
	}
	return strings.Join(parts, ",")
}

// brokerAddress is how clients reach a managed broker: Docker lab nodes publish their listener
// on localhost, and a server in a container on the lab network reaches their internal listener
// by container name; agent nodes are reached at their advertised host.
func (m *Manager) brokerAddress(s Settings, docker bool, host string, id int32) string {
	if docker && !m.Cfg.ServerInContainer {
		return fmt.Sprintf("localhost:%d", s.PortBase+int(id))
	}
	return net.JoinHostPort(host, "9092")
}

// controllerAddress is how the server reaches the controller of a managed controller or combined
// node directly.
func (m *Manager) controllerAddress(s Settings, docker bool, role, host string, id int32) string {
	if docker && !m.Cfg.ServerInContainer {
		return fmt.Sprintf("localhost:%d", controllerPort(s, role, id))
	}
	return net.JoinHostPort(host, "9093")
}

// controllerPort is the port on localhost that publishes the controller listener of a node of a
// Docker cluster. A combined node publishes its client listener at port base + node id, as
// brokers do, so its controller listener moves 900 ports up, past the ids brokers get.
func controllerPort(s Settings, role string, id int32) int {
	if role == RoleCombined {
		return s.PortBase + 900 + int(id)
	}
	return s.PortBase + int(id)
}

// dataDirOf is the log.dirs of node n: the Kafka data directory its agent reports, else
// defaultDataDir. Docker nodes keep their data volume at defaultDataDir.
func dataDirOf(docker bool, n *store.Node) (string, error) {
	var st struct {
		KafkaDataDir string `json:"kafkaDataDir"`
	}
	if docker || json.Unmarshal(n.Status, &st) != nil || st.KafkaDataDir == "" {
		return defaultDataDir, nil
	}
	if err := agentcfg.ValidateDataDir(st.KafkaDataDir); err != nil {
		return "", fmt.Errorf("the agent of %s reports a data directory Brokerium cannot configure: %w", n.Name, err)
	}
	return st.KafkaDataDir, nil
}

// renderProperties builds server.properties for a node. host is the container name of a Docker
// node and the advertised host of an agent node, dataDir its Kafka data directory. quorum is
// either a bootstrap server list (dynamic) or a voters string (static). Docker lab nodes get a
// second client listener published on localhost; agent nodes have one, advertised on their host.
// Combined nodes have the client listeners of a broker and the controller listener of a
// controller.
func renderProperties(s Settings, docker bool, role, host, dataDir string, id int32, rack, quorumKind, quorum string) map[string]string {
	p := map[string]string{
		"node.id":                          strconv.Itoa(int(id)),
		"controller.listener.names":        "CONTROLLER",
		"log.dirs":                         dataDir,
		"num.partitions":                   "6",
		"default.replication.factor":       strconv.Itoa(s.ReplicationFactor),
		"min.insync.replicas":              strconv.Itoa(s.MinISR),
		"auto.create.topics.enable":        "false",
		"group.initial.rebalance.delay.ms": "0",
		// ACLs are enforced once defined; without any ACL access stays open, and the
		// PLAINTEXT lab principal remains a super user.
		"authorizer.class.name":          "org.apache.kafka.metadata.authorizer.StandardAuthorizer",
		"allow.everyone.if.no.acl.found": "true",
		"super.users":                    "User:ANONYMOUS",
	}
	if quorumKind == "static" {
		p["controller.quorum.voters"] = quorum
	} else {
		p["controller.quorum.bootstrap.servers"] = quorum
	}
	controller := "CONTROLLER://" + net.JoinHostPort(host, "9093")
	controllerBind := controller
	if !docker {
		controllerBind = "CONTROLLER://0.0.0.0:9093"
	}
	if role == RoleController {
		p["process.roles"] = "controller"
		p["listeners"] = controllerBind
		p["advertised.listeners"] = controller
		p["listener.security.protocol.map"] = "CONTROLLER:PLAINTEXT"
		return p
	}
	var listeners, advertised []string
	if docker {
		port := s.PortBase + int(id)
		listeners = []string{"INTERNAL://0.0.0.0:9092", fmt.Sprintf("EXTERNAL://0.0.0.0:%d", port)}
		advertised = []string{"INTERNAL://" + net.JoinHostPort(host, "9092"), fmt.Sprintf("EXTERNAL://localhost:%d", port)}
		p["inter.broker.listener.name"] = "INTERNAL"
		p["listener.security.protocol.map"] = "CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT"
	} else {
		listeners = []string{"PLAINTEXT://0.0.0.0:9092"}
		advertised = []string{"PLAINTEXT://" + net.JoinHostPort(host, "9092")}
		p["inter.broker.listener.name"] = "PLAINTEXT"
		p["listener.security.protocol.map"] = "CONTROLLER:PLAINTEXT,PLAINTEXT:PLAINTEXT"
	}
	p["process.roles"] = "broker"
	if role == RoleCombined {
		p["process.roles"] = "broker,controller"
		listeners = append(listeners, controllerBind)
		advertised = append(advertised, controller)
	}
	p["listeners"] = strings.Join(listeners, ",")
	p["advertised.listeners"] = strings.Join(advertised, ",")
	p["offsets.topic.replication.factor"] = strconv.Itoa(s.ReplicationFactor)
	p["transaction.state.log.replication.factor"] = strconv.Itoa(s.ReplicationFactor)
	p["transaction.state.log.min.isr"] = strconv.Itoa(s.MinISR)
	p["share.coordinator.state.topic.replication.factor"] = strconv.Itoa(s.ReplicationFactor)
	p["share.coordinator.state.topic.min.isr"] = strconv.Itoa(s.MinISR)
	p["log.segment.bytes"] = "16777216"
	if rack != "" {
		p["broker.rack"] = rack
	}
	return p
}
