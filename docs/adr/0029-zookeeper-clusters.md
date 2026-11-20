# ADR 0029: ZooKeeper-based clusters: adoption, upgrades and the migration to KRaft

- Status: accepted
- Date: 2026-11-20

## Context

From 0.4.0 Brokerium watches ZooKeeper-based clusters from Kafka 3.5 ([ADR 0022](0022-kafka-versions-and-modes.md)).
Kafka 4.0 removed ZooKeeper, so such a cluster reaches the current releases only through a bridge
release: Kafka 3.9.1 or later with `inter.broker.protocol.version=3.9`, a migration of the metadata
to a KRaft quorum on dedicated controllers (KIP-866 does not migrate to combined nodes), then the
upgrade to 4.x. Each step is a rolling change of every broker, done by hand today.

## Decision

- **Adoption.** The agent reads `broker.id` from `server.properties` or from `meta.properties`
  version 0, and `zookeeper.connect`. Nodes get the role `zookeeper-broker` (a new value of the
  role check and of adoption), and the cluster stores the ZooKeeper connection string and the
  ZooKeeper client settings of its brokers (`zookeeper.ssl.*`, `zookeeper.set.acl`, the JAAS
  `Client` section, sealed). Managing them needs the takeover of
  [ADR 0028](0028-takeover-of-existing-nodes.md); the ZooKeeper ensemble itself stays outside
  Brokerium.
- **Releases and compatibility as data.** The server lists the Apache Kafka releases from the
  mirror or the archive (cached for a day, with a built-in list for offline installations). A
  compatibility table in `internal/kafkacompat` describes, per release line, the Java versions of
  brokers (3.x: 11 and 17; 4.x: 17 and later), the upgrade paths, the protocol and metadata
  versions and the known defects that the plans warn about (such as KAFKA-20979). The current
  version of a cluster comes from the versions its agents run, not from the settings.
- **Hints.** The Upgrade tab proposes the next targets in order: the latest patch of the current
  line; for ZooKeeper-based clusters the latest 3.9 release and then the migration; for KRaft
  clusters the next allowed line up to the latest 4.x. Each hint says why.
- **ZooKeeper upgrades in two passes.** `rolling-upgrade` of a ZooKeeper-based cluster first
  installs the new release with `inter.broker.protocol.version` pinned to the current value, one
  broker at a time with health gates, then raises the protocol version in a second rolling
  restart. Mixed versions are aligned before.
- **`migrate-zookeeper`.** The operator chooses three free agent hosts for the controllers (one
  for tests); node ids start at 3000 so that they cannot meet a `broker.id`. Phases, each with
  checks before it and health gates between nodes:
  1. Controllers: format the first one `--standalone` with the cluster id of the ZooKeeper
     cluster, start it with `zookeeper.metadata.migration.enable=true`, `zookeeper.connect` and the
     inter-broker listener of the brokers, then add the other two as voters (dynamic quorum).
  2. Brokers into migration mode: one by one with the controller quorum, the controller listener
     and the migration flag; the protocol version stays 3.9. The migration starts after the last
     broker.
  3. Wait until the migration completes: `ZkMigrationState` from the JMX exporter that Brokerium
     installs on the controllers, and the line "Completed migration of metadata from Zookeeper to
     KRaft" in the log of the active controller.
  4. Brokers into KRaft mode: `process.roles=broker`, `node.id` instead of `broker.id`, no
     ZooKeeper settings, `StandardAuthorizer` instead of `AclAuthorizer`.
  5. Finalize: the controllers leave migration mode one by one. This step is irreversible and needs
     the cluster name typed; the plan suggests waiting some days in phase 4 first.

  Until phase 5 the operation can be cancelled: Brokerium rolls the brokers back to ZooKeeper mode
  and stops the controllers, and shows the `zookeeper-shell.sh` commands of the Kafka procedure for
  the controller epoch, `/controller` and `/migration`, because it has no ZooKeeper client.
- **After the migration** the cluster is a KRaft cluster with dedicated controllers: the usual
  upgrade to 4.x and `finalize-upgrade` follow, and the problems tell when ZooKeeper is no longer
  used and can be turned off.
- **Edition.** Adoption of ZooKeeper-based brokers, their upgrades and the migration are the
  Enterprise feature `zookeeper`; upgrade hints for KRaft clusters belong to `node-takeover`.

## Consequences

- A ZooKeeper-based cluster on 3.5 reaches the latest 4.x in Brokerium without downtime, with
  three more hosts during and after the migration.
- The revert during a migration still needs the operator in ZooKeeper.
- The compatibility table grows with every Kafka release.
