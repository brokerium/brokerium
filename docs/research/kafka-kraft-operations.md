# Apache Kafka KRaft operations: controller quorum, broker lifecycle, upgrades

Research date: 2026-10-15

Source: research report A (translated from Russian). "(unverified)" marks statements the researcher flagged as uncertain. Releases, Docker images, reassignment, metrics and security are covered in [kafka-releases-docker-reassignment-metrics.md](kafka-releases-docker-reassignment-metrics.md).

## Versions at research time

- Latest releases: 4.3.1 (2026-06-25) and 4.2.2 (2026-09-29) ([downloads](https://kafka.apache.org/community/downloads/), [release catalog](https://release-catalog.apache.org/kafka/index.html)).
- 4.4.0: code freeze was on 2026-08-12. Release candidates were built, but as of ~2026-10-13 there was no release ([4.4 release plan](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/429064575/Release+Plan+4.4.0), [mailing list thread](https://lists.apache.org/thread/94x5qlshh1g0qsldw5ng85xt8jf143zr)). Report B adds that the 4.4.0 vote passed on 2026-10-13 and artifacts were being published, with no announcement yet.
- (unverified) Below, "4.4" means "already merged into 4.4, but not released".

## 1. Controller quorum

### Static vs dynamic quorum

- **Static quorum.** Every node sets `controller.quorum.voters` (`id@host:port`); `kraft.version=0`.
- **Dynamic quorum (KIP-853).**
  - The voter set is stored in `__cluster_metadata` (`VotersRecord`).
  - Nodes discover the quorum via `controller.quorum.bootstrap.servers`; `controller.quorum.voters` must not be set.
  - `kraft.version` ≥ 1 ([KRaft docs 4.3](https://kafka.apache.org/43/operations/kraft/), [KIP-853](https://cwiki.apache.org/confluence/display/KAFKA/KIP-853%3A+KRaft+Controller+Membership+Changes)).
- **Availability.** Dynamic quorum exists since **3.9.0** (2024-11-06) and was not marked as preview. Limitation in 3.9: a static quorum cannot be converted to dynamic ([3.9 release blog](https://kafka.apache.org/blog/2024/11/06/apache-kafka-3.9.0-release-announcement/)). Since 4.0, dynamic is the default mode ([KAFKA-18737](https://issues.apache.org/jira/browse/KAFKA-18737)).
- **How to tell which type a cluster uses.** The type is fixed at `format` time. Check with `kafka-features.sh --bootstrap-controller h:9093 describe`: if `kraft.version` is 0 or absent, the quorum is static; if it is ≥ 1, it is dynamic.

### `kafka-storage.sh format --cluster-id <id> --config <f>`

- `--standalone`: the node becomes the only voter. This is the recommended way to bootstrap:
  - it creates `meta.properties` with a random `directory.id`;
  - it creates a zero checkpoint containing `KRaftVersionRecord` and `VotersRecord`;
  - the flag is "unsafe": run it once, on one node.
- `--initial-controllers "0@c0:9093:<dirUUID>,1@c1:9093:<dirUUID>,…"`: the value must be identical on all initial controllers. Generate the UUIDs with `kafka-storage.sh random-uuid`.
- `--no-initial-controllers`: for new controllers and brokers joining an existing cluster.
  - The code requires one of these three flags only from nodes that have the controller role and no `controller.quorum.voters` ([diff](https://apache.googlesource.com/kafka/+/e3751a838c1a58ba5fe2570b266a9ca1bca8fdae%5E%21/)).
  - For a broker the flag is optional, but the docs show it.

### Adding a controller (dynamic quorum only)

1. Format the node with `--no-initial-controllers` and start it; it becomes an observer.
2. Wait until it catches up with the leader: `kafka-metadata-quorum.sh --bootstrap-controller h:9093 describe --replication`.
3. Run `kafka-metadata-quorum.sh --command-config new.properties --bootstrap-controller h:9093 add-controller` (or use `--bootstrap-server`).
   - The tool reads `node.id`, the listeners and `directory.id` from the config and `meta.properties` of the **new** node, so it must run where those files are available.
   - Authentication settings go into the same file.

### What the leader checks when a voter is added

Per [KIP-853](https://cwiki.apache.org/confluence/display/KAFKA/KIP-853%3A+KRaft+Controller+Membership+Changes):

- There is no uncommitted `VotersRecord`: one change at a time.
- A `LeaderChangeMessage` for the current epoch has been committed.
- The new node supports the current `kraft.version`.
- The new node's fetch offset has caught up with the leader's LEO. Otherwise the request fails with `REQUEST_TIMED_OUT`; the CLI uses a 30 s timeout.
- The change is committed by a majority of the **new** voter set.
- Possible errors: `DUPLICATE_VOTER` (the id is already in the voter set, even with a different directory id) and `UNSUPPORTED_VERSION` (`kraft.version=0`).

### Removing a controller

- Command: `kafka-metadata-quorum.sh --bootstrap-controller h:9093 remove-controller --controller-id <id> --controller-directory-id <uuid>`. Both arguments are required.
- Order since 4.0 (after pre-vote, KIP-996): remove first, then stop the node. The 3.9 docs recommended the reverse order ([3.9 docs](https://kafka.apache.org/39/operations/kraft/), [KAFKA-16164](https://issues.apache.org/jira/browse/KAFKA-16164)).
- The leader can be removed; it resigns once the change is committed. The last voter cannot be removed (`INVALID_REQUEST`).

### Admin API (since 3.9)

Per the [Admin javadoc 4.3](https://kafka.apache.org/43/javadoc/org/apache/kafka/clients/admin/Admin.html):

- `addRaftVoter(int voterId, Uuid voterDirectoryId, Set<RaftVoterEndpoint>[, AddRaftVoterOptions])`.
- `removeRaftVoter(int, Uuid[, RemoveRaftVoterOptions])`.
- `describeMetadataQuorum()`.
- The options accept a `clusterId`; on mismatch the call fails with `InconsistentClusterIdException`.
- `RaftVoterEndpoint.name()` is deprecated since 4.1; use `listener()` instead.

### Controller auto-join (4.2)

`controller.quorum.auto.join.enable=true` (default `false`) ([upgrade notes](https://kafka.apache.org/43/getting-started/upgrade/), [KIP-1186](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/369166805/KIP-1186+Update+AddRaftVoterRequest+RPC+to+support+auto-join)).

- The observer itself removes voter entries that have its id and a different directory id, then adds itself.
- With auto-join enabled, a controller must be **stopped before** `remove-controller`; otherwise it rejoins the quorum. Report C cites [KAFKA-19850](https://issues.apache.org/jira/browse/KAFKA-19850) for this caveat.

### Static → dynamic conversion (since 4.1.0)

Sources: [KAFKA-16538](https://issues.apache.org/jira/browse/KAFKA-16538), [KRaft docs 4.3](https://kafka.apache.org/43/operations/kraft/).

1. Roll all controllers and brokers to version ≥ 4.1.
2. Run `kafka-features.sh --bootstrap-server b:9092 upgrade --feature kraft.version=1`. Alternatively, `--release-version` with 4.1 or later upgrades all features, including `kraft.version`.
3. Verify that `FinalizedVersionLevel=1`.
4. Rolling restart: replace `controller.quorum.voters` with `controller.quorum.bootstrap.servers` on all nodes. Removing the voters setting is safe once the metric `ignored-static-voters` (on controllers: `IgnoredStaticVoters`) equals 1 on all nodes.

Caveats:

- Downgrading `kraft.version` from 1 to 0 is rejected.
- The leader checks that all voters and all registered brokers support the new version.
- The operation is not supported in 4.0 ([KAFKA-18979](https://issues.apache.org/jira/browse/KAFKA-18979)).
- *Takeaway for the control plane:* finalizing `--release-version` ≥ 4.1 on a static cluster irreversibly makes it dynamic. Make this a separate step that requires confirmation.

### Quorum sizing and combined mode

- A majority is required, N = 2f+1: 3 controllers survive 1 failure, 5 survive 2. An even count adds no fault tolerance.
- The docs choose "3 or 5" based on cost and the number of simultaneous failures to tolerate; there is no link to the number of brokers. Typical sizing: 5 GB RAM and 5 GB disk for metadata.
- Combined mode (`process.roles=broker,controller`) is "not recommended in critical deployment environments": controllers cannot be rolled or scaled separately from brokers.

### Replacing a controller

Because of `DUPLICATE_VOTER`, replacing a controller while keeping the same `node.id` only works as remove, then add. During that window the quorum shrinks from 3 to 2 voters with no failure headroom ([example](https://medium.com/@prakashpsgcse/hands-on-kraft-part-4-day-2-operations-disk-or-node-recovery-quorum-descaling-and-05c1f7f2037c)). It is safer to use a new id: add the new controller, then remove the old one (3 → 4 → 3).

## 2. Brokers

### Adding a broker

- **`node.id`.** Must be unique across the shared broker and controller id space ([3.9 docs](https://kafka.apache.org/39/operations/kraft/)). `broker.id` is deprecated in 4.4 ([KIP-1232](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1232%3A+Deprecate+broker.id+config)).
- **Formatting:** `kafka-storage.sh format --cluster-id <existing> --config server.properties --no-initial-controllers`.
- **Registration** ([KIP-631](https://cwiki.apache.org/confluence/display/KAFKA/KIP-631%3A+The+Quorum-based+Kafka+Controller)):
  - the broker sends an incarnation id (a new UUID on every start), its listeners, features and log directory UUIDs;
  - the controller assigns a broker epoch and rejects a foreign cluster id or unsupported features;
  - the broker starts fenced and becomes unfenced once it has caught up with the metadata.
- **Partitions do not move by themselves.** The docs say "new servers will not automatically be assigned any data partitions". Move them with `kafka-reassign-partitions.sh` using `--throttle` ([operations 4.3](https://kafka.apache.org/43/operations/basic-kafka-operations/)).

### Decommissioning a broker (docs 4.3)

1. Cordon: `kafka-configs.sh --bootstrap-server b:9092 --alter --add-config cordoned.log.dirs="*" --entity-type brokers --entity-name <id>`.
2. Reassign all of its replicas. The tooling does not yet generate a decommission plan.
3. Stop the broker. This is a controlled shutdown (`controlled.shutdown.enable=true` by default).
4. Unregister: `kafka-cluster.sh unregister --bootstrap-server b:9092 --id <id>`.
   - This is `Admin.unregisterBroker(int)`: `@Unstable`, available since 2.8, RPC from KIP-631, requires the ALTER ACL on CLUSTER.
   - The command does not touch partition assignments.
   - It works via `--bootstrap-controller` since 4.1 ([KAFKA-17039](https://issues.apache.org/jira/browse/KAFKA-17039)).

### What happens without unregister

The broker stays "registered but fenced":

- follower replicas can be placed on it when there are not enough unfenced brokers ([StripedReplicaPlacer](https://github.com/apache/kafka/blob/trunk/metadata/src/main/java/org/apache/kafka/metadata/placement/StripedReplicaPlacer.java));
- it blocks `metadata.version` upgrades.

Such brokers are listed by `kafka-cluster.sh list-endpoints --include-fenced-brokers` (4.0, [KIP-1073](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1073%3A+Return+fenced+brokers+in+DescribeCluster+response)).

*Researcher's inference, not described in the docs:* after unregister, delete the `cordoned.log.dirs` setting for that id via `--bootstrap-controller`. Since 4.3, deleting a non-existent key is a no-op. Otherwise a broker that later gets the same id will come up cordoned.

### KIP-1066: cordoning brokers and log directories

Accepted; shipped in **4.3.0** (moved from 4.2) ([4.3 release blog](https://kafka.apache.org/blog/2026/05/22/apache-kafka-4.3.0-release-announcement/), [KIP-1066](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1066%3A+Mechanism+to+cordon+brokers+and+log+directories)).

- Config `cordoned.log.dirs`: list, per broker, dynamic; `*` means all directories.
- Requires a finalized `metadata.version` that supports the feature. (unverified) Most likely 4.3-IV0.
- Effect on requests:
  - CreateTopics: `INVALID_REPLICATION_FACTOR` or `INVALID_REPLICA_ASSIGNMENT`;
  - reassignment onto a cordoned broker: `INELIGIBLE_REPLICA`;
  - AlterReplicaLogDirs: `KAFKA_STORAGE_ERROR`.
- Where the state is visible:
  - `DescribeLogDirs` v5 (`IsCordoned`) and `LogDirDescription.isCordoned()`;
  - metrics `LogDirectoryCordoned` and `CordonedLogDirectoryCount`.

### Replacing a failed broker

(unverified) No separate official procedure was found in the Apache docs.

**Option 1: same `node.id`, new disk.**

- Format with the previous `cluster.id`; the node gets new `directory.id` values ([KIP-858](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/217392620/KIP-858+Handle+JBOD+broker+disk+failure+in+KRaft)).
- On start it has a new incarnation id. While the old session is still alive, registration fails with `DUPLICATE_BROKER_REGISTRATION`; wait for `broker.session.timeout.ms` (9 s) ([KAFKA-19047](https://issues.apache.org/jira/browse/KAFKA-19047), improved in 4.1).
- The broker gets a new broker epoch:
  - [KIP-903](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/240880955/KIP-903+Replicas+with+stale+broker+epoch+should+not+be+allowed+to+join+the+ISR) (3.5) keeps an empty replica with a stale epoch out of the ISR;
  - [ELR](https://kafka.apache.org/41/operations/eligible-leader-replicas/) (KIP-966; enabled by default for new clusters since 4.1) treats the shutdown as unclean because the clean-shutdown file is missing.
- On JBOD, replicas from unknown directories are reassigned via `AssignReplicasToDirs` while the broker is fenced.
- Data is copied again from the leaders. Since 4.3 the flag `follower.fetch.last.tiered.offset.enable` (KIP-1023) skips data already uploaded to tiered storage.

**Option 2: new `node.id`.** Add a broker, reassign the dead id's replicas to it, then unregister the old id.

## 3. Upgrades

### Prerequisites for 3.x → 4.x

Sources: [upgrade notes](https://kafka.apache.org/43/getting-started/upgrade/), [Java versions](https://kafka.apache.org/43/operations/java-version/).

- KRaft only; the software version and `metadata.version` must be ≥ 3.3. KRaft clusters older than 3.3 must first be upgraded to 3.9.x.
- ZooKeeper clusters must first migrate on a bridge release; the last bridge release is 3.9.
- Clients and brokers must be ≥ 2.1 (KIP-896).
- Java: brokers, Connect and tools need 17+; clients and Streams need 11+. 4.3 fully supports Java 17, 21 and 25.

### Procedure (the same for 4.0–4.3)

1. Roll the nodes one at a time.
2. Check cluster behavior and performance.
3. Finalize: `kafka-features.sh --bootstrap-server b:9092 upgrade --release-version 4.3`.

(unverified) Apache does not prescribe a controllers-first or brokers-first order. CFK rolls controllers first ([CFK docs](https://docs.confluent.io/operator/current/co-upgrade-cp.html)); the CP 8.3 docs roll brokers first ([CP docs](https://docs.confluent.io/platform/current/installation/upgrade.html)). Finalize only when all nodes run the new binaries.

See also KAFKA-20979 (time index truncation not fsynced on clean shutdown; matters for rolling restarts) in [kafka-releases-docker-reassignment-metrics.md](kafka-releases-docker-reassignment-metrics.md#1-releases-and-java).

### `kafka-features.sh`

Per [KIP-1022](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/293047057/KIP-1022+Formatting+and+Updating+Features):

- `describe`; the `--node-id` flag exists since 4.2.
- `upgrade --release-version`; the old `--metadata` flag is deprecated since 4.0.
- `upgrade` and `downgrade` with `--feature name=level`.
- `version-mapping` and `feature-dependencies`.
- All subcommands also work via `--bootstrap-controller`.

### Metadata downgrade limits

- A downgrade is possible only if there are no metadata changes between the versions: downgrading from 4.2 is possible, from 4.0 and 4.3 it is not.
- `--unsafe` is not implemented ([KAFKA-15537](https://issues.apache.org/jira/browse/KAFKA-15537)).
- Full downgrade support is described in [KIP-1155](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1155:+Metadata+Version+Downgrades), which is still Under discussion.
- `kraft.version` cannot be downgraded; ELR can (`eligible.leader.replicas.version=0`).

## 4. KIPs 2025–2026 relevant to cluster management

### Released

| KIP / JIRA | What | Version |
|---|---|---|
| KIP-996 | Pre-vote | 4.0 |
| [KIP-1073](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1073%3A+Return+fenced+brokers+in+DescribeCluster+response) | Fenced brokers in DescribeCluster | 4.0 |
| KIP-966 ([ELR docs](https://kafka.apache.org/41/operations/eligible-leader-replicas/)) | Eligible leader replicas (ELR) | 4.0 as opt-in; default for new clusters since 4.1 |
| [KAFKA-16538](https://issues.apache.org/jira/browse/KAFKA-16538) | Static → dynamic quorum conversion | 4.1 |
| [KIP-1186](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/369166805/KIP-1186+Update+AddRaftVoterRequest+RPC+to+support+auto-join) | Controller auto-join | 4.2 |
| [KIP-1066](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1066%3A+Mechanism+to+cordon+brokers+and+log+directories) | Cordon | 4.3 |
| KIP-1219 | Limits `controller.quorum.fetch(.snapshot).max.bytes` | 4.3 |
| [KIP-1023](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/293047026/KIP-1023+Follower+fetch+from+tiered+offset) | Follower fetch from tiered offset | 4.3 |
| KAFKA-18928 | Dynamic configs for controllers in a dynamic quorum | 4.3 |

### Merged into 4.4 (not released at research time)

- [KIP-1312](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/406623954/KIP-1312+Support+unregistering+controllers) ([KAFKA-20395](https://issues.apache.org/jira/browse/KAFKA-20395)): unregistering controllers.
  - `kafka-cluster.sh unregister-controller`;
  - `remove-controller --unregister`;
  - `Admin.unregisterController`;
  - requires a new `metadata.version`; the ban on unregistering voters comes in 4.5.
- [KIP-1170](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1170:+Unify+cluster+metadata+bootstrapping): removes `bootstrap.checkpoint`.
- [KIP-1220](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1220%3A+Unify+kafka-broker-api-versions+tool+to+cluster-tool+and+support+bootstrap+controllers): `kafka-cluster.sh api-versions` with `--bootstrap-controller` support.
- [KIP-1232](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1232%3A+Deprecate+broker.id+config): deprecates `broker.id`.

### Accepted or under discussion

- [KIP-1141](https://cwiki.apache.org/confluence/spaces/KAFKA/pages/340037949/KIP-1141+Simplifying+Add+Remove+Voter+in+MetadataQuorumCommand): add/remove-controller without a local config; the directory id becomes optional. Accepted, target 4.5. (unverified)
- [KIP-1150](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1150:+Diskless+Topics) Diskless Topics: accepted on 2026-03-02 only as a direction, without code ([vote](https://www.mail-archive.com/dev@kafka.apache.org/msg154859.html)).
  - KIP-1163 and KIP-1164: Under discussion.
  - KIP-1176: withdrawn.
  - KIP-1183: Under discussion.

### Not found

(unverified) No KIP was found for built-in automatic rebalancing or for generating a decommission plan; an external tool such as Cruise Control is still needed for that.

## Control-plane takeaways

Collected from the sections above:

- Make finalizing `--release-version` ≥ 4.1 on a static-quorum cluster a separate, explicitly confirmed step: it irreversibly converts the cluster to a dynamic quorum.
- Replace controllers with a new id (add, then remove: 3 → 4 → 3). Reusing the same id forces remove-then-add and leaves no failure headroom.
- With auto-join enabled, stop the controller before `remove-controller`.
- Always unregister decommissioned brokers: fenced registrations can receive follower replicas and block `metadata.version` upgrades. Afterwards delete the id's `cordoned.log.dirs` setting (researcher's inference).
- Kafka does not move partitions onto new brokers and does not generate decommission plans, so placement and drain plans are the control plane's job.
- `add-controller` needs the new node's `directory.id` from `meta.properties`; report C concludes from this that a host agent is needed. KIP-1141 (target 4.5, unverified) would make the directory id optional.
