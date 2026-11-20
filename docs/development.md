# Development guide

How the repository is organized and how to build, run and test Brokerium locally. Contribution
rules are in [CONTRIBUTING.md](../CONTRIBUTING.md).

## Repository layout

| Path | What lives there |
| --- | --- |
| `cmd/brokerium-server` | Server entrypoint (API, dashboard, operations engine) |
| `cmd/brokerium-agent` | Agent entrypoint (runs on Kafka hosts or as lab node PID 1) |
| `internal/kafka` | franz-go client factory, KRaft wrappers over `kmsg`, direct controller access, cluster snapshots |
| `internal/kafkaconfig` | Catalog of the broker and topic keys of each supported Kafka minor release, generated data in `catalog/` |
| `internal/cluster` | Cluster registry, connection profiles, capabilities, health evaluation |
| `internal/metrics` | Agentless sampler: log dir sizes and offsets to rates |
| `internal/balancer` | Pure rebalancing planner (no I/O, deterministic) |
| `internal/ops` | Operations engine and Kafka-level operation types (rebalance, drain, finalize, ...) |
| `internal/nodes` | Node lifecycle operations (bootstrap, add, replace, quorum, restart, upgrade, Strimzi pools) |
| `internal/agentgw` | gRPC gateway for agents, internal CA, join tokens |
| `internal/agent` | Agent runtime: Kafka installer, config writer, storage format, process manager |
| `internal/provider` | `docker` (lab containers), `strimzi` (Kubernetes REST client and Strimzi resources) |
| `internal/api` | HTTP handlers, auth middleware, JSON contracts, agent install script |
| `internal/auth`, `internal/secrets` | Password hashing, tokens, roles; sealing of stored credentials |
| `internal/store` | PostgreSQL access, embedded migrations, audit log |
| `internal/config`, `internal/version` | Environment configuration, build version |
| `internal/edition` | Edition of the build, catalog of Enterprise features, license manager interface |
| `internal/webui` | Embedded dashboard build output |
| `internal/tools/catalog` | Generator of the Kafka configuration catalog (`make catalog`) |
| `internal/tools/notices` | Generator of `THIRD_PARTY_NOTICES` and of the third-party section of `NOTICE` (`make notices`) |
| `notices.go` | Embeds `LICENSE`, `NOTICE` and `THIRD_PARTY_NOTICES` into the server for the Licenses page |
| `ee/` | Brokerium Enterprise server code (build tag `enterprise`): `ee.Setup`, `ee/licensing` (license keys and their manager) |
| `web/` | React + TypeScript dashboard (Vite, Mantine, TanStack Query) |
| `web/src/ee`, `web/src/ee-community` | Enterprise dashboard code and its Community stand-in, imported as `@ee` |
| `api/openapi.yaml` | Public REST API description |
| `proto/brokerium/agent/v1` | Agent protocol (generated Go code is committed) |
| `deploy/lab` | Docker Compose lab (alpha, beta, gamma), `strimzi/` (kind + Strimzi), `vm/` (systemd hosts), `gencerts/` |
| `deploy/docker`, `deploy/helm` | Images (server, agent, lab node) and their Docker Hub texts in `hub/`, Helm chart |
| `docs/guide` | User guide: install, clusters, operations, security |
| `docs/adr`, `docs/runbooks` | Architecture decisions, operating procedures |
| `test/e2e` | End-to-end scenarios against the lab (build tag `e2e`) |

## Commands

The toolchain is pinned in [.mise.toml](../.mise.toml); prefix commands with `mise exec --` or
activate mise in your shell.

| Command | Purpose |
| --- | --- |
| `make lab-up` | Start PostgreSQL and Kafka cluster alpha (3 controllers + 3 brokers, dynamic quorum) |
| `make lab-up PROFILES="alpha beta gamma"` | Start more lab clusters |
| `make lab-down` / `make lab-reset` | Stop the lab / stop and delete all lab volumes |
| `make dev` | Run the server on the host against the lab (http://localhost:8080), with Docker clusters (`BROKERIUM_DOCKER_PROVIDER=on`) |
| `make web-dev` | Vite dev server for the dashboard with API proxy |
| `make build` | Build the dashboard and both binaries into `bin/` |
| `make build EDITION=enterprise` | The same for Brokerium Enterprise: `bin/brokerium-server-enterprise` with the Enterprise dashboard |
| `make test` | Unit tests (Go and web), the Go tests for both editions |
| `make lint` | golangci-lint for both editions and web lint |
| `make proto` | Regenerate the agent protocol code |
| `make catalog` | Generate the Kafka configuration catalog in Docker; `VERSIONS="4.4.0"` for one release |
| `make notices` | Regenerate `THIRD_PARTY_NOTICES` and the third-party section of `NOTICE` after a dependency change |
| `make lab-certs` | Throwaway CA and certificates for the SASL and SSL listeners of beta |
| `make images` | Build the server, agent and lab node images locally |
| `make e2e` | End-to-end scenarios against a running lab and server |
| `make screenshots` | Recreate cluster alpha with demo data and capture the dashboard into `docs/images` in both themes |
| `deploy/lab/strimzi/strimzi.sh up\|down` | kind cluster with Strimzi and Kafka "lab"; kubeconfig `.data/kind.kubeconfig` |
| `deploy/lab/vm/vm.sh up\|down` | Containerized server on port 18080 and systemd hosts for the agent installer |

The store tests run against PostgreSQL when `BROKERIUM_TEST_DATABASE_URL` points to a database,
for example `postgres://brokerium:brokerium@localhost:55432/brokerium_test?sslmode=disable` in
the lab PostgreSQL after `CREATE DATABASE brokerium_test`; they apply the migrations and remove
the rows they create. Without the variable they are skipped.

## Editions

Brokerium Enterprise ([ADR 0016](adr/0016-editions-and-license-keys.md)) is built from the same
tree: Go with `-tags enterprise`, the dashboard with `VITE_EDITION=enterprise`. Every file in `ee/`
carries the build tag, and the Community server must not depend on any `ee` package; the `go` job
of CI checks it with `go list -deps ./cmd/brokerium-server`. Dashboard code imports Enterprise
code only through `@ee`, which resolves to `web/src/ee` in Enterprise builds and to
`web/src/ee-community` otherwise; `web/scripts/check-edition.mjs` checks a build for the marker of
`web/src/ee`.

The `licensetest` tag adds a test signing key, whose private seed is public in
`ee/licensing/testdata/test-signing.key`, so tests can create license keys:

```bash
go test -tags enterprise,licensetest ./...
golangci-lint run --build-tags enterprise,licensetest ./...
```

Release builds never use `licensetest`, and the release workflow refuses binaries built with it.

## End-to-end tests

`make e2e` runs the scenarios in `test/e2e` against a running lab and server. Variables:

- `BROKERIUM_E2E_HEAVY=1` enables scenarios that create Docker clusters. A server on the host
  offers them only with `BROKERIUM_DOCKER_PROVIDER=on`, which `make dev` sets.
- `BROKERIUM_E2E_UPGRADE_FROM` and `BROKERIUM_E2E_UPGRADE_TO` (default `4.3.1` and `4.4.0`) are
  the releases of `TestRollingUpgrade`.
- `BROKERIUM_E2E_CONTROLLERS` lists controller addresses, from which `TestControllersDirect`
  reads the quorum without brokers.
- `BROKERIUM_E2E_STRIMZI_KUBECONFIG` points to the kubeconfig of the Strimzi lab.
- `BROKERIUM_E2E_VM=1` with `BROKERIUM_E2E_URL` runs the agent installer scenarios against the
  VM lab. With `BROKERIUM_E2E_VM_PREVIOUS` set to a directory with the `install.sh` and the
  Linux `brokerium-agent` of an earlier release, the hosts are installed with those first and
  then converted by the current installer.
- `BROKERIUM_E2E_KEEP=1` keeps the clusters a test created.
- `BROKERIUM_E2E_CERT_LIFETIME=3m` waits for an agent certificate renewal; start the server
  with the same `BROKERIUM_AGENT_CERT_LIFETIME`.
- `BROKERIUM_E2E_INSECURE_PROXY=localhost:9443` runs `TestInsecureAgentBehindProxy` through a
  TLS-terminating proxy in front of `BROKERIUM_AGENT_INSECURE_ADDR`; the agents `proxied-c1`
  and `proxied-b101` must be enrolled through it and resolvable by the server, as the `e2e`
  workflow sets them up.

The Strimzi lab uses its own kubeconfig in `.data/` and never touches `~/.kube/config`.

The Enterprise scenarios have the build tags `e2e` and `enterprise`. They sign license keys with
the test key, so they need a server built with `-tags enterprise,licensetest` and the lab cluster
beta registered as `beta`:

```bash
go build -tags enterprise,licensetest -o bin/brokerium-server-enterprise ./cmd/brokerium-server
go test -tags e2e,enterprise -count=1 -run TestEnterprise ./test/e2e
```

In CI, the `e2e` workflow runs on pull requests, nightly and on demand, in three parallel jobs:

- `api`: the scenarios against beta and the small Docker cluster `ci-small`, then the scenario of
  agents behind OpenResty;
- `docker-clusters`: the scenarios that create Docker clusters. Their agents download the Kafka
  releases they install from a mirror on the runner (`BROKERIUM_KAFKA_MIRROR`), which serves
  archives cached from the Apache mirrors;
- `enterprise`: the Enterprise scenarios against beta.

Pull requests skip the jobs their changes do not need, for example changes of the documentation,
the dashboard or the Helm chart, and the job `e2e`, the required check, passes when every other
job passed or was skipped. Each Community scenario belongs to one job (`API_SCENARIOS`,
`PROXY_SCENARIOS` and `DOCKER_SCENARIOS` in the workflow), and the `api` job fails for a scenario
that is in none of them. The `e2e-strimzi` workflow runs `TestStrimzi` on a kind cluster nightly,
on demand and on pull requests that change the Strimzi support.

## Lab

The clusters, ports and node ids are described in [deploy/lab/README.md](../deploy/lab/README.md).
A few facts that matter while developing:

- Node ids: controllers `1..5`, brokers `101..106`. The host listener of broker `N` is
  `localhost:<cluster base port + N>` (alpha 19000, beta 29000, gamma 39000).
- The lab needs about 6 CPUs and 16 GB of memory for Docker; JVM heaps are 512 MB for brokers
  and 384 MB for controllers. Start only the profiles you need.
- Docker clusters created by the Docker provider get their own port base and use the lab node
  image `bvaliev/brokerium-agent:lab`; rebuild it with `make images` after agent changes. A
  server on the host offers Docker clusters only with `BROKERIUM_DOCKER_PROVIDER=on` and reaches
  their nodes through the ports they publish on localhost; a server in a container reaches them
  by container name ([ADR 0019](adr/0019-docker-clusters-for-docker-installations.md)). The
  server of the VM lab runs in a container without the Docker socket and offers none.
- Strimzi lab: kind cluster `brokerium`, Kafka `lab` with node pools `controllers` (ids 0-9)
  and `brokers` (ids 100+), node ports 32100-32105 on localhost.
- VM lab: Debian containers with systemd (`vm-host-1` to `vm-host-3`) and the server container on
  port 18080.

## Kafka configuration catalog

`internal/kafkaconfig` embeds the broker and topic keys of every supported Kafka minor release
([ADR 0025](adr/0025-kafka-configuration.md)), one file per release line in `catalog/<line>.json`:
type, default, documentation, sensitivity, importance and, for broker keys, whether they are
read-only and their update mode. A cluster on a release without a catalog uses the catalog of the
closest older line.

`make catalog` regenerates the files from the releases listed in `internal/tools/catalog/main.go`,
the latest patch of each line. For every release it starts a single combined KRaft node in Docker
(`brokerium-catalog-<version>`, removed afterwards), creates a topic and reads DescribeConfigs of
the broker and the topic with synonyms and documentation. The update modes and the importance
come from the configuration pages of the Kafka documentation of the line,
`https://kafka.apache.org/<line without the dot>/generated/kafka_config.html` and
`topic_config.html`; a line without those pages uses the closest line that has them, and the file
names the pages it used. Releases from 3.7 run the `apache/kafka` image. 3.5 and 3.6 run the
release tarball on Eclipse Temurin 17: the generator downloads it once into `.data/catalog/` and
checks it against the SHA-512 sum Apache publishes.

To add a Kafka release:

1. Put the release into `releases` of `internal/tools/catalog/main.go`, in place of the older
   patch of its line; a new line also goes into `supported` of
   `internal/kafkaconfig/kafkaconfig_test.go`.
2. Run `make catalog VERSIONS="4.5.0"`, which takes a few seconds once the image is pulled.
3. Read the warnings of the generator, where Kafka and its documentation disagree, and the
   difference to the previous line, for example
   `diff internal/kafkaconfig/catalog/4.4.json internal/kafkaconfig/catalog/4.5.json`.
4. Run `go test ./internal/kafkaconfig/ ./internal/tools/catalog/`, and extend the known keys of
   `kafkaconfig_test.go` where a default or an update mode changed.

The generator takes the default of a broker key from its DEFAULT_CONFIG synonym, because the value
reflects the configuration of the node; a key without that synonym has no default. A topic key
defaults to the DEFAULT_CONFIG synonym of the broker key it inherits from, or, for keys whose
synonyms Kafka drops (`retention.ms`, `segment.ms`), to the value of the new topic. That is why
the node sets only the keys it needs to start, and the generator stops when any other key comes
from anywhere but the defaults. Kafka from 4.1 records `min.insync.replicas` as a cluster-wide
dynamic value when it creates a cluster; it equals the default and is accepted. When the
documentation and the read-only flag of Kafka disagree, Kafka decides, and a dynamic key the
documentation does not place counts as per-broker, the update every dynamic key accepts. The
throttles of reassignments (`leader.replication.throttled.rate`,
`follower.replication.throttled.rate`, `replica.alter.log.dirs.io.max.bytes.per.second`) are in
the documentation from 4.3, but Kafka reports them only once they are set and the operations
engine owns them, so they are not in the catalog.

## Releases

A tag `vX.Y.Z` publishes a release:

- the `release` workflow attaches the binaries of both editions, the Helm chart
  `brokerium-X.Y.Z.tgz`, `LICENSE`, `LICENSE-enterprise`, `NOTICE`, `THIRD_PARTY_NOTICES`,
  `SHA256SUMS` and an SBOM to the GitHub release, with the notes of the matching section of
  `CHANGELOG.md`;
- it pushes the chart to `oci://ghcr.io/brokerium/charts` and runs the `helm-repo` workflow,
  which rebuilds the index of https://charts.brokerium.kaufen (GitHub Pages) from the charts of
  all releases;
- the `CI` workflow publishes `bvaliev/brokerium-server`, `bvaliev/brokerium-server-enterprise`
  and `bvaliev/brokerium-agent` as `X.Y.Z`, `X.Y` and `latest`, and the lab node image as
  `bvaliev/brokerium-agent:X.Y.Z-lab`.

The tag sets the chart version and `appVersion`. The `helm` workflow checks every change to the
chart: lint, schema validation of each networking mode and an install on kind.

Between releases the `CI` workflow publishes the images of `main` as `edge` and the lab node image
as `bvaliev/brokerium-agent:lab` every night and when it is run by hand on `main`. The install of
the `helm` workflow uses `edge`, so a chart change that needs a newer server than the last nightly
build waits for the next one, or for a manual run of `CI` on `main`.
