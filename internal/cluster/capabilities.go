// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package cluster

import (
	"github.com/brokerium/brokerium/internal/kafka"
	"github.com/brokerium/brokerium/internal/store"
)

// Capabilities tell the API and the dashboard which actions a cluster supports (ADR 0004).
type Capabilities struct {
	ManageTopics      bool     `json:"manageTopics"`
	ManageConfigs     bool     `json:"manageConfigs"`
	ManageACLs        bool     `json:"manageAcls"`
	ManageSCRAM       bool     `json:"manageScram"`
	ManageQuotas      bool     `json:"manageQuotas"`
	ManageGroups      bool     `json:"manageGroups"`
	BrowseMessages    bool     `json:"browseMessages"`
	Reassign          bool     `json:"reassign"`
	ElectLeaders      bool     `json:"electLeaders"`
	Cordon            bool     `json:"cordon"`
	UnregisterBrokers bool     `json:"unregisterBrokers"`
	DescribeQuorum    bool     `json:"describeQuorum"`
	RemoveVoters      bool     `json:"removeVoters"`
	AddControllers    bool     `json:"addControllers"`
	ProvisionNodes    bool     `json:"provisionNodes"`
	RestartNodes      bool     `json:"restartNodes"`
	UpgradeVersion    bool     `json:"upgradeVersion"`
	FinalizeFeatures  bool     `json:"finalizeFeatures"`
	MigrateQuorum     bool     `json:"migrateQuorum"`
	ScaleNodePools    bool     `json:"scaleNodePools"`
	Notes             []string `json:"notes"`
}

// CapabilitiesFor computes capabilities from the provider, the flavor, the mode and versions of
// the brokers and the quorum type (ADR 0022). s is nil while no broker answers.
func CapabilitiesFor(c *store.Cluster, s *kafka.Snapshot) Capabilities {
	var mode string
	if s != nil {
		mode = s.Mode
	}
	static := StaticQuorum(s)
	caps := Capabilities{
		ManageTopics: true, ManageConfigs: true, ManageACLs: true, ManageSCRAM: true, ManageQuotas: true,
		ManageGroups: true, BrowseMessages: true, Reassign: true, ElectLeaders: true, Cordon: BrokersAtLeast(s, 4, 3),
		UnregisterBrokers: true, DescribeQuorum: true, RemoveVoters: !static, FinalizeFeatures: true,
		Notes: []string{},
	}
	switch c.Flavor {
	case store.FlavorMSK:
		caps.ManageSCRAM, caps.ManageQuotas, caps.Cordon, caps.UnregisterBrokers = false, false, false, false
		caps.DescribeQuorum, caps.RemoveVoters, caps.FinalizeFeatures = false, false, false
		caps.Notes = append(caps.Notes, "Amazon MSK manages brokers, controllers and versions; SCRAM users live in AWS Secrets Manager.")
	case store.FlavorConfluentCloud:
		caps.ManageSCRAM, caps.ManageQuotas, caps.Reassign, caps.ElectLeaders, caps.Cordon = false, false, false, false, false
		caps.UnregisterBrokers, caps.DescribeQuorum, caps.RemoveVoters, caps.FinalizeFeatures = false, false, false, false
		caps.Notes = append(caps.Notes, "Confluent Cloud manages placement and brokers; only data-plane features are available.")
	case store.FlavorAiven:
		caps.ManageSCRAM, caps.ManageQuotas, caps.Reassign, caps.ElectLeaders, caps.Cordon = false, false, false, false, false
		caps.UnregisterBrokers, caps.DescribeQuorum, caps.RemoveVoters, caps.FinalizeFeatures = false, false, false, false
		caps.Notes = append(caps.Notes, "Aiven manages nodes, users and placement; only data-plane features are available.")
	}
	switch c.Provider {
	case store.ProviderAgent, store.ProviderDocker:
		caps.ProvisionNodes = c.Provider == store.ProviderDocker
		caps.AddControllers = !static
		caps.RestartNodes, caps.UpgradeVersion, caps.MigrateQuorum = true, true, static && BrokersAtLeast(s, 4, 1)
		if static && !caps.MigrateQuorum {
			caps.Notes = append(caps.Notes, "Static controller quorum: migrating it to a dynamic quorum needs Kafka 4.1 or later on every node.")
		}
	case store.ProviderStrimzi:
		caps.ScaleNodePools, caps.UpgradeVersion, caps.RestartNodes = true, true, true
		caps.ManageSCRAM = false
		caps.Notes = append(caps.Notes, "Strimzi manages pods and users; controller quorum changes wait for dynamic quorum support in Strimzi.")
	default:
		if c.Flavor == store.FlavorApache && !ZooKeeperBased(mode) {
			caps.Notes = append(caps.Notes, "Install brokerium-agent on the nodes to manage brokers, controllers and upgrades.")
		}
	}
	if static && c.Provider == store.ProviderExternal && c.Flavor == store.FlavorApache {
		caps.Notes = append(caps.Notes, "Static controller quorum: controllers cannot be added or removed until it is migrated (Kafka 4.1+).")
	}
	switch mode {
	case kafka.ModeZooKeeper, kafka.ModeMigration:
		caps.Cordon, caps.UnregisterBrokers, caps.DescribeQuorum, caps.RemoveVoters, caps.AddControllers = false, false, false, false, false
		caps.FinalizeFeatures, caps.MigrateQuorum, caps.ProvisionNodes, caps.RestartNodes, caps.UpgradeVersion = false, false, false, false, false
		caps.ScaleNodePools = false
		note := "ZooKeeper mode: Brokerium watches the cluster and manages its data and partition placement; controller, node and version operations need KRaft."
		if mode == kafka.ModeMigration {
			note = "The cluster is migrating from ZooKeeper to KRaft: controller, node and version operations wait until the migration is finished."
		}
		caps.Notes = append([]string{note}, caps.Notes...)
	default:
		if c.Flavor == store.FlavorApache && !caps.Cordon && everyVersion(s, func(kafka.VersionRange) bool { return true }) {
			caps.Notes = append(caps.Notes, "Cordoning and draining brokers need Kafka 4.3 or later on every broker.")
		}
	}
	return caps
}

// StaticQuorum reports whether a KRaft cluster runs a static controller quorum: kraft.version is
// 0, or there is no kraft.version (Kafka 3.3 to 3.8). When the features were not read, voters
// without a directory id or brokers before Kafka 3.9 tell.
func StaticQuorum(s *kafka.Snapshot) bool {
	if s == nil || ZooKeeperBased(s.Mode) {
		return false
	}
	for _, f := range s.Features {
		if f.Name == "kraft.version" {
			return f.Finalized == 0
		}
	}
	if len(s.Features) > 0 {
		return true
	}
	if s.Quorum != nil && len(s.Quorum.Voters) > 0 {
		for _, v := range s.Quorum.Voters {
			if v.DirectoryID == "" {
				return true
			}
		}
		return false
	}
	return BrokersBelow(s, 3, 9)
}

// BrokersAtLeast reports whether every broker of known version runs major.minor or later, and the
// version of at least one broker is known.
func BrokersAtLeast(s *kafka.Snapshot, major, minor int) bool {
	return everyVersion(s, func(r kafka.VersionRange) bool { return r.AtLeast(major, minor) })
}

// BrokersBelow reports whether every broker of known version runs a release before major.minor,
// and the version of at least one broker is known.
func BrokersBelow(s *kafka.Snapshot, major, minor int) bool {
	return everyVersion(s, func(r kafka.VersionRange) bool { return r.Below(major, minor) })
}

func everyVersion(s *kafka.Snapshot, ok func(kafka.VersionRange) bool) bool {
	if s == nil {
		return false
	}
	known := false
	for _, b := range s.Brokers {
		r, parsed := kafka.ParseVersion(b.Version)
		if !parsed {
			continue
		}
		if !ok(r) {
			return false
		}
		known = true
	}
	return known
}
