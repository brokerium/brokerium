# AGENTS.md

Brokerium ("Brokerium for Apache Kafka") is a source-available control plane and web dashboard
for Apache Kafka KRaft clusters: multi-cluster view, broker and controller lifecycle, rebalancing,
rolling upgrades, and data-plane management. Owner and sole maintainer: Bulat Valiev (`bvaliev`).

## Working protocol

1. At the start of every session read this file and [docs/STATUS.md](docs/STATUS.md).
2. Pick the task from the GitHub project "Brokerium Roadmap" or from STATUS.md "Next".
3. At the end of a session update [docs/STATUS.md](docs/STATUS.md) (done, in progress, next,
   blockers), add or amend an ADR in [docs/adr/](docs/adr/) when a decision changes, and
   update the related GitHub issue.
4. Talk to the owner in Russian. Everything in the repository is written in English, except
   the owner-facing playbooks in [docs/legal/](docs/legal/), which are in Russian.
5. Research results go to [docs/research/](docs/research/) with source links, so they are not
   repeated.

## Repository map

| Path | What lives there |
| --- | --- |
| `cmd/brokerium-server` | Server entrypoint (API, dashboard, operations engine) |
| `cmd/brokerium-agent` | Agent entrypoint (runs on Kafka hosts or as lab node PID 1) |
| `cmd/brokerium-loadgen` | Load generator for the lab |
| `internal/kafka` | franz-go client factory, KRaft wrappers over `kmsg`, cluster snapshots |
| `internal/cluster` | Cluster registry, connection profiles, capabilities, health evaluation |
| `internal/metrics` | Agentless sampler: log dir sizes and offsets to rates |
| `internal/balancer` | Pure rebalancing planner (no I/O, deterministic) |
| `internal/ops` | Operations engine and operation types (rebalance, drain, add broker, ...) |
| `internal/agentgw` | gRPC gateway for agents, internal CA, join tokens |
| `internal/agent` | Agent runtime: Kafka installer, config writer, storage format, process manager |
| `internal/provider` | Node providers: `docker` (lab), `strimzi` (Kubernetes) |
| `internal/api` | HTTP handlers, auth middleware, JSON contracts |
| `internal/auth`, `internal/audit` | Users, sessions, roles; audit log |
| `internal/store` | PostgreSQL access and embedded migrations |
| `internal/webui` | Embedded dashboard build output |
| `web/` | React + TypeScript dashboard (Vite, Mantine, TanStack Query) |
| `api/openapi.yaml` | Public REST API description |
| `proto/brokerium/agent/v1` | Agent protocol (generated Go code committed) |
| `deploy/lab` | Docker Compose lab with Kafka clusters alpha, beta, gamma |
| `deploy/docker`, `deploy/helm`, `deploy/install` | Images, Helm chart, install scripts |
| `docs/adr`, `docs/research`, `docs/runbooks`, `docs/legal` | Decisions, research, procedures, legal playbooks |
| `test/e2e` | End-to-end scenarios against the lab (build tag `e2e`) |

## Commands

The toolchain is pinned in `.mise.toml` (Go 1.27.2, Node 24.22.0, pnpm 12.11.2,
golangci-lint 2.14.0, buf 1.73.0). Prefix commands with `mise exec --` or activate mise.

| Command | Purpose |
| --- | --- |
| `make lab-up` | Start PostgreSQL and Kafka cluster alpha (3 controllers + 3 brokers, dynamic quorum, Kafka 4.3.1) |
| `make lab-up PROFILES="alpha beta gamma"` | Start more lab clusters |
| `make lab-down` / `make lab-reset` | Stop the lab / stop and delete all lab volumes |
| `make dev` | Run the server on the host against the lab (http://localhost:8080) |
| `make web-dev` | Vite dev server for the dashboard with API proxy |
| `make build` | Build the dashboard and both binaries into `bin/` |
| `make test` | Unit tests (Go and web) |
| `make lint` | golangci-lint and web lint |
| `make proto` | Regenerate the agent protocol code |
| `make e2e` | End-to-end scenarios against a running lab |

## Conventions

- Every source file starts with `// SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0`
  (or the comment syntax of its language).
- Go: standard library HTTP, `log/slog`, `context` everywhere, wrapped errors, table-driven tests,
  `CGO_ENABLED=0`. The global environment of the owner's Mac has `GO111MODULE=off`; mise sets it
  to `on` for this project.
- Dashboard: strict TypeScript, Mantine components, TanStack Query for server state, API types in
  `web/src/api`.
- Commits: imperative mood, personal identity
  `Bulat Valiev <39081197+bvaliev@users.noreply.github.com>` (configured in `.git/config`), never
  an employer email.
- The owner pushes to `main` through the ruleset bypass; external changes come as pull requests
  with a signed CLA.

## Kafka safety rules (summary)

Full text: [docs/adr/0008-operations-engine.md](docs/adr/0008-operations-engine.md).

- Plan and preview, explicit confirmation, step-by-step execution, audit.
- Health gate before every step: no offline partitions, no under-replicated partitions except
  those being moved, nothing under `min.insync.replicas`, quorum leader present and voters caught up.
- One operation per cluster. One quorum change at a time; add the new controller before removing
  the old one.
- Throttle every reassignment and always remove throttles.
- Irreversible actions require typing the cluster name. Every operation has a dry run.

## Lab facts

- Kafka image `apache/kafka:4.3.1` (arm64 and amd64). The image formats storage only for static
  quorums, so alpha and beta nodes start through `deploy/lab/kafka/start.sh`.
- Node ids: controllers `1..5`, brokers `101..106`. Host listener of broker `N` is
  `localhost:<cluster base port + N>` (alpha 19000, beta 29000, gamma 39000).
- Docker Desktop is set to 6 CPUs and 16 GB; JVM heaps are 512 MB for brokers and 384 MB for
  controllers. Start only the profiles you need.
