# ADR 0028: Takeover of existing nodes

- Status: accepted
- Date: 2026-11-20

## Context

Existing Kafka nodes can be adopted: the agent starts with `--kafka-config`, Brokerium restarts and
upgrades them, but never writes their `server.properties`, and their own systemd unit keeps
running Kafka. Settings, listeners, security, the exporter and the upgrades of ZooKeeper-based
clusters all need Brokerium to own the configuration, the unit and the installation of a node.
Operators also want to leave tools such as hand edits and Ansible behind for these nodes.

## Decision

- **Import.** `takeover-nodes` plans per node: the agent reads the operator's `server.properties`,
  the unit that runs Kafka (`ExecStart`, `User`, `Environment`, `EnvironmentFile`), the JVM options
  of the running process including a JMX exporter javaagent, and the installed Kafka release. The
  server turns this into the node's part of the cluster configuration
  ([ADR 0025](0025-kafka-configuration.md)) and the listener model
  ([ADR 0026](0026-listeners-and-security.md)), keeping every key, and shows the rendered result
  next to the original as a diff.
- **Confirmation.** The operator confirms the diff by typing the cluster name. From then on
  Brokerium owns the file; the plan says that other tools must stop changing it.
- **Installation.** The agent installs the same Kafka release into its own directory, so the node
  runs exactly one Kafka version from a clean `libs` directory. Data directories stay where they
  are.
- **Unit switch.** One node at a time: stop the operator's unit, disable it, start
  `brokerium-kafka.service` with the rendered configuration, wait until the broker is back with
  every replica in sync and check the running process (PID and version), then the next node after
  the health gate. If the new unit does not come up, the step starts the old unit again and the
  operation stops. The install script grants the agent `stop`, `start`, `disable` and `enable` of
  the unit named in `BROKERIUM_AGENT_TAKEOVER_UNIT`.
- **After the takeover** a node is managed like a node Brokerium created: configuration changes,
  restarts and upgrades re-render its file, and `migrate-quorum` rewrites the static voters to
  bootstrap servers.
- **Edition.** Takeover is the Enterprise feature `node-takeover`. Adoption as it is stays in
  Community.

## Consequences

- Clusters installed by hand or by other tools can be managed entirely by Brokerium, one node at a
  time and without downtime.
- A failed switch leaves the node running on its old unit.
- Rolling the takeover back later means re-creating the operator's unit and configuration outside
  Brokerium.
