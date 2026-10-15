# Market landscape: Kafka management tools and Cruise Control

Research date: 2026-10-15

Source: research report C, sections 1–2 (translated from Russian). "(unverified)" marks statements the researcher flagged as not verified. Go clients are covered in [go-kafka-clients.md](go-kafka-clients.md) and naming in [naming-and-trademarks.md](naming-and-trademarks.md).

## Summary

No open-source tool fully covers KRaft clusters on VMs or bare metal. The biggest gaps are controller quorum management, the full broker lifecycle and rolling upgrades. Cruise Control fits as a pluggable backend, not as the core. The best Go client is franz-go. "Kafka" cannot be used in the product name.

Kafka context used by the report (as of 2026-10-15):

- The latest stable Kafka is 4.3.1 (2026-06-25); 4.4 was at the RC stage in September ([Kafka downloads](https://kafka.apache.org/community/downloads/)).
- The report states that the dynamic controller quorum (KIP-853) works since 4.1, including static → dynamic migration ([KRaft operations](https://kafka.apache.org/43/operations/kraft/)). Report A is more precise: dynamic quorum exists since 3.9.0, and static → dynamic conversion since 4.1.0 (see [kafka-kraft-operations.md](kafka-kraft-operations.md)).
- Controller auto-join appeared in 4.2.
- Controller unregistration (`UnregisterController`, [KIP-1312](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/406623954/KIP-1312+Support+unregistering+controllers)) lands in 4.4.

In short:

- **Market gap.** Data-plane UIs (Kafbat, AKHQ, Kafdrop) do not manage brokers or the cluster. Ops tools either have no multi-cluster UI (Cruise Control) or are Kubernetes-only (Strimzi). No open-source UI manages the controller quorum.
- **Cruise Control.** On 2026-10-05 the project moved to the Linux Foundation; since 3.1.0 the license is Apache-2.0. Use it as an optional backend and write the balancing core in Go, with no jar on the brokers.
- **franz-go.** `kmsg` has all the KRaft requests we need. `kadm` lacks convenience wrappers for quorum, voters, unregister and DescribeCluster, but they are easy to write.
- **Name.** "Kafka" in the product name is not allowed. Acceptable forms: `<Name> for Apache Kafka` (at the ASF's discretion) and `<Name>, Powered by Apache Kafka`.

## 1. Landscape

### Comparison table

Columns: "Multi" = multi-cluster dashboard; "Data" = topics, ACLs, consumer groups and message browsing. ✓ = yes, ~ = partial, ✗ = no, ? = not checked.

| Product | License · stack | Latest release | Multi | Data | Brokers add/drain | KRaft quorum | Rebalance | Rolling upgrade |
|---|---|---|---|---|---|---|---|---|
| Cruise Control | Apache-2.0 since 3.1.0 (previously BSD-2) · Java 17 | 3.1.0-rc2 (2026-10-14); GA 2.5.146 (2025-11-05) | ✗ (1 instance per cluster) | ✗ | ~ (moves replicas) | ✗ | ✓ manual + self-healing | ✗ |
| cruise-control-ui | BSD-2 · Vue | last commit 2024-08 (abandoned) | ✓ (on top of several CC instances) | ✗ | ~ via CC | ✗ | ~ via CC | ✗ |
| Strimzi | Apache-2.0 · Java | 1.3.0 (2026-10-12) | ~ (via CRs, no UI) | ~ (Topic/User CRs; no messages) | ✓ (NodePool + auto-rebalance) | ✗ (static only) | ✓ (via CC) | ✓ (K8s only) |
| Kafbat UI | Apache-2.0 · Java/Spring + React | 1.5.0 (2026-04-20) | ✓ | ✓ | ✗ | ~ (shows quorum type and active controller) | ✗ (RF change only) | ✗ |
| AKHQ | Apache-2.0 · Java/Micronaut + React | 0.28.0 (2026-08-06) | ✓ | ✓ (ACLs read-only) | ✗ | ✗ | ✗ | ✗ |
| Redpanda Console | BSL 1.1 + RCL · Go (franz-go) + React | 3.12.0 (2026-09-16) | ✗ (1 cluster per instance) | ✓ (+SCRAM) | ✗ | ✗ | ~ manual wizard (Enterprise only) | ✗ |
| Conduktor Console | proprietary; Community edition ≤ 3 clusters / 50 users | 1.47.2 (2026-09-24) | ✓ | ✓ | ✗ | ✗ | ✗ | ✗ |
| Lenses | proprietary (Community edition available) | 6.3 (2026-10-09) | ✓ (HQ + Agents) | ✓ (+SQL) | ✗ | ✗ | ✗ | ✗ |
| Kpow | proprietary (Community edition available) · Clojure/JVM | 96.5 (2026-09-24) | ✓ | ✓ | ✗ | ✗ | ~ (view and cancel reassignments) | ✗ |
| Confluent Control Center (next-gen) | Confluent Enterprise · Java + Prometheus | 2.6.1 (2026-09-28) | ✓ (100+ clusters) | ✓ | ✓ broker removal (via SBC) | ? | via SBC | ✗ (done by CFK/cp-ansible) |
| Confluent Self-Balancing Clusters (SBC) | Confluent Server only | part of CP 8.x | — | — | ✓ `kafka-remove-brokers` with automatic UnregisterBroker | ✗ | ✓ automatic | ✗ |
| Klaw | Apache-2.0 · Java/Spring + React | 2.10.6 (2026-09-16), mostly dependency updates | ✓ | ~ (requests with approval) | ✗ | ✗ | ✗ | ✗ |
| CMAK | Apache-2.0 · Scala/Play | no commits since 2023-08; requires ZooKeeper, does not work with Kafka 4.x | ✓ | ~ | ✗ | ✗ | ~ manual reassignment + leader election | ✗ |
| Kafdrop | Apache-2.0 · Java 25/Spring Boot 4.1 | 4.3.0 (2026-08-31) | ✗ | ✓ (ACLs view-only) | ✗ | ✗ | ✗ | ✗ |
| DataDog kafka-kit | Apache-2.0 · Go CLI | 4.2.1 (2023-07), last commit 2025-01 | ✗ | ✗ | ~ (`topicmappr rebuild`) | ✗ | ~ (by storage size + autothrottle) | ✗ |

### Notes per product

- **Cruise Control:** see [section 2](#2-cruise-control) ([releases](https://github.com/cruise-control-for-kafka/cruise-control/releases)).
- **cruise-control-ui:** [repository](https://github.com/linkedin/cruise-control-ui).
- **Strimzi** ([releases](https://github.com/strimzi/strimzi-kafka-operator/releases)):
  - `KafkaRebalance` supports the modes `full`, `add-brokers`, `remove-brokers` and `remove-disks`. A rebalance runs automatically when the cluster is scaled.
  - Since 1.2.0, when scaling down on Kafka 4.3+, the broker is cordoned first.
  - Dynamic quorum exists only as a proposal so far ([proposal #203](https://github.com/strimzi/proposals/pull/203)).
  - Support for controller unregistration ([issue #12712](https://github.com/strimzi/strimzi-kafka-operator/issues/12712)) is expected together with Kafka 4.4.
- **Kafbat UI:** [releases](https://github.com/kafbat/kafka-ui/releases).
- **AKHQ:** [releases](https://github.com/tchiotludo/akhq/releases), [AKHQ roles](https://akhq.io/docs/configuration/authentifications/groups.html).
- **Redpanda Console:** [releases](https://github.com/redpanda-data/console/releases), [issue #2129](https://github.com/redpanda-data/console/issues/2129), [Redpanda licensing](https://docs.redpanda.com/current/get-started/licensing/overview/).
- **Conduktor Console:** [Community edition](https://docs.conduktor.io/guide/conduktor-in-production/manage-licenses/console-community-edition).
- **Lenses:** [6.3 changelog](https://docs.lenses.io/latest/changelog/lenses-6.3/agent).
- **Kpow:** [changelog](https://factorhouse.io/products/kpow/changelog/).
- **Confluent Control Center:** [release notes](https://docs.confluent.io/control-center/current/release-notes.html).
- **Confluent SBC** supports KRaft. (unverified) In CP ≥ 7.9, dynamic controllers are available only to clusters that were originally created with a dynamic quorum ([SBC configuration](https://docs.confluent.io/platform/current/clusters/sbc/configuration-options.html), [KRaft in CP](https://docs.confluent.io/platform/current/kafka-metadata/kraft.html)).
- **Klaw:** [releases](https://github.com/Aiven-Open/klaw/releases).
- **CMAK:** [repository](https://github.com/yahoo/CMAK). Its rename from "Kafka Manager" is described in [naming-and-trademarks.md](naming-and-trademarks.md).
- **Kafdrop:** [releases](https://github.com/obsidiandynamics/kafdrop/releases).
- **DataDog kafka-kit:** [releases](https://github.com/DataDog/kafka-kit/releases).
- **Cloudera Manager + SMM + Cruise Control** (not in the table): a commercial option for VMs. KRaft is GA there since 7.3.2, with Add/Remove KRaft Controller actions, but it is a heavy, all-in-one platform ([Cloudera 7.3.2](https://docs.cloudera.com/runtime/7.3.2/public-release-notes/topics/rt-whats-new-kafka.html)).

### Newcomers

- **Calinora Pilot** (commercial, 2026) is the closest to the Brokerium idea ([Calinora Pilot](https://www.calinora.io/products/pilot/)):
  - A single Go binary without agents: no reporter jar and no JMX needed. It estimates load from changes in watermark offsets and log directory sizes.
  - Builds deterministic rack-aware plans and can model "what if" scenarios.
  - Broker drain with RF and min.ISR protection; scheduled balancing.
  - Rolling restart via an optional mTLS agent.
  - Monitoring is free; making changes requires a per-broker license.
  - Quorum management is not mentioned in its documentation.
- **StreamsHub Console** 0.14.1 (Apache-2.0): a UI for Strimzi that sees many clusters and can approve/refresh/stop a `KafkaRebalance`. Kubernetes-only ([StreamsHub](https://www.streamshub.io/console/docs/main/)).
- Small projects: Brokr (AGPL-3.0), a CMAK fork with KRaft (mordp1/CMAK 3.0.1), Blazing KRaft, Kouncil.
- Trend in 2025–26: MCP and AI integrations (Kafbat, AKHQ, Lenses, Conduktor, Pilot).

### Gaps no open-source tool covers end to end (especially outside Kubernetes)

1. **Controller quorum.**
   - Missing: viewing voters and observers with their lag, adding and removing voters, static → dynamic migration via `kraft.version`, and controller unregistration (from 4.4). Today there is only the `kafka-metadata-quorum.sh` CLI.
   - `add-controller` needs the `directory.id` from the new node's `meta.properties`, so an agent is needed on the hosts.
   - Auto-join (4.2) requires stopping a controller before removing it ([KAFKA-19850](https://issues.apache.org/jira/browse/KAFKA-19850)), so the order of steps must be driven by orchestration.
2. **Broker lifecycle on VMs.** Open source has no ready-made chain of:
   1. formatting storage;
   2. starting the broker;
   3. waiting for registration and unfencing;
   4. filling it with partitions;
   5. cordon (KIP-1066);
   6. draining partitions;
   7. stopping it;
   8. `UnregisterBroker`.

   Cruise Control only covers replica movement.
3. **Rolling restart and upgrade with health checks.** This needs checks for URP, min.ISR and controller lag, then moving leaders back to the preferred replicas after the restart, and `UpdateFeatures` for `metadata.version`. Today this exists only in Strimzi, that is, only on Kubernetes.
4. **Auto-balancing without a jar on the brokers.** Available only in commercial products: Pilot and SBC.
5. **A single pane for data and operations across many clusters.** Open-source UIs do not do operations, and the Cruise Control UI is abandoned.
6. **Kafka 4.x features in UIs** (share groups, KIP-714 client metrics subscriptions, streams groups, cordon) appear to be supported only in fragments. (unverified) Not checked tool by tool.

## 2. Cruise Control

### Status

- On 2026-10-05 the project moved to the Linux Foundation ([announcement](https://www.mail-archive.com/search?l=dev@kafka.apache.org&q=subject:%22Cruise+Control+for+Apache+Kafka+moves+to+the+Linux+Foundation%22)). The technical council has 15 members: chair Viktor Somogyi-Vass, co-chair Kyle Liberti; it includes people from Strimzi and Kafka.
- Artifacts are now published as `io.cruise-control:*` on Maven Central ([README](https://github.com/cruise-control-for-kafka/cruise-control)).
- Version 3.1.0 continues the 2.5.146 line. The 3.0.x line is an internal LinkedIn fork and is unrelated.
- (unverified) The final 3.1.0 has not been released yet.

### KRaft support

- ZooKeeper was removed in 2.5.145 (2025-10-02) in favor of the Admin API ([PR removing ZooKeeper](https://github.com/linkedin/cruise-control/pull/2244)).
- Kafka 4.3.1 is supported in 3.1.0-rc.
- Support for new Kafka versions lags: about 6.5 months for 4.0 (released 2025-03-18) and 5 months for 4.3.

### Limitations

- Not aware of controllers or the quorum.
- In KRaft, `UnregisterBroker` must be called separately after `remove_broker`.
- Relies on Kafka internal APIs (moving off them is in the council's long-term plans).
- Requires a separate JVM process per cluster.
- Needs a broker capacity file.
- Metrics take time to accumulate after startup.
- (unverified) Self-healing in KRaft is poorly documented (issue #2317), and Red Hat Streams on RHEL does not officially support it ([Red Hat: Cruise Control on RHEL](https://docs.redhat.com/en/documentation/red_hat_streams_for_apache_kafka/2.7/html/using_streams_for_apache_kafka_on_rhel_in_kraft_mode/cruise-control-concepts-str)).

### Requirements on the brokers

- The `cruise-control-metrics-reporter` jar in `libs/` and `metric.reporters=com.linkedin.kafka.cruisecontrol.metricsreporter.CruiseControlMetricsReporter`, which means a rolling restart of the brokers.
- The topic `__CruiseControlMetrics` with the delete cleanup policy.
- Client settings with the prefix `cruise.control.metrics.reporter.*`.

The jar can be avoided with `PrometheusMetricSampler` if Prometheus already scrapes broker JMX metrics under the expected names. Java 17 is required. Configuration reference: [Cruise Control configurations](https://github.com/linkedin/cruise-control/wiki/Configurations).

### Strimzi integration

Enabled via `spec.cruiseControl`. The operator itself:

- installs the metrics reporter;
- generates the capacity file;
- locks down the REST API;
- provides `KafkaRebalance`: a proposal first, then approve/refresh/stop, with optional auto-approval;
- triggers an automatic rebalance on scaling ([Strimzi auto-rebalance](https://strimzi.io/blog/2024/11/25/autorebalancing-on-scaling/)).

Strimzi does not use Cruise Control self-healing. Proposal #211 (auto-rebalance on imbalance) is still open.

### Recommendation: our own Go balancer, Cruise Control as an optional backend

- As an optional adapter (for Strimzi users, and for CPU and network goals), Cruise Control is a reasonable integration.
- As the product core for VMs, it is not: a separate JVM per cluster, changes on the brokers, lag behind Kafka releases, and no connection to the broker lifecycle.
- Recommendation: write our own planner in Go behind a `Balancer` interface, and plug Cruise Control in as one of the backends.

### Balancing ideas worth replicating

- **Cruise Control goal hierarchy.** Goals are hard or soft, and a lower-priority goal cannot violate a goal that is already optimized. Goals:
  - rack awareness;
  - disk, network and CPU capacity limits;
  - even distribution of replicas, leaders, and replicas per topic;
  - even disk, network and CPU usage;
  - LeaderBytesIn and PotentialNwOut;
  - preferred leader election;
  - disk balancing inside a broker.

  Actions: replica move, replica swap, leadership move, plus imbalance thresholds. The executor limits concurrency, applies throttles and chooses the order of movements ((unverified) strategy names such as PostponeUrpReplicaMovementStrategy are quoted from memory).
- **kafka-kit.** Rebalancing by storage size with a tolerance. `rebuild` with substitution affinity replaces a broker with a minimum of movements. Reassignments in chunks. Autothrottle: a dynamic throttle only on the brokers taking part in the movement.
- **Pilot and SBC.** Deterministic plans, choosing movements with follower lag in mind, and refusing a drain that would drop below min.ISR.
- **Kafka itself.** [KIP-455](https://cwiki.apache.org/confluence/display/KAFKA/KIP-455%3A+Create+an+Administrative+API+for+Replica+Reassignment): incremental reassignments with cancellation. [KIP-1066](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1066%3A+Mechanism+to+cordon+brokers+and+log+directories): cordoning brokers and log directories. KIP-860: protection against accidental RF changes.
