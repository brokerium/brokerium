# Brokerium for Apache Kafka

Brokerium is a control plane and web dashboard for Apache Kafka® clusters, in the
spirit of Rancher for Kubernetes. One place to see every cluster, scale brokers and
KRaft controllers on the fly, drain and replace nodes, rebalance data across brokers,
upgrade Kafka versions, and manage topics, users, ACLs and messages.

> **Status: alpha (v0.1.0).** Every lab scenario below runs as an automated end-to-end test,
> but interfaces and storage formats may still change between releases. Try it on test
> clusters before production ones.

![Partition distribution and one-click balancing](docs/images/distribution.png)

## Why Brokerium

Existing tools cover only part of the job: data-plane UIs browse topics and messages,
Strimzi automates Kafka only on Kubernetes, and Cruise Control rebalances partitions
without a UI and without knowing about controllers. Brokerium covers the whole
lifecycle of KRaft clusters on virtual machines, bare metal, Kubernetes and managed
services:

- **Multi-cluster dashboard:** health, brokers (including fenced ones), the KRaft
  controller quorum with voters, observers and lag, topics, partition distribution,
  consumer groups and configuration.
- **Controllers without downtime:** add and remove KRaft controllers in dynamic
  quorums, and migrate static quorums to dynamic ones.
- **Broker lifecycle:** add brokers, cordon and drain a broker, replace a failed
  broker, decommission and unregister it.
- **Rebalancing you control:** move replicas and leaders by hand with a preview, or
  balance the whole cluster evenly in one click, with throttling and health checks
  between batches.
- **Rolling upgrades:** one node at a time with health gates, then explicit
  finalization of the new metadata version.
- **Data plane:** topics, ACLs, SCRAM users, quotas, consumer and share groups with
  offset resets, message browsing and producing.
- **Everywhere Kafka runs:** virtual machines and bare metal through a small agent,
  Kubernetes through Strimzi node pools, Amazon MSK (IAM), Confluent Cloud and Aiven
  through the Kafka protocol, and a Docker provider for labs.
- **Safe by construction:** every change is a dry-run plan you confirm, executed step by
  step behind health gates, throttled, pausable and audited; irreversible steps ask for
  the cluster name.

## How it works

```mermaid
flowchart LR
  ui["Dashboard"] --> server["brokerium-server"]
  server -->|"Kafka protocol"| kafka["Kafka brokers and controllers"]
  agent["brokerium-agent on each VM"] -->|"outbound gRPC, mTLS"| server
  agent --> kafka
```

- `brokerium-server` is a single Go binary with the API, the dashboard, the
  operations engine and the rebalancing planner. It stores its state in PostgreSQL.
- Data-plane features and rebalancing need nothing on the brokers: Brokerium talks
  the Kafka protocol with a service account.
- Node lifecycle on virtual machines (install, format, start, stop, upgrade) uses a
  small `brokerium-agent` on each host. The agent connects out to the server and
  executes only a fixed set of typed commands.
- On Kubernetes Brokerium works through Strimzi; on managed services (Amazon MSK,
  Confluent Cloud, Aiven) it offers what the provider allows.

## Quick start

**Kubernetes (Helm):**

```bash
helm install brokerium deploy/helm/brokerium -n brokerium --create-namespace
kubectl -n brokerium port-forward svc/brokerium 8080:8080
```

**Docker:**

```bash
docker network create brokerium
docker run -d --name brokerium-db --network brokerium -e POSTGRES_USER=brokerium \
  -e POSTGRES_PASSWORD=brokerium -e POSTGRES_DB=brokerium postgres:18-alpine
docker run -d --name brokerium --network brokerium -p 8080:8080 -p 8443:8443 \
  -v brokerium-data:/var/lib/brokerium -e BROKERIUM_ADMIN_PASSWORD=change-me \
  -e BROKERIUM_DATABASE_URL='postgres://brokerium:brokerium@brokerium-db:5432/brokerium?sslmode=disable' \
  bvaliev/brokerium-server:0.1.0
```

Open http://localhost:8080, sign in as `admin`, then **Add cluster** to connect an existing
cluster by its bootstrap servers, or create one in the Docker lab.

**Local lab from source** (Docker Desktop with 6 CPUs and 16 GB,
[mise](https://mise.jdx.dev) for the pinned toolchain):

```bash
mise install
make lab-up      # Kafka 4.3.1 cluster "alpha": 3 controllers + 3 brokers
make dev         # Brokerium server on http://localhost:8080
```

## Documentation

- [Installation](docs/guide/install.md): Helm, Docker, binaries, agents on VMs
- [Clusters and providers](docs/guide/clusters.md): existing clusters, agents, Strimzi,
  Amazon MSK, Confluent Cloud, Aiven
- [Operations](docs/guide/operations.md): what each operation does and how it stays safe
- [Security](docs/guide/security.md): users, roles, sessions, credentials, the agent channel
- [REST API](api/openapi.yaml) (OpenAPI 3.1)
- [Runbooks](docs/runbooks/) and the lab scenarios they automate
- [Architecture decisions](docs/adr/), [roadmap](docs/ROADMAP.md),
  [research notes on Kafka 4.x operations](docs/research/)

![Rebalance plan with a before and after preview](docs/images/rebalance-plan.png)

## License

Brokerium is **source-available** under the
[PolyForm Shield License 1.0.0](LICENSE): free to use, including in production,
but you may not use it to build a competing product. Read the plain-language
summary in [LICENSING.md](LICENSING.md).

Contributions are welcome under the [CLA](CLA.md); see [CONTRIBUTING.md](CONTRIBUTING.md).

Apache®, Apache Kafka® and Kafka® are either registered trademarks or trademarks of
the Apache Software Foundation in the United States and/or other countries.
Brokerium is not affiliated with, endorsed by, or sponsored by the Apache Software
Foundation.
