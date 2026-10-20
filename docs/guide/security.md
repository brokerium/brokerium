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
  access. Kafka releases are downloaded from Apache (or the mirror in the command) and checked
  against the SHA-512 published on `downloads.apache.org`, never against a checksum from the
  server.
- What the agent accepts from the server: Kafka versions in the `x.y.z` form; properties for
  `server.properties` whose names consist of letters, digits, `.`, `_` and `-` and whose values
  contain no control characters; heap options made only of `-Xms<size>` and `-Xmx<size>`. The
  server checks the same rules when cluster settings are saved and before it sends a command,
  so neither an operator nor a server can add lines to `server.properties` or the systemd unit
  or pass other JVM options.
- On hosts the agent runs as root to manage the Kafka unit; Kafka runs as the `kafka` account.
  The join token lives in `/etc/brokerium-agent.env` (mode 0600).
- The install script checks the agent binary against the `SHA256SUMS` of its release.
- Agent certificates and the certificate of the agent gateway are valid for
  `BROKERIUM_AGENT_CERT_LIFETIME` (one year by default). After two thirds of the lifetime the
  agent creates a new key and requests a new certificate over its mutual TLS connection; the
  connection and Kafka keep running, and the next connection uses the new certificate. The
  certificate the renewal was requested with stays valid until the agent connects with the new
  one, so a lost response does not lock the agent out. The server replaces its own gateway
  certificate the same way.
- The **Nodes** page shows when each agent certificate expires. A certificate that was not
  renewed in time (within 30 days of its expiry, or a sixth of a short lifetime) is a warning
  in the health of its cluster, an expired one is critical. Agents older than 0.2.0 do not
  renew: upgrade them before their certificates expire.
- An agent with an expired certificate cannot connect. Run the install command with a new
  join token on the host: the agent enrolls again under the same node name and Kafka is not
  touched.

### Agents behind a TLS-terminating proxy

A proxy that terminates TLS breaks the CA check of the join token and mutual TLS. With
`BROKERIUM_AGENT_TRANSPORT=insecure` the agent trusts the connection for nothing and the
messages carry the authenticity instead ([ADR 0017](../adr/0017-agent-transport-behind-proxies.md),
[setup](install.md#agents-behind-a-tls-terminating-proxy)):

- Enrollment: the agent proves the join token with an HMAC keyed by the hash the server stores
  for the token, so the token secret never leaves the host. The proof covers a new X25519 key of
  the agent; the server answers with a random 32-byte node secret encrypted to that key
  (X25519, HKDF-SHA256, AES-256-GCM) and signed with its CA key, and the agent checks the CA
  against the fingerprint in the token. A proof that does not match leaves the token valid; a
  used token cannot be redeemed again.
- The server keeps the node secret sealed with its secret key, like cluster credentials; the
  agent keeps it in `/var/lib/brokerium-agent/node.secret` (mode 0600).
- Sessions: agent and server each contribute a random nonce, and the agent's hello must be less
  than five minutes old. Every command carries a sequence number and an ECDSA P-256 signature of
  the CA over the node, both nonces, the number and the command; every message of the agent
  carries an HMAC with the node secret over the same fields. The agent executes only commands
  that verify, the server uses results and status only after they verify, and any failure ends
  the session and is logged. Messages of earlier sessions or of other nodes are refused, and
  reordered or dropped messages are detected.
- The transport gives no confidentiality: the proxy and every segment without TLS read commands,
  results and status. Keep TLS up to the proxy (`BROKERIUM_AGENT_TLS=system` verifies its
  certificate) and the plaintext listener on a network you trust. Anyone on the path can still
  block connections, or use up a join token by forwarding its request first.
- Nodes of the insecure transport have no certificate, and their secret neither expires nor
  rotates. Enrolling the node again with a new join token replaces it; enrolling with the other
  transport revokes the credentials of the old one.

## Reporting vulnerabilities

See [SECURITY.md](../../SECURITY.md).

## Checks in CI

`govulncheck`, `golangci-lint` (including gosec rules), dependency license checks for Go and
the dashboard, an SPDX SBOM per build and per release.

## Security review

The review before 0.1.0, with its fixes and the accepted risks (for example, viewers can read
message payloads and operators choose which addresses the server connects to):
[security-review-0.1.0.md](../security-review-0.1.0.md).
