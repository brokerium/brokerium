// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package cluster

import (
	"context"
	"encoding/json"
	"time"

	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/store"
)

// brokerConfigs returns the configurations of the brokers of a cluster, read again when they are
// older than BrokerConfigInterval or a live broker is missing from them.
func (s *Service) brokerConfigs(ctx context.Context, id string, cl *kafka.Client, snap *kafka.Snapshot) map[int32][]kafka.ConfigEntry {
	s.mu.Lock()
	cached := s.configs[id]
	s.mu.Unlock()
	var alive []int32
	missing := false
	for _, b := range snap.Brokers {
		if b.Alive {
			alive = append(alive, b.ID)
			missing = missing || cached == nil || cached.brokers[b.ID] == nil
		}
	}
	if cached != nil {
		age := time.Since(cached.readAt)
		if age < brokerConfigRetry || (!missing && age < BrokerConfigInterval) {
			return cached.brokers
		}
	}
	read, err := cl.BrokerConfigs(ctx, alive)
	if err != nil {
		s.log.Debug("reading broker configurations", "cluster", id, "error", err)
	}
	if cached != nil {
		for _, b := range snap.Brokers {
			if _, ok := read[b.ID]; !ok && cached.brokers[b.ID] != nil {
				read[b.ID] = cached.brokers[b.ID]
			}
		}
	}
	s.mu.Lock()
	s.configs[id] = &cachedConfigs{readAt: time.Now(), brokers: read}
	s.mu.Unlock()
	return read
}

// completeBrokers adds to the brokers of a snapshot what its requests could not tell: the version
// and mode the previous snapshot knew for brokers that did not answer ApiVersions now, the
// settings of their configuration with the migration flag, and the exact versions their agents
// report. It then summarizes the snapshot again.
func completeBrokers(snap, prev *kafka.Snapshot, configs map[int32][]kafka.ConfigEntry, nodes []*store.Node) {
	known := map[int32]kafka.Broker{}
	if prev != nil {
		for _, b := range prev.Brokers {
			known[b.ID] = b
		}
	}
	agents := agentVersions(nodes)
	for i := range snap.Brokers {
		b := &snap.Brokers[i]
		if p, ok := known[b.ID]; ok && b.Mode == "" {
			b.Mode = p.Mode
		}
		if p, ok := known[b.ID]; ok && b.Version == "" {
			b.Version, b.VersionExact = p.Version, p.VersionExact
		}
		settings := map[string]string{}
		for _, e := range configs[b.ID] {
			settings[e.Name] = e.Value
		}
		b.ProcessRoles, b.ZooKeeperConnect = settings["process.roles"], settings["zookeeper.connect"]
		if b.Mode == "" && configs[b.ID] != nil {
			b.Mode = kafka.ModeKRaft
			if b.ProcessRoles == "" {
				b.Mode = kafka.ModeZooKeeper
			}
		}
		if b.Mode != "" && settings["zookeeper.metadata.migration.enable"] == "true" {
			b.Mode = kafka.ModeMigration
		}
		if ZooKeeperBased(b.Mode) {
			b.InterBrokerProtocolVersion = settings["inter.broker.protocol.version"]
		}
		if v, ok := agents[b.ID]; ok {
			b.Version, b.VersionExact = v, true
		}
	}
	snap.Summarize()
}

// agentVersions are the Kafka versions the agents of broker and combined nodes report, by node id.
func agentVersions(nodes []*store.Node) map[int32]string {
	out := map[int32]string{}
	for _, n := range nodes {
		if n.KafkaNodeID == nil || (n.Role != "broker" && n.Role != "combined") {
			continue
		}
		var st struct {
			ActiveVersion string `json:"activeVersion"`
		}
		if json.Unmarshal(n.Status, &st) != nil {
			continue
		}
		if _, ok := kafka.ParseVersion(st.ActiveVersion); ok {
			out[*n.KafkaNodeID] = st.ActiveVersion
		}
	}
	return out
}
