# Operations

Every change to cluster topology is an **operation**. Planning is a dry run: the server
computes the steps and, for data moves, the before and after distribution. Nothing changes
until you confirm; irreversible operations ask for the cluster name. Steps run one at a time
and are stored in PostgreSQL, so an operation continues after a server restart. You can pause,
resume or cancel; cleanup steps (for example removing replication throttles) run in every
case. Only one operation runs per cluster. Each step and its result are in the audit log.

## Safety rules

- A health gate runs before the first step and after every batch or node: no offline
  partitions, nothing under `min.insync.replicas`, no under-replicated partitions except
  those being moved, a quorum leader, no lagging voters.
- Reassignments carry full target replica lists, run in batches, and are throttled on the
  brokers and topics involved; throttles are always removed afterwards.
- Cordoned and offline brokers never receive replicas; racks are respected.
- One quorum change at a time: a new controller catches up as an observer before it becomes a
  voter; the last voter is never removed.
- Storage is formatted with the Kafka release that will run the node.

## Types

| Operation | What it does |
| --- | --- |
| Rebalance | Plans moves with the built-in balancer (hard goals: replication factor, racks, capacity, excluded brokers; soft goals: replicas, leaders, data, traffic and per-topic spread), then executes them throttled in batches and elects preferred leaders. Modes: even, replicas only, leaders only. |
| Move partitions | Your own target replica lists, with the same preview and execution. |
| Elect preferred leaders | Moves leadership back to the first replica. |
| Cordon broker | Excludes a broker from new replicas (KIP-1066, Kafka 4.3+). |
| Drain / decommission broker | Cordons, moves every replica away, then (decommission) stops the node and unregisters the broker. |
| Unregister broker | Removes a dead broker's registration. |
| Add brokers | Provisions brokers on agents or Docker, waits for them, rebalances onto them. |
| Replace broker | For a lost broker: the same id on an empty disk (followers re-replicate), or a new id with its replicas moved over and the old id unregistered. |
| Add / remove controller | Grows or shrinks a dynamic quorum one voter at a time. |
| Migrate quorum | Moves a static quorum to a dynamic one (`kraft.version` 1, Kafka 4.1+). Irreversible. |
| Rolling restart | Restarts nodes one at a time, controllers first and the quorum leader last. |
| Rolling upgrade | Downloads the release on every node, then restarts them one at a time on it. Warns about known defects (KAFKA-20979 in 4.1.0 to 4.3.1) and checks Java 17+. |
| Finalize upgrade | Raises `metadata.version` and other features. Irreversible. |
| Scale node pool | Strimzi broker pools, see [clusters](clusters.md#strimzi). |
| Create / delete cluster nodes | Docker lab clusters and agent hosts. |

The runbooks in [docs/runbooks](../runbooks/) show the same procedures with the Kafka CLI, and
[test/e2e](../../test/e2e) runs every scenario against the lab.
