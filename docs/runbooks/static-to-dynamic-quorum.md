# Migrate a static quorum to a dynamic quorum (scenario 10)

Validated on Kafka 4.3.1, cluster gamma: controllers 1-3 and broker 101, started with the stock
`apache/kafka` entrypoint and `controller.quorum.voters` (`kraft.version` 0).

## 1. Upgrade `kraft.version` (irreversible)

```bash
./lab.sh cli kafka-features.sh --bootstrap-server gamma-b1:9092 upgrade --feature kraft.version=1
# kraft.version was upgraded to 1.
```

Afterwards `describe --status` lists the voters **with** directory ids, which were written to the
metadata log:

```text
CurrentVoters: [{"id": 1, "directoryId": "Br1Mlbn32KWfvP9t7TMdrw", ...}, {"id": 2, ...}, {"id": 3, ...}]
```

There is no downgrade from `kraft.version` 1 to 0.

## 2. Rolling restart onto `controller.quorum.bootstrap.servers`

Replace `controller.quorum.voters` with
`controller.quorum.bootstrap.servers=gamma-c1:9093,gamma-c2:9093,gamma-c3:9093` on every node, one
node at a time: followers first, the leader last, the broker at the end.

The stock image entrypoint cannot start a controller without `controller.quorum.voters` (the
storage tool demands a format flag before checking for existing data), so in the lab the nodes
switch to `kafka/start.sh` through `compose.gamma-dynamic.yaml`:

```bash
docker compose -f compose.yaml -f compose.gamma-dynamic.yaml --profile gamma \
  up -d --no-deps --wait gamma-c2
```

Observed after each restart (leader 1 at the start):

```text
gamma-c2: 1:Leader 2:Follower 3:Follower 101:Observer (lag 0)
gamma-c3: 1:Leader 2:Follower 3:Follower 101:Observer (lag 0)
gamma-c1: 2:Leader 1:Follower 3:Follower 101:Observer (lag 0)
gamma-b1: 2:Leader 1:Follower 3:Follower 101:Observer (lag 0)
```

## 3. Prove the quorum is dynamic

```bash
./lab.sh cli kafka-metadata-quorum.sh --bootstrap-controller gamma-c1:9093 remove-controller \
  --controller-id 3 --controller-directory-id k4QzOLq3mPMM36Og2NkR_A
# 3 becomes an observer
docker compose -f compose.yaml -f compose.gamma-dynamic.yaml --profile gamma exec -T gamma-c3 \
  /opt/kafka/bin/kafka-metadata-quorum.sh --bootstrap-controller gamma-c1:9093 \
  --command-config /tmp/server.properties add-controller
# 3 is a voter again
```

## Notes for Brokerium

- The migration is a separate operation with typed confirmation; it needs Kafka 4.1+ on all nodes.
- Finalizing `--release-version 4.1` or later on a static cluster also upgrades `kraft.version`;
  warn about it in the upgrade flow.
- Agent-managed nodes rewrite their configuration and restart one by one with health gates.
