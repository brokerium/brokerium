# Changelog

All notable changes to Brokerium are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

## [0.1.0] - 2026-10-18

First public alpha. Every lab scenario of the roadmap runs as an automated end-to-end test.

### Added

- Multi-cluster dashboard: health, brokers (including fenced and cordoned), the KRaft quorum
  with voters, observers and lag, topics, partition distribution, consumer and share groups,
  configuration, versions and features. The quorum is read straight from the controllers when
  brokers are down.
- Connection profiles: PLAINTEXT, SASL/PLAIN, SASL/SCRAM, TLS and mTLS, Amazon MSK IAM (static
  keys or the AWS default credential chain), Confluent Cloud and Aiven presets. Stored
  credentials are sealed with the server key.
- Operations engine: dry-run plans, explicit confirmation, durable steps, health gates, pause,
  resume and cancel, one operation per cluster, audit log.
- Rebalancing: manual replica and leader moves with a preview, one-click even balancing with a
  deterministic planner, throttled reassignments that always clear their throttles.
- Node lifecycle through `brokerium-agent`: create clusters, add, drain, decommission and
  replace brokers, add and remove controllers, migrate static quorums to dynamic ones, rolling
  restarts, adoption of existing nodes.
- Rolling upgrades one node at a time with preflight checks (Java version, known issues such as
  KAFKA-20979) and a separate finalization of the metadata version.
- Data plane: topics, ACLs, SCRAM users, client quotas, consumer and share groups with offset
  resets, message browsing and producing.
- Users with roles (admin, operator, viewer); sessions as cookies for the dashboard or bearer
  tokens for scripts; CSRF protection.
- Providers: `external`, `agent` (systemd hosts), `docker` (labs) and `strimzi` (node pool
  scaling, rolling restarts and upgrades through Strimzi custom resources).
- Installation: Helm chart, container images, release binaries, an agent install script served
  by the server, OpenAPI 3.1 description of the REST API.
- Docker lab with three Kafka clusters, a Strimzi lab in kind and a lab of systemd hosts.
- Repository foundation: PolyForm Shield 1.0.0 license, CLA, contribution and governance
  policies.

### Security

- Failed logins are limited per user and address, password checks run at most four at a time,
  and unknown usernames take as long to reject as known ones.
- A new password or a disabled account ends the user's other sessions.
- Strict Content-Security-Policy for the dashboard.
- The agent install script verifies the binary against the checksums of its release.
- Findings and accepted risks:
  [security review](https://github.com/brokerium/brokerium/blob/v0.1.0/docs/research/security-review-0.1.0.md).

### Known limitations

- Controller quorum changes are not offered for Strimzi clusters; Strimzi owns the quorum.
- The Helm chart installs from a checkout; it is not published to a chart repository yet.
- Agent certificates are valid for one year and are not renewed automatically yet.

[Unreleased]: https://github.com/brokerium/brokerium/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/brokerium/brokerium/releases/tag/v0.1.0
