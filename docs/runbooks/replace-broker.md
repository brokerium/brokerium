# Replace a failed broker (scenario 4)

Validated on Kafka 4.3.1, cluster alpha with brokers 101-105.

## Variant A: new node id

Simulate losing broker 103 together with its disk:

```bash
./lab.sh rm alpha-b3
./lab.sh cli kafka-cluster.sh list-endpoints --bootstrap-server alpha-b1:9092 --include-fenced-brokers
```

Broker 103 becomes `fenced`; 42 partitions are under-replicated. Start the replacement broker 106
(same rack, rack-c):

```bash
./lab.sh up alpha-replace
```

Build a plan that replaces 103 with 106 in every replica list, keeping the order so preferred
leaders stay in place:

```json
{"version":1,"partitions":[{"topic":"clicks","partition":0,"replicas":[106,101,105]}, "..."]}
```

Execute it **without `--throttle`**:

```bash
./lab.sh cli kafka-reassign-partitions.sh --bootstrap-server alpha-b1:9092,alpha-b2:9092 \
  --execute --reassignment-json-file /tmp/replace.json
```

With `--throttle` the tool tries to set throttles on every broker in the current replica sets,
including the dead broker 103. `incrementalAlterConfigs` for that broker waits for a node that does
not exist and fails after about a minute with `Timed out waiting for a node assignment`; no
reassignment is started.

The move completed in about 20 seconds and under-replicated partitions dropped to zero. Then:

```bash
./lab.sh cli kafka-cluster.sh unregister --bootstrap-server alpha-b1:9092 --id 103
./lab.sh cli kafka-leader-election.sh --bootstrap-server alpha-b1:9092 \
  --election-type PREFERRED --all-topic-partitions
```

Unregister only after the reassignment has completed; check with `--verify` and `dist`, not just
by the absence of "in progress" in the output.

## Variant B: same node id on an empty disk

```bash
./lab.sh rm alpha-b2          # 21 partitions under-replicated
./lab.sh start alpha-b2       # same node.id 102, empty volume
```

The node formats storage again with the existing cluster id and gets a **new** `directory.id`
(`qrkkW7mfLD_2A67arBRD_g` before, `rp6yy6V6LZdUOfdXaUHdTw` after). It registers as 102 and the
followers re-replicate from the leaders automatically; under-replicated partitions were back to
zero within seconds for this data size. No reassignment is needed.

## Notes for Brokerium

- Throttle only live brokers. A dead broker is not a source of data anyway.
- Prefer variant B when the node id can be reused; it needs no plan.
- Verify completion against the target replica sets before unregistering.
