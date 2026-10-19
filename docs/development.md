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
| `internal/webui` | Embedded dashboard build output |
| `web/` | React + TypeScript dashboard (Vite, Mantine, TanStack Query) |
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
| `make test` | Unit tests (Go and web) |
| `make lint` | golangci-lint and web lint |
| `make proto` | Regenerate the agent protocol code |
| `make lab-certs` | Throwaway CA and certificates for the SASL and SSL listeners of beta |
| `make images` | Build the server, agent and lab node images locally |
| `make e2e` | End-to-end scenarios against a running lab and server |
| `make screenshots` | Recreate cluster alpha with demo data and capture the dashboard into `docs/images` in both themes |
| `deploy/lab/strimzi/strimzi.sh up\|down` | kind cluster with Strimzi and Kafka "lab"; kubeconfig `.data/kind.kubeconfig` |
| `deploy/lab/vm/vm.sh up\|down` | Containerized server on port 18080 and systemd hosts for the agent installer |

## End-to-end tests

`make e2e` runs the scenarios in `test/e2e` against a running lab and server. Variables:

- `BROKERIUM_E2E_HEAVY=1` enables scenarios that create Docker clusters.
- `BROKERIUM_E2E_STRIMZI_KUBECONFIG` points to the kubeconfig of the Strimzi lab.
- `BROKERIUM_E2E_VM=1` with `BROKERIUM_E2E_URL` and `BROKERIUM_E2E_CONTROLLERS` runs the
  agent installer scenarios against the VM lab.
- `BROKERIUM_E2E_KEEP=1` keeps the clusters a test created.

The Strimzi lab uses its own kubeconfig in `.data/` and never touches `~/.kube/config`.

In CI, the `e2e` workflow runs the scenarios against the beta lab and Docker clusters on every
pull request. The `e2e-strimzi` workflow runs `TestStrimzi` on a kind cluster nightly, on demand
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
