# Monitoring (Enterprise)

The Enterprise feature `monitoring` ([ADR 0024](../adr/0024-monitoring-through-prometheus.md)) adds:

- a **Monitoring** tab on every cluster with charts of the broker, topic and JVM metrics that your
  Prometheus collects from the JMX exporters of the brokers;
- `GET /metrics`, which exposes what only Brokerium knows about all clusters, such as their health
  and consumer group lag, so that Prometheus keeps its history and alerts on it.

Brokerium stores no metrics. The server queries Prometheus with fixed queries per panel: the
browser never contacts Prometheus and the API takes panel ids, not PromQL.

## Exporters on the brokers

The panels use the metric names of the example configurations of the
[Prometheus JMX exporter](https://github.com/prometheus/jmx_exporter): `kafka-2_0_0.yml` for clusters
with ZooKeeper and `kafka-kraft-3_0_0.yml` for KRaft, both with `lowercaseOutputName: true`. The
exporter runs as a Java agent of every broker, for example in `KAFKA_OPTS`:

```bash
-javaagent:/opt/kafka/prometheus/jmx_prometheus_javaagent-1.5.0.jar=9141:/opt/kafka/prometheus/kafka-kraft-3_0_0.yml
```

Prometheus scrapes port 9141 of every broker, and of the controllers if they run the exporter too.
A configuration that renames metrics needs its own selector or shows only the current values.

## Data source

One Prometheus serves the whole installation. It can also be a server with the same HTTP API, such
as Thanos, Mimir or VictoriaMetrics; give its URL with the path prefix, for example
`https://mimir.example.com/prometheus`.

- **Dashboard:** administrators open **Monitoring** in the sidebar, enter the URL, the
  authentication (none, basic or a bearer token), a CA certificate and the timeout per query, and
  test and save the source. Passwords and tokens are stored sealed with the secret key of the
  server, like cluster credentials, and are never returned. The audit log records
  `monitoring.source.update` and `monitoring.source.delete`.
- **Environment:** the variables below take precedence; the dashboard then shows the source
  read-only. The files are read on every use, so replaced Secrets take effect without a restart.

| Variable | Description |
| --- | --- |
| `BROKERIUM_PROMETHEUS_URL` | Base URL of the Prometheus HTTP API, `http` or `https`. |
| `BROKERIUM_PROMETHEUS_USERNAME` | Username of basic authentication. |
| `BROKERIUM_PROMETHEUS_PASSWORD_FILE` | File with the password of basic authentication. |
| `BROKERIUM_PROMETHEUS_TOKEN_FILE` | File with a bearer token, instead of basic authentication. |
| `BROKERIUM_PROMETHEUS_CA_FILE` | File with the PEM CA certificates that sign the certificate of Prometheus. |

- **Helm:** `monitoring.prometheus.url`, `username`, and existing Secrets for the password, the
  token and the CA, which the chart mounts as files (see the
  [chart README](../../deploy/helm/brokerium/README.md#monitoring)).

## Selectors

Each cluster finds its metrics with a PromQL label selector. By default it matches the exporters by
their `instance` label: host and exporter port of every broker, and of every controller whose
endpoint the quorum reports (KRaft with a dynamic quorum), for example
`instance=~"kafka-1\\.example\\.com:9141|kafka-2\\.example\\.com:9141"`. The port is 9141 unless an
administrator sets another one for the cluster.

When the targets in Prometheus carry other addresses or labels, an administrator replaces the
selector on the Monitoring tab with **Selector**, for example `job="kafka-eu"` or
`cluster="prod", env=~"eu-.*"`. A selector is a list of label matchers (`=`, `!=`, `=~`, `!~`) and
cannot match `__name__`, because the panels choose the metrics. Changes are audited as
`cluster.monitoring.update`. The charts name the series of an `instance` by the broker or the
controller on its host.

## Profiles

Exporter 0.x calls the JVM heap `jvm_memory_bytes_used`, exporter 1.x `jvm_memory_used_bytes` and
adds `_total` to counters. The server asks Prometheus which metrics the selector matches and keeps
the answer for five minutes: the generation of the exporter, whether the metrics of the KRaft
configuration are present, and how many brokers report. The tab shows the profile next to the
selector and the KRaft panel only for clusters with KRaft metrics.

## Panels

The tab has three sections, **Cluster**, **Brokers** and **Topics**, and the time ranges 1 hour,
6 hours, 24 hours and 7 days. Panels load when they scroll into view and refresh while the tab is
open. In the queries `$sel` is the selector, `$rate` the rate window (five minutes, or the step of
the range when it is longer) and `$range` the window that ranks topics (the time range, at most six
hours).

| Panel | Query |
| --- | --- |
| Bytes in and out | `sum(rate(kafka_server_brokertopicmetrics_bytesin_total{$sel,topic=""}[$rate]))`, the same with `bytesout` |
| Messages in | `sum(rate(kafka_server_brokertopicmetrics_messagesin_total{$sel,topic=""}[$rate]))` |
| Produce and fetch requests | `sum by (request) (rate(kafka_network_requestmetrics_requests_total{$sel,request=~"Produce\|FetchConsumer\|FetchFollower"}[$rate]))` |
| Request time, 99th percentile | `max by (request) (kafka_network_requestmetrics_totaltimems{$sel,request=~"Produce\|FetchConsumer\|FetchFollower",quantile="0.99"})` |
| Partitions at risk | `sum(kafka_server_replicamanager_underreplicatedpartitions{$sel})`, `sum(kafka_server_replicamanager_underminisrpartitioncount{$sel})`, `max(kafka_controller_kafkacontroller_offlinepartitionscount{$sel})` |
| Active controllers | `sum(kafka_controller_kafkacontroller_activecontrollercount{$sel})` |
| ISR shrinks and expands | `60 * sum(rate(kafka_server_replicamanager_isrshrinks_total{$sel}[$rate]))`, the same with `isrexpands` |
| Metadata quorum epoch (KRaft) | `max(kafka_server_raft_metrics_current_epoch{$sel})` |
| Bytes in, bytes out, messages in per broker | the cluster queries with `sum by (instance)` |
| Produce and consumer fetch requests per broker | `sum by (instance) (rate(kafka_network_requestmetrics_requests_total{$sel,request="Produce"}[$rate]))`, the same with `FetchConsumer` |
| Produce and consumer fetch time, 99th percentile | `max by (instance) (kafka_network_requestmetrics_totaltimems{$sel,request="Produce",quantile="0.99"})`, the same with `FetchConsumer` |
| Under-replicated partitions, partitions under min ISR | `sum by (instance) (kafka_server_replicamanager_underreplicatedpartitions{$sel})`, the same with `underminisrpartitioncount` |
| ISR shrinks | `60 * sum by (instance) (rate(kafka_server_replicamanager_isrshrinks_total{$sel}[$rate]))` |
| Partitions, leaders | `sum by (instance) (kafka_server_replicamanager_partitioncount{$sel})`, the same with `leadercount` |
| Request handler idle | `100 * sum by (instance) (rate(kafka_server_kafkarequesthandlerpool_requesthandleravgidlepercent_count_total{$sel}[$rate])) / 1e9` (exporter 0.x without `_total`) |
| Network processor idle | `100 * avg by (instance) (kafka_network_socketserver_networkprocessoravgidlepercent{$sel})` |
| JVM heap used | `sum by (instance) (jvm_memory_used_bytes{$sel,area="heap"})` (exporter 0.x: `jvm_memory_bytes_used`) |
| Time in garbage collection | `100 * sum by (instance) (rate(jvm_gc_collection_seconds_sum{$sel}[$rate]))` |
| CPU | `sum by (instance) (rate(process_cpu_seconds_total{$sel}[$rate]))` |
| Open files | `sum by (instance) (process_open_fds{$sel})` |
| Log size | `sum by (instance) (kafka_log_log_size{$sel})` |
| Top topics by bytes in, bytes out, messages in | ranked by `topk(10, sum by (topic) (rate(kafka_server_brokertopicmetrics_bytesin_total{$sel,topic!=""}[$range])))`, then `sum by (topic) (rate(...{$sel,topic=~"<the ten topics>"}[$rate]))` |
| Top topics by size | ranked by `topk(10, sum by (topic) (kafka_log_log_size{$sel}))` |

A panel returns at most 50 series and 1000 points per series. Errors of Prometheus appear on the
panel: unreachable, refused credentials, a rejected query (for example too many samples) or a
timeout.

Without a data source, and while no exporter metrics match the selector, the tab shows the values
Brokerium samples now: the rates per broker and topic that it estimates from offsets and log sizes
over five minutes, and the disk usage of the latest snapshot.

The API is `GET /api/v1/clusters/{id}/monitoring` (the source state, the selector, the profile and
the panels), `PUT` on the same path for the selector (admins), and
`GET /api/v1/clusters/{id}/monitoring/panels/{panel}?range=1h` or with `start`, `end` and `step`;
the source is `GET /api/v1/monitoring`, `PUT` and `DELETE /api/v1/monitoring/source` and
`POST /api/v1/monitoring/source/test` (see the [REST API](../../api/openapi.yaml)).

## The metrics endpoint

`GET /metrics` serves the Prometheus text format and needs a bearer token: set
`BROKERIUM_METRICS_TOKEN`, or `BROKERIUM_METRICS_TOKEN_FILE` with a file that holds it (read on
every scrape). Without a token the endpoint answers 404, with a wrong one 401, and without a license
that covers `monitoring` 403. The values come from the snapshots the server takes anyway, so a
scrape never waits for Kafka.

| Metric | Labels | Meaning |
| --- | --- | --- |
| `brokerium_build_info` | `version`, `commit`, `edition` | Always 1. |
| `brokerium_cluster_info` | `cluster`, `provider`, `flavor`, `kafka_cluster_id` | Always 1 for every registered cluster. |
| `brokerium_cluster_health_status` | `cluster` | 0 ok, 1 warning, 2 critical or unreachable. |
| `brokerium_cluster_reachable` | `cluster` | 1 when the brokers answered the last snapshot. |
| `brokerium_cluster_last_snapshot_timestamp_seconds` | `cluster` | When the server last evaluated the cluster. |
| `brokerium_cluster_operations` | `cluster`, `status` | Operations that are `pending`, `running` or `paused`. |
| `brokerium_cluster_brokers`, `_brokers_alive`, `_brokers_fenced` | `cluster` | Registered, serving and fenced brokers. |
| `brokerium_cluster_topics`, `_partitions` | `cluster` | Topics and partitions. |
| `brokerium_cluster_partitions_under_replicated`, `_under_min_isr`, `_offline` | `cluster` | Partitions at risk, as in the health of the cluster. |
| `brokerium_cluster_quorum_voters`, `_quorum_voters_lagging` | `cluster` | Voters of the KRaft quorum and those that lag. |
| `brokerium_broker_disk_used_bytes`, `_disk_total_bytes` | `cluster`, `broker` | Volumes of the log directories. |
| `brokerium_broker_log_size_bytes` | `cluster`, `broker` | Bytes of the partition replicas of the broker. |
| `brokerium_broker_estimated_bytes_in_per_second`, `_messages_in_per_second` | `cluster`, `broker` | Estimated writes to the replicas of the broker. |
| `brokerium_topic_estimated_bytes_in_per_second`, `_messages_in_per_second` | `cluster`, `topic` | Estimated writes to the topic. |
| `brokerium_consumer_group_lag` | `cluster`, `group`, `topic` | Lag of a consumer or share group on a topic, the sum over its partitions. |
| `brokerium_consumer_group_members` | `cluster`, `group` | Members of the group. |

Health, brokers and partitions are reported for reachable clusters only; follow
`brokerium_cluster_reachable` for the others. There are no partition labels, and the server
collects at most 500 groups per cluster. Other Enterprise features add their metrics to the same
endpoint, such as the problems of a cluster by code and severity.

A scrape configuration:

```yaml
scrape_configs:
  - job_name: brokerium
    metrics_path: /metrics
    authorization:
      credentials_file: /etc/prometheus/brokerium-token
    static_configs:
      - targets: [brokerium.example.com:8080]
```

With the Prometheus Operator, the Helm chart creates the ServiceMonitor (`metrics.enabled`,
`metrics.serviceMonitor.enabled`). Without the chart, a ServiceMonitor looks like this, with the
token in the Secret `brokerium-metrics`:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: brokerium
  labels:
    release: kube-prometheus-stack
spec:
  selector:
    matchLabels:
      app.kubernetes.io/name: brokerium
      app.kubernetes.io/component: server
  endpoints:
    - port: http
      path: /metrics
      interval: 60s
      authorization:
        type: Bearer
        credentials:
          name: brokerium-metrics
          key: token
```

Alerts on these metrics, for example:

```yaml
groups:
  - name: brokerium
    rules:
      - alert: KafkaClusterCritical
        expr: brokerium_cluster_health_status >= 2
        for: 5m
      - alert: KafkaPartitionsOffline
        expr: brokerium_cluster_partitions_offline > 0
      - alert: KafkaConsumerGroupLagging
        expr: sum by (cluster, group) (brokerium_consumer_group_lag) > 100000
        for: 15m
```
