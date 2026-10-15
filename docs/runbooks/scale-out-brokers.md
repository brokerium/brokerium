# Add brokers and rebalance with throttling (scenario 2)

Validated on Kafka 4.3.1, cluster alpha (controllers 1-3, brokers 101-103 on racks a, b, c),
topics `orders` (12 partitions), `payments` (6), `clicks` (24), RF 3, about 300 MB of data.

## 1. Start the new brokers

```bash
./lab.sh up alpha-scale          # brokers 104 (rack-a) and 105 (rack-b)
./lab.sh cli kafka-cluster.sh list-endpoints --bootstrap-server alpha-b1:9092
```

The new nodes format storage with `--no-initial-controllers`, register and become `unfenced`
within seconds. They receive **no partitions**:

```text
broker 101 replicas=42 leaders=14
broker 102 replicas=42 leaders=14
broker 103 replicas=42 leaders=14
```

## 2. Plan

```bash
echo '{"version":1,"topics":[{"topic":"orders"},{"topic":"payments"},{"topic":"clicks"}]}' \
  | ./lab.sh put /tmp/topics.json
./lab.sh cli kafka-reassign-partitions.sh --bootstrap-server alpha-b1:9092 --generate \
  --topics-to-move-json-file /tmp/topics.json --broker-list 101,102,103,104,105
```

Save the "Proposed partition reassignment configuration" JSON as `/tmp/plan.json` in the tools
container (`./lab.sh put /tmp/plan.json`). The proposal changed 29 of 42 partitions with 42 new
replica placements.

## 3. Execute with a throttle and verify

```bash
./lab.sh cli kafka-reassign-partitions.sh --bootstrap-server alpha-b1:9092 --execute \
  --reassignment-json-file /tmp/plan.json --throttle 20000000
./lab.sh cli kafka-reassign-partitions.sh --bootstrap-server alpha-b1:9092 --verify \
  --reassignment-json-file /tmp/plan.json      # repeat until nothing is in progress
```

`--execute` sets `leader.replication.throttled.rate` and `follower.replication.throttled.rate` on
the brokers and the throttled-replica lists on the topics. `--verify` removes them once the
reassignment completes. The move finished in about 5 seconds; no under-replicated partitions.

## 4. Result

```text
broker 101 replicas=21 leaders=7
broker 102 replicas=21 leaders=4
broker 103 replicas=42 leaders=19
broker 104 replicas=21 leaders=7
broker 105 replicas=21 leaders=5
```

Broker 103 keeps 42 replicas because it is the only broker in rack-c: with RF 3 over three racks,
every partition needs one replica in each rack. Even placement needs the same number of brokers per
rack. Leaders are not balanced by the stock tool even where the rack constraint allows it; run a
preferred leader election or let the planner reorder replicas.

## Notes for Brokerium

- Show the rack constraint explicitly ("rack-c has one broker; it must host every partition").
- Balance leaders separately from replicas.
