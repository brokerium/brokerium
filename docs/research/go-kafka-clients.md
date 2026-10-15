# Go Kafka clients for the Brokerium control plane

Research date: 2026-10-15

Source: research report C, section 3 (translated from Russian). "(unverified)" marks statements the researcher flagged as needing verification. Java Admin API names used below are described in [kafka-kraft-operations.md](kafka-kraft-operations.md) and [kafka-releases-docker-reassignment-metrics.md](kafka-releases-docker-reassignment-metrics.md).

## Summary

- The best Go client is **franz-go**.
- `kmsg` has all the KRaft requests a control plane needs. `kadm` lacks convenience wrappers for quorum, voters, unregister and DescribeCluster, but they are easy to write.
- No gaps were found in the protocol itself; only convenience wrappers and request routing are missing.
- sarama lags on admin and protocol features; confluent-kafka-go (librdkafka, CGO) is not suitable for a control plane.

## franz-go (BSD-3)

- Versions: v1.22.2 (2026-10-13), `kadm` v1.19.0 (2026-09-18), `kmsg` v1.15.0 (2026-10-12) ([franz-go tags](https://github.com/twmb/franz-go/tags)).
- Supports Kafka 0.8 through 4.4. `kmsg` defines all API keys from 0 to 94 ([protocol definitions](https://github.com/twmb/franz-go/tree/master/generate/definitions)).
- There is essentially one maintainer (twmb; the changelog is written in the first person). This is a risk if he steps away from the project.
- Users listed in the README include Redpanda Console, Redpanda Data, Benthos, Conduit, DeltaStream, Hilton, Mux, ShareChat, Unity, Zomato and others.
- `kfake`: a built-in fake broker for tests, with fault injection.

### `kadm` vs `kmsg` coverage

What is covered by the high-level `kadm` package ([kadm docs](https://pkg.go.dev/github.com/twmb/franz-go/pkg/kadm)) and what is available only as raw `kmsg` requests:

| API | `kadm` | `kmsg` |
|---|---|---|
| DescribeQuorum, AddRaftVoter, RemoveRaftVoter | ✗ (deliberately; [issue #952](https://github.com/twmb/franz-go/issues/952): "too niche") | ✓ |
| UnregisterBroker; UnregisterController (Kafka 4.4) | ✗ | ✓ |
| DescribeCluster (controllers, fenced brokers) | ✗ | ✓ |
| UpdateFeatures / DescribeFeatures | ✓ / via `ApiVersions` and the kversion helpers | ✓ |
| AlterPartitionReassignments / ListPartitionReassignments | ✓ (plus `ForbidAlterRf`) | ✓ |
| DescribeLogDirs (with IsCordoned), ElectLeaders, SCRAM, ACLs, quotas | ✓ | ✓ |
| Client metrics (KIP-714) | sending telemetry ✓, `ListConfigResources` ✓, altering CLIENT_METRICS configs ✗ | ✓ |
| Share groups (KIP-932) | ✓ (consumer, describe, offsets) | ✓ |
| Streams groups | ✗ | ✓ |

### Request routing caveats

- DescribeQuorum is marked as an admin request: it goes to a broker, and the broker forwards it to the controller.
- Adding and removing controllers and unregistration also work through brokers.
- franz-go has no `bootstrap.controllers` mode ([KIP-919](https://cwiki.apache.org/confluence/display/KAFKA/KIP-919%3A+Allow+AdminClient+to+Talk+Directly+with+the+KRaft+Controller+Quorum+and+add+Controller+Registration)), and controllers do not answer Metadata requests.
- (unverified) Sending requests directly to controllers is probably possible via `SeedBrokers()` and `Broker.Request` with raw `kmsg` requests. This needs to be verified with a prototype.

### Gaps vs the Java Admin client

Available in the [Java Admin 4.3](https://kafka.apache.org/43/javadoc/org/apache/kafka/clients/admin/Admin.html) but not in `kadm`:

- `describeMetadataQuorum`;
- `add/removeRaftVoter`;
- `unregisterBroker`;
- `describeCluster` for controllers;
- streams group operations;
- `fenceProducers`;
- `terminateTransaction`;
- configs for resource types other than topic and broker;
- the `bootstrap.controllers` mode.

No gaps were found in the protocol itself: only convenience wrappers and routing are missing.

## sarama (IBM)

- v1.61.1 (2026-09-27), actively developed ([releases](https://github.com/IBM/sarama/releases)).
- Its ClusterAdmin has no quorum, voter, UnregisterBroker or share group support.
- It catches up with the protocol with a delay: cooperative rebalancing and KIP-848 describe appeared only in 1.61.0.

## confluent-kafka-go

- v2.16.0 (2026-10-07); requires CGO ([releases](https://github.com/confluentinc/confluent-kafka-go/releases)).
- In librdkafka, partition reassignment (KIP-455), quotas (KIP-546) and features (KIP-584) are marked "Not supported" ([librdkafka KIP support](https://github.com/confluentinc/librdkafka/blob/master/INTRODUCTION.md)).
- Not suitable for a control plane.

## Implications for Brokerium

- Use franz-go; write thin wrappers over raw `kmsg` requests for DescribeQuorum, AddRaftVoter, RemoveRaftVoter, UnregisterBroker, UnregisterController and DescribeCluster.
- Build a prototype for talking to controllers directly (`SeedBrokers()` + `Broker.Request`) before relying on it; until then, quorum requests go through brokers.
- `kfake` can back tests with fault injection.
- Keep the single-maintainer risk of franz-go in mind.
