# ADR 0023: Problems

- Status: accepted
- Date: 2026-11-20

## Context

Health covers what is broken now: offline and under-replicated partitions, the quorum, disks.
Clusters also carry settings that break later or hide a risk: topics whose replication factor
cannot satisfy min.insync.replicas, a replication factor equal to min.insync.replicas that stops
writes on every broker restart, internal topics with too few replicas, brokers without data,
settings that differ between brokers, brokers on different versions, listeners without
authentication. Operators find these by reading configurations by hand, cluster by cluster.

## Decision

- **Package.** `ee/problems` (Enterprise feature `problems`) evaluates rules after every
  snapshot of a cluster. Rules read the snapshot, the health issues, the configurations of all
  brokers (DescribeConfigs, refreshed every 5 minutes and cached), the nodes and their agents.
- **Finding.** A stable code (for example `topic-rf-below-min-isr`), a severity (`critical`,
  `warning`, `info`), the object (cluster, broker, topic or node), a title, an explanation, the
  evidence (the values that triggered it), a recommendation and fixes: none, one, or alternatives
  after the recommended first one (raise the replication factor, or lower min.insync.replicas).
  The fingerprint of the code and the object identifies a finding across snapshots. A topic gets
  at most one of the replication findings, the most severe that applies.
- **State.** The table `problem_states` keeps, per cluster and fingerprint, when a finding was
  first and last seen, who acknowledged it and when, and until when it is muted. Findings
  themselves are not stored; every snapshot evaluates them again, in the background after each
  refresh, and the last-seen time is written at most once a minute. A muted finding is hidden
  until the time passes; an acknowledged one stays visible with its mark, which can be removed.
  Both hold while a finding comes and goes. States not seen for 30 days are deleted, and those of
  a deleted cluster go with it. The table belongs to the shared migrations, so both editions use
  one database.
- **Rules in 0.4.0**, all without agents:
  - a topic replication factor below the effective min.insync.replicas (critical), equal to it
    (warning), or 1 on a cluster with more brokers (warning); min.insync.replicas 1 with more
    replicas (info);
  - internal topics (`__consumer_offsets`, `__transaction_state`, `__share_group_state`) with
    fewer than 3 replicas on a cluster with 3 or more brokers, and the broker settings that create
    them so (`offsets.topic.replication.factor`, `transaction.state.log.replication.factor`,
    `transaction.state.log.min.isr`);
  - `__consumer_offsets` with fewer partitions than brokers (info, because Kafka cannot change
    it);
  - alive brokers without replicas, and data or replicas skewed between brokers;
  - configurations that differ between brokers, except per-broker keys such as ids, listeners,
    log directories, racks and TLS stores;
  - brokers on different versions; a metadata.version or inter.broker.protocol.version behind the
    software;
  - PLAINTEXT listeners while the authorizer allows everyone without ACLs; SASL PLAIN without TLS;
  - agents not seen for more than a day;
  - the current health issues, so the tab lists everything in one place.
- **Fixes.** A fix names an operation and its parameters. The dashboard opens the parameters
  filled in, still editable, and then the plan preview; the usual confirmation, steps, health
  gates and audit apply. The health gates of `change-replication-factor` do not count the
  under-replicated and under-min-ISR partitions of the topics it changes, which are often the
  reason for the change; everything else in the cluster must be healthy. Fixes in 0.4.0:
  - `change-replication-factor` (new): raises or lowers the replication factor of topics,
    internal topics included, with throttled reassignments and racks taken into account;
  - `alter-topic-configs` (new): a planned change of topic configurations such as
    min.insync.replicas;
  - `align-broker-configs` (new): one cluster-wide value instead of differing per-broker values
    of dynamic keys;
  - `rebalance` and `finalize-upgrade`.
- **API.** `GET /api/v1/clusters/{id}/problems`, `POST .../problems/{fingerprint}/acknowledge`
  and `DELETE .../problems/{fingerprint}/acknowledge`, `POST .../problems/{fingerprint}/mute` with
  an end time and `DELETE .../problems/{fingerprint}/mute`, plus `GET /api/v1/problems` with the
  counts per cluster for the overview. Viewers read; operators acknowledge and mute, which the
  audit log records.
- **Dashboard.** A Problems tab for every cluster and a problems column on the overview, both in
  `web/src/ee`.

## Consequences

- Teams see the risks of their clusters with an explanation and a planned fix, without agents.
- Rules that need facts from the hosts, such as duplicate Kafka jars, kernel limits or growing
  log files, arrive with the host facts of the agent in 0.5.0.
- Rules are added without migrations. Their codes are stable, so mutes and alerts on the
  Prometheus endpoint ([ADR 0024](0024-monitoring-through-prometheus.md)) keep working.
