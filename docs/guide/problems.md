# Problems (Enterprise)

Health shows what is broken now. **Problems** also show the settings and the placement that break
later or hide a risk: topics whose replication factor cannot satisfy `min.insync.replicas`,
internal topics with too few replicas, brokers without data, agents that stopped reporting. Each
problem explains the risk, names the values behind it and, where Brokerium can fix it, offers an
operation with its parameters filled in ([ADR 0023](../adr/0023-problems.md)).

Problems are a feature of [Brokerium Enterprise](enterprise.md) (feature id `problems`). The
cluster page has a **Problems** tab, and the overview has a **Problems** column with the number of
critical, warning and info problems of every cluster.

## How problems are found

Brokerium evaluates the rules after every snapshot of a cluster: every 15 seconds by default
(`BROKERIUM_SNAPSHOT_INTERVAL`), when someone opens or refreshes the cluster and while operations
run. The rules read the snapshot, the health of the cluster and the agent nodes assigned to it;
apart from `agent-not-seen` they work without agents.

The problems themselves are not stored. The database keeps a state for each problem: when it was
first and last seen, who acknowledged it and until when it is muted. A problem is identified by its
code and its object (the cluster, a broker, a topic or an agent node), so its state holds while the
problem comes and goes, for example while a broker restarts. States of problems that were not seen
for 30 days are deleted, and so are the states of deleted clusters.

Severities:

- **critical:** writes fail or data is at risk now;
- **warning:** the next broker failure, restart or growth turns the setting into an outage or data
  loss;
- **info:** works, but a better setting is known.

## Rules

| Code | Severity | Finds | Fixes |
| --- | --- | --- | --- |
| `topic-rf-below-min-isr` | critical | A topic whose replication factor is below its effective `min.insync.replicas` (the value of the topic or the broker default): every write with `acks=all` fails with `NOT_ENOUGH_REPLICAS`. | Raise the replication factor; or set `min.insync.replicas` to one below the replication factor, at least 1. |
| `internal-topic-rf-low` | warning | `__consumer_offsets`, `__transaction_state` or `__share_group_state` with fewer than 3 replicas on a cluster with 3 or more brokers. | Raise the replication factor to 3. Also set the broker setting that creates the topic (`offsets.topic.replication.factor`, `transaction.state.log.replication.factor`, `share.coordinator.state.topic.replication.factor`) in `server.properties`. |
| `topic-rf-one` | warning | A topic with one replica per partition on a cluster with more than one serving broker. | Raise the replication factor. |
| `topic-rf-equals-min-isr` | warning | A replication factor equal to `min.insync.replicas` and above 1: while one broker of a partition is down, writes with `acks=all` to it fail. | Raise the replication factor; or lower `min.insync.replicas` by one. |
| `topic-min-isr-one` | info | `min.insync.replicas` 1 with 2 or more replicas: `acks=all` acknowledges writes that only the leader has. | Set `min.insync.replicas` to 2 (replication factor 3 or more), or raise the replication factor to 3 first. |
| `consumer-offsets-partitions` | info | `__consumer_offsets` with fewer partitions than brokers: some brokers coordinate no consumer groups. Kafka cannot change the number of partitions of this topic. | None. |
| `broker-without-replicas` | warning | A serving, uncordoned broker without replicas while the cluster has at least as many replicas as such brokers, usually a broker added without a rebalance. | Rebalance. |
| `replica-skew` | info | Brokers with more than 1.3 times the mean number of replicas and at least 10 replicas above the mean. | Rebalance. |
| `data-skew` | warning | Brokers with more than 1.5 times the mean data and more than 1 GiB of data. | Rebalance. |
| `agent-not-seen` | warning | A node of the cluster whose agent was not seen for more than a day, or never connected in its first day. | None: check the `brokerium-agent` service on the host. |
| `health-<code>` | of the issue | Every current health issue of the cluster, for example `health-under_min_isr` or `health-broker_fenced`, so that the tab lists everything in one place. | None. |

Notes on the rules:

- A topic gets at most one of the replication findings: the first of the table that applies.
- The `min.insync.replicas` rules apply only when Brokerium could read the configs of the topic.
- Raising the replication factor targets 3, or `min.insync.replicas` + 1 when that is higher, but
  never more than the brokers that serve and are not cordoned.
- The placement rules (`broker-without-replicas`, `replica-skew`, `data-skew`) compare the brokers
  that serve and are not cordoned. They wait while partitions are reassigned and skip clusters whose
  service manages placement (Confluent Cloud, Aiven).

## Fixes

A problem can offer fixes: operations with parameters that the server computed. The button of a
problem carries the label of the recommended fix, for example **Raise the replication factor to
3**; **Other fixes** lists the alternatives. A fix opens its parameters, which you can change, for
example the replication factor or the throttle, and then the plan preview of the operation. Nothing
changes until you confirm the plan, and the operation runs like every other one: step by step, behind
health gates, recorded in the audit log ([Operations](operations.md)).

The fixes use these operations:

- `change-replication-factor` (Enterprise): raises or lowers the replication factor of topics,
  internal topics included;
- `alter-topic-configs` (Enterprise): sets or removes topic configs such as `min.insync.replicas`;
- `align-broker-configs` (Enterprise): gives dynamic broker configs one cluster-wide value;
- `rebalance`.

## Acknowledging and muting

- **Acknowledge** marks a problem as known: it stays in the list with the name of the user who
  acknowledged it. **Unacknowledge** removes the mark.
- **Mute** hides a problem for an hour, a day, 7 days or 30 days (the API takes any time in the
  future). **Show muted** lists muted problems, and the overview counts them apart from the others.
  **Unmute** shows the problem again.

Viewers see problems; operators and admins acknowledge, mute and plan fixes. The audit log records
`problem.acknowledge`, `problem.unacknowledge`, `problem.mute` and `problem.unmute`.

## API

| Request | Role | What it does |
| --- | --- | --- |
| `GET /api/v1/clusters/{id}/problems` | viewer | The current problems of a cluster with their states; muted problems only with `?muted=true`. |
| `GET /api/v1/problems` | viewer | The number of problems of every cluster by severity, muted problems counted apart. |
| `POST /api/v1/clusters/{id}/problems/{fingerprint}/acknowledge` | operator | Acknowledges a current problem; `DELETE` removes the acknowledgement. |
| `POST /api/v1/clusters/{id}/problems/{fingerprint}/mute` | operator | Mutes a current problem until `until` (RFC 3339); `DELETE` unmutes it. |

Without a license that covers `problems`, these routes and the Enterprise fix operations answer 403
`license_required`. See the [REST API](../../api/openapi.yaml) for the schemas.
