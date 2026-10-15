// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

package cluster

import "github.com/bvaliev/brokerium/internal/store"

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

// CapabilitiesFor computes capabilities from the provider, flavor and quorum type.
// staticQuorum is true when kraft.version is 0.
func CapabilitiesFor(c *store.Cluster, staticQuorum bool) Capabilities {
	caps := Capabilities{
		ManageTopics: true, ManageConfigs: true, ManageACLs: true, ManageSCRAM: true, ManageQuotas: true,
		ManageGroups: true, BrowseMessages: true, Reassign: true, ElectLeaders: true, Cordon: true,
		UnregisterBrokers: true, DescribeQuorum: true, RemoveVoters: !staticQuorum, FinalizeFeatures: true,
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
		caps.AddControllers = !staticQuorum
		caps.RestartNodes, caps.UpgradeVersion, caps.MigrateQuorum = true, true, staticQuorum
	case store.ProviderStrimzi:
		caps.ScaleNodePools, caps.UpgradeVersion = true, true
		caps.ManageSCRAM = false
		caps.Notes = append(caps.Notes, "Strimzi manages pods and users; controller quorum changes wait for dynamic quorum support in Strimzi.")
	default:
		if c.Flavor == store.FlavorApache {
			caps.Notes = append(caps.Notes, "Install brokerium-agent on the nodes to manage brokers, controllers and upgrades.")
		}
	}
	if staticQuorum && c.Provider != store.ProviderAgent && c.Provider != store.ProviderDocker && c.Flavor == store.FlavorApache {
		caps.Notes = append(caps.Notes, "Static controller quorum: controllers cannot be added or removed until it is migrated (Kafka 4.1+).")
	}
	return caps
}
