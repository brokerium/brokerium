# ADR 0021: Combined broker and controller nodes

- Status: accepted
- Date: 2026-11-18

## Context

Kafka can run the broker and the controller of a node in one process
(`process.roles=broker,controller`). Small clusters for development, tests and evaluations
need fewer hosts that way, and Kafka's own quickstart runs a single combined node. Apache Kafka
does not recommend combined nodes for critical clusters: a busy broker slows the quorum down,
both roles fail and restart together, and the controllers cannot be scaled apart from the
brokers. Migration 0005 ([ADR 0018](0018-explicit-node-assignment.md)) allowed the node role
`combined`, which operations refused until now.

## Decision

- A combined node renders one `server.properties` with `process.roles=broker,controller`: the
  client listeners of a broker (port 9092 on the advertised host; on Docker the internal listener
  and the listener published at `localhost:<port base + node id>`) and the controller listener
  on port 9093. Agent and Docker clusters share the rendering and the quorum logic. It runs with
  the broker heap.
- A combined node counts as a voter and as a broker. It takes its id from the voter range, is
  among the initial controllers, the quorum bootstrap servers and the static voters, and among
  the bootstrap servers and the controller servers the server records. The replication factor of
  a new cluster counts it as a broker.
- `bootstrap-cluster` takes the role `combined` for agent hosts and `combined: <count>` for
  Docker clusters, where voters get the ids from 1 (controllers before combined nodes) and
  brokers the ids from 101. Voters start before the dedicated brokers. The plan warns about
  combined nodes. Docker containers of combined nodes are named `<cluster>-cb<id>` and publish
  their controller listener at `localhost:<port base + 900 + node id>`.
- Rolling restarts, upgrades and quorum migrations restart combined nodes in the controller
  phase, followers first and the quorum leader last, before the dedicated brokers; each restart
  of a combined node also waits until its broker is back and every replica is in sync.
- `add-broker` and `add-controller` keep adding dedicated nodes. `replace-broker`,
  `drain-broker` (also with `decommission`) and `remove-controller` refuse a combined node with a
  message that names it: they would stop, replace or empty one role while the other one keeps
  running, and a replacement would give a voter a new directory id. The node managers refuse the
  Kafka-level operations through a hook of the operations engine (`NodeChecker`), which also
  detects a combined node Brokerium does not run by its id being both a registered broker and
  a voter. Deleting the nodes of a Docker cluster removes combined containers like the others.
- Adopted nodes with `process.roles=broker,controller` become combined nodes.

## Consequences

- Small clusters need one host per node instead of separate controller hosts.
- A combined node cannot be moved, replaced or retired by Brokerium. A cluster outgrows its
  combined nodes by adding dedicated brokers and controllers and moving the partitions off the
  combined nodes; taking a combined node out of the quorum and the cluster is done with the
  Kafka tools ([clusters guide](../guide/clusters.md#combined-nodes)).
- Brokers with ids from 901 in a Docker cluster with combined nodes would publish the port of a
  combined controller listener; Docker then refuses to start the container.
