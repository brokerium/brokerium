# Clusters and providers

A cluster in Brokerium has a **provider** (who runs the nodes) and a **flavor** (which Kafka
distribution or service). Together they decide which actions the dashboard offers; actions a
cluster cannot support are not shown.

| Provider | Nodes are run by | Brokerium can |
| --- | --- | --- |
| `external` | you | everything over the Kafka protocol: health, topics, ACLs, quotas, groups, messages, rebalancing, leader elections, cordon and unregister brokers, remove controllers from dynamic quorums |
| `agent` | `brokerium-agent` on your hosts | all of the above plus creating clusters on hosts you choose, adding, replacing and decommissioning brokers, adding controllers, quorum migration, rolling restarts and upgrades |
| `strimzi` | the Strimzi operator on Kubernetes | Kafka-protocol features plus node pool scaling with rebalancing, rolling restarts and upgrades through the operator |
| `docker` | the Docker provider (labs) | everything, with nodes as containers on the server's Docker host |

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
| `replace-broker` | `{"brokerId": 103, "mode": "same-id"}` starts broker 103 again on its host with an empty disk; add `"node"` to move the id to another free host. `{"brokerId": 103, "mode": "new-id", "node": "<node id>"}` starts a broker with a new id on that host and moves the replicas over. |

Decommissioning a broker and removing a controller stop Kafka on the host and release it: the
host has no role and no cluster afterwards, but keeps its Kafka data. Clear its data directory
before it runs a node of another cluster or with another node id. Deleting the registration of an
agent cluster releases its hosts too and leaves Kafka running on them.

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

**Add cluster → Create a cluster in Docker** builds a cluster from the agent image on the
server's Docker host (labs and tests). **Settings → Delete cluster nodes** removes the
containers and volumes again.

The Docker provider has these requirements:

- The server runs directly on the Docker host. The provider publishes the client listener of
  every node on a port of the host and advertises it as `localhost:<port>`, so a server in a
  container (unless it uses the host network), on another machine or in Kubernetes cannot reach
  the nodes.
- The server reaches the Docker API at `BROKERIUM_DOCKER_HOST` (by default
  `unix:///var/run/docker.sock`).
- The Docker network `BROKERIUM_LAB_NETWORK` (by default `brokerium-lab`) exists. The lab
  creates it; elsewhere run `docker network create brokerium-lab`.
- Nodes run the lab node image `BROKERIUM_AGENT_IMAGE`: Apache Kafka with the agent as the main
  process. A release of the server defaults to `bvaliev/brokerium-agent:<version>-lab`, other
  builds to `bvaliev/brokerium-agent:lab`, published from `main`. `make images` builds the image
  locally after agent changes.
- Nodes connect back to the agent gateway at `BROKERIUM_AGENT_ENDPOINT`, by default
  `host.docker.internal:8443`.
