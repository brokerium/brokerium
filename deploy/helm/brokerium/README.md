# Brokerium Helm chart

[Brokerium](https://brokerium.kaufen) is a control plane and dashboard for Apache Kafka® KRaft
clusters: a multi-cluster view, broker and controller lifecycle, rebalancing, rolling upgrades
and data-plane management. This chart runs the Brokerium server (API, dashboard and operations
engine) with an optional bundled PostgreSQL.

## Install

```bash
helm repo add brokerium https://charts.brokerium.kaufen
helm install brokerium brokerium/brokerium -n brokerium --create-namespace
kubectl -n brokerium get secret brokerium -o jsonpath='{.data.admin-password}' | base64 -d
kubectl -n brokerium port-forward svc/brokerium 8080:8080
```

The chart is also published as an OCI artifact:

```bash
helm install brokerium oci://ghcr.io/brokerium/charts/brokerium -n brokerium --create-namespace
```

Upgrade with `helm repo update` and `helm upgrade brokerium brokerium/brokerium -n brokerium`.

## Database and keys

- `postgresql.enabled` (default `true`) runs a single PostgreSQL for evaluations. For production
  set it to `false` and pass `database.url` or an `existingSecret`.
- `secretKey` seals stored cluster credentials. It is generated on install and kept on upgrades.
  Back up the Secret: without the key, stored credentials cannot be read.
- The chart runs one replica with the `Recreate` strategy, because the operations engine must not
  run twice.

## Networking

The server listens on two ports, and on a third with `agents.insecure.enabled`:

- `http` (8080): dashboard and API, plain HTTP;
- `grpc` (8443): the agent gateway, which authenticates agents on Kafka hosts with mutual TLS
  against its own CA. Proxies must pass this port through as TCP or TLS, never terminate TLS;
- `grpc-insecure` (8444): plaintext gRPC for agents behind a proxy that terminates TLS (see
  [below](#agents-behind-a-tls-terminating-proxy)).

A ClusterIP service named after the release (`brokerium` in these examples) always carries both
ports. Agents inside the Kubernetes cluster and Strimzi clusters managed through in-cluster
access need nothing else. To reach the server from outside, use a Gateway, a LoadBalancer
service or both.

### Gateway API

Needs Gateway API 1.5 or later (`gateway.networking.k8s.io/v1`, including `TLSRoute`) and
Kubernetes 1.31 or later, with a Gateway implementation that supports TLS passthrough.

- `gateway.http` creates an `HTTPRoute` for the dashboard; TLS terminates at the gateway.
- `gateway.agents` creates a `TLSRoute` for agents; the gateway routes by SNI and passes TLS
  through, so mutual TLS stays end to end.

Create a Gateway with a certificate from cert-manager:

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

Or attach the routes to an existing Gateway with an HTTPS listener and a TLS listener in
passthrough mode:

```yaml
gateway:
  parentRefs:
    - name: shared
      namespace: gateways
  http:
    enabled: true
    hostname: brokerium.example.com
  agents:
    enabled: true
    hostname: agents.brokerium.example.com
```

### LoadBalancer service

`loadBalancer` adds a service of type `LoadBalancer`. One service carries both ports, which works
with the load balancers of AWS, Google Cloud, Azure and MetalLB, and one DNS record covers both.
`loadBalancer.hostname` becomes the `external-dns.alpha.kubernetes.io/hostname` annotation.

```yaml
loadBalancer:
  enabled: true
  ports: [grpc]
  hostname: agents.brokerium.example.com
```

The `http` port serves plain HTTP: expose it only on internal networks, with the TLS annotations
of your provider, or serve the dashboard through the Gateway instead.

### Agents behind a TLS-terminating proxy

When agents can reach the server only through a reverse proxy that terminates TLS, for example
OpenResty or nginx with `grpc_pass`, mutual TLS cannot work: the proxy shows its own certificate
and keeps the client certificate. `agents.insecure` adds a plaintext gRPC listener for the
insecure agent transport, where the server signs every command with its CA key and agents
authenticate every message with a secret from their enrollment:

```yaml
agents:
  insecure:
    enabled: true
    # The proxy agents dial.
    endpoint: agents.example.com:443
    # TLS between agents and the proxy: system, skip-verify or none.
    tls: system
```

The service port `grpc-insecure` (8444, `appProtocol: kubernetes.io/h2c`) speaks HTTP/2 without
TLS: point the proxy at `grpc://<service>.<namespace>.svc:8444`, or add `grpc-insecure` to
`loadBalancer.ports` for a proxy outside the Kubernetes cluster, and keep the path from the proxy
to the server on a network you trust. The install command on the **Nodes** page then uses the
insecure transport; agents installed for mutual TLS keep working. The installation guide shows a
[proxy configuration and the alternatives with mutual TLS](https://brokerium.kaufen/docs/guide/install/#agents-behind-a-tls-terminating-proxy).

### Derived addresses

When they are empty, the chart derives these settings:

| Value | Gateway route | LoadBalancer port with a host name | Otherwise |
| --- | --- | --- | --- |
| `publicURL` | `https://<gateway.http.hostname>` | `http://<loadBalancer.hostname>:8080` | `http://localhost:8080` |
| `agents.endpoint` | `<gateway.agents.hostname>:<gateway.agents.port>` | `<loadBalancer.hostname>:8443` | `<service>.<namespace>.svc:8443` |

The certificate of the agent gateway always includes the service names, the host of
`agents.endpoint`, `gateway.agents.hostname` and `loadBalancer.hostname`; add more with
`agents.serverNames`. Session cookies are marked secure when the public URL uses HTTPS.

## Upgrading from 0.1

Version 0.2 removes the Ingress and the service type switches. The chart rejects the old values:

| 0.1 | 0.2 |
| --- | --- |
| `ingress.*` | `gateway.http.*` with a Gateway, or your own Ingress in front of the service, port `http` |
| `service.type: LoadBalancer` | `loadBalancer.enabled: true` |
| `service.grpcType: LoadBalancer` | `loadBalancer.enabled: true` with `loadBalancer.ports: [grpc]` |

## Values

All values are described in [values.yaml](values.yaml) and validated by
[values.schema.json](values.schema.json).

## Links

- [Documentation](https://brokerium.kaufen/docs/)
- [Source code and issues](https://github.com/brokerium/brokerium)

Source-available under the PolyForm Shield License 1.0.0: free to use, including in production.
Apache®, Apache Kafka® and Kafka® are either registered trademarks or trademarks of the Apache
Software Foundation in the United States and/or other countries. Brokerium is not affiliated
with, endorsed by, or sponsored by the Apache Software Foundation.
