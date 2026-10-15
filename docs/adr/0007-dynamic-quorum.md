# ADR 0007: Dynamic KRaft quorum by default

- Status: accepted
- Date: 2026-10-15

## Context

With a static quorum (`controller.quorum.voters`) controllers cannot be added or removed
without downtime. Kafka 3.9 introduced dynamic quorums (KIP-853), Kafka 4.1 added the
static-to-dynamic migration (`kraft.version=1`, irreversible), Kafka 4.2 added controller
auto-join, and Kafka 4.4 adds unregistering controllers (KIP-1312).

## Decision

- Clusters created by Brokerium always use a dynamic quorum
  (`controller.quorum.bootstrap.servers`, storage formatted with `--initial-controllers` on
  the initial controllers and `--no-initial-controllers` on every later node).
- Brokerium never enables `controller.quorum.auto.join.enable`; voters are added and
  removed explicitly, one change at a time, after the new node has caught up.
- Replacing a controller adds the new voter first and removes the old one second
  (3 -> 4 -> 3), so fault tolerance never drops.
- Migrating a static quorum is a separate, explicitly confirmed, irreversible operation
  available for Kafka 4.1 and later.

## Consequences

- The official `apache/kafka` image formats storage only for static quorums, so lab nodes
  and agent-managed nodes format storage themselves.
- `add-controller` needs the new node's `directory.id`, which only the agent can read from
  `meta.properties`.
