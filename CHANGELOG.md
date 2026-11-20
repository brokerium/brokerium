# Changelog

All notable changes to Brokerium are documented here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/), and the project uses
[semantic versioning](https://semver.org/).

## [Unreleased]

## [0.3.2] - 2026-11-20

Fixes from clusters on real hosts, the removal of agents of deleted nodes, the licenses of every
component, and Node.js 26.

### Added

- The license texts and notices of every third-party component: `THIRD_PARTY_NOTICES` lists the
  Go modules of the server and the agent and the npm packages of the dashboard with their
  licenses, and `NOTICE` reproduces the NOTICE files of the components
  ([ADR 0030](docs/adr/0030-third-party-notices.md)). The images carry them in `/licenses`, the
  releases attach them with the licenses of Brokerium, and the **Licenses** page of the dashboard
  shows them (`GET /api/v1/notices`). The lab node image keeps the license files of Apache Kafka.
- Removing an agent from a host: `GET /api/v1/agent/uninstall.sh` removes the agent, its units,
  its sudo rule and its state; `--stop-kafka`, `--remove-kafka` and `--delete-data` also stop and
  remove Kafka and its data. After deleting a node the dashboard shows the command
  ([ADR 0031](docs/adr/0031-node-deletion-and-agent-removal.md)).
- Agents report the Kafka process they run (process id, start time and the Kafka version of its
  classpath) and the Kafka processes on the host that they do not run; the **Nodes** page shows
  them.
- `GET /api/v1/server` reports `defaultKafkaVersion`, the release the creation forms and the
  **Upgrade** tab offer.

### Changed

- Deleting a node closes the session of its agent, and the agent exits with status 6 instead of
  reconnecting forever; the unit of the install script does not restart it
  (`RestartPreventExitStatus=6`). On the insecure transport the agent stops only on an answer
  signed by the CA. Nodes that run a broker or a controller cannot be deleted, connected or not;
  decommission them first. A combined node can be deleted once its cluster no longer knows it.
- Restarts and upgrades wait for a new Kafka process with the expected version. A Kafka process on
  the host that the agent does not run, such as the old unit of an adopted node, fails starts,
  restarts and upgrades of its node with its process id and user, and plans warn about it.
- New clusters and nodes get Kafka 4.4.0 by default. Clusters whose settings record no version
  keep 4.3.1.
- From 0.4.0, new features are part of Brokerium Enterprise; everything published up to 0.3.1
  stays in the Community edition ([ADR 0016](docs/adr/0016-editions-and-license-keys.md)).
- The Brokerium Commercial License 1.1 names Czech law and the courts of the Czech Republic and
  excludes the CISG. It, CLA and CCLA 1.1 and GOVERNANCE.md speak of an exclusive license and of
  assigning the licenses received instead of transferring rights.
- The dashboard is built with Node.js 26 LTS. The images of the Community server, the agent and
  the lab node carry their own title and license labels.

### Fixed

- `log.dirs` of agent nodes follows the Kafka data directory of the agent
  (`BROKERIUM_AGENT_KAFKA_DATA_DIR`) instead of always `/var/lib/kafka/data`.
- `PATCH /api/v1/clusters/{id}` merges `settings` into the stored ones and `null` removes a key.
  Before, a partial update dropped the Kafka version, the quorum type, the replication settings
  and the racks. The keys Brokerium reads are validated.
- ACLs on `USER` resources can be created and deleted.
- The documentation says that clusters Brokerium creates accept unauthenticated clients
  (PLAINTEXT listeners, `User:ANONYMOUS` as a super user), and the creation wizard shows it. The
  operations guide describes what the audit log records.

### Security

- typescript-eslint 8.73, whose dependencies no longer include a minimatch vulnerable to
  GHSA-g3hv-75wg-m249 (a development dependency, not part of the dashboard bundle).

### Upgrading

- Update the server, then the agents: create a join token for the node on the **Nodes** page and
  run its command on the host again, so that the agent unit gets `RestartPreventExitStatus=6`.
  Agents before 0.3.2 keep reconnecting after their node is deleted.
- Scripts that delete nodes get `200` with `uninstallCommand` instead of `204`, and `409` for
  nodes that run a broker or a controller.
- Scripts that update cluster settings keep the keys they do not send; send `null` to remove one.
- A host whose agent was installed with another `BROKERIUM_AGENT_KAFKA_DATA_DIR` than
  `/var/lib/kafka/data` while its Kafka writes to `/var/lib/kafka/data` gets the configured
  directory on the next restart: move the data first or install the agent again with the
  directory Kafka uses.

## [0.3.1] - 2026-11-18

Clusters created from the dashboard on hosts with agents, hosts that keep their own names,
broker and controller on one node, and Docker clusters for the Docker installation.

### Added

- Clusters on hosts with brokerium-agent from the dashboard: **Add cluster → Create on hosts
  with agents** lists the free hosts; give each one a role (controller, broker, or controller and
  broker), a node id, a rack and its advertised host, add a host with its install command in
  place, check the choice and start the plan. **Add broker**, **Add controller** and **Replace
  broker** of agent clusters choose free hosts, and the **Nodes** page shows and edits the
  advertised host, rack and name of every node.
- Combined nodes, broker and controller in one Kafka process
  ([ADR 0021](docs/adr/0021-combined-nodes.md)): `bootstrap-cluster` takes the role `combined`
  on agent hosts and `combined: <count>` for Docker clusters, and the plan warns about them.
  They are voters and brokers; rolling restarts and upgrades restart them with the controllers,
  the quorum leader last, and wait for their replicas. `replace-broker`, `drain-broker` and
  `remove-controller` refuse combined nodes, and adopted nodes with both roles become combined
  nodes.
- `GET /api/v1/server` reports the version, the edition and whether the server offers Docker
  clusters, with the reason when it does not.
- The Docker installation creates and manages Docker clusters: the server in its container
  reaches the nodes by container name on the network `brokerium-lab`. The install guide shows
  the `docker run` commands with the Docker socket. The Docker provider pulls a missing node
  image.
- `BROKERIUM_DOCKER_AGENT_ENDPOINT` is the address the agents in the nodes of Docker clusters
  dial, `brokerium:8443` in the Docker installation. It defaults to `BROKERIUM_AGENT_ENDPOINT`,
  which stays the address in the join commands for agents on other hosts.
- Agent nodes have an advertised host, the address their listeners advertise and the server
  connects to (by default the host name the agent reports), and a rack, the default
  `broker.rack` of a broker on them. `PATCH /api/v1/nodes/{id}` changes them and the node name.
- Join tokens without a node name: the node is named after the host name its agent reports,
  with `-2`, `-3` and so on when that name is taken. A join token for `nodeId` enrolls an
  existing node again, which keeps its name and role.
- Kafka releases from an internal mirror: `BROKERIUM_KAFKA_MIRROR` (Helm `agents.kafkaMirror`)
  goes into every install command, and agents try it before the Apache mirrors; checksums keep
  coming from Apache.

### Changed

- Docker clusters are offered only where they work
  ([ADR 0019](docs/adr/0019-docker-clusters-for-docker-installations.md)): with
  `BROKERIUM_DOCKER_PROVIDER=auto`, the default, only by a server in a Docker container outside
  Kubernetes that reaches the Docker API; `on` offers them for development on the Docker host and
  `off` never, as set by the Helm chart. Otherwise creating a Docker cluster and operations that
  change its nodes fail with 409 `docker_unavailable`; existing Docker clusters stay monitored.
- The certificate of the agent gateway always includes the hosts of `BROKERIUM_AGENT_ENDPOINT` and
  `BROKERIUM_DOCKER_AGENT_ENDPOINT`.
- Agent clusters run on the hosts you choose instead of hosts named `<cluster>-c<id>` and
  `<cluster>-b<id>` ([ADR 0018](docs/adr/0018-explicit-node-assignment.md)):
  `bootstrap-cluster` takes `nodes` with a role and an optional node id and rack per host,
  `add-broker` takes `nodes`, and `add-controller` and `replace-broker` take `node`. Plans refuse
  hosts that are not connected, already run Kafka, belong to another cluster or hold the data of
  another node, and the steps check the hosts again. Docker clusters keep their parameters.
- Removing a node from an agent cluster (decommission, removing a controller, replacing a
  broker) releases its host for other clusters, and so does deleting the registration of a
  cluster. A host that is down at that moment stops Kafka when its agent connects again, so that
  it does not run the node next to its replacement.
- Planning `add-broker` or `add-controller` again on a host that kept its role from a failed
  attempt continues its node, as long as the broker is not registered or the controller is not a
  voter.
- Rolling restarts, upgrades and quorum migrations of agent clusters record the bootstrap
  servers again at the end, so that changed advertised hosts are used; clusters with adopted
  nodes keep theirs.
- A controller quorum grows to at most five voters.
- The dashboard offers **Create in Docker** only when the server offers Docker clusters, and
  **Connect an existing cluster** no longer offers the Docker provider.
- `edge` and `lab` images are built nightly from `main` instead of on every push, and `sha-*`
  tags are no longer published ([ADR 0020](docs/adr/0020-ci-by-need.md)).

### Fixed

- Kafka starts again after a host reboot: the agent starts `brokerium-kafka.service` when Kafka
  was running before, while a node stopped from Brokerium stays stopped.
- Decommissioning a broker and removing a controller of an agent cluster stop Kafka on the host.
  Before, the decommission waited for the broker to stop and failed.
- A stopped `brokerium-kafka.service` is inactive instead of failed (`SuccessExitStatus=143`), so
  monitoring of failed units no longer reports planned stops. Agents running as root rewrite
  their unit when they start Kafka; run the install command again on hosts with the agent
  without root.

### Security

- Built with Go 1.27.4, whose `crypto/x509` distrusts compromised intermediate certificates
  (GO-2026-6809); the agent verifies certificates when it renews its own.

### Upgrading

- Update the server first, then the agents: on the **Nodes** page create a join token with the
  name of the node and run its command on the host again.
- A server that runs on the Docker host and creates Docker clusters, in development or tests,
  needs `BROKERIUM_DOCKER_PROVIDER=on`; `make dev` sets it.
- Existing agent clusters keep their addresses: nodes that run Kafka get their name as advertised
  host, the other nodes the host name their agent reported. Check the advertised hosts on the
  **Nodes** page before you add nodes.
- Scripts that plan `bootstrap-cluster`, `add-broker`, `add-controller` or `replace-broker` on
  agent clusters pass the hosts with `nodes` or `node`.
- To enroll a host again, for example to switch the agent transport or after its certificate
  expired, create the join token for its node (`nodeId`) or with its node name; a token without
  either enrolls the host as a new node.

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

[Unreleased]: https://github.com/brokerium/brokerium/compare/v0.3.1...HEAD
[0.3.1]: https://github.com/brokerium/brokerium/releases/tag/v0.3.1
[0.3.0]: https://github.com/brokerium/brokerium/releases/tag/v0.3.0
[0.2.1]: https://github.com/brokerium/brokerium/releases/tag/v0.2.1
[0.2.0]: https://github.com/brokerium/brokerium/releases/tag/v0.2.0
[0.1.0]: https://github.com/brokerium/brokerium/releases/tag/v0.1.0
