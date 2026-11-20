# Operations

Every change to cluster topology is an **operation**. Planning is a dry run: the server
computes the steps and, for data moves, the before and after distribution. Nothing changes
until you confirm; irreversible operations ask for the cluster name. Steps run one at a time
and are stored in PostgreSQL, so an operation continues after a server restart. You can pause,
resume or cancel; cleanup steps (for example removing replication throttles) run in every
case. Only one operation runs per cluster. Each step, with its progress, result or error, is
stored with its operation and shown on the operation page. The audit log records who planned,
confirmed, paused, resumed, cancelled or discarded an operation (`operation.plan`,
`operation.confirm`, `operation.pause`, `operation.resume`, `operation.cancel`,
`operation.discard`) and, when it ends, its result as `operation.<type>`, for example
`operation.rolling-restart`, next to the other changes made through the API, such as
`cluster.update` or `node.update`.

## Safety rules

- A health gate runs before the first step and after every batch or node: no offline
  partitions, nothing under `min.insync.replicas`, no under-replicated partitions except
  those being moved, a quorum leader (the controller broker on ZooKeeper-based clusters), no
  lagging voters and no fenced or offline broker except the one the operation replaces.
- Before a batch of replica moves, no broker that receives replicas in the batch may have a
  volume 90% full or more. Brokers that only give replicas away may be full, so a full broker
  can be drained or rebalanced onto new brokers; restarts, upgrades and quorum changes add no
  data and ignore disks.
- Reassignments carry full target replica lists, run in batches, and are throttled on the
  brokers and topics involved; throttles are always removed afterwards.
- Cordoned and offline brokers never receive replicas; racks are respected.
- One quorum change at a time: a new controller catches up as an observer before it becomes a
  voter; the last voter is never removed.
- Storage is formatted with the Kafka release that will run the node.
- Kafka starts only on hosts where no other Kafka process runs, such as the operator's own unit
  of an adopted node; plans warn about such hosts and their steps fail with the process id and
  its user. After a restart or an upgrade the agent must report a new Kafka process that runs
  the expected release (agents from 0.3.2 on, which read the process from `/proc`).

## Types

| Operation | What it does |
| --- | --- |
| Rebalance | Plans moves with the built-in balancer (hard goals: replication factor, racks, capacity, excluded brokers; soft goals: replicas, leaders, data, traffic and per-topic spread), then executes them throttled in batches and elects preferred leaders. Modes: even, replicas only, leaders only. |
| Move partitions | Your own target replica lists, with the same preview and execution. |
| Elect preferred leaders | Moves leadership back to the first replica. |
| Cordon broker | Excludes a broker from new replicas (KIP-1066, Kafka 4.3+ on every broker). |
| Drain / decommission broker | Cordons, moves every replica away, then (decommission) stops the node, releases its agent host and unregisters the broker. Kafka 4.3+ on every broker. Refused for [combined nodes](clusters.md#combined-nodes). |
| Unregister broker | Removes a dead broker's registration. |
| Add brokers | Starts brokers on agent hosts you choose or in new Docker containers, waits for them, rebalances onto them. |
| Replace broker | For a lost broker: the same id on an empty disk, on its host or another one (followers re-replicate), or a new id on another host with its replicas moved over and the old id unregistered. Refused for combined nodes. |
| Add / remove controller | Grows or shrinks a dynamic quorum one voter at a time; a new controller runs on an agent host you choose. Combined nodes are not removed. |
| Migrate quorum | Moves a static quorum to a dynamic one (`kraft.version` 1, Kafka 4.1+ on every node). Irreversible. |
| Rolling restart | Restarts nodes one at a time: controllers and combined nodes first with the quorum leader last, then brokers. Each node must come back as a new Kafka process; a combined node also waits for its replicas to be in sync. |
| Rolling upgrade | Downloads the release on every node, then restarts them one at a time on it and checks that each runs the new release. Warns about known defects (KAFKA-20979 in 4.1.0 to 4.3.1) and checks Java 17+. |
| Finalize upgrade | Raises `metadata.version` and other features. Irreversible. |
| Scale node pool | Strimzi broker pools, see [clusters](clusters.md#strimzi). |
| Create / delete cluster nodes | Creates the controllers, brokers and combined nodes of a cluster on agent hosts you choose ([agent clusters](clusters.md#agent-clusters)) or in Docker, on Kafka 4.2.0 or later; deletes Docker lab clusters. |

Which operations a cluster offers depends on its Kafka mode and the versions of its brokers
([Kafka versions and modes](clusters.md#kafka-versions-and-modes)): ZooKeeper-based clusters
offer rebalancing, partition moves and leader elections.

The runbooks in [docs/runbooks](../runbooks/) show the same procedures with the Kafka CLI, and
[test/e2e](../../test/e2e) runs every scenario against the lab.
