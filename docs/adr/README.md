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

New ADRs use the next free number and the same structure: status, date, context, decision,
consequences. Superseded ADRs stay in place with their status changed.
