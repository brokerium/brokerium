// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package cluster

import (
	"reflect"
	"testing"
	"time"

	"github.com/bvaliev/brokerium/internal/kafka"
	"github.com/bvaliev/brokerium/internal/store"
)

func healthySnapshot(now time.Time) *kafka.Snapshot {
	ts := now.UnixMilli()
	return &kafka.Snapshot{
		Brokers: []kafka.Broker{
			{ID: 101, Alive: true, DiskTotalBytes: 100, DiskUsableBytes: 60},
			{ID: 102, Alive: true, DiskTotalBytes: 100, DiskUsableBytes: 70},
			{ID: 103, Alive: true, DiskTotalBytes: 100, DiskUsableBytes: 80},
		},
		Topics: []kafka.Topic{{Name: "orders", MinISR: 2, Partitions: []kafka.Partition{
			{ID: 0, Leader: 101, Replicas: []int32{101, 102, 103}, ISR: []int32{101, 102, 103}},
			{ID: 1, Leader: 102, Replicas: []int32{102, 103, 101}, ISR: []int32{102, 103, 101}},
		}}},
		Quorum: &kafka.Quorum{LeaderID: 1, Voters: []kafka.QuorumReplica{
			{NodeID: 1, Role: "leader", LastCaughtUpTimestamp: ts},
			{NodeID: 2, Role: "follower", LastCaughtUpTimestamp: ts},
			{NodeID: 3, Role: "follower", LastCaughtUpTimestamp: ts},
		}},
		Features: []kafka.Feature{{Name: "metadata.version", Finalized: 30}, {Name: "kraft.version", Finalized: 1}},
	}
}

func codes(h Health) []string {
	out := []string{}
	for _, i := range h.Issues {
		out = append(out, i.Code)
	}
	return out
}

func TestEvaluate(t *testing.T) {
	t.Parallel()
	now := time.Now()
	tests := []struct {
		name   string
		mutate func(s *kafka.Snapshot)
		status string
		codes  []string
	}{
		{"healthy", func(*kafka.Snapshot) {}, HealthOK, []string{}},
		{"offline partition", func(s *kafka.Snapshot) {
			s.Topics[0].Partitions[0].Leader = -1
		}, HealthCritical, []string{"offline_partitions"}},
		{"under min ISR", func(s *kafka.Snapshot) {
			s.Topics[0].Partitions[0].ISR = []int32{101}
		}, HealthCritical, []string{"under_min_isr", "under_replicated"}},
		{"under-replicated", func(s *kafka.Snapshot) {
			s.Topics[0].Partitions[1].ISR = []int32{102, 103}
		}, HealthWarning, []string{"under_replicated"}},
		{"replicas being added are not under-replicated", func(s *kafka.Snapshot) {
			s.Topics[0].Partitions[1].Replicas = []int32{102, 103, 101, 104}
			s.Reassignments = []kafka.Reassignment{{Topic: "orders", Partition: 1, Replicas: []int32{102, 103, 101, 104}, Adding: []int32{104}}}
		}, HealthOK, []string{"reassigning"}},
		{"fenced broker", func(s *kafka.Snapshot) {
			s.Brokers[2].Fenced = true
		}, HealthWarning, []string{"broker_fenced"}},
		{"full disk", func(s *kafka.Snapshot) {
			s.Brokers[0].DiskUsableBytes = 5
		}, HealthCritical, []string{"disk_full"}},
		{"no quorum leader", func(s *kafka.Snapshot) {
			s.Quorum.LeaderID = -1
		}, HealthCritical, []string{"no_quorum_leader"}},
		{"majority of voters lagging", func(s *kafka.Snapshot) {
			s.Quorum.Voters[1].Lag = VoterLagThreshold + 1
			s.Quorum.Voters[2].LastCaughtUpTimestamp = now.Add(-time.Minute).UnixMilli()
		}, HealthCritical, []string{"quorum_at_risk"}},
		{"one voter lagging", func(s *kafka.Snapshot) {
			s.Quorum.Voters[1].Lag = VoterLagThreshold + 1
		}, HealthWarning, []string{"voter_lagging"}},
		{"even voter count", func(s *kafka.Snapshot) {
			s.Quorum.Voters = append(s.Quorum.Voters, kafka.QuorumReplica{NodeID: 4, Role: "follower", LastCaughtUpTimestamp: now.UnixMilli()})
		}, HealthOK, []string{"even_voters"}},
		{"quorum unknown", func(s *kafka.Snapshot) {
			s.Quorum = nil
		}, HealthWarning, []string{"quorum_unknown"}},
		{"quorum not offered by a managed service", func(s *kafka.Snapshot) {
			s.Quorum, s.Skipped = nil, []string{kafka.PartQuorum}
		}, HealthOK, []string{}},
		{"stale observer", func(s *kafka.Snapshot) {
			s.Quorum.Observers = []kafka.QuorumReplica{{NodeID: 5, Role: "observer", LastFetchTimestamp: now.Add(-time.Hour).UnixMilli()}}
		}, HealthOK, []string{"stale_observers"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := healthySnapshot(now)
			tc.mutate(s)
			h := Evaluate(s, now)
			if h.Status != tc.status {
				t.Errorf("status = %s, want %s (issues %v)", h.Status, tc.status, h.Issues)
			}
			if got := codes(h); !reflect.DeepEqual(got, tc.codes) {
				t.Errorf("issues = %v, want %v", got, tc.codes)
			}
		})
	}
}

func TestEvaluateCounters(t *testing.T) {
	t.Parallel()
	now := time.Now()
	h := Evaluate(healthySnapshot(now), now)
	if h.Brokers != 3 || h.BrokersAlive != 3 || h.Topics != 1 || h.Partitions != 2 || h.Voters != 3 || h.QuorumLeader != 1 {
		t.Errorf("counters %+v", h)
	}
	if h.DiskUsedPercentMax != 40 || h.MetadataVersion != "4.3-IV0" || h.KRaftVersion != 1 {
		t.Errorf("disk %v, metadata %s, kraft %d", h.DiskUsedPercentMax, h.MetadataVersion, h.KRaftVersion)
	}
}

func TestMetadataVersionName(t *testing.T) {
	t.Parallel()
	for level, want := range map[int16]string{7: "3.3-IV3", 21: "3.9-IV0", 30: "4.3-IV0", 33: "4.4-IV2", 99: "level 99"} {
		if got := MetadataVersionName(level); got != want {
			t.Errorf("MetadataVersionName(%d) = %q, want %q", level, got, want)
		}
	}
}

func TestControllerServers(t *testing.T) {
	t.Parallel()
	c := &store.Cluster{Settings: map[string]any{"controllerBootstrapServers": []any{"c1:9093", "", 5, "c2:9093"}}}
	if got := ControllerServers(c); !reflect.DeepEqual(got, []string{"c1:9093", "c2:9093"}) {
		t.Errorf("ControllerServers = %v", got)
	}
	if got := ControllerServers(&store.Cluster{}); got != nil {
		t.Errorf("ControllerServers without settings = %v", got)
	}
}

func TestCapabilitiesFor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		c      store.Cluster
		static bool
		check  func(Capabilities) bool
	}{
		{"external apache: data and placement, no node lifecycle", store.Cluster{Provider: store.ProviderExternal, Flavor: store.FlavorApache}, false,
			func(c Capabilities) bool {
				return c.Reassign && c.RemoveVoters && !c.ProvisionNodes && !c.RestartNodes && !c.AddControllers
			}},
		{"docker: everything", store.Cluster{Provider: store.ProviderDocker, Flavor: store.FlavorApache}, false,
			func(c Capabilities) bool {
				return c.ProvisionNodes && c.AddControllers && c.RestartNodes && c.UpgradeVersion && !c.MigrateQuorum
			}},
		{"agent with a static quorum: migrate, no controller changes", store.Cluster{Provider: store.ProviderAgent, Flavor: store.FlavorApache}, true,
			func(c Capabilities) bool {
				return c.MigrateQuorum && !c.AddControllers && !c.RemoveVoters && !c.ProvisionNodes
			}},
		{"msk: reassign but no quorum", store.Cluster{Provider: store.ProviderExternal, Flavor: store.FlavorMSK}, false,
			func(c Capabilities) bool {
				return c.Reassign && !c.DescribeQuorum && !c.ManageSCRAM && !c.FinalizeFeatures
			}},
		{"confluent cloud: data plane only", store.Cluster{Provider: store.ProviderExternal, Flavor: store.FlavorConfluentCloud}, false,
			func(c Capabilities) bool { return c.ManageTopics && c.ManageACLs && !c.Reassign && !c.ElectLeaders }},
		{"strimzi: node pools", store.Cluster{Provider: store.ProviderStrimzi, Flavor: store.FlavorApache}, false,
			func(c Capabilities) bool { return c.ScaleNodePools && !c.ProvisionNodes && !c.ManageSCRAM }},
	}
	for _, tc := range tests {
		if got := CapabilitiesFor(&tc.c, tc.static); !tc.check(got) {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
}
