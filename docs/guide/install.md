# Installation

Brokerium is one server (`brokerium-server`: API, dashboard, operations engine) with a
PostgreSQL database, plus an optional agent (`brokerium-agent`) on Kafka hosts you want
Brokerium to manage at the node level.

## Server

### Kubernetes with Helm

```bash
helm install brokerium deploy/helm/brokerium -n brokerium --create-namespace \
  --set publicURL=https://brokerium.example.com
kubectl -n brokerium get secret brokerium -o jsonpath='{.data.admin-password}' | base64 -d
```

Values worth setting (see [values.yaml](../../deploy/helm/brokerium/values.yaml)):

| Value | Purpose |
| --- | --- |
| `postgresql.enabled` | Bundled single PostgreSQL for evaluations (default). Set `false` and `database.url` for a managed database. |
| `secretKey`, `existingSecret` | The key that seals stored cluster credentials. Generated on install and kept on upgrades; back up the Secret. |
| `agents.endpoint`, `service.grpcType` | Address agents on VMs dial, for example a LoadBalancer for the gRPC port 8443. |
| `rbac.strimzi` | ClusterRole for managing Strimzi clusters in the same Kubernetes cluster with the pod's service account. |
| `ingress.*` | Ingress for the dashboard. |

The chart runs one replica with the `Recreate` strategy: the operations engine must not run
twice.

### Docker

```bash
docker run -d --name brokerium -p 8080:8080 -p 8443:8443 \
  -v brokerium-data:/var/lib/brokerium \
  -e BROKERIUM_ADMIN_PASSWORD=change-me \
  -e BROKERIUM_DATABASE_URL='postgres://brokerium:secret@db.example.com:5432/brokerium?sslmode=require' \
  bvaliev/brokerium-server:0.1.0
```

Keep `/var/lib/brokerium` on a volume or set `BROKERIUM_SECRET_KEY`: the server refuses to
start when the database was sealed with another key.

Images are published to Docker Hub as `bvaliev/brokerium-server` and `bvaliev/brokerium-agent`
for `linux/amd64` and `linux/arm64`. To build them from source, run
`make images VERSION=0.1.0`.

### Binary

Release assets contain `brokerium-server-<os>-<arch>` with the dashboard built in.

### Configuration

| Variable | Default | Meaning |
| --- | --- | --- |
| `BROKERIUM_DATABASE_URL` | lab database | PostgreSQL connection string. |
| `BROKERIUM_SECRET_KEY` | generated into the data dir | 32 bytes, base64; seals credentials. |
| `BROKERIUM_DATA_DIR` | `.data/server` (`/var/lib/brokerium` in the image) | Holds the generated secret key. |
| `BROKERIUM_ADMIN_USERNAME`, `BROKERIUM_ADMIN_PASSWORD` | `admin`, none | First administrator, created when there are no users. |
| `BROKERIUM_HTTP_ADDR`, `BROKERIUM_GRPC_ADDR` | `:8080`, `:8443` | Dashboard and API; agent gateway. |
| `BROKERIUM_PUBLIC_URL` | `http://localhost:8080` | Used in links and agent install commands. |
| `BROKERIUM_AGENT_ENDPOINT` | `host.docker.internal:8443` | Address agents dial. |
| `BROKERIUM_AGENT_SERVER_NAMES` | `localhost,host.docker.internal,brokerium,brokerium-server` | Names in the gateway certificate. |
| `BROKERIUM_SECURE_COOKIES` | `false` | Set `true` behind HTTPS. |
| `BROKERIUM_SNAPSHOT_INTERVAL` | `15s` | How often clusters are polled. |
| `BROKERIUM_DOCKER_HOST`, `BROKERIUM_LAB_NETWORK`, `BROKERIUM_AGENT_IMAGE` | lab values | Docker provider (labs only). |
| `BROKERIUM_DEBUG` | `0` | `1` logs every API request. |

## Agents on virtual machines and bare metal

Agents let Brokerium create clusters, add, replace and remove brokers and controllers,
restart and upgrade nodes. They connect out to the server (no inbound port on the host),
authenticate with a one-time join token and then with a client certificate from the server's
own CA, and execute only typed commands (install a Kafka release with its SHA-512 checked,
write the configuration, format storage, start, stop, report status).

1. In the dashboard open **Nodes**, create a join token for the host and copy the command:

   ```bash
   curl -fsSL https://brokerium.example.com/api/v1/agent/install.sh | \
     sudo BROKERIUM_AGENT_SERVER=brokerium.example.com:8443 BROKERIUM_AGENT_TOKEN=brk1.… sh
   ```

2. The script installs `/usr/local/bin/brokerium-agent`, writes `/etc/brokerium-agent.env`
   (mode 0600) and starts `brokerium-agent.service`. Kafka itself runs as
   `brokerium-kafka.service` under the `kafka` account, so restarting or upgrading the agent
   never stops Kafka. Hosts need Java 17 or later and `curl`.
3. Name hosts the way Brokerium names nodes of a cluster: `<cluster>-c<id>` for controllers
   (ids 1-99) and `<cluster>-b<id>` for brokers (ids from 101). Brokerium advertises these names
   as listener addresses, so they must resolve in DNS for clients and other nodes.

Installer variables: `BROKERIUM_AGENT_VERSION` (release tag), `BROKERIUM_AGENT_URL` (download
from your own mirror) with `BROKERIUM_AGENT_SHA256` (its expected checksum),
`BROKERIUM_AGENT_KAFKA_DATA_DIR` (log.dirs, default `/var/lib/kafka/data`). Binaries from
GitHub releases are always checked against the `SHA256SUMS` of the release.

### Adopting running nodes

Set `BROKERIUM_AGENT_KAFKA_CONFIG` to the node's `server.properties` and
`BROKERIUM_AGENT_PREINSTALLED` to its Kafka directory. The agent then never rewrites that
configuration and never formats storage; the server binds the node to the cluster from its
`meta.properties` (node id, role). Restarts and upgrades work; reconfiguration stays with
you. Combined broker+controller nodes are not adopted.
