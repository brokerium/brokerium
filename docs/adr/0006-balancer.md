# ADR 0006: Own rebalancing planner in Go

- Status: accepted
- Date: 2026-10-15

## Context

Cruise Control needs a metrics-reporter jar on every broker, a separate JVM per cluster and
lags behind new Kafka releases; it knows nothing about controllers or node lifecycle. The
owner wants to control rebalancing directly and to balance a cluster evenly in one click.

## Decision

- `internal/balancer` is a pure, deterministic planner with no I/O. Input: a cluster model
  (brokers with racks, capacity and state; partitions with replicas, leader, size and byte
  rate). Output: a plan of replica and leader moves.
- Hard goals: replication factor preserved, no two replicas on one broker, rack awareness
  when racks are known, no replicas on excluded or cordoned brokers, disk capacity limits.
- Soft goals, in priority order: replica count, disk usage, leader count, incoming byte rate
  and per-topic spread across brokers.
- The planner prefers moves that transfer the least data and stops when improvements fall
  below a threshold or a move budget is reached.
- Cruise Control may be added later as an alternative backend behind the same interface.

## Consequences

- Rebalancing works without installing anything on brokers.
- CPU-based goals are not available until per-broker CPU metrics are collected (ADR 0010).
