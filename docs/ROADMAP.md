# Roadmap

The live board is the GitHub project "Brokerium Roadmap". Lab scenarios referenced below are
listed at the end of this file.

## Phase 0: Foundation (done)

Repository, license and policies, GitHub project, Docker lab, manual validation of
the lab scenarios with the Kafka CLI, code skeleton, CI and the first container images.

## Phase 1: Multi-cluster read-only dashboard (done)

- Cluster registry with connection profiles: PLAINTEXT, SASL/PLAIN, SASL/SCRAM, TLS and mTLS.
- Overview of all clusters with health.
- Cluster page: brokers (including fenced), quorum (voters, observers, lag, `kraft.version`),
  topics and partitions, distribution of replicas, leaders and disk per broker, consumer groups,
  configuration, versions and features.
- Single administrator login.
- Spike: direct franz-go access to controllers.
- Acceptance: scenario 7 in read-only mode.

## Phase 2: Rebalancing (done)

- Operations engine: durable steps, per-cluster lock, pause, resume, cancel, audit.
- Manual replica and leader moves with preview.
- Planner with hard and soft goals and minimal data movement.
- Executor: batches, throttling, health gates between batches.
- "Balance evenly" in one click and preferred leader election.
- Agentless load estimation from partition sizes and offsets.
- Acceptance: scenarios 2 and 6.

## Phase 3: Agent and node lifecycle (done)

- Agent with enrollment, mTLS stream and typed commands; Docker provider for the lab.
- Operations: create cluster, add broker, drain and decommission broker, replace broker, add and
  remove controller, migrate a static quorum, rolling restart.
- Adoption of existing nodes with a standard layout.
- Acceptance: scenarios 1-5 and 10 entirely from the dashboard.

## Phase 4: Data plane and access (done)

- Topics, ACLs, SCRAM users, quotas, consumer groups and offset reset, message browsing and
  producing, share groups.
- Brokerium users, roles and audit log.
- Acceptance: scenario 9.

## Phase 5: Upgrades (done)

- Compatibility matrix and preflight checks (Java, known bugs such as KAFKA-20979).
- Rolling upgrade one node at a time with health gates; finalization through
  `--release-version` with explicit confirmation.
- Acceptance: scenario 8.

## Phase 6: Kubernetes and managed services (done)

- Strimzi provider: node pool scaling, our rebalancing, upgrades through Strimzi.
- Amazon MSK (IAM), Confluent Cloud and Aiven connection profiles and capability presets.

## Phase 7: Public release v0.1.0 (done, 2026-10-18)

- Documentation, server and agent installers, Helm chart, end-to-end tests in CI, security review.

## Phase 8: v0.2.0 (done, 2026-10-19)

- Agent certificate renewal, Gateway API routes and a LoadBalancer service in the Helm chart, the
  chart repository https://charts.brokerium.kaufen, the published lab node image, the Strimzi
  scenario in CI and fixes for cluster ids and Strimzi scale-ups.

## Phase 9: v0.2.1 (done, 2026-10-20)

- Validation of the configuration commands agents receive, topic creation with the broker
  defaults, retried topic deletion, node buttons for agent clusters, the install script through
  HTTP CONNECT proxies.

## Phase 10: v0.3.0 (done, 2026-10-20)

- Community and Enterprise editions with license keys ([ADR 0016](adr/0016-editions-and-license-keys.md)).
- Enterprise message tools: delete records and empty topics, search, export and copy messages.
- Free: tombstones, binary values and paging in the message views; agents behind
  TLS-terminating proxies ([ADR 0017](adr/0017-agent-transport-behind-proxies.md)); the agent
  without root.

## Phase 11: v0.3.1 (next)

- Node.js 26 LTS for the dashboard build.
- Kafka starts again after a host reboot; the Enterprise features in the end-to-end scenarios.

## Later: Enterprise

- Fleet features: cluster groups and tags, comparison of configurations and versions, rolling
  upgrades and restarts across clusters in waves, configuration policies and drift.
- Access: SSO (OIDC, SAML), SCIM, roles per cluster and custom roles, approvals by a second
  person.
- Audit export to a SIEM, retention, tamper evidence.
- Scheduled and goal-driven rebalancing, self-healing, capacity forecasts, an assistant over MCP.
- Schema Registry decoding and data masking in the message tools.
- Support with SLAs and long-term support builds.

## Lab scenarios

1. Deploy cluster alpha and generate load.
2. Add two brokers (3 to 5), rebalance with throttling and verify an even distribution.
3. Grow the controller quorum from 3 to 5 and shrink it back to 3, watching voters, observers and lag.
4. Replace a "failed" broker (container and volume deleted): with a new node id, and with the same
   id on an empty disk.
5. Decommission a broker: cordon, move replicas, stop, unregister.
6. Create load skew and balance the cluster in one click.
7. Show several clusters in one dashboard, including a cluster without an agent.
8. Rolling upgrade from 4.3.1 to 4.4.0 and finalization.
9. Topics, ACLs, SCRAM users, quotas, consumer groups, messages.
10. Migrate cluster gamma from a static to a dynamic quorum.
