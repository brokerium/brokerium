// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package cluster

import (
	"strings"
	"testing"

	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/store"
)

// kraftSnapshot is a KRaft cluster of three brokers on version with the given kraft.version.
func kraftSnapshot(version string, kraftVersion int16) *kafka.Snapshot {
	s := &kafka.Snapshot{Mode: kafka.ModeKRaft,
		Features: []kafka.Feature{{Name: "metadata.version", Finalized: 30}, {Name: "kraft.version", Finalized: kraftVersion}}}
	for id := int32(101); id <= 103; id++ {
		s.Brokers = append(s.Brokers, kafka.Broker{ID: id, Alive: true, Mode: kafka.ModeKRaft, Version: version})
	}
	return s
}

func zookeeperSnapshot(mode, version string) *kafka.Snapshot {
	s := &kafka.Snapshot{Mode: mode, ControllerID: 101, Features: []kafka.Feature{}}
	for id := int32(101); id <= 103; id++ {
		s.Brokers = append(s.Brokers, kafka.Broker{ID: id, Alive: true, Mode: mode, Version: version})
	}
	return s
}

func TestCapabilitiesFor(t *testing.T) {
	t.Parallel()
	external := store.Cluster{Provider: store.ProviderExternal, Flavor: store.FlavorApache}
	agent := store.Cluster{Provider: store.ProviderAgent, Flavor: store.FlavorApache}
	docker := store.Cluster{Provider: store.ProviderDocker, Flavor: store.FlavorApache}
	tests := []struct {
		name  string
		c     store.Cluster
		s     *kafka.Snapshot
		check func(Capabilities) bool
		note  string
	}{
		{"external KRaft 4.3: data, placement, cordoning and voters, no node lifecycle", external, kraftSnapshot("4.3", 1),
			func(c Capabilities) bool {
				return c.Reassign && c.Cordon && c.RemoveVoters && c.DescribeQuorum && c.FinalizeFeatures && c.UnregisterBrokers &&
					!c.ProvisionNodes && !c.RestartNodes && !c.AddControllers
			}, "Install brokerium-agent"},
		{"external KRaft 4.2: no cordoning", external, kraftSnapshot("4.2", 1),
			func(c Capabilities) bool { return !c.Cordon && c.Reassign && c.RemoveVoters }, "need Kafka 4.3 or later"},
		{"one broker before 4.3: no cordoning", external, func() *kafka.Snapshot {
			s := kraftSnapshot("4.3.1", 1)
			s.Brokers[2].Version = "4.2.1"
			return s
		}(), func(c Capabilities) bool { return !c.Cordon }, ""},
		{"external KRaft 3.8 without kraft.version: a static quorum", external, func() *kafka.Snapshot {
			s := kraftSnapshot("3.8", 0)
			s.Features = s.Features[:1]
			return s
		}(), func(c Capabilities) bool { return !c.RemoveVoters && c.DescribeQuorum && !c.Cordon }, "Static controller quorum"},
		{"docker on 4.3: everything but the migration", docker, kraftSnapshot("4.3.1", 1),
			func(c Capabilities) bool {
				return c.ProvisionNodes && c.AddControllers && c.RestartNodes && c.UpgradeVersion && c.Cordon && !c.MigrateQuorum
			}, ""},
		{"docker before the first broker answers: create and delete nodes", docker, nil,
			func(c Capabilities) bool { return c.ProvisionNodes && c.RestartNodes && !c.Cordon }, ""},
		{"agent with a static quorum on 4.1: migrate, no controller changes", agent, kraftSnapshot("4.1", 0),
			func(c Capabilities) bool {
				return c.MigrateQuorum && !c.AddControllers && !c.RemoveVoters && !c.ProvisionNodes
			}, ""},
		{"agent with a static quorum on 4.0: no migration", agent, kraftSnapshot("4.0", 0),
			func(c Capabilities) bool { return !c.MigrateQuorum && !c.AddControllers }, "needs Kafka 4.1 or later"},
		{"external ZooKeeper 3.5: data plane and placement only", external, zookeeperSnapshot(kafka.ModeZooKeeper, "3.5"),
			func(c Capabilities) bool {
				return c.ManageTopics && c.ManageACLs && c.ManageSCRAM && c.ManageQuotas && c.ManageGroups && c.BrowseMessages &&
					c.Reassign && c.ElectLeaders && !c.Cordon && !c.UnregisterBrokers && !c.DescribeQuorum && !c.RemoveVoters &&
					!c.FinalizeFeatures && !c.MigrateQuorum && !c.AddControllers
			}, "ZooKeeper mode"},
		{"adopted ZooKeeper brokers: no node lifecycle", agent, zookeeperSnapshot(kafka.ModeZooKeeper, "3.9"),
			func(c Capabilities) bool {
				return !c.RestartNodes && !c.UpgradeVersion && !c.ProvisionNodes && !c.MigrateQuorum
			}, "ZooKeeper mode"},
		{"migration: no quorum operations", external, zookeeperSnapshot(kafka.ModeMigration, "3.9"),
			func(c Capabilities) bool {
				return c.Reassign && !c.DescribeQuorum && !c.RemoveVoters && !c.FinalizeFeatures
			}, "migrating"},
		{"msk: reassign but no quorum", store.Cluster{Provider: store.ProviderExternal, Flavor: store.FlavorMSK}, kraftSnapshot("4.3", 1),
			func(c Capabilities) bool {
				return c.Reassign && !c.DescribeQuorum && !c.ManageSCRAM && !c.FinalizeFeatures && !c.Cordon
			}, "Amazon MSK"},
		{"confluent cloud: data plane only", store.Cluster{Provider: store.ProviderExternal, Flavor: store.FlavorConfluentCloud}, nil,
			func(c Capabilities) bool { return c.ManageTopics && c.ManageACLs && !c.Reassign && !c.ElectLeaders }, ""},
		{"strimzi: node pools", store.Cluster{Provider: store.ProviderStrimzi, Flavor: store.FlavorApache}, kraftSnapshot("4.3", 0),
			func(c Capabilities) bool { return c.ScaleNodePools && !c.ProvisionNodes && !c.ManageSCRAM }, "Strimzi"},
	}
	for _, tc := range tests {
		got := CapabilitiesFor(&tc.c, tc.s)
		if !tc.check(got) {
			t.Errorf("%s: %+v", tc.name, got)
		}
		if tc.note != "" && !strings.Contains(strings.Join(got.Notes, "\n"), tc.note) {
			t.Errorf("%s: notes %q do not mention %q", tc.name, got.Notes, tc.note)
		}
	}
}

func TestStaticQuorum(t *testing.T) {
	t.Parallel()
	withoutFeatures := func(directoryID string) *kafka.Snapshot {
		s := kraftSnapshot("4.0", 0)
		s.Features = []kafka.Feature{}
		s.Quorum = &kafka.Quorum{LeaderID: 1, Voters: []kafka.QuorumReplica{{NodeID: 1, DirectoryID: directoryID}}}
		return s
	}
	tests := []struct {
		name string
		s    *kafka.Snapshot
		want bool
	}{
		{"kraft.version 0", kraftSnapshot("4.3", 0), true},
		{"kraft.version 1", kraftSnapshot("4.3", 1), false},
		{"no kraft.version on Kafka 3.8", func() *kafka.Snapshot {
			s := kraftSnapshot("3.8", 0)
			s.Features = s.Features[:1]
			return s
		}(), true},
		{"features not read, voters without directory ids", withoutFeatures(""), true},
		{"features not read, voters with directory ids", withoutFeatures("tQZ2Eap5SyKCiBmkOuSVBQ"), false},
		{"features and quorum not read on Kafka 3.7", func() *kafka.Snapshot {
			s := kraftSnapshot("3.7", 0)
			s.Features = []kafka.Feature{}
			return s
		}(), true},
		{"ZooKeeper", zookeeperSnapshot(kafka.ModeZooKeeper, "3.5"), false},
		{"no snapshot", nil, false},
	}
	for _, tc := range tests {
		if got := StaticQuorum(tc.s); got != tc.want {
			t.Errorf("%s: StaticQuorum = %v, want %v", tc.name, got, tc.want)
		}
	}
}
