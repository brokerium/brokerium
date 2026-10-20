# Clusters and providers

A cluster in Brokerium has a **provider** (who runs the nodes) and a **flavor** (which Kafka
distribution or service). Together they decide which actions the dashboard offers; actions a
cluster cannot support are not shown.

| Provider | Nodes are run by | Brokerium can |
| --- | --- | --- |
| `external` | you | everything over the Kafka protocol: health, topics, ACLs, quotas, groups, messages, rebalancing, leader elections, cordon and unregister brokers, remove controllers from dynamic quorums |
| `agent` | `brokerium-agent` on your hosts | all of the above plus creating clusters, adding, replacing and decommissioning brokers, adding controllers, quorum migration, rolling restarts and upgrades |
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

**Add cluster → Create a cluster in Docker** builds a cluster from the lab node image as
containers on the server's Docker host, for evaluations, labs and tests. **Settings → Delete
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
  `bvaliev/brokerium-agent:lab`, published from `main`. The provider pulls the image when it is
  missing; `make images` builds it locally after agent changes;
- the address of the agent gateway for the agents in the nodes, `BROKERIUM_AGENT_ENDPOINT`:
  `brokerium:8443` in the Docker installation, by default `host.docker.internal:8443` for a
  server on the Docker host.
