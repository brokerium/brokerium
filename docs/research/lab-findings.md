# Lab findings (Kafka 4.3.1)

Research date: 2026-10-15. Observed in the Brokerium Docker lab while validating the runbooks in
[docs/runbooks/](../runbooks/). Each finding changes how Brokerium implements an operation.

| # | Finding | Consequence for Brokerium |
| --- | --- | --- |
| 1 | With RF 3 over three racks, a rack with a single broker forces that broker to host a replica of every partition (`kafka-reassign-partitions --generate` gave 42 replicas to the lone rack-c broker and 21 to the others). | The planner reports rack constraints and the best achievable balance; the dashboard suggests adding brokers to the short rack. |
| 2 | The stock reassignment tool does not balance leaders (19 leaders on one broker, 4-7 on others after a rebalance). | Balance leaders as a separate goal (replica order plus preferred leader election). |
| 3 | `kafka-reassign-partitions --execute --throttle` fails when a broker in the current replica sets is dead: `incrementalAlterConfigs` times out ("Timed out waiting for a node assignment") and no reassignment starts. | Set throttles only on live brokers; never let a dead broker block a replacement. |
| 4 | `cordoned.log.dirs=*` (KIP-1066) keeps new topics off a broker, but a reassignment onto the cordoned broker was accepted. | Brokerium rejects moves onto cordoned or draining brokers itself. |
| 5 | After `unregister`, a broker's dynamic config cannot be changed (`BrokerIdNotRegisteredException`), yet it survives: a later broker with the same id was cordoned from its first start. | Remove the cordon before unregistering; on registration of a known id, check for leftover configs. |
| 6 | Removed and stopped controllers remain in the observer list of `DescribeQuorum` with growing lag; Kafka 4.3 cannot unregister them (KIP-1312 is 4.4). | Hide stale observers that are not registered brokers or flag them as "removed controller". |
| 7 | `add-controller` must run where the new node's config and `meta.properties` are (it reads `directory.id` locally). | The agent reports `directory.id`; Brokerium calls AddRaftVoter itself. |
| 8 | A broker re-created with the same id on an empty disk gets a new `directory.id` and re-replicates automatically; no plan is needed. | Offer "replace in place" as the cheapest replacement path. |
| 9 | In KRaft the controller applies `default.replication.factor`, `num.partitions` and `min.insync.replicas` when topics are created; with these set only on brokers, new topics got `min.insync.replicas=1` as a topic config. | Lab and agent-managed nodes set cluster defaults on controllers too; the dashboard shows topic-level `min.insync.replicas`. |
| 10 | The stock image entrypoint formats storage only for static quorums and cannot start a controller that has `controller.quorum.bootstrap.servers` only. | The agent formats storage itself (ADR 0007). |
| 11 | A controller listener bound to the host name does not answer on `127.0.0.1`. | Health checks connect to the advertised host. |
| 12 | `kafka-producer-perf-test.sh` in 4.3 takes `--bootstrap-server`; `--command-property bootstrap.servers=...` is not applied. | Lab tooling uses `--bootstrap-server`. |
| 13 | Replacement with a new id keeps the preferred-replica order, but leadership returns only after the automatic leader rebalance (up to 300 s) or an explicit preferred election. | Run a preferred leader election at the end of every move operation. |
