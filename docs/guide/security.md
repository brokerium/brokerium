# Security

## Users and roles

Local users with argon2id password hashes and one of three roles:

| Role | Can |
| --- | --- |
| viewer | read everything, including the audit log |
| operator | plus change clusters: topics, configs, ACLs, SCRAM, quotas, groups, messages, operations |
| admin | plus manage users and delete clusters |

Sessions are random tokens stored as SHA-256 hashes, sent as an `HttpOnly`, `SameSite=Lax`
cookie (set `BROKERIUM_SECURE_COOKIES=true` behind HTTPS) or as `Authorization: Bearer`.
A new password or a disabled account ends the user's other sessions.
Every request other than GET must carry the `X-Brokerium-Request` header, which browsers do
not add to cross-site requests. Responses set a strict `Content-Security-Policy`,
`X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`.

After five failed logins for the same user from the same address, Brokerium refuses further
attempts for 30 seconds, doubling up to 15 minutes, and logs every failed login.

SSO, per-cluster roles and audit export are planned for a paid edition
([ADR 0011](../adr/0011-editions.md)).

## Credentials of clusters

Passwords, client keys, AWS keys, OAuth tokens and kubeconfigs are sealed with AES-256-GCM
using the server's secret key (`BROKERIUM_SECRET_KEY` or `<data dir>/secret.key`) and are
never returned by the API. Back up the key together with the database.

## Agents

- An agent enrolls with a one-time join token (`brk1.<secret>.<CA fingerprint>`, valid for one
  hour by default). It checks the server's CA against the fingerprint in the token, so a
  token cannot be replayed against another server.
- The server signs a client certificate for the agent from its own CA; afterwards the agent
  connects with mutual TLS 1.3.
- The agent executes a closed set of typed commands. There is no shell and no arbitrary file
  access. Kafka releases are downloaded from Apache and checked against the published
  SHA-512.
- On hosts the agent runs as root to manage the Kafka unit; Kafka runs as the `kafka` account.
  The join token lives in `/etc/brokerium-agent.env` (mode 0600).
- The install script checks the agent binary against the `SHA256SUMS` of its release.
- Agent certificates are valid for one year; automatic renewal is planned for 0.2.0.

## Reporting vulnerabilities

See [SECURITY.md](../../SECURITY.md).

## Checks in CI

`govulncheck`, `golangci-lint` (including gosec rules), dependency license checks for Go and
the dashboard, an SPDX SBOM per build and per release.

## Security review

The review before 0.1.0, with its fixes and the accepted risks (for example, viewers can read
message payloads and operators choose which addresses the server connects to):
[security-review-0.1.0.md](../research/security-review-0.1.0.md).
