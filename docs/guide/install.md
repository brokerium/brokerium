# Installation

Brokerium is one server (`brokerium-server`: API, dashboard, operations engine) with a
PostgreSQL database, plus an optional agent (`brokerium-agent`) on Kafka hosts you want
Brokerium to manage at the node level.

This guide installs the Community edition. Brokerium Enterprise installs the same way from its own
image and binaries and needs a license key; see [Editions and license keys](enterprise.md).

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
| `agents.insecure.*` | Plaintext listener (port 8444) and install command for [agents behind a TLS-terminating proxy](#agents-behind-a-tls-terminating-proxy). |
| `rbac.strimzi` | ClusterRole for managing Strimzi clusters in the same Kubernetes cluster with the pod's service account. |
| `edition`, `license.*` | `enterprise` runs Brokerium Enterprise; `license.existingSecret` or `license.key` passes its license key. See [Editions and license keys](enterprise.md). |

The agent port (8443) uses mutual TLS with the server's own CA, so a proxy in front of it must
pass TLS through instead of terminating it; a proxy that terminates TLS needs the
[insecure agent transport](#agents-behind-a-tls-terminating-proxy). A dashboard with an HTTPS
Gateway and agents on virtual machines:

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
  bvaliev/brokerium-server:0.3.0
```

Keep `/var/lib/brokerium` on a volume or set `BROKERIUM_SECRET_KEY`: the server refuses to
start when the database was sealed with another key.

Images are published to Docker Hub as `bvaliev/brokerium-server` and `bvaliev/brokerium-agent`
for `linux/amd64` and `linux/arm64`, and Brokerium Enterprise as
`bvaliev/brokerium-server-enterprise` with the same tags. To build them from source, run
`make images VERSION=0.3.0` (add `EDITION=enterprise` for the Enterprise server image).

### Binary

Release assets contain `brokerium-server-<os>-<arch>` with the dashboard built in, and
`brokerium-server-enterprise-<os>-<arch>` for Brokerium Enterprise.

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
| `BROKERIUM_AGENT_INSECURE_ADDR` | none | Plaintext listener of the insecure agent transport for a proxy that terminates TLS, for example `:8444` ([details](#agents-behind-a-tls-terminating-proxy)). |
| `BROKERIUM_AGENT_TRANSPORT` | `mtls` | Transport of the agent install command: `mtls`, or `insecure` with the listener and the endpoint below. |
| `BROKERIUM_AGENT_INSECURE_ENDPOINT` | none | `host:port` agents of the insecure transport dial, usually the proxy. |
| `BROKERIUM_AGENT_INSECURE_TLS` | `system` | How those agents connect to the proxy: `system` (TLS verified against the system CAs), `skip-verify` or `none` (plaintext). |
| `BROKERIUM_AGENT_CERT_LIFETIME` | `8760h` (one year) | Lifetime of agent certificates and the gateway certificate; they are renewed after two thirds of it. At least `1m`. |
| `BROKERIUM_SECURE_COOKIES` | `false` | Set `true` behind HTTPS. |
| `BROKERIUM_SNAPSHOT_INTERVAL` | `15s` | How often clusters are polled. |
| `BROKERIUM_DOCKER_HOST`, `BROKERIUM_LAB_NETWORK`, `BROKERIUM_AGENT_IMAGE` | lab values | Docker provider (labs only, see [Docker lab clusters](clusters.md#docker-lab-clusters)). |
| `BROKERIUM_DEBUG` | `0` | `1` logs every API request. |
| `BROKERIUM_LICENSE` | none | License key of Brokerium Enterprise; takes precedence over a key added on the **License** page. The Community edition ignores it. |
| `BROKERIUM_LICENSE_FILE` | none | File that contains the license key, for example a mounted Secret; used when `BROKERIUM_LICENSE` is empty. |

## Agents on virtual machines and bare metal

Agents let Brokerium create clusters, add, replace and remove brokers and controllers,
restart and upgrade nodes. They connect out to the server (no inbound port on the host),
authenticate with a one-time join token and then with a client certificate from the server's
own CA (or, [behind a TLS-terminating proxy](#agents-behind-a-tls-terminating-proxy), with
signed messages), and execute only typed commands (install a Kafka release with its SHA-512
checked, write the configuration, format storage, start, stop, report status).

1. In the dashboard open **Nodes**, create a join token for the host and copy the command:

   ```bash
   curl -fsSL https://brokerium.example.com/api/v1/agent/install.sh | \
     sudo BROKERIUM_AGENT_SERVER=brokerium.example.com:8443 BROKERIUM_AGENT_TOKEN=brk1.… sh
   ```

2. The script installs `/usr/local/bin/brokerium-agent`, writes `/etc/brokerium-agent.env`
   (mode 0600) and starts `brokerium-agent.service`, which runs as the `brokerium-agent`
   account, not as root. Kafka itself runs as `brokerium-kafka.service` under the `kafka`
   account, so restarting or upgrading the agent never stops Kafka. Hosts need systemd, sudo,
   Java 17 or later and `curl`.
3. Name hosts the way Brokerium names nodes of a cluster: `<cluster>-c<id>` for controllers
   (ids 1-99) and `<cluster>-b<id>` for brokers (ids from 101). Brokerium advertises these names
   as listener addresses, so they must resolve in DNS for clients and other nodes.

Installer variables: `BROKERIUM_AGENT_VERSION` (release tag), `BROKERIUM_AGENT_URL` (download
from your own mirror) with `BROKERIUM_AGENT_SHA256` (its expected checksum),
`BROKERIUM_AGENT_KAFKA_DATA_DIR` (log.dirs, default `/var/lib/kafka/data`), and
`BROKERIUM_AGENT_TRANSPORT`, `BROKERIUM_AGENT_TLS` and `BROKERIUM_AGENT_CA_FILE` for
[agents behind a TLS-terminating proxy](#agents-behind-a-tls-terminating-proxy). Binaries from
GitHub releases are always checked against the `SHA256SUMS` of the release.

### What the installer sets up

| Account or path | Owner and mode | Purpose |
| --- | --- | --- |
| `kafka` | system account without login | Runs Kafka. |
| `brokerium-agent` | system account without login, member of `kafka` | Runs the agent. |
| `/var/lib/brokerium-agent` | `brokerium-agent`, 0711 | Agent state: identity, client certificate and key (0600), `server.properties`, `kafka.env`. Kafka writes its logs to `logs/`. |
| `/opt/brokerium/kafka` | `brokerium-agent:kafka`, 0755 | Kafka releases the agent installed; `current` points at the active one. |
| `/var/lib/kafka/data` | `kafka:kafka`, 2770 | Kafka data. New files belong to the `kafka` group, through which the agent formats and wipes storage. |
| `brokerium-kafka.service` | root | `User=kafka`, `UMask=0007`, heap options from `kafka.env`, starts the release behind `current`. |
| `brokerium-agent.service` | root | `User=brokerium-agent`, `SupplementaryGroups=kafka`, `ProtectSystem=full`, `ProtectHome=yes`, `PrivateTmp=yes`. |
| `/etc/sudoers.d/brokerium-agent` | root, 0440 | The sudo rule below. |

The agent controls Kafka through one sudo rule (the path of `systemctl` is the one on the
host):

```text
Defaults:brokerium-agent !requiretty
brokerium-agent ALL=(root) NOPASSWD: /usr/bin/systemctl start brokerium-kafka.service, /usr/bin/systemctl stop brokerium-kafka.service, /usr/bin/systemctl restart brokerium-kafka.service
```

The script checks the rule with `visudo` and `sudo -l` before it relies on it. It stops
without changing the agent when sudo is not installed, when `visudo` rejects the rule or when
`/etc/sudoers` does not include `/etc/sudoers.d`. sudo cannot gain root when the no new
privileges flag is set, so the agent unit keeps `NoNewPrivileges=no` and avoids options that
imply it, such as `SystemCallFilter=`, `RestrictNamespaces=` or `PrivateDevices=`.

### Hosts with an agent running as root

Agents installed by earlier versions of the script run as root and write the Kafka unit
themselves. Run the install command from the **Nodes** page on such a host again; the agent
keeps its identity, so the new join token stays unused. The script:

- stops the agent and gives its state and the Kafka releases to `brokerium-agent`;
- lets the `kafka` account open its data and log files to the `kafka` group;
- points `/opt/brokerium/kafka/current` at the release Kafka runs, writes the unit and the sudo
  rule above and starts the agent under its own account.

Kafka keeps running from the unit the root agent wrote until it restarts; should systemd
restart it, it starts the same release. Run a rolling restart afterwards to move Kafka to the
new unit. Directories Kafka creates before that restart stay closed to the group: run the
install command once more after the restart, so that the agent can wipe them.

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
A proxy that terminates TLS for the agent gateway needs the
[insecure agent transport](#agents-behind-a-tls-terminating-proxy) instead.

### Agents behind a TLS-terminating proxy

Some networks let agents reach the server only through a reverse proxy that terminates TLS, such
as OpenResty or nginx. The proxy shows its own certificate and keeps the client certificate of
the agent, so mutual TLS cannot work. The insecure agent transport
([ADR 0017](../adr/0017-agent-transport-behind-proxies.md)) authenticates every message instead
of the connection: the server signs its commands with the key of its CA, which the join token
pins, and the agent authenticates everything it sends with a secret that it received encrypted
at enrollment. The join token itself never crosses the proxy.

1. Start the server with the plaintext listener and let the install command offer the transport:

   ```bash
   BROKERIUM_AGENT_INSECURE_ADDR=:8444
   BROKERIUM_AGENT_TRANSPORT=insecure
   BROKERIUM_AGENT_INSECURE_ENDPOINT=agents.example.com:443
   ```

   With Helm, set `agents.insecure.enabled=true` and `agents.insecure.endpoint`; the chart adds
   the service port `grpc-insecure` (8444).

2. Point the proxy at port 8444 of the server, for example with OpenResty or nginx 1.25.1 or
   later:

   ```nginx
   server {
       listen 443 ssl;
       http2 on;
       server_name agents.example.com;
       ssl_certificate     /etc/nginx/tls/agents.example.com.crt;
       ssl_certificate_key /etc/nginx/tls/agents.example.com.key;

       location / {
           grpc_pass grpc://brokerium.internal:8444;
           grpc_read_timeout 1h;
           grpc_send_timeout 1h;
           client_body_timeout 1h;
       }
   }
   ```

   The command stream stays open as long as the agent runs; with these timeouts the proxy ends
   an idle stream after an hour at the earliest, and the agent reconnects at once.

3. Create a join token on the **Nodes** page and run its command on the host:

   ```bash
   curl -fsSL https://brokerium.example.com/api/v1/agent/install.sh | \
     sudo BROKERIUM_AGENT_TRANSPORT=insecure BROKERIUM_AGENT_TLS=system \
     BROKERIUM_AGENT_SERVER=agents.example.com:443 BROKERIUM_AGENT_TOKEN=brk1.… sh
   ```

`BROKERIUM_AGENT_TLS` (server: `BROKERIUM_AGENT_INSECURE_TLS`) sets how the agent connects to
the proxy: `system` verifies the certificate of the proxy against the system CAs and
`BROKERIUM_AGENT_CA_FILE` (a PEM file, for example with a corporate CA, outside `/home` and
readable by the `brokerium-agent` account, such as `/etc/brokerium-agent-ca.pem`), `skip-verify` accepts
any certificate, and `none` connects without TLS. None of them affects who can send commands,
which the signatures decide, only who can read them: the proxy and every segment without TLS see
commands, results and status in clear. Keep TLS up to the proxy and the plaintext port 8444 on a
network you trust.

Running the install command again with a new join token switches an agent between the transports
without stopping Kafka; the credentials of the old transport stop working. Agents of the insecure
transport have no certificate to renew, and their secret does not expire: to replace it, enroll
the node again with a new join token. `HTTPS_PROXY` works with this transport too.

Where the proxy can stay out of the TLS connection, keep mutual TLS end to end instead:

- publish the agent port 8443 with a `LoadBalancer` service (`loadBalancer.ports: [grpc]` in the
  chart), announced for example with Cilium BGP or MetalLB;
- pass the TCP connection through the proxy with an nginx `stream` block instead of terminating
  TLS:

  ```nginx
  stream {
      server {
          listen 8443;
          proxy_pass brokerium.internal:8443;
      }
  }
  ```

- or route by SNI with TLS passthrough through a Gateway (`gateway.agents` in the chart).

### Adopting running nodes

Set `BROKERIUM_AGENT_KAFKA_CONFIG` to the node's `server.properties` and
`BROKERIUM_AGENT_PREINSTALLED` to its Kafka directory. The agent then never rewrites that
configuration and never formats storage; the server binds the node to the cluster from its
`meta.properties` (node id, role). Restarts and upgrades work; reconfiguration stays with
you. Combined broker+controller nodes are not adopted. The agent reads that `server.properties`
and the `meta.properties` of the first log directory as `brokerium-agent`, so both must be
readable for the `kafka` group or for everyone.
