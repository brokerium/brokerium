# ADR 0003: Go, React and PostgreSQL

- Status: accepted
- Date: 2026-10-15

## Context

The control plane talks the Kafka protocol to many clusters, runs long operations and serves
a dashboard. The agent runs on customer hosts next to Kafka and must be small, static and easy
to install. The team is small, so one backend language is preferred.

## Decision

- **Go 1.27** for both `brokerium-server` and `brokerium-agent`: static binaries, small images,
  strong concurrency, mature Kafka client (franz-go, see ADR 0005).
- **React + TypeScript + Vite** for the dashboard, built into `internal/webui/dist` and embedded
  into the server binary with `go:embed`. UI kit: Mantine; data fetching: TanStack Query.
- **PostgreSQL 18** as the only state store (clusters, credentials, operations, audit, users),
  accessed with pgx; schema migrations are embedded SQL files applied at startup.
- **REST + JSON** for the dashboard and the public API, described in `api/openapi.yaml`.
- **gRPC with mTLS** between agents and the server (see ADR 0009).
- Toolchain versions are pinned in `.mise.toml`; `CGO_ENABLED=0` everywhere.

## Consequences

- No JVM in Brokerium itself; Java is only needed inside Kafka nodes.
- Kafka features that exist only in the Java AdminClient must be implemented with raw protocol
  requests (`kmsg`).
