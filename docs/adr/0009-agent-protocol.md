# ADR 0009: Agent protocol over gRPC with mTLS

- Status: accepted
- Date: 2026-10-15

## Context

Agents run on customer hosts, often behind NAT or firewalls. Security teams will review what
the agent can do on a Kafka host.

## Decision

- The agent always connects **out** to the server; hosts need no inbound ports.
- Enrollment: the agent calls `Enroll` over TLS with a single-use join token and a CSR; the
  server's internal CA signs a client certificate bound to the node id.
- Operation: the agent opens a bidirectional `Connect` stream with mTLS. The server sends typed
  commands; the agent answers with results and sends periodic status reports.
- The command set is closed: install a Kafka version (SHA-512 verified), write configuration,
  format storage, start, stop, restart, read node identity (`meta.properties`), collect status.
  There is no command for running arbitrary shell.
- The protocol is defined in `proto/brokerium/agent/v1/agent.proto`; generated code is committed.

## Consequences

- The server needs a reachable gRPC port and a CA; certificates rotate by re-enrolling.
- New capabilities on hosts require a new agent version, which is intentional.
