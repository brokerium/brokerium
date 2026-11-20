# ADR 0025: Kafka configuration of clusters

- Status: accepted
- Date: 2026-11-20

## Context

Brokerium renders `server.properties` of the nodes it runs from a fixed map in
`internal/nodes/render.go`, with values tuned for the lab: 16 MiB segments, 6 partitions per new
topic, no initial rebalance delay, a 512 MB heap. Cluster settings are a free JSON map that the
API stores as given. Operators can change the dynamic configuration of one broker and of topics,
but they cannot see the defaults of their Kafka version, change static settings, or set storage,
retention, threads, buffers or topic defaults when they create a cluster.

## Decision

- **Catalog.** For every supported Kafka minor release from 3.5, the server embeds a catalog of
  broker and topic keys: type, default, documentation, sensitivity, whether the key is read-only
  and its update mode (`read-only`, `per-broker`, `cluster-wide`). `make catalog` generates it in
  the lab: it starts each release, reads DescribeConfigs with documentation and synonyms from a
  broker and a new topic, and takes the update modes from the configuration pages of the Kafka
  documentation of that release. The catalog is data in `internal/kafkaconfig`; adding a release
  means generating one more file.
- **Typed configuration.** A cluster has a desired configuration: values for broker keys for the
  whole cluster and, where a key is per-broker, for single nodes. Brokerium owns the keys it
  derives from other models (`node.id`, `process.roles`, the quorum, listeners, `log.dirs` and
  `metadata.log.dir`) and refuses them in the configuration. Unknown keys are refused unless the
  catalog of the cluster's version lists them.
- **Defaults.** An empty field means the default of the cluster's Kafka version and is shown
  greyed with its source. Clusters that Brokerium creates keep these reliability values unless
  the operator changes them: `default.replication.factor` and the replication factor of internal
  topics 3 (or the number of brokers when smaller), `min.insync.replicas` and
  `transaction.state.log.min.isr` 2 (or 1 with one broker), `auto.create.topics.enable=false`.
  Docker clusters keep the lab values. Agent clusters created before 0.5.0 keep the values they
  run with: a migration writes them into their configuration, so nothing changes on the next
  restart.
- **Changes are plans.** `apply-configuration` compares the desired with the current
  configuration. Dynamic keys change through IncrementalAlterConfigs, cluster-wide or per broker,
  without a restart. Static keys change the rendered `server.properties` of every managed node,
  followed by a rolling restart with health gates. Clusters without agents only change dynamic
  keys; the dashboard disables the static ones and says why.
- **Heap.** Broker and controller heaps are part of the configuration (`-Xms` and `-Xmx`, by
  default the 1 GB of `kafka-server-start.sh` for clusters created from 0.5.0).
- **Dashboard.** A Configuration tab with sections: storage and retention, log cleaner, threads
  and socket buffers, topic and group defaults (`auto.create.topics.enable`, `num.partitions`,
  `group.initial.rebalance.delay.ms`), heap, paths, listeners. The creation wizard has the same
  sections with the defaults of the chosen version. Configuration tables of brokers and topics
  show the default, the documentation and whether a key is read-only.
- **Edition.** The catalog, the configuration API, the tab and the wizard sections are the
  Enterprise feature `configuration`. Community keeps rendering clusters from the built-in
  defaults; correcting those defaults for agent clusters (Kafka defaults except the reliability
  values above) is a fix in both editions.

## Consequences

- Operators set the configuration of a cluster in one place and see what Kafka would use
  without them.
- Every change has a plan; static changes cost a rolling restart, dynamic ones do not.
- The catalog has to be generated for every new Kafka minor release; a cluster on a release
  without a catalog uses the catalog of the closest older release and says so.
