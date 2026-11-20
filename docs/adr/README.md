# Architecture decision records

| ADR | Decision |
| --- | --- |
| [0001](0001-product-name.md) | Product name "Brokerium" and Apache Kafka trademark rules |
| [0002](0002-license.md) | PolyForm Shield 1.0.0 with a CLA; source-available |
| [0003](0003-tech-stack.md) | Go, React + TypeScript, PostgreSQL, REST, gRPC |
| [0004](0004-architecture.md) | Control plane, optional agent, providers and capabilities |
| [0005](0005-kafka-client.md) | franz-go with raw KRaft requests |
| [0006](0006-balancer.md) | Own deterministic rebalancing planner |
| [0007](0007-dynamic-quorum.md) | Dynamic KRaft quorum by default |
| [0008](0008-operations-engine.md) | Durable operations engine with safety gates |
| [0009](0009-agent-protocol.md) | Agent protocol over gRPC with mTLS |
| [0010](0010-metrics.md) | Agentless metrics first |
| [0012](0012-strimzi-provider.md) | Strimzi provider through the Kubernetes REST API |
| [0013](0013-distribution.md) | Releases, images, Helm chart and the agent installer |
| [0014](0014-agent-certificate-renewal.md) | Agent certificate renewal over the mutual TLS connection |
| [0015](0015-chart-networking-and-repository.md) | Helm chart networking with Gateway API and the chart repository |
| [0016](0016-editions-and-license-keys.md) | Community and Enterprise editions, `ee/` and license keys |
| [0017](0017-agent-transport-behind-proxies.md) | Insecure agent transport with signed messages behind TLS-terminating proxies |
| [0018](0018-explicit-node-assignment.md) | Operators choose the agent hosts of cluster nodes; nodes advertise their advertised host |
| [0019](0019-docker-clusters-for-docker-installations.md) | Docker clusters only for the Docker installation, with nodes reached by container name |
| [0020](0020-ci-by-need.md) | CI runs the jobs a change needs; nightly `edge` images |
| [0021](0021-combined-nodes.md) | Combined broker and controller nodes: voters and brokers, restarted with the controllers, not replaced or drained |
| [0022](0022-kafka-versions-and-modes.md) | Mode and version of every broker; capabilities by Kafka mode and version; ZooKeeper-based clusters from 3.5, new clusters from 4.2 |
| [0023](0023-problems.md) | Problems: rules after every snapshot, findings with fixes, only their state stored |
| [0024](0024-monitoring-through-prometheus.md) | Monitoring through the team's Prometheus with fixed panels; a server `/metrics` endpoint |
| [0025](0025-kafka-configuration.md) | Kafka configuration of clusters: a catalog per release, typed settings, planned changes, Kafka defaults except reliability values |
| [0026](0026-listeners-and-security.md) | Listeners, ports, bind addresses, TLS and SASL of clusters Brokerium runs; `change-listeners` |
| [0027](0027-agent-commands-for-0-5.md) | Agent commands for sealed files, artifacts, host facts, bounded logs and autostart |
| [0028](0028-takeover-of-existing-nodes.md) | Takeover of existing nodes: import, diff, own installation, unit switch node by node |

New ADRs use the next free number and the same structure: status, date, context, decision,
consequences. Superseded ADRs stay in place with their status changed.
