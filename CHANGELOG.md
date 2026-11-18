# Changelog

All notable changes to Brokerium are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

### Fixed

- Kafka starts again after a host reboot: the agent starts `brokerium-kafka.service` when Kafka
  was running before, while a node stopped from Brokerium stays stopped.

## [0.3.0] - 2026-10-20

Brokerium Enterprise with the message tools, agents behind TLS-terminating proxies and without
root, and tombstones, binary values and paging in the message views.

### Added

- Editions ([ADR 0016](docs/adr/0016-editions-and-license-keys.md)): Brokerium Community stays
  free, and Brokerium Enterprise unlocks paid features with a license key that the server checks
  offline. Enterprise ships as the image `bvaliev/brokerium-server-enterprise` and the binaries
  `brokerium-server-enterprise-<os>-<arch>`; the Helm chart selects it with `edition: enterprise`
  and reads the key from `license.existingSecret`. `GET /api/v1/license` and the **License** page
  show the edition, its features and the license in both editions, and admins of an Enterprise
  server set the key there or with `BROKERIUM_LICENSE` or `BROKERIUM_LICENSE_FILE`.
- Enterprise message tools: delete the records before offsets or a time, or empty a topic, with a
  preview and confirmation by the topic name; search records by key, value, header and time;
  export them as NDJSON, JSON or CSV; copy or replay them to a topic of the same or another
  cluster with the `copy-messages` operation. Deletions and exports are recorded in the audit log.
- Agents behind TLS-terminating proxies such as OpenResty or nginx
  ([ADR 0017](docs/adr/0017-agent-transport-behind-proxies.md)): an optional plaintext listener on
  the server (`BROKERIUM_AGENT_INSECURE_ADDR`, Helm `agents.insecure`) and the agent transport
  `insecure`, in which every command is signed with the key of the internal CA and every message
  of an agent carries an HMAC of its node secret; enrollment proves the join token without
  sending it.
- **Produce** writes tombstones and binary keys, values and headers (base64), and **Messages**
  pages through a topic with **Older** and **Newer**.

### Changed

- The agent runs as the `brokerium-agent` account. The install script, which now needs `sudo`,
  creates the account, a fixed `brokerium-kafka.service` unit and a sudo rule that allows only
  starting, stopping and restarting that unit. Running the install command again converts hosts
  installed by earlier versions without restarting Kafka.
- `POST /api/v1/clusters/{id}/topics/{topic}/messages` requires `value`; `null` produces a
  tombstone.
- The **Nodes** page shows the transport of agent nodes.

### Fixed

- The agent moves to the next mirror when a download of Kafka receives no data for a minute,
  instead of waiting until the step times out.

### Upgrading

- Upgrade the server first, then run the install command with a new join token on each host. Do
  not pin `BROKERIUM_AGENT_VERSION` to an agent older than 0.3.0: the install script of 0.3.0
  sets up hosts for an agent without root, which older agents cannot run on.

## [0.2.1] - 2026-10-20

A security update for the agent; upgrade the agents on your hosts with the install command.

### Security

- The agent and the server validate the configuration that the agent writes: property names
  of letters, digits, `.`, `_` and `-`, values without control characters, and heap options
  made only of `-Xms` and `-Xmx`. Before, a server, or an operator who could change the heap
  settings of a cluster, could make the agent write arbitrary lines into `server.properties`
  and into the systemd unit of Kafka, which the agent writes as root. The API now also checks
  the heap settings when cluster settings are saved.

### Added

- The install script uses `HTTPS_PROXY` and `NO_PROXY` for its downloads and passes them to
  the agent, which reaches the server through HTTP CONNECT proxies with mutual TLS intact.
  `BROKERIUM_AGENT_PROXY` and `BROKERIUM_AGENT_NO_PROXY` (Helm `agents.proxy`) add them to the
  install command.

### Fixed

- Partitions and replication factor 0 create a topic with the broker defaults, as the API
  documents; the dashboard leaves the fields empty for that.
- Deleting a topic no longer fails with an unknown topic when a retried request finds it
  deleted by the first attempt.
- Add broker and Replace broker are enabled again for agent clusters in the dashboard.

## [0.2.0] - 2026-10-19

Agents renew their certificates, and the Helm chart gets Gateway API routes, a LoadBalancer
service and its own chart repository.

### Added

- Agent certificate renewal: after two thirds of its lifetime an agent renews its certificate
  over the existing mutual TLS connection, without restarting Kafka. The **Nodes** page shows
  when each certificate expires, and a certificate that was not renewed in time becomes a
  health issue of its cluster. The server renews the certificate of the agent gateway as well.
  `BROKERIUM_AGENT_CERT_LIFETIME` sets both lifetimes (one year by default).
- Helm chart networking: an optional `LoadBalancer` service with both ports and an external-dns
  host name, and Gateway API routes on a created or an existing Gateway: an `HTTPRoute` for the
  dashboard with HTTPS at the gateway and a `TLSRoute` in passthrough mode for agents. The
  chart derives the public URL, the agent endpoint and the gateway certificate names from these
  settings and validates its values with a JSON schema.
- Helm chart repository: `helm repo add brokerium https://charts.brokerium.kaufen`. The chart is
  also published as `oci://ghcr.io/brokerium/charts/brokerium` and attached to the release.
- Lab node image for the Docker provider: `bvaliev/brokerium-agent:0.2.0-lab`, and `:lab` from
  `main`. A release of the server uses the lab image of its own version by default.

### Changed

- The Brokerium logo in the dashboard, the README and the chart icon.
- The repository, the images and the agent downloads moved to the `brokerium` organization.
- On narrow screens the cluster overview keeps the cluster column in view and hides secondary
  columns.
- End-to-end scenarios run on every pull request, the Strimzi scenario runs on kind nightly, and
  the Helm chart is linted, validated and installed on kind when it changes.

### Fixed

- Storage formatting failed for generated cluster ids that start with `-`. New ids never start
  with a dash, and clusters with such an id format as well.
- Scaling a Strimzi node pool could start the rebalance before the new brokers accepted
  connections. The operation now waits until every broker of the pool answers on its listener,
  and the rebalance retries transient network errors while it plans.
- The distribution and topic views round the rate window.

### Removed

- The Ingress of the Helm chart and the `service.type` and `service.grpcType` values. Use
  `gateway.*` or `loadBalancer.*`; the chart README describes the upgrade from chart 0.1.

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
  [security review](https://github.com/brokerium/brokerium/blob/v0.1.0/docs/security-review-0.1.0.md).

### Known limitations

- Controller quorum changes are not offered for Strimzi clusters; Strimzi owns the quorum.
- The Helm chart installs from a checkout; it is not published to a chart repository yet.
- Agent certificates are valid for one year and are not renewed automatically yet.

[Unreleased]: https://github.com/brokerium/brokerium/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/brokerium/brokerium/releases/tag/v0.3.0
[0.2.1]: https://github.com/brokerium/brokerium/releases/tag/v0.2.1
[0.2.0]: https://github.com/brokerium/brokerium/releases/tag/v0.2.0
[0.1.0]: https://github.com/brokerium/brokerium/releases/tag/v0.1.0
