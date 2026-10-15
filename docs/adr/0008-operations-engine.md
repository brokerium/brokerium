# ADR 0008: Durable operations engine with safety gates

- Status: accepted
- Date: 2026-10-15

## Context

Scaling, draining, replacing, rebalancing and upgrading are long, multi-step changes to
production clusters. They must survive server restarts, be observable step by step, and
never put data at risk.

## Decision

- Every change to a cluster is an **operation** stored in PostgreSQL with ordered **steps**.
  An operation goes through `planned -> running -> succeeded | failed | cancelled`, and can
  be `paused` between steps.
- Only one operation runs per cluster at a time (a database-level lock).
- Steps are idempotent; after a restart the engine re-runs the current step.
- Safety rules enforced by the engine and by every operation:
  1. Plan and preview first, then explicit confirmation, then execution, then audit record.
  2. Health gate before each step: no offline partitions, no under-replicated partitions
     (except those being moved), no partitions under `min.insync.replicas`, a quorum leader
     exists and voters are caught up, no other operation on the cluster.
  3. Every reassignment is throttled; throttles are removed whatever the outcome.
  4. Irreversible actions (finalizing `metadata.version` or `kraft.version`, unregistering
     nodes, wiping disks) require typing the cluster name.
  5. Every operation supports a dry run that produces the plan without changing anything.

## Consequences

- Operations are explicit and auditable; a declarative "desired cluster spec" may be built
  on top later.
