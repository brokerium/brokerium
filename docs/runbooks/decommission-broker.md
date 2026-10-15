# Decommission a broker (scenario 5)

Validated on Kafka 4.3.1 (`metadata.version` 4.3-IV0), cluster alpha, broker 105.

## 1. Cordon

```bash
./lab.sh cli kafka-configs.sh --bootstrap-server alpha-b1:9092 --alter \
  --add-config 'cordoned.log.dirs=*' --entity-type brokers --entity-name 105
```

New topics avoid the broker: a topic with 12 partitions and RF 3 got no replica on 105.

**Cordon does not block reassignments in 4.3.1.** Moving a replica of `after-cordon-0` onto 105
was accepted ("Successfully started partition reassignment"), and the replica was created there.
Brokerium must reject such moves itself.

## 2. Drain

Plan: replace 105 in every replica list. Rack-b now has only broker 102, so 105 is replaced by 102
in place. Execute with a throttle (all involved brokers are alive):

```bash
./lab.sh cli kafka-reassign-partitions.sh --bootstrap-server alpha-b1:9092 --execute \
  --reassignment-json-file /tmp/drain.json --throttle 20000000
./lab.sh cli kafka-reassign-partitions.sh --bootstrap-server alpha-b1:9092 --verify \
  --reassignment-json-file /tmp/drain.json
```

21 partitions moved in about 10 seconds; the throttle rates were visible on the brokers during the
move and removed by `--verify`.

## 3. Remove the cordon, stop and unregister

```bash
./lab.sh cli kafka-configs.sh --bootstrap-server alpha-b1:9092 --alter \
  --delete-config cordoned.log.dirs --entity-type brokers --entity-name 105
./lab.sh stop alpha-b5      # controlled shutdown: "The controller has asked us to exit controlled shutdown"
./lab.sh cli kafka-cluster.sh unregister --bootstrap-server alpha-b1:9092 --id 105
```

Delete the cordon **before** unregistering. After `unregister`, changing the broker's dynamic
config fails with `BrokerIdNotRegisteredException` through both `--bootstrap-server` and
`--bootstrap-controller`, but the config survives: a new broker that registered later with id 105
(empty disk) was immediately cordoned and received no replicas. Deleting the config while that
broker was registered fixed it.

## Notes for Brokerium

- Order: cordon, drain, verify, uncordon, stop, unregister.
- Check min ISR and free disk on targets before draining; refuse when RF cannot be preserved.
