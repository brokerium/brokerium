# Development guide

How the repository is organized and how to build, run and test Brokerium locally. Contribution
rules are in [CONTRIBUTING.md](../CONTRIBUTING.md).

## Repository layout

| Path | What lives there |
| --- | --- |
| `cmd/brokerium-server` | Server entrypoint (API, dashboard, operations engine) |
| `cmd/brokerium-agent` | Agent entrypoint (runs on Kafka hosts or as lab node PID 1) |
| `internal/kafka` | franz-go client factory, KRaft wrappers over `kmsg`, direct controller access, cluster snapshots |
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
| `make dev` | Run the server on the host against the lab (http://localhost:8080) |
| `make web-dev` | Vite dev server for the dashboard with API proxy |
| `make build` | Build the dashboard and both binaries into `bin/` |
| `make build EDITION=enterprise` | The same for Brokerium Enterprise: `bin/brokerium-server-enterprise` with the Enterprise dashboard |
| `make test` | Unit tests (Go and web), the Go tests for both editions |
| `make lint` | golangci-lint for both editions and web lint |
| `make proto` | Regenerate the agent protocol code |
| `make lab-certs` | Throwaway CA and certificates for the SASL and SSL listeners of beta |
| `make images` | Build the server, agent and lab node images locally |
| `make e2e` | End-to-end scenarios against a running lab and server |
| `make screenshots` | Recreate cluster alpha with demo data and capture the dashboard into `docs/images` in both themes |
| `deploy/lab/strimzi/strimzi.sh up\|down` | kind cluster with Strimzi and Kafka "lab"; kubeconfig `.data/kind.kubeconfig` |
| `deploy/lab/vm/vm.sh up\|down` | Containerized server on port 18080 and systemd hosts for the agent installer |

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

- `BROKERIUM_E2E_HEAVY=1` enables scenarios that create Docker clusters.
- `BROKERIUM_E2E_STRIMZI_KUBECONFIG` points to the kubeconfig of the Strimzi lab.
- `BROKERIUM_E2E_VM=1` with `BROKERIUM_E2E_URL` and `BROKERIUM_E2E_CONTROLLERS` runs the
  agent installer scenarios against the VM lab. With `BROKERIUM_E2E_VM_PREVIOUS` set to a
  directory with the `install.sh` and the Linux `brokerium-agent` of an earlier release, the
  hosts are installed with those first and then converted by the current installer.
- `BROKERIUM_E2E_KEEP=1` keeps the clusters a test created.
- `BROKERIUM_E2E_CERT_LIFETIME=3m` waits for an agent certificate renewal; start the server
  with the same `BROKERIUM_AGENT_CERT_LIFETIME`.
- `BROKERIUM_E2E_INSECURE_PROXY=localhost:9443` runs `TestInsecureAgentBehindProxy` through a
  TLS-terminating proxy in front of `BROKERIUM_AGENT_INSECURE_ADDR`; the agents `proxied-c1`
  and `proxied-b101` must be enrolled through it and resolvable by the server, as the `e2e`
  workflow sets them up.

The Strimzi lab uses its own kubeconfig in `.data/` and never touches `~/.kube/config`.

In CI, the `e2e` workflow runs the scenarios against the beta lab and Docker clusters on every
pull request, and then the scenario of agents behind OpenResty. The `e2e-strimzi` workflow runs `TestStrimzi` on a kind cluster nightly, on demand
and on pull requests that change the Strimzi support.

## Lab

The clusters, ports and node ids are described in [deploy/lab/README.md](../deploy/lab/README.md).
A few facts that matter while developing:

- Node ids: controllers `1..5`, brokers `101..106`. The host listener of broker `N` is
  `localhost:<cluster base port + N>` (alpha 19000, beta 29000, gamma 39000).
- The lab needs about 6 CPUs and 16 GB of memory for Docker; JVM heaps are 512 MB for brokers
  and 384 MB for controllers. Start only the profiles you need.
- Docker clusters created by the Docker provider get their own port base and use the lab node
  image `bvaliev/brokerium-agent:lab`; rebuild it with `make images` after agent changes.
- Strimzi lab: kind cluster `brokerium`, Kafka `lab` with node pools `controllers` (ids 0-9)
  and `brokers` (ids 100+), node ports 32100-32105 on localhost.
- VM lab: Debian containers with systemd and the server container on port 18080.

## Releases

A tag `vX.Y.Z` publishes a release:

- the `release` workflow attaches the binaries of both editions, the Helm chart
  `brokerium-X.Y.Z.tgz`, `SHA256SUMS` and an SBOM to the GitHub release, with the notes of the
  matching section of `CHANGELOG.md`;
- it pushes the chart to `oci://ghcr.io/brokerium/charts` and runs the `helm-repo` workflow,
  which rebuilds the index of https://charts.brokerium.kaufen (GitHub Pages) from the charts of
  all releases;
- the `CI` workflow publishes `bvaliev/brokerium-server`, `bvaliev/brokerium-server-enterprise`
  and `bvaliev/brokerium-agent` as `X.Y.Z`, `X.Y` and `latest`, and the lab node image as
  `bvaliev/brokerium-agent:X.Y.Z-lab`.

The tag sets the chart version and `appVersion`. The `helm` workflow checks every change to the
chart: lint, schema validation of each networking mode and an install on kind.
