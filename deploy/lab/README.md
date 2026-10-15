# Brokerium lab

Local Kafka clusters in Docker for developing and testing Brokerium. Requires Docker Desktop with
6 CPUs and 16 GB of memory. All commands run from the repository root.

## Clusters

| Profile | Nodes | Quorum | Host listeners |
| --- | --- | --- | --- |
| `alpha` | controllers 1-3, brokers 101-103 (racks a, b, c) | dynamic, `--initial-controllers` | `localhost:19101-19103` |
| `alpha-scale` | brokers 104, 105 | joins alpha | `localhost:19104-19105` |
| `alpha-quorum` | controllers 4, 5 | join alpha as observers | none |
| `alpha-replace` | broker 106 | joins alpha | `localhost:19106` |
| `beta` | controller 1, broker 101 | dynamic, `--standalone` | `localhost:29101` |
| `gamma` | controllers 1-3, broker 101 | static, stock image entrypoint | `localhost:39101` |

PostgreSQL for Brokerium always starts: `postgres://brokerium:brokerium@localhost:55432/brokerium`.

Inside the Docker network brokers listen on `<service>:9092` and controllers on `<service>:9093`.
Every cluster has its own `CLUSTER_ID` (see `compose.yaml`).

## Commands

```bash
deploy/lab/lab.sh up alpha                 # or: make lab-up PROFILES="alpha beta gamma"
deploy/lab/lab.sh ps
deploy/lab/lab.sh quorum alpha             # voters, observers, lag
deploy/lab/lab.sh load alpha 100000        # topics orders, payments, clicks with 1 KB records
deploy/lab/lab.sh dist alpha               # replicas and leaders per broker
deploy/lab/lab.sh urp alpha                # under-replicated partitions
deploy/lab/lab.sh cli kafka-topics.sh --bootstrap-server alpha-b1:9092 --list
deploy/lab/lab.sh stop alpha-b3            # simulate a node failure
deploy/lab/lab.sh rm alpha-b3              # simulate losing the node and its disk
deploy/lab/lab.sh down                     # keep data
deploy/lab/lab.sh reset                    # delete all lab data
```

## Why a custom start script

The stock `apache/kafka` entrypoint formats storage only for static quorums. `kafka/start.sh`
writes `server.properties`, formats storage with `--initial-controllers`, `--standalone` or
`--no-initial-controllers`, and starts Kafka. Cluster defaults (`num.partitions`,
`default.replication.factor`, `min.insync.replicas`) are set on controllers too, because in KRaft
the controller applies them when topics are created.

Scenarios and their procedures are in [docs/runbooks/](../../docs/runbooks/).
