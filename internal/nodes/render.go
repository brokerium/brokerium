// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package nodes

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/bvaliev/brokerium/internal/store"
)

// Roles of managed nodes.
const (
	RoleBroker     = "broker"
	RoleController = "controller"
)

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
		s.KafkaVersion = "4.3.1"
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

// NodeName is the name of a managed node: <cluster>-c<id> or <cluster>-b<id>.
func NodeName(cluster, role string, id int32) string {
	if role == RoleController {
		return fmt.Sprintf("%s-c%d", cluster, id)
	}
	return fmt.Sprintf("%s-b%d", cluster, id)
}

// RandomUUID returns a Kafka-style UUID string.
func RandomUUID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

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
	out := managed{nodes: nodes}
	for _, n := range nodes {
		if n.KafkaNodeID == nil {
			continue
		}
		switch n.Role {
		case RoleController:
			out.controllers = append(out.controllers, n)
		case RoleBroker:
			out.brokers = append(out.brokers, n)
		}
	}
	byID := func(ns []*store.Node) {
		sort.Slice(ns, func(i, j int) bool { return *ns[i].KafkaNodeID < *ns[j].KafkaNodeID })
	}
	byID(out.controllers)
	byID(out.brokers)
	return out, nil
}

// quorumBootstrap lists controller endpoints for controller.quorum.bootstrap.servers.
func quorumBootstrap(controllers []string) string {
	var parts []string
	for _, c := range controllers {
		parts = append(parts, c+":9093")
	}
	return strings.Join(parts, ",")
}

// renderProperties builds server.properties for a node. quorum is either a bootstrap server
// list (dynamic) or a voters string (static).
func renderProperties(s Settings, role, name string, id int32, rack, quorumKind, quorum string) map[string]string {
	p := map[string]string{
		"node.id":                          strconv.Itoa(int(id)),
		"controller.listener.names":        "CONTROLLER",
		"log.dirs":                         "/var/lib/kafka/data",
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
	if role == RoleController {
		p["process.roles"] = "controller"
		p["listeners"] = "CONTROLLER://" + name + ":9093"
		p["advertised.listeners"] = "CONTROLLER://" + name + ":9093"
		p["listener.security.protocol.map"] = "CONTROLLER:PLAINTEXT"
		return p
	}
	port := s.PortBase + int(id)
	p["process.roles"] = "broker"
	p["listeners"] = fmt.Sprintf("INTERNAL://0.0.0.0:9092,EXTERNAL://0.0.0.0:%d", port)
	p["advertised.listeners"] = fmt.Sprintf("INTERNAL://%s:9092,EXTERNAL://localhost:%d", name, port)
	p["inter.broker.listener.name"] = "INTERNAL"
	p["listener.security.protocol.map"] = "CONTROLLER:PLAINTEXT,INTERNAL:PLAINTEXT,EXTERNAL:PLAINTEXT"
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
