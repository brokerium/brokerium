# Grow the controller quorum to 5 and back to 3 (scenario 3)

Validated on Kafka 4.3.1, cluster alpha with a dynamic quorum (voters 1-3, formatted with
`--initial-controllers`).

## Add controllers 4 and 5

```bash
./lab.sh up alpha-quorum      # controllers 4 and 5, formatted with --no-initial-controllers
./lab.sh cli kafka-metadata-quorum.sh --bootstrap-server alpha-b1:9092 describe --replication
```

The new controllers join as **observers** and catch up immediately:

```text
NodeId DirectoryId            LogEndOffset Lag Status
2      NO4NIwnXQ46uHuKwobUSPw 567          0   Leader
1      tQZ2Eap5SyKCiBmkOuSVBQ 567          0   Follower
3      5fgXeFksS8C1SNc1k54cVQ 567          0   Follower
4      rdpjoVmk8o-q99KdwJNdmA 567          0   Observer
5      WX68_ay2o6wl-FwTAoPi0A 567          0   Observer
```

Promote them one at a time. `add-controller` reads `node.id`, the controller listener and the
`directory.id` from the **new node's** configuration and `meta.properties`, so it runs on that node:

```bash
./lab.sh exec alpha-c4 /opt/kafka/bin/kafka-metadata-quorum.sh \
  --bootstrap-controller alpha-c1:9093 --command-config /tmp/server.properties add-controller
# Added controller 4 with directory id rdpjoVmk8o-q99KdwJNdmA and endpoints: CONTROLLER://alpha-c4:9093
./lab.sh exec alpha-c5 /opt/kafka/bin/kafka-metadata-quorum.sh \
  --bootstrap-controller alpha-c1:9093 --command-config /tmp/server.properties add-controller
```

`describe --status` then lists five voters.

## Remove controllers 5 and 4

Since Kafka 4.0 the order is: remove the voter, then stop the node.

```bash
./lab.sh cli kafka-metadata-quorum.sh --bootstrap-controller alpha-c1:9093 remove-controller \
  --controller-id 5 --controller-directory-id WX68_ay2o6wl-FwTAoPi0A
./lab.sh stop alpha-c5
./lab.sh cli kafka-metadata-quorum.sh --bootstrap-controller alpha-c1:9093 remove-controller \
  --controller-id 4 --controller-directory-id rdpjoVmk8o-q99KdwJNdmA
./lab.sh stop alpha-c4
```

Removing a voter that is no longer in the set fails with `VoterNotFoundException`.

## Observations

- Stopped former voters stay in the observer list with growing lag. Kafka 4.3 has no way to
  unregister a controller; `unregister-controller` (KIP-1312) arrives in 4.4.
- Quorum changes are committed in about a second when the new node has caught up.
- Brokerium must collect `directory.id` from the node (agent) to call `AddRaftVoter`.
