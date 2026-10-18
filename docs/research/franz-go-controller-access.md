# Direct franz-go access to KRaft controllers (spike)

Research date: 2026-10-15. Kafka 4.3.1, franz-go v1.22.2, lab cluster `delta` (3 controllers,
4 brokers, dynamic quorum). Issue: [#14](https://github.com/brokerium/brokerium/issues/14).

## Question

The Java admin client can talk to controllers without brokers (`bootstrap.controllers`,
[KIP-919](https://cwiki.apache.org/confluence/display/KAFKA/KIP-919%3A+Allow+AdminClient+to+Talk+Directly+with+the+KRaft+Controller+Quorum+and+add+Controller+Registration)).
franz-go has no such mode. Can Brokerium still read and change the quorum through the
controller listeners alone?

## Method

`test/e2e/controllers_test.go` (`TestControllersDirect`, build tag `e2e`) runs inside the lab
network against `delta-c1..c3:9093`:

```sh
GOOS=linux GOARCH=arm64 go test -c -tags e2e -o .data/e2e.test ./test/e2e
docker run --rm --network brokerium-lab -v "$PWD/.data:/t:ro" \
  -e BROKERIUM_E2E_CONTROLLERS=delta-c2:9093,delta-c1:9093,delta-c3:9093 \
  --entrypoint /t/e2e.test apache/kafka:4.3.1 -test.v -test.run TestControllersDirect
```

## Findings

1. **Requests sent straight to a seed connection work.** `kgo.Client.SeedBrokers()[i].Request`
   skips metadata routing. `DescribeCluster` with `EndpointType=2` returned the cluster id,
   the active controller and the controller endpoints; `DescribeQuorum` returned leader,
   epoch, voters and observers.
2. **Only the quorum leader answers DescribeQuorum.** Other controllers reply
   `NOT_LEADER_OR_FOLLOWER`; trying the seeds in turn finds the leader (seed order c2, c1, c3
   found leader 1).
3. **Controllers do not serve Metadata.** The normal franz-go path (`req.RequestWith(ctx, cl)`)
   needs cluster metadata, and the controller closes the connection on the Metadata request.
   franz-go reports it as "broker closed the connection immediately after a request was
   issued, which often happens when SASL is required".
4. **APIs on the controller listener (41):** AddRaftVoter, AllocateProducerIDs,
   AlterClientQuotas, AlterConfigs, AlterPartition, AlterPartitionAssignments,
   AlterUserSCRAMCredentials, ApiVersions, AssignReplicasToDirs, BeginQuorumEpoch,
   BrokerHeartbeat, BrokerRegistration, ControllerRegistration, CreateACLs,
   CreateDelegationToken, CreatePartitions, CreateTopics, DeleteACLs, DeleteTopics,
   DescribeACLs, DescribeCluster, DescribeConfigs, DescribeDelegationToken, DescribeQuorum,
   DescribeUserSCRAMCredentials, ElectLeaders, EndQuorumEpoch, Envelope, ExpireDelegationToken,
   Fetch, FetchSnapshot, IncrementalAlterConfigs, ListPartitionReassignments, RemoveRaftVoter,
   RenewDelegationToken, SASLAuthenticate, SASLHandshake, UnregisterBroker, UpdateFeatures,
   UpdateRaftVoter, Vote. Not served: Metadata, ListOffsets, DescribeLogDirs, group APIs,
   Produce.
5. **Removed controllers stay registered in 4.3.1.** `DescribeCluster` with the controller
   endpoint type still listed controllers 4 and 5, removed from the quorum and deleted earlier
   in the lab. Unregistering controllers arrives with
   [KIP-1312](https://cwiki.apache.org/confluence/display/KAFKA/KIP-1312) in 4.4; until then
   the controller registration list is not a reliable inventory. Use the voter set from
   DescribeQuorum.
6. **ApiVersions v3+ needs a client software name.** A raw `ApiVersionsRequest` without
   `ClientSoftwareName`/`ClientSoftwareVersion` gets `INVALID_REQUEST` and an empty key list.

## Consequences for Brokerium

- Default path stays as in [ADR 0005](../adr/0005-kafka-client.md): KRaft requests go to
  brokers, which forward them to the active controller. Brokers are reachable from the server
  in every deployment; controller listeners often are not.
- `kafka.ControllerClient` implements the direct path (seed requests, leader search). The
  cluster service uses it as a fallback: when no broker answers and the cluster has
  `settings.controllerBootstrapServers`, the dashboard still shows the quorum, and health
  reports `brokers_unreachable` instead of a blank cluster.
- Docker lab clusters publish controller port 9093 on `localhost:<port base + node id>` and
  fill `controllerBootstrapServers` automatically.
- A cluster snapshot (topics, partitions, log dirs, groups) still needs brokers.
