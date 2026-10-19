# ADR 0014: Agent certificate renewal

- Status: accepted
- Date: 2026-10-19

## Context

Agent certificates and the certificate of the agent gateway were valid for one year, and the
only way to replace an agent certificate was to enroll the node again with a new join token
(ADR 0009). Without renewal, every agent stops connecting a year after its enrollment, and a
long-running server serves an expired gateway certificate.

## Decision

- `RenewCertificate(csr)` is a unary call over the mutual TLS connection of the agent. The
  server signs a certificate with the node id as common name, like at enrollment.
- The agent calls it after two thirds of the validity of its certificate, computed from the
  certificate itself, so reconnects do not move the schedule. It creates a new key for every
  renewal and replaces key and certificate in one atomic file write (`agent.pem`). The running
  connection and Kafka are not touched; the next connection uses the new certificate.
- The server stores the fingerprint of the new certificate as current and keeps the fingerprint
  of the certificate the renewal was requested with as previous. Both authenticate until the
  agent connects with the current one. A lost response or a failed write on the agent therefore
  never locks the agent out; it simply renews again.
- The server records the expiry of the current certificate (`nodes.cert_not_after`), shows it on
  the Nodes page and reports certificates that were not renewed in time as health issues of
  their cluster.
- One setting, `BROKERIUM_AGENT_CERT_LIFETIME`, sets the lifetime of agent certificates and of
  the gateway certificate. The gateway issues its certificate through `GetCertificate` and
  replaces it after two thirds of its lifetime. NotBefore lies up to an hour in the past (a
  tenth of short lifetimes) for clock skew.
- An agent with an expired certificate that is given a new join token enrolls again under the
  same node name.

## Consequences

- Agents renew without operator action; end-to-end tests run with a three-minute lifetime, so
  every scenario also exercises renewal.
- Agents older than 0.2.0 do not renew and have to be upgraded within a year of enrollment.
- The previous certificate stays valid on a long-lived connection until that connection ends;
  stealing it gains nothing beyond what the connection already allows.
