// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0

// Package agenthost is the layout of a host that runs brokerium-agent under systemd: the
// accounts, units and paths that the install script sets up, the agent uses and the uninstall
// script removes.
package agenthost

const (
	// Account runs the agent; it is a member of the group of KafkaAccount.
	Account = "brokerium-agent"
	// KafkaAccount runs Kafka.
	KafkaAccount = "kafka"

	Binary      = "/usr/local/bin/brokerium-agent"
	EnvFile     = "/etc/brokerium-agent.env"
	SudoersFile = "/etc/sudoers.d/brokerium-agent"
	UnitDir     = "/etc/systemd/system"
	AgentUnit   = "brokerium-agent.service"
	KafkaUnit   = "brokerium-kafka.service"

	// StateDir holds the identity of the agent, the configuration of Kafka and its logs.
	StateDir = "/var/lib/brokerium-agent"
	// KafkaDir holds the Kafka releases the agent installs.
	KafkaDir = "/opt/brokerium/kafka"
	// DataDir is the default Kafka data directory (log.dirs).
	DataDir = "/var/lib/kafka/data"

	// ExitNodeDeleted is the exit status of an agent whose node was deleted in Brokerium. The
	// agent unit lists it in RestartPreventExitStatus, so systemd does not start it again.
	ExitNodeDeleted = 6
)
