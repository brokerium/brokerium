# Clusters and providers

A cluster in Brokerium has a **provider** (who runs the nodes) and a **flavor** (which Kafka
distribution or service). Together they decide which actions the dashboard offers; actions a
cluster cannot support are not shown.

| Provider | Nodes are run by | Brokerium can |
| --- | --- | --- |
| `external` | you | everything over the Kafka protocol: health, topics, ACLs, quotas, groups, messages, rebalancing, leader elections, cordon and unregister brokers, remove controllers from dynamic quorums |
| `agent` | `brokerium-agent` on your hosts | all of the above plus creating clusters on hosts you choose, adding, replacing and decommissioning brokers, adding controllers, quorum migration, rolling restarts and upgrades |
| `strimzi` | the Strimzi operator on Kubernetes | Kafka-protocol features plus node pool scaling with rebalancing, rolling restarts and upgrades through the operator |
| `docker` | the Docker provider (Docker installation, labs) | everything, with nodes as containers on the server's Docker host |

## Connection profiles

`PLAINTEXT`, `SSL` (with optional client certificates for mTLS), `SASL_PLAINTEXT` and
`SASL_SSL` with `PLAIN`, `SCRAM-SHA-256`, `SCRAM-SHA-512`, `AWS_MSK_IAM` or `OAUTHBEARER`.
Passwords, keys, tokens and kubeconfigs are write-only: they are sealed with the server's
secret key and never returned by the API. The principal needs admin rights for the features
you use (Describe/Alter on the cluster, topics and groups; `ClusterAction` for
reassignments and elections).

When no broker answers but the cluster has controller addresses
(`settings.controllerBootstrapServers`, filled in automatically for agent and Docker
clusters), Brokerium still reads the controller quorum directly from the controllers.

## Agent clusters

Clusters of the provider `agent` run on hosts with `brokerium-agent`
([installation](install.md#agents-on-virtual-machines-and-bare-metal)). You choose the host of
every Kafka node; Brokerium installs the Kafka release, writes the configuration with the
advertised host of the host, formats the storage and starts the node. A host can be chosen when
its agent is connected, it runs no Kafka node, it belongs to no other cluster (a join token for
a cluster reserves its host for that cluster) and its Kafka data directory is empty or holds
the data of the same node. The dashboard offers these hosts; through the API, take their ids
from `GET /api/v1/nodes`.

Create the cluster record, then plan the creation of its nodes:

```bash
api() { curl -fsS -H "Authorization: Bearer $TOKEN" -H 'X-Brokerium-Request: 1' -H 'Content-Type: application/json' "$@"; }
api -d '{"name": "prod", "provider": "agent", "skipTest": true, "bootstrapServers": []}' \
  https://brokerium.example.com/api/v1/clusters
api -d '{"type": "bootstrap-cluster", "params": {"version": "4.3.1", "nodes": [
      {"node": "3f9c0d2a6b1e4c7d", "role": "controller"},
      {"node": "8a41e6b0c2d94f13", "role": "controller"},
      {"node": "c07d5e9b1a2f3468", "role": "controller"},
      {"node": "1b2c3d4e5f607182", "role": "broker", "rack": "rack-a"},
      {"node": "92a8b7c6d5e4f301", "role": "broker", "rack": "rack-b"},
      {"node": "e5f4d3c2b1a09887", "role": "broker", "nodeId": 110}]}}' \
  https://brokerium.example.com/api/v1/clusters/<cluster id>/operations
```

- Choose one to five controllers and at least one broker. Combined broker and controller nodes
  are not supported yet.
- Node ids default to 1, 2, ... for controllers and 101, 102, ... for brokers in the order of
  the list; `nodeId` sets one explicitly (unique, at least 0).
- `rack` defaults to the rack of the host; `quorum` is `dynamic` (default) or `static`.
- The plan lists the hosts with their node ids, racks and advertised hosts. Confirm it with
  `POST /api/v1/operations/<operation id>/confirm`. Afterwards the cluster connects to the
  advertised hosts of its brokers.

Later operations name their hosts the same way:

| Operation | Parameters |
| --- | --- |
| `add-broker` | `{"nodes": [{"node": "<node id>", "rack": "rack-c"}], "rebalance": true}`; `nodeId` per host is optional, by default ids continue after the highest broker. |
| `add-controller` | `{"node": "<node id>"}`, optionally `"nodeId": 4`; a quorum has at most five voters. |
| `replace-broker` | `{"brokerId": 103, "mode": "same-id"}` starts broker 103 again on its host, for example after its disk was replaced, and followers re-replicate what the disk lacks; add `"node"` to move the id to another free host. `{"brokerId": 103, "mode": "new-id", "node": "<node id>"}` starts a broker with a new id on that host and moves the replicas over. |

Decommissioning a broker, removing a controller and replacing a broker stop Kafka on the old host
and release it: the host has no role and no cluster afterwards, but keeps its Kafka data. A host
that is down at that moment is released at once and stops Kafka as soon as its agent connects
again, so it does not run the old node next to its replacement. Clear the data directory of a
released host before it runs a node of another cluster or with another node id. Deleting the
registration of an agent cluster releases its hosts too and leaves Kafka running on them.

An operation that fails halfway leaves the hosts it already reached with their role; a failed
operation cannot be resumed. After a failed `add-broker` or `add-controller`, fix the cause and
plan it again with the same hosts: a host whose broker is not registered, or whose controller is
not a voter, continues that node. After a failed creation, delete the cluster registration, stop
Kafka on its hosts (`sudo systemctl stop brokerium-kafka.service`) and clear their data
directories before you choose them again.

## Amazon MSK

- Bootstrap servers: the IAM endpoint (port 9098), protocol `SASL_SSL`, mechanism `AWS_MSK_IAM`,
  the cluster region.
- Credentials: an access key, or nothing to use the AWS default chain (environment, EKS web
  identity, ECS task role, instance profile) of the Brokerium server.
- IAM policy: `kafka-cluster:Connect`, `DescribeCluster`, `AlterCluster`, `DescribeTopic`,
  `CreateTopic`, `AlterTopic`, `DeleteTopic`, `DescribeGroup`, `AlterGroup`, `DeleteGroup`,
  `ReadData`, `WriteData` on the cluster's resources.
- Available: data plane and rebalancing. MSK runs brokers, controllers and versions, so the
  quorum, SCRAM and finalization are not offered.

## Confluent Cloud

Protocol `SASL_SSL`, mechanism `PLAIN`, a Kafka API key as username and its secret as
password. Placement, brokers and versions belong to Confluent: Brokerium offers the data
plane only.

## Aiven

Protocol `SSL` with the service CA and the access certificate and key, or `SASL_SSL` with
SCRAM. Aiven manages nodes, users and placement: Brokerium offers the data plane only.

## Strimzi

Settings: the namespace and name of the `Kafka` resource, and either a kubeconfig (embedded
token or client certificate; exec plugins are not supported) or in-cluster access when
Brokerium runs in the same Kubernetes cluster (Helm value `rbac.strimzi`). Strimzi 1.x
(`kafka.strimzi.io/v1`) and 0.4x (`v1beta2`) are detected automatically. Validated with Strimzi
1.3.0 in the lab ([deploy/lab/strimzi](../../deploy/lab/strimzi)).

- **Scale node pool:** growing a broker pool waits for the new brokers and rebalances onto
  them; shrinking first moves every replica off the brokers Strimzi will remove (the highest
  ids, pinned with `strimzi.io/remove-node-ids`), then lowers `spec.replicas`.
- **Rolling restart:** annotates each pool's `StrimziPodSet` with
  `strimzi.io/manual-rolling-update`, controllers first, health gate after each pool.
- **Upgrade:** sets `spec.kafka.version` and pins `metadataVersion` to the current level, so
  the operator does not finalize on its own; **Finalize** sets `metadataVersion`.
- Controller pools are not scaled: Strimzi does not support dynamic controller quorums yet.

## Docker lab clusters

**Add cluster → Create in Docker** builds a cluster from the lab node image as containers on the
server's Docker host, for evaluations, labs and tests. Give it the numbers of controllers, brokers
and combined nodes (broker and controller in one process). **Settings → Delete
cluster nodes** removes the containers and volumes again.

The dashboard offers it only when the server offers Docker clusters, which
`BROKERIUM_DOCKER_PROVIDER` decides
([ADR 0019](../adr/0019-docker-clusters-for-docker-installations.md)):

| Value | Docker clusters |
| --- | --- |
| `auto` (default) | Only in the [Docker installation](install.md#docker-clusters): the server runs in a Docker container (`/.dockerenv` exists), not in Kubernetes (`KUBERNETES_SERVICE_HOST` is not set), and the Docker API at `BROKERIUM_DOCKER_HOST` answers. The server asks the Docker API again at most every 30 seconds, so a Docker daemon that starts later is found. |
| `on` | Always, for development with the server on the Docker host (`make dev`, the end-to-end tests). |
| `off` | Never. The Helm chart sets it. |

`GET /api/v1/server` reports the result in `providers.docker`, with the reason when Docker
clusters are unavailable. Creating a Docker cluster then fails with 409 `docker_unavailable`, and
so do operations that change the nodes of an existing Docker cluster. Such clusters stay
registered and monitored over the Kafka protocol, and Kafka-level operations such as rebalancing
keep working.

How the server reaches the nodes depends on where it runs:

- **In a container** (the Docker installation): the server is attached to the network
  `BROKERIUM_LAB_NETWORK` and reaches brokers at `<container>:9092` and controllers at
  `<container>:9093`, the names Brokerium records as the bootstrap and controller servers of the
  cluster.
- **On the Docker host** (development): through the ports the nodes publish on `localhost`.

In both cases every broker also publishes a listener on `localhost:<port base + node id>` for
clients on the Docker host. Clusters keep the addresses recorded when they were created: after
moving the server between the Docker host and a container, change the bootstrap servers of
existing Docker clusters.

The Docker provider also needs:

- the Docker API at `BROKERIUM_DOCKER_HOST` (by default `unix:///var/run/docker.sock`);
- the Docker network `BROKERIUM_LAB_NETWORK` (by default `brokerium-lab`). The lab creates it;
  elsewhere run `docker network create brokerium-lab`;
- the lab node image `BROKERIUM_AGENT_IMAGE`: Apache Kafka with the agent as the main process. A
  release of the server defaults to `bvaliev/brokerium-agent:<version>-lab`, other builds to
  `bvaliev/brokerium-agent:lab`, published nightly from `main`. The provider pulls the image when it is
  missing; `make images` builds it locally after agent changes;
- the address of the agent gateway for the agents in the nodes, `BROKERIUM_DOCKER_AGENT_ENDPOINT`:
  `brokerium:8443` in the Docker installation. It defaults to `BROKERIUM_AGENT_ENDPOINT`, whose
  default `host.docker.internal:8443` suits a server on the Docker host. `BROKERIUM_AGENT_ENDPOINT`
  itself is the address in the join commands for agents on other hosts.
