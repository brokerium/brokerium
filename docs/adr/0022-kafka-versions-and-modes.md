# ADR 0022: Kafka versions and modes

- Status: accepted
- Date: 2026-11-20

## Context

Brokerium was written for KRaft clusters and never checked the Kafka version. Capabilities
depend on the provider, the managed-service flavor and the quorum type, although
[ADR 0004](0004-architecture.md) bases them on the Kafka version too. A ZooKeeper-based cluster
connects and its data plane works, because the client negotiates the protocol versions, but its
health reports a missing quorum on every snapshot and the dashboard offers KRaft operations that
fail on it. Operators run clusters from Kafka 3.5 in both modes and want to see them in
Brokerium.

## Decision

- **Supported versions.** Existing clusters from Kafka 3.5.0, in ZooKeeper or KRaft mode.
  Clusters that Brokerium creates from Kafka 4.2.0; `bootstrap-cluster` refuses older versions.
- **Detection per broker**, without agents:
  - ApiVersions of each broker on every snapshot: the version range from franz-go's `kversion`
    (for example `3.5`, or `3.4 to 3.5` when the protocol versions do not decide), and the mode:
    a broker that offers the ZooKeeper inter-broker APIs (LeaderAndIsr) runs in ZooKeeper mode;
  - DescribeConfigs of each broker, refreshed every 5 minutes: `process.roles`,
    `zookeeper.connect`, `zookeeper.metadata.migration.enable` and
    `inter.broker.protocol.version`;
  - an agent refines the version of its node with the exact release it runs.

  The cluster mode is `kraft` when every broker runs KRaft without the migration flag,
  `zookeeper` when every broker runs ZooKeeper, and `migration` otherwise. The snapshot carries
  the mode, the version of each broker and whether the versions differ; the API and the
  dashboard show them.
- **Capabilities by mode and version:**
  - ZooKeeper and migration mode: no quorum description, controller changes, broker
    unregistration, feature finalization, cordoning or quorum migration;
  - cordoning (KIP-1066) needs every broker on Kafka 4.3 or later, and so does `drain-broker`,
    which cordons first;
  - `migrate-quorum` needs Kafka 4.1 or later, checked by its planner too; adding and removing
    controllers needs a dynamic quorum;
  - a KRaft cluster without the `kraft.version` feature (Kafka 3.3 to 3.8) has a static quorum.

  The dashboard shows tabs and actions by capabilities.
- **Health.**
  - ZooKeeper-based clusters: the active controller comes from Metadata and `no_controller` is
    critical; the quorum checks do not apply and do not make the snapshot partial.
  - The metadata.version levels 1 to 6 get their names (3.0-IV1 to 3.3-IV2).
  - The health gate of operations also stops on fenced brokers that the operation does not stop
    itself and on disks above 90%.
- All of this is Community: it makes existing features correct on the clusters they meet
  ([ADR 0016](0016-editions-and-license-keys.md), amendment 2026-11-20).

## Consequences

- ZooKeeper-based clusters can be watched and their data plane used from 0.4.0; managing their
  nodes, their upgrades and their migration to KRaft come later in Enterprise.
- Version estimates from ApiVersions name a minor release, not a patch; exact versions need an
  agent.
- Clusters on Kafka 3.5 to 4.2 no longer offer actions their brokers would refuse.
