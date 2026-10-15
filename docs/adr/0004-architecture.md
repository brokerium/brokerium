# ADR 0004: Control plane, optional agent and providers

- Status: accepted
- Date: 2026-10-15

## Context

Clusters run on virtual machines, bare metal, Kubernetes and managed services. Data-plane
operations work over the Kafka protocol everywhere, but node lifecycle (install, format,
start, stop, upgrade) needs access to the hosts, which only exists in some environments.

## Decision

- `brokerium-server` is the control plane: API, dashboard, operations engine, rebalancing
  planner and executor, Kafka gateway, agent gateway, users and audit.
- Every cluster has a **provider** that defines how its nodes are managed:
  - `agent`: virtual machines or bare metal running `brokerium-agent`; full lifecycle.
  - `docker`: the local lab; the server creates node containers that run the agent, so the
    lab exercises the same code path as virtual machines.
  - `strimzi`: Kubernetes clusters managed through Strimzi custom resources.
  - `external`: any cluster reachable only over the Kafka protocol, including Amazon MSK,
    Confluent Cloud and Aiven.
- Each cluster exposes a **capability set** computed from its provider, the managed-service
  flavor and the Kafka version (for example `nodes.provision`, `quorum.manage`,
  `partitions.reassign`). The API rejects operations outside the set and the dashboard hides
  them.
- Like Rancher, a cluster is either **created** on registered nodes or **imported** by its
  bootstrap servers; nodes join with a one-line agent install command and a join token.

## Consequences

- Rebalancing and data-plane features work without installing anything on brokers.
- Lifecycle features for existing clusters require adopting their nodes with the agent.
