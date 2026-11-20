// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package cluster

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/store"
)

func healthySnapshot(now time.Time) *kafka.Snapshot {
	ts := now.UnixMilli()
	return &kafka.Snapshot{
		Mode:         kafka.ModeKRaft,
		ControllerID: 102,
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
	if h.Brokers != 3 || h.BrokersAlive != 3 || h.Topics != 1 || h.Partitions != 2 || h.Voters != 3 || h.QuorumLeader != 1 ||
		h.Controller != 1 || h.Mode != kafka.ModeKRaft {
		t.Errorf("counters %+v", h)
	}
	if h.DiskUsedPercentMax != 40 || h.MetadataVersion != "4.3-IV0" || h.KRaftVersion != 1 {
		t.Errorf("disk %v, metadata %s, kraft %d", h.DiskUsedPercentMax, h.MetadataVersion, h.KRaftVersion)
	}
}

func TestEvaluateZooKeeper(t *testing.T) {
	t.Parallel()
	now := time.Now()
	zookeeper := func() *kafka.Snapshot {
		s := healthySnapshot(now)
		s.Quorum, s.Features, s.Skipped, s.Mode, s.ControllerID = nil, nil, []string{kafka.PartQuorum}, kafka.ModeZooKeeper, 102
		for i := range s.Brokers {
			s.Brokers[i].Mode, s.Brokers[i].Version = kafka.ModeZooKeeper, "3.5"
		}
		return s
	}
	tests := []struct {
		name       string
		mutate     func(s *kafka.Snapshot)
		status     string
		codes      []string
		controller int32
	}{
		{"the controller comes from Metadata", func(*kafka.Snapshot) {}, HealthOK, []string{}, 102},
		{"no controller", func(s *kafka.Snapshot) { s.ControllerID = -1 }, HealthCritical, []string{"no_controller"}, -1},
		{"migration without a described quorum", func(s *kafka.Snapshot) { s.Mode = kafka.ModeMigration }, HealthOK, []string{}, 102},
		{"migration with the quorum of the KRaft controllers", func(s *kafka.Snapshot) {
			s.Mode, s.Skipped = kafka.ModeMigration, nil
			s.Quorum = &kafka.Quorum{LeaderID: 3000, Voters: []kafka.QuorumReplica{{NodeID: 3000, Role: "leader"}}}
		}, HealthOK, []string{}, 3000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := zookeeper()
			tc.mutate(s)
			h := Evaluate(s, now)
			if h.Status != tc.status || !reflect.DeepEqual(codes(h), tc.codes) || h.Controller != tc.controller {
				t.Errorf("status %s, issues %v, controller %d; want %s, %v, %d", h.Status, codes(h), h.Controller, tc.status, tc.codes, tc.controller)
			}
			if h.MetadataVersion != "" || !reflect.DeepEqual(h.Versions, []string{"3.5"}) {
				t.Errorf("metadata.version %q, versions %v", h.MetadataVersion, h.Versions)
			}
		})
	}
}

func TestEvaluateVersions(t *testing.T) {
	t.Parallel()
	now := time.Now()
	s := healthySnapshot(now)
	s.Brokers[0].Version, s.Brokers[1].Version, s.Brokers[2].Version = "4.3.1", "4.4.0", "4.3.1"
	s.MixedVersions = true
	h := Evaluate(s, now)
	if !reflect.DeepEqual(h.Versions, []string{"4.3.1", "4.4.0"}) || !h.MixedVersions || h.Status != HealthOK {
		t.Errorf("versions %v, mixed %v, status %s", h.Versions, h.MixedVersions, h.Status)
	}
	if len(h.Issues) != 1 || h.Issues[0].Code != "mixed_versions" || h.Issues[0].Message != "the brokers run different Kafka versions: 4.3.1 (101, 103), 4.4.0 (102)" {
		t.Errorf("issues %+v", h.Issues)
	}

	s = healthySnapshot(now)
	s.Brokers[0].Version = "3.4"
	if h := Evaluate(s, now); h.Status != HealthWarning || !reflect.DeepEqual(codes(h), []string{"unsupported_version"}) {
		t.Errorf("Kafka 3.4: status %s, issues %v", h.Status, h.Issues)
	}
}

func TestMetadataVersionName(t *testing.T) {
	t.Parallel()
	for level, want := range map[int16]string{1: "3.0-IV1", 2: "3.1-IV0", 3: "3.2-IV0", 4: "3.3-IV0", 5: "3.3-IV1", 6: "3.3-IV2",
		7: "3.3-IV3", 21: "3.9-IV0", 30: "4.3-IV0", 33: "4.4-IV2", 99: "level 99"} {
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

func TestCertificateIssues(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 10, 19, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { v := now.Add(d); return &v }
	nodes := []*store.Node{
		{Name: "alpha-b101", CertNotAfter: at(200 * 24 * time.Hour)},
		{Name: "alpha-b102", CertNotAfter: at(10 * 24 * time.Hour)},
		{Name: "alpha-b103", CertNotAfter: at(-time.Hour)},
		{Name: "alpha-b104"},
	}
	got := map[string]string{}
	for _, i := range CertificateIssues(nodes, now, 30*24*time.Hour) {
		got[i.Code] += i.Severity
		if !strings.Contains(i.Message, "alpha-b10") {
			t.Errorf("issue %q does not name the node", i.Message)
		}
	}
	want := map[string]string{"agent_certificate_expiring": HealthWarning, "agent_certificate_expired": HealthCritical}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("CertificateIssues = %v, want %v", got, want)
	}

	h := Health{Status: HealthOK, Issues: CertificateIssues(nodes[:2], now, 30*24*time.Hour)}
	h.settle()
	if h.Status != HealthWarning {
		t.Errorf("status %s with an expiring certificate, want %s", h.Status, HealthWarning)
	}
}
