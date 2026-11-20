# ADR 0024: Monitoring through Prometheus

- Status: accepted
- Date: 2026-11-20
- Amends: [ADR 0010](0010-metrics.md)

## Context

The dashboard shows rates that the server estimates from offsets and log sizes over the last five
minutes, without history. Teams that run Kafka in production already collect broker metrics with
the Prometheus JMX exporter into their Prometheus and alert there. Brokerium should show those
metrics next to the cluster instead of storing a second copy.

## Decision

- **No metric storage in Brokerium.** The Monitoring tab (Enterprise feature `monitoring`)
  queries the team's Prometheus. Without one it shows the values the server samples now.
- **Data source.** One Prometheus per installation, set by an admin on the monitoring settings
  page or through `BROKERIUM_PROMETHEUS_URL` with basic or bearer authentication and a CA
  (`BROKERIUM_PROMETHEUS_USERNAME`, `BROKERIUM_PROMETHEUS_PASSWORD_FILE`,
  `BROKERIUM_PROMETHEUS_TOKEN_FILE`, `BROKERIUM_PROMETHEUS_CA_FILE`; Helm values
  `monitoring.prometheus.*`). The environment takes precedence; stored credentials are sealed like
  cluster credentials.
- **Cluster selector.** Each cluster has a PromQL label selector. By default it matches the
  exporter targets of its brokers, `instance=~"host1:9141|host2:9141"`, built from the broker
  addresses and the exporter port (9141 unless set for the cluster). An admin can replace it, for
  example with `job="kafka-eu"`.
- **Profiles.** Panels use the metric names of the JMX exporter's example configurations
  `kafka-2_0_0.yml` and `kafka-kraft-3_0_0.yml` with `lowercaseOutputName: true`, for example
  `kafka_server_brokertopicmetrics_bytesin_total`,
  `kafka_server_replicamanager_underreplicatedpartitions` and
  `kafka_network_requestmetrics_totaltimems{quantile="0.99"}`. JVM metrics differ between exporter
  0.x (`jvm_memory_bytes_used`) and 1.x (`jvm_memory_used_bytes`); the server detects which one a
  cluster exports and whether the KRaft metrics are present.
- **Queries.** The server runs range queries for a fixed set of panels: bytes in and out,
  messages in, produce and fetch request rates and their 99th percentile, partitions, leaders,
  under-replicated and offline partitions, ISR shrinks and expands, the active controller, request
  handler and network processor idle time, JVM heap, GC time, CPU and open files; per cluster,
  broker and topic. The browser never calls Prometheus, and the API takes panel ids, not PromQL.
- **Server metrics.** `GET /metrics` (Enterprise) exposes for every cluster its health and
  reachability, the problems by code and severity, consumer group lag per topic, the estimated
  rates, disk usage, and the under-replicated, offline and under-min-ISR partitions. It needs the
  bearer token `BROKERIUM_METRICS_TOKEN` and is off without it. The Helm chart can create a
  ServiceMonitor with that token. Prometheus then keeps the history of what only Brokerium
  computes, such as consumer group lag.

## Consequences

- Teams keep one metrics store and their alerts; Brokerium adds the cluster context.
- Clusters without an exporter show only current values until an exporter runs on their brokers;
  0.5.0 installs one on managed nodes.
- Exporter configurations that rename metrics need a custom selector or fall back to the current
  values; the profiles are data and can grow.
