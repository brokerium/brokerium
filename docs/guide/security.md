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
Every request other than GET must carry the `X-Brokerium-Request` header, which browsers do
not add to cross-site requests. Responses set `X-Frame-Options: DENY`,
`X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`.

SSO, per-cluster roles and audit export are planned for a paid edition
(see the [roadmap](../ROADMAP.md)).

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

## Reporting vulnerabilities

See [SECURITY.md](../../SECURITY.md).

## Checks in CI

`govulncheck`, `golangci-lint` (including gosec rules), dependency license checks for Go and
the dashboard, an SPDX SBOM per build and per release.
