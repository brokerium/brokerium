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

## Later: commercial layer

SSO, fine-grained roles, audit export, scheduled rebalancing and self-healing, an MCP / AI
assistant, a hosted edition.

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
