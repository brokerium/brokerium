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

- The server needs a reachable gRPC port and a CA. Agents renew their certificates over the
  mutual TLS connection (ADR 0014).
- A proxy that terminates TLS breaks both the CA pinning and mutual TLS. Agents behind such a
  proxy use the optional insecure transport, which signs the messages instead (ADR 0017).
- New capabilities on hosts require a new agent version, which is intentional.
- On hosts the agent runs without root, as the `brokerium-agent` account in the `kafka` group.
  The install script writes a static Kafka unit and a sudo rule that allows only starting,
  stopping and restarting it; the agent switches releases through the link
  `/opt/brokerium/kafka/current`, passes heap options through an environment file of the unit
  and formats and wipes storage through the group. Commands that would need more, such as
  changing the unit, need a new installer, not just a new agent. Agents that run as root on
  hosts installed earlier still write the unit themselves; running the install command again
  converts such a host without restarting Kafka. In lab containers Kafka stays a child process
  of the agent.
