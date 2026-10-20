# Installation

Brokerium is one server (`brokerium-server`: API, dashboard, operations engine) with a
PostgreSQL database, plus an optional agent (`brokerium-agent`) on Kafka hosts you want
Brokerium to manage at the node level.

## Server

### Kubernetes with Helm

```bash
helm repo add brokerium https://charts.brokerium.kaufen
helm install brokerium brokerium/brokerium -n brokerium --create-namespace
kubectl -n brokerium get secret brokerium -o jsonpath='{.data.admin-password}' | base64 -d
kubectl -n brokerium port-forward svc/brokerium 8080:8080
```

The chart is also published as `oci://ghcr.io/brokerium/charts/brokerium`. Upgrade with
`helm repo update` and `helm upgrade brokerium brokerium/brokerium -n brokerium`.

Values worth setting (see the [chart README](../../deploy/helm/brokerium/README.md) and
[values.yaml](../../deploy/helm/brokerium/values.yaml)):

| Value | Purpose |
| --- | --- |
| `postgresql.enabled` | Bundled single PostgreSQL for evaluations (default). Set `false` and `database.url` for a managed database. |
| `secretKey`, `existingSecret` | The key that seals stored cluster credentials. Generated on install and kept on upgrades; back up the Secret. |
| `gateway.*` | Gateway API routes: an `HTTPRoute` for the dashboard with HTTPS at the gateway and a `TLSRoute` in passthrough mode for agents. Needs Gateway API 1.5 and Kubernetes 1.31 or later. |
| `loadBalancer.*` | A `LoadBalancer` service with the dashboard port, the agent port or both, and a host name for external-dns. |
| `publicURL`, `agents.endpoint` | The dashboard URL and the address agents dial. Derived from `gateway.*` or `loadBalancer.hostname` when empty. |
| `rbac.strimzi` | ClusterRole for managing Strimzi clusters in the same Kubernetes cluster with the pod's service account. |

The agent port (8443) uses mutual TLS with the server's own CA, so a proxy in front of it must
pass TLS through instead of terminating it. A dashboard with an HTTPS Gateway and agents on
virtual machines:

```yaml
gateway:
  create: true
  className: eg
  annotations:
    cert-manager.io/cluster-issuer: letsencrypt
  http:
    enabled: true
    hostname: brokerium.example.com
  agents:
    enabled: true
    hostname: agents.brokerium.example.com
```

Chart 0.2 removed the Ingress: use `gateway.http`, or an Ingress of your own in front of the
`brokerium` service. `service.type` and `service.grpcType` became `loadBalancer.*`.

The chart runs one replica with the `Recreate` strategy: the operations engine must not run
twice.

### Docker

```bash
docker run -d --name brokerium -p 8080:8080 -p 8443:8443 \
  -v brokerium-data:/var/lib/brokerium \
  -e BROKERIUM_ADMIN_PASSWORD=change-me \
  -e BROKERIUM_DATABASE_URL='postgres://brokerium:secret@db.example.com:5432/brokerium?sslmode=require' \
  bvaliev/brokerium-server:0.2.1
```

Keep `/var/lib/brokerium` on a volume or set `BROKERIUM_SECRET_KEY`: the server refuses to
start when the database was sealed with another key.

Images are published to Docker Hub as `bvaliev/brokerium-server` and `bvaliev/brokerium-agent`
for `linux/amd64` and `linux/arm64`. To build them from source, run
`make images VERSION=0.2.1`.

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
| `BROKERIUM_AGENT_PROXY`, `BROKERIUM_AGENT_NO_PROXY` | none | `HTTPS_PROXY` and `NO_PROXY` in the agent install command. |
| `BROKERIUM_AGENT_CERT_LIFETIME` | `8760h` (one year) | Lifetime of agent certificates and the gateway certificate; they are renewed after two thirds of it. At least `1m`. |
| `BROKERIUM_SECURE_COOKIES` | `false` | Set `true` behind HTTPS. |
| `BROKERIUM_SNAPSHOT_INTERVAL` | `15s` | How often clusters are polled. |
| `BROKERIUM_DOCKER_HOST`, `BROKERIUM_LAB_NETWORK`, `BROKERIUM_AGENT_IMAGE` | lab values | Docker provider (labs only, see [Docker lab clusters](clusters.md#docker-lab-clusters)). |
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

### Agents behind an HTTP proxy

When hosts reach the server only through an HTTP proxy, pass `HTTPS_PROXY` (and `NO_PROXY`
for names that go direct) after `sudo`, which drops the variables of the caller:

```bash
HTTPS_PROXY=http://proxy.example.com:3128 curl -fsSL https://brokerium.example.com/api/v1/agent/install.sh | \
  sudo HTTPS_PROXY=http://proxy.example.com:3128 NO_PROXY=localhost,.internal \
  BROKERIUM_AGENT_SERVER=brokerium.example.com:8443 BROKERIUM_AGENT_TOKEN=brk1.… sh
```

The script downloads through the proxy and writes both variables to `/etc/brokerium-agent.env`,
so the agent and its Kafka downloads use it too. With `BROKERIUM_AGENT_PROXY` and
`BROKERIUM_AGENT_NO_PROXY` on the server (Helm: `agents.proxy.https` and
`agents.proxy.noProxy`) the command on the **Nodes** page contains them already.

The agent opens an HTTP CONNECT tunnel, so mutual TLS stays end to end. The proxy has to:

- allow CONNECT to the port of the agent gateway (Squid allows only 443 by default: add the port
  to `SSL_ports`, or publish the gateway on 443 with `loadBalancer` or `gateway.agents.port`);
- leave that connection uninspected (for Squid with SSL bump, `splice` it);
- resolve the name of the server; the agent does not resolve it itself.

Proxy URLs use `http://`, optionally with Basic credentials (`http://user:password@proxy:3128`).
A proxy that terminates TLS for the agent gateway needs the insecure agent transport instead.

### Adopting running nodes

Set `BROKERIUM_AGENT_KAFKA_CONFIG` to the node's `server.properties` and
`BROKERIUM_AGENT_PREINSTALLED` to its Kafka directory. The agent then never rewrites that
configuration and never formats storage; the server binds the node to the cluster from its
`meta.properties` (node id, role). Restarts and upgrades work; reconfiguration stays with
you. Combined broker+controller nodes are not adopted.
