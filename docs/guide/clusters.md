# Clusters and providers

A cluster in Brokerium has a **provider** (who runs the nodes) and a **flavor** (which Kafka
distribution or service). Together they decide which actions the dashboard offers; actions a
cluster cannot support are not shown.

| Provider | Nodes are run by | Brokerium can |
| --- | --- | --- |
| `external` | you | everything over the Kafka protocol: health, topics, ACLs, quotas, groups, messages, rebalancing, leader elections, cordon and unregister brokers, remove controllers from dynamic quorums |
| `agent` | `brokerium-agent` on your hosts | all of the above plus creating clusters, adding, replacing and decommissioning brokers, adding controllers, quorum migration, rolling restarts and upgrades |
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
