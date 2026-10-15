# Apache Kafka releases, Docker images, reassignment, metrics and security

Research date: 2026-10-15

Source: research report B (translated from Russian). "(unverified)" marks inaccuracies and conclusions the researcher did not verify by running them. Quorum, broker lifecycle and upgrade procedures are covered in [kafka-kraft-operations.md](kafka-kraft-operations.md).

## Summary

- The latest officially announced stable release is **4.3.1** (2026-06-25).
- **4.4.0** passed its vote on 2026-10-13 and was being published on the research date: `kafka-clients 4.4.0` has been on Maven Central since 2026-10-13, and the `4.4.0/` directory appeared on downloads.apache.org on 2026-10-15. There was no announcement and no `apache/kafka:4.4.0` Docker tag yet.
- The official image is built for arm64 (all tags are multi-arch) and formats storage by itself, but only for a static quorum. Controllers in a dynamic quorum need a custom entrypoint.

## 1. Releases and Java

- **4.3.1** (2026-06-25) fixes a RocksDB memory leak in Streams and an Admin API hang in partition-leader operations when the cached leader has left the cluster (KAFKA-20673). This matters for a control plane. 4.3.0 was released on 2026-05-22 ([downloads](https://kafka.apache.org/community/downloads/)).
- **What matters operationally in 4.3** ([announcement](https://kafka.apache.org/blog/2026/05/22/apache-kafka-4.3.0-release-announcement/), [upgrade notes](https://kafka.apache.org/43/getting-started/upgrade/)):
  - KIP-1066: cordon via `cordoned.log.dirs`; the value `*` cordons the whole broker.
  - KIP-1023: `follower.fetch.last.tiered.offset.enable`.
  - KIP-1257: metric `RetentionSizeInPercent`.
  - Dynamic configs for controllers in a dynamic quorum (KAFKA-18928).
  - `--delete-config` became idempotent, so configs of offline brokers can be changed via `--bootstrap-controller`.
  - The upgrade is finalized with `kafka-features.sh upgrade --release-version 4.3` (`Admin#updateFeatures` in the API). MV 4.3 contains metadata changes, so a downgrade after finalization is impossible.
- **4.4.0.** Planned for 2026-09-09; RC4 was built on 2026-10-06 and the vote passed on 2026-10-13 ([scalac](https://scalac.io/blog/the-signal-what-matters-in-distributed-systems-7/), [dist](https://downloads.apache.org/kafka/)). Relevant changes ([upgrade.md](https://raw.githubusercontent.com/apache/kafka/4.4/docs/getting-started/upgrade.md)):
  - KIP-1170: directories formatted by `kafka-storage` 4.4 are not forward-compatible. The broker must be the same version as the formatter or newer.
  - KIP-1312: `Admin#unregisterController`; requires MV `4.4-IV2`.
  - KIP-1220: `kafka-cluster.sh api-versions`.
  - KIP-1232: `broker.id` is deprecated.
  - A broker using OAUTHBEARER with JWKS does not start without `expected.audience` and `expected.issuer`.
  - KIP-1276 (CIDR in the ACL host) is marked "Complete" in the [release plan](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/429064575/Release+Plan+4.4.0) but is missing from the upgrade notes (unverified).
  - The next release, 4.5, is expected roughly in January–April 2027 (unverified).
- **Bugfix lines:** 4.3.x (4.3.2 at RC3 as of 2026-10-11) and 4.2.x (4.2.2 from 2026-09-29). Per project policy, bugfix releases are made for the last three minor versions ([wiki](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/65867320/Time+Based+Release+Plan)).
- **Java** ([Java versions](https://kafka.apache.org/43/operations/java-version/)):
  - Brokers, Connect and CLI tools: Java 17, 21, 25 (25 recommended).
  - Clients and Streams: Java 11 or newer.
  - The 4.3.x image is built on Temurin 21 JRE Alpine; the 4.4 image on Temurin 25.

### Critical bugs to track

| Issue | Impact | Status |
|---|---|---|
| [KAFKA-20979](https://issues.apache.org/jira/browse/KAFKA-20979) | (unverified) Critical for rolling restarts: on clean shutdown the time index truncation is not flushed to disk (fsync), so after a restart retention can delete fresh segments. | Affects 4.1.0 and 4.3.0. Fixed in 4.3.2 and 4.4.0; no fix in 4.2.x. |
| KAFKA-20673 | Admin API hangs in partition-leader operations when the cached leader has left the cluster. | Fixed in 4.3.1. |
| KAFKA-20732 | Size-based retention deleted data after a leader change (raised in the tiered storage context). | Fixed in 4.3.2. |
| [KAFKA-20075](https://issues.apache.org/jira/browse/KAFKA-20075) | Automatic preferred-leader rebalance can happen at any moment 0–300 s after a broker comes back; plan drains and upgrades around it. | Behavior in 4.1+. |
| [KAFKA-19007](https://issues.apache.org/jira/browse/KAFKA-19007) | JVM crashes with SIGILL on M4 with macOS 15.2+ and JDK up to and including 21.0.6. | Fixed in JDK 21.0.7+ and Docker Desktop 4.39+; workaround `JAVA_TOOL_OPTIONS=-XX:UseSVE=0`. |

Security CVEs are listed in [section 5](#5-security-and-share-groups).

## 2. Official Docker images

### Tags

- `latest` points to 4.3.1; `4.2.2`, `4.4.0-rc4` and `4.3.2-rc3` also exist. All tags are multi-arch: **amd64 and arm64** ([apache/kafka](https://hub.docker.com/r/apache/kafka)).
- `apache/kafka-native` is a GraalVM native build (about 52 MB vs 228 MB for the JVM image) with the same tags. It is for dev/test only and contains no CLI scripts ([apache/kafka-native](https://hub.docker.com/r/apache/kafka-native)).

### Environment variable mapping

Per the [Docker examples guide](https://raw.githubusercontent.com/apache/kafka/trunk/docker/examples/README.md):

- Environment variables become properties with the prefix `KAFKA_`; in the property name, `.` becomes `_`, `_` becomes `__`, and `-` becomes `___`.
- Examples: `KAFKA_PROCESS_ROLES`, `KAFKA_NODE_ID`, `KAFKA_CONTROLLER_QUORUM_VOTERS`, `CLUSTER_ID`.
- If at least one `KAFKA_*` variable is set, the built-in `server.properties` is not used at all.
- Custom config files are mounted into `/mnt/shared/config`; environment variables override them.

### Storage formatting behavior

- On every start, `KafkaDockerWrapper` runs `kafka-storage format --cluster-id=$CLUSTER_ID` without `--standalone`, `--initial-controllers` or `--no-initial-controllers`. It ignores the "already formatted" error, so repeated starts are safe.
- The image is deliberately designed for a static quorum.
- A controller that only has `KAFKA_CONTROLLER_QUORUM_BOOTSTRAP_SERVERS` set never starts, not even once: StorageTool requires one of the three flags before it checks for "already formatted".
- For brokers, bootstrap servers work.
- Sources: [launch](https://raw.githubusercontent.com/apache/kafka/trunk/docker/jvm/launch), [KafkaDockerWrapper](https://raw.githubusercontent.com/apache/kafka/trunk/core/src/main/scala/kafka/docker/KafkaDockerWrapper.scala), [StorageTool](https://raw.githubusercontent.com/apache/kafka/4.3/core/src/main/scala/kafka/tools/StorageTool.scala).

This is why controllers in a dynamic quorum need a custom entrypoint (see below). The format flags themselves are described in [kafka-kraft-operations.md](kafka-kraft-operations.md).

### Example: 3 controllers + 3 brokers (static quorum)

- Available in the "Multiple nodes" section on Docker Hub and in the repository at `docker/examples/docker-compose-files/cluster/isolated/plaintext`.
- Controllers have ids 1–3, brokers have ids 4–6.
- Each broker has two listeners: `broker-N:19092` for the Docker network and `localhost:29092/39092/49092` for access from the host.

### Dynamic quorum with a custom entrypoint

A dynamic quorum is only possible by bypassing the wrapper: set your own `command:` and mount ready-made `.properties` files. The layout is assembled from the [KRaft docs](https://kafka.apache.org/43/operations/kraft/) and a [community example](https://webcodingcenter.com/apache-kafka/installing-apache-kafka-4-x-in-docker-with-kraft--formatting-storage-and-cluster-metadata.html); the researcher did not run it (unverified).

```bash
# command: ["bash","/boot/start.sh"]; env: CLUSTER_ID, NODE_ID, FORMAT_ARGS
CFG=/boot/node-$NODE_ID.properties
/opt/kafka/bin/kafka-storage.sh format -t "$CLUSTER_ID" -c "$CFG" --ignore-formatted $FORMAT_ARGS
exec /opt/kafka/bin/kafka-server-start.sh "$CFG"
```

- All nodes: `controller.quorum.bootstrap.servers=controller-1:9093,controller-2:9093,controller-3:9093`; `controller.quorum.voters` is not set; `log.dirs=/var/lib/kafka/data`.
- controller-1: `--standalone` and `listeners=CONTROLLER://controller-1:9093`. The host in the listener is mandatory: without it, `--standalone` records the voter as `localhost`.
- controller-2 and controller-3: `--no-initial-controllers` plus `controller.quorum.auto.join.enable=true` (introduced in 4.2). Instead of auto-join you can call `Admin#addRaftVoter`.
- Brokers: `--no-initial-controllers`.
- Alternative: the same `--initial-controllers "1@controller-1:9093:<dirId>,2@…,3@…"` on all three controllers.
- Verification: `kafka-metadata-quorum.sh describe --status`; `kafka-features.sh describe` should show `kraft.version=1`.

### Gotchas

1. The default `CLUSTER_ID` is the constant `5L6g3nShT-eMCtK--X86sw`, so every cluster gets the same ID. Generate your own: for a multi-cluster control plane it is the natural cluster key.
2. Without `KAFKA_LOG_DIRS`, data is written to `/tmp/kafka-logs` inside the container and is lost when the container is recreated. Set `KAFKA_LOG_DIRS=/var/lib/kafka/data` (a declared `VOLUME` owned by `appuser`, uid 1000) and attach a named volume. On Linux a bind mount needs `chown 1000`.
3. The `configure` script forbids `KAFKA_ADVERTISED_LISTENERS` on controllers, although Kafka itself allows it since 3.9. Put the controller host in `KAFKA_LISTENERS`. For brokers, `KAFKA_LISTENERS` is derived from the advertised listeners with the host replaced by `0.0.0.0`.
4. The default heap is `-Xmx1G -Xms1G` per JVM, so 6 nodes take more than 6 GB. Set `KAFKA_HEAP_OPTS` and check the Docker Desktop memory limit.
5. SCRAM for inter-broker traffic requires `format --add-scram` before the first start, and the image's auto-format cannot do that.
6. JMX is enabled via `KAFKA_JMX_PORT` (and `KAFKA_JMX_HOSTNAME`). The RMI port equals the JMX port; there is no authentication and no TLS.
7. On M4 with macOS 15.2+ and JDK up to and including 21.0.6, the JVM crashes with SIGILL ([KAFKA-19007](https://issues.apache.org/jira/browse/KAFKA-19007)). Fixed in JDK 21.0.7+ and Docker Desktop 4.39+; workaround: `JAVA_TOOL_OPTIONS=-XX:UseSVE=0`.
8. (unverified) Inferred from the code: after a static → dynamic migration (in 4.1+: `--feature kraft.version=1` and replacing voters with bootstrap servers), the standard entrypoint will stop starting controllers.

## 3. Reassignment via the Admin API

### `alterPartitionReassignments`

`alterPartitionReassignments(Map<TopicPartition, Optional<NewPartitionReassignment>>)` ([KIP-455](https://cwiki.apache.org/confluence/display/KAFKA/KIP-455%3A+Create+an+Administrative+API+for+Replica+Reassignment), [Admin javadoc](https://kafka.apache.org/43/javadoc/org/apache/kafka/clients/admin/Admin.html)):

- You pass the full target replica list; the first replica in the list becomes the preferred leader.
- The controller sets the replica set to the union of the old and target replicas and fills in `addingReplicas` and `removingReplicas`. Once all adding replicas have joined the ISR, it drops the removing replicas and re-elects the leader if the leader is not in the target list.
- A new request for the same partition replaces the current one.
- `Optional.empty()` cancels the movement and restores the original replica set (order is not guaranteed). If no reassignment is in progress, the result is `NoReassignmentInProgressException`.
- Results come back per partition; the operation is not transactional.
- Since 4.1 there is `allowReplicationFactorChange(false)` (KIP-860), which protects against races that silently change the RF.
- `listPartitionReassignments()` returns the replicas plus the adding and removing replicas.
- If the target broker is fully cordoned, the result is `INELIGIBLE_REPLICA`.

### Throttling

Per the [operations docs](https://kafka.apache.org/43/operations/basic-kafka-operations/):

- On brokers: dynamic `leader.replication.throttled.rate` and `follower.replication.throttled.rate`, in bytes per second, set via `incrementalAlterConfigs` on the BROKER resource.
- On topics: `leader.replication.throttled.replicas` and `follower.replication.throttled.replicas`, in the format `partition:broker,…` or `*`.
- The CLI puts the leader throttle on the source replicas and the follower throttle on the destination replicas. Throttling only applies to replicas outside the ISR.
- Between disks inside one broker, speed is limited by `replica.alter.log.dirs.io.max.bytes.per.second`.
- The throttle must be higher than the maximum BytesInPerSec of the partitions being moved, otherwise the move makes no progress. `FetcherLagMetrics…ConsumerLag` should be decreasing.
- Remove throttles after completion, including on failure (in a `finally` block).

### Log directories

- `describeLogDirs` returns size, offsetLag and isFuture for each replica, totalBytes and usableBytes for each directory, and since 4.3 also `isCordoned`.
- `alterReplicaLogDirs` moves a replica between disks inside a broker via a temporary `-future` copy.

### Leader election

- `electLeaders(PREFERRED|UNCLEAN, partitions | null for all)`.
- `auto.leader.rebalance.enable=true` is a static setting; `leader.imbalance.check.interval.seconds=300`.
- `leader.imbalance.per.broker.percentage` was removed in 4.0: KRaft moves leadership back on any deviation from the preferred leader.
- In 4.1+ this can happen at any moment 0–300 s after a broker comes back ([KAFKA-20075](https://issues.apache.org/jira/browse/KAFKA-20075)). Take this into account during drains and upgrades.

### Draining a broker

Per the 4.3 docs and [KIP-1066](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1066%3A+Mechanism+to+cordon+brokers+and+log+directories):

- Cordon with `*` first, then move all replicas away, stop the broker and call `Admin#unregisterBroker`.
- Fenced brokers are visible via `describeCluster(includeFencedBrokers(true))` (KIP-1073, since 4.0).
- Controllers are removed with `removeRaftVoter`; 4.4 adds `unregisterController`.

### Large rebalances

- Move partitions in batches. A reference point is the Cruise Control defaults: 5 concurrent movements per broker and 1250 per cluster ([Cruise Control configs](https://github.com/linkedin/cruise-control/wiki/Configurations)).
- Start the next batch only when Offline=0, UnderMinIsr=0 and URP=0. Replicas being added by a reassignment are not counted as URP (KIP-352).
- Move leadership as a separate step, and check usableBytes on the target brokers in advance.
- In 4.1+ ELR (KIP-966) is enabled on new clusters, so set `min.insync.replicas` at the cluster or topic level.

### Tiered storage

Per the [tiered storage docs](https://kafka.apache.org/43/operations/tiered-storage/):

- Production-ready since 3.9.0 (November 2024). There is no built-in RemoteStorageManager, so a plugin is required. Compacted topics are not supported.
- Reassignment really is cheaper: a new replica copies only the local part of the leader's log and takes auxiliary state from remote storage.
- Since 4.3, with `follower.fetch.last.tiered.offset.enable=true` (off by default), copying starts from the earliest pending upload offset. Downside: if such a replica becomes leader, reads go to remote storage ([KIP-1023](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/293047026/KIP-1023+Follower+fetch+from+tiered+offset)).
- Use at least 4.3.2: it fixes KAFKA-20732, which made size-based retention delete data after a leader change.

## 4. Metrics

### Without installing anything

- Admin API: `describeTopics` (ISR, URP, offline), `describeLogDirs`, `describeMetadataQuorum` (leader, epoch, HW, LEO, plus lastFetch and lastCaughtUp for voters and observers), `describeCluster`, `describeFeatures`.
- Remote JMX: `JmxReporter` is enabled by default; remote access is opened with `JMX_PORT`.
- Throughput, idle % and latencies are only available as metrics.

### With a jar plugin (requires a broker restart)

- Prometheus JMX Exporter **1.7.0** (2026-10-05) as a javaagent: exposes metrics over HTTP and/or pushes them over OTLP ([releases](https://github.com/prometheus/jmx_exporter/releases/)).
- Strimzi Metrics Reporter **0.3.0** (February 2026, early access) exposes metrics over HTTP without JMX. Configuration: `metric.reporters=io.strimzi.kafka.metrics.prometheus.ServerKafkaMetricsReporter` and `kafka.metrics.reporters=…ServerYammerMetricsReporter` ([repo](https://github.com/strimzi/metrics-reporter)).
- URP, Offline, ActiveController and BrokerTopicMetrics live in the Yammer registry, so a reporter must cover both registries.

### OpenTelemetry

- Apache Kafka has no built-in OTLP export of broker metrics; no KIP on this was found (unverified).
- KIP-714 (3.7+) covers only telemetry that clients send to the broker.
- Working options: the OTel Java agent with `otel.jmx.target.system=kafka-broker`, or the OTel jmx-scraper (target `kafka`) over remote JMX ([OTel JMX docs](https://opentelemetry.io/docs/languages/java/jmx/)); also third-party OTLP reporters.

### Key MBeans

Per the [monitoring docs](https://kafka.apache.org/43/operations/monitoring/):

- `kafka.server:type=BrokerTopicMetrics,name={BytesInPerSec|BytesOutPerSec|ReplicationBytesInPerSec|ReassignmentBytesInPerSec}[,topic=…]`
- `kafka.server:type=KafkaRequestHandlerPool,name=RequestHandlerAvgIdlePercent` and `kafka.network:type=SocketServer,name=NetworkProcessorAvgIdlePercent` (healthy above 0.3)
- `kafka.network:type=RequestMetrics,name=TotalTimeMs,request={Produce|FetchConsumer|FetchFollower}`
- `kafka.server:type=ReplicaManager,name={UnderReplicatedPartitions|UnderMinIsrPartitionCount|PartitionCount|LeaderCount|ReassigningPartitions}`
- `kafka.log:type=Log,name={Size|RetentionSizeInPercent},topic=…,partition=…`
- `kafka.controller:type=KafkaController,name={ActiveControllerCount|OfflinePartitionsCount|FencedBrokerCount|PreferredReplicaImbalanceCount|LastAppliedRecordLagMs}`
- `kafka.server:type=raft-metrics`: `current-state`, `current-leader`, `current-epoch`, `high-watermark`, `log-end-offset`, `commit-latency-avg/max`, `election-latency-avg/max`, `poll-idle-ratio-avg`, and since 4.1 also `number-of-voters` and `uncommitted-voter-change`.

Related metrics from report A: `LogDirectoryCordoned` and `CordonedLogDirectoryCount` (cordon, 4.3) and `ignored-static-voters` / `IgnoredStaticVoters` (static → dynamic migration); see [kafka-kraft-operations.md](kafka-kraft-operations.md).

## 5. Security and share groups

- **Authentication** ([SASL](https://kafka.apache.org/43/security/authentication-using-sasl/)):
  - SASL: GSSAPI, PLAIN, SCRAM-SHA-256/512, OAUTHBEARER.
  - mTLS: `ssl.client.auth=required` and `ssl.principal.mapping.rules`.
  - Delegation tokens: `delegation.token.secret.key` must be set on brokers **and** controllers. By default a token is renewed every 24 hours and its maximum lifetime is 7 days.
- **OAUTHBEARER** ([CVE list](https://kafka.apache.org/community/cve-list/)):
  - Since 4.0 the JVM property `org.apache.kafka.sasl.oauthbearer.allowed.urls` is empty by default, so token and JWKS endpoint URLs must be allowed explicitly (CVE-2025-27817).
  - Versions 4.1.0–4.1.1 accepted unsigned JWTs (CVE-2026-33557).
- **Authorization** ([ACLs](https://kafka.apache.org/43/security/authorization-and-acls/)):
  - `org.apache.kafka.metadata.authorizer.StandardAuthorizer` is configured on all nodes; ACLs are stored in the metadata log.
  - If a resource has no ACLs, access is denied to everyone except `super.users`.
  - Admin requests are forwarded to the controller in an Envelope, so a custom principal builder must implement `KafkaPrincipalSerde`.
  - API: `createAcls`, `describeAcls`, `deleteAcls` (pattern types LITERAL and PREFIXED).
  - `ConsumerGroupDescribe` requires DESCRIBE on GROUP (CVE-2026-41115).
- **SCRAM:** `alterUserScramCredentials` creates or updates credentials (at least 4096 iterations) or deletes them. `describeUserScramCredentials` returns only the mechanism and the iteration count.
- **Quotas:** `alterClientQuotas` and `describeClientQuotas` by user, client-id or ip. Keys: `producer_byte_rate`, `consumer_byte_rate`, `request_percentage`, `controller_mutation_rate`, `connection_creation_rate`.
- **The control plane's own client:** use kafka-clients 4.3.1 or newer. Do not enable DEBUG logging for `NetworkClient`: SASL/SCRAM requests end up in the log (CVE-2026-33558). `kafka-clients` is the Java client; for the Go client choice see [go-kafka-clients.md](go-kafka-clients.md).
- **Share groups (KIP-932):**
  - GA since 4.2. On new clusters `share.version=1` is enabled by default; on upgraded clusters it is enabled via `kafka-features`.
  - Admin API: `listGroups` (`listConsumerGroups` is deprecated since 4.1), `describeShareGroups`, `listShareGroupOffsets`, `alterShareGroupOffsets` (empty groups only), `deleteShareGroupOffsets`, `deleteShareGroups`.
  - The internal topic `__share_group_state` is created with RF 3.
  - 4.4 adds DLQ (requires `share.version=2`) and offset initialization from a file (KIP-1323).

## Control-plane takeaways

Collected from the sections above:

- Generate a unique `CLUSTER_ID` per cluster; it is the natural cluster key in a multi-cluster control plane.
- Controllers in a dynamic quorum need a custom container entrypoint; the stock wrapper cannot format them, and (unverified) stops starting controllers after a static → dynamic migration.
- Run reassignments in batches, gate each batch on Offline=0, UnderMinIsr=0 and URP=0, move leadership as a separate step, check usableBytes first, and always remove throttles in a `finally` block.
- Expect automatic leader moves 0–300 s after a broker returns (4.1+, KAFKA-20075) during drains and upgrades.
- Track fixed versions: 4.3.1 for the Admin API hang (KAFKA-20673); 4.3.2 for tiered storage retention (KAFKA-20732) and, per the unverified KAFKA-20979 entry, for rolling restarts on 4.3 (no fix in 4.2.x).
- A metrics reporter must cover both the Yammer and the Kafka metrics registries.
