# ADR 0010: Agentless metrics first

- Status: accepted
- Date: 2026-10-15

## Context

Balancing needs partition sizes and traffic. Broker JMX metrics require a JMX port, a
javaagent or a metrics-reporter jar on brokers, which many users cannot install.

## Decision

- The server samples every cluster over the Kafka protocol:
  - `DescribeLogDirs` for partition sizes per broker and disk usage;
  - `ListOffsets` (latest) for message rates per partition;
  - byte rates are estimated from the growth of partition sizes between samples.
- Samples are kept in memory per cluster; derived rates feed the planner and the dashboard.
- When an agent manages a node, it may expose richer metrics (Prometheus JMX exporter) later;
  external Prometheus can be added as an optional source.

## Consequences

- Rebalancing and the dashboard work out of the box on any cluster, including managed ones.
- Rates are estimates; retention deletions can make size growth negative and are ignored.
