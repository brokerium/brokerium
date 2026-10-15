# ADR 0011: Boundary between the free product and a future paid edition

- Status: accepted
- Date: 2026-10-15

## Context

[GOVERNANCE.md](../../GOVERNANCE.md) promises that features free in a published version are
never moved behind a paywall later. Phase 4 ships Brokerium users, roles and the audit log, so
the line has to be drawn before v0.1.0 is published; otherwise anything released free narrows
what a paid edition (or a buyer, see [docs/legal/sale-readiness.md](../legal/sale-readiness.md))
can offer.

## Decision

Everything that keeps one team's Kafka clusters healthy is free, under PolyForm Shield 1.0.0:

- multi-cluster dashboard, health, snapshots, agentless rates;
- operations engine with plans, confirmations, pause, resume, cancel;
- balancer, manual moves, throttled reassignments, leader elections;
- node lifecycle through the agent, the Docker provider and the Strimzi provider: create,
  add, replace, drain and decommission brokers, add and remove controllers, quorum migration,
  rolling restarts and upgrades with finalization;
- data plane: topics, configs, messages, ACLs, SCRAM, quotas, consumer and share groups;
- local users with the three fixed roles (admin, operator, viewer), bearer tokens, the audit
  log in the dashboard and through the API;
- installers, Helm chart, OpenAPI.

Reserved for a paid edition (none of it is in v0.1.0):

- single sign-on (OIDC, SAML), SCIM provisioning;
- fine-grained access control: roles per cluster, custom roles, approvals by a second person
  (four-eyes) for irreversible operations;
- audit export and streaming to a SIEM, retention policies, tamper evidence;
- scheduled and goal-driven rebalancing, self-healing (automatic replacement of failed
  brokers), capacity forecasts;
- AI assistant over MCP;
- the hosted SaaS, support with SLAs, long-term support builds.

Rules for later releases:

- A paid feature is additive and never required to operate a cluster safely: the safety
  gates, dry runs and audit stay free.
- Before each release the owner checks new features against this list; borderline features
  are decided in an amendment to this ADR before they are published.

## Consequences

- v0.1.0 can ship roles and the audit log without closing doors for a paid edition.
- The code keeps paid features out of the free binary instead of hiding them behind flags,
  so the published source matches what users get.
- Contributors know in advance which proposals belong to the free product.
