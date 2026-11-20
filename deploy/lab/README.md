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
| `delta` | ZooKeeper `delta-zk`, brokers 101-103 (racks a, b, c) on Kafka 3.5.2 | ZooKeeper mode | `localhost:61101-61103` |
| `epsilon` | ZooKeeper `epsilon-zk`, brokers 101-103 (racks a, b, c) on Kafka 3.9.2 | ZooKeeper mode | `localhost:62101-62103` |

PostgreSQL for Brokerium always starts: `postgres://brokerium:brokerium@localhost:55432/brokerium`.

Inside the Docker network brokers listen on `<service>:9092` and controllers on `<service>:9093`.
Every KRaft cluster has its own `CLUSTER_ID` (see `compose.yaml`).

## ZooKeeper-based clusters

`delta` and `epsilon` run brokers in ZooKeeper mode with one ZooKeeper 3.8 each, the
`AclAuthorizer` with `allow.everyone.if.no.acl.found=true` and `super.users=User:ANONYMOUS`,
replication factor 3 and `min.insync.replicas` 2. Their brokers start through
`kafka/start-zk-broker.sh`. The `apache/kafka` images start at 3.7.0, so the brokers of `delta`
run `brokerium-lab/kafka:3.5.2`, which compose builds from the Apache release with
`kafka/Dockerfile` and checks against the SHA-512 in `compose.yaml`. The download from
archive.apache.org can be slow; a tarball placed in `kafka/dist/` is used instead. `epsilon`
runs `apache/kafka:3.9.2`. Their host listeners sit above the port bases of Docker clusters
(21000-59000).

```bash
deploy/lab/lab.sh up delta epsilon         # or: make lab-up PROFILES="delta epsilon"
```

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
