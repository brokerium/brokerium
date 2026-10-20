# Security

## Users and roles

Local users with argon2id password hashes and one of three roles:

| Role | Can |
| --- | --- |
| viewer | read everything, including the audit log |
| operator | plus change clusters: topics, configs, ACLs, SCRAM, quotas, groups, messages, operations |
| admin | plus manage users, the license key and delete clusters |

Sessions are random tokens stored as SHA-256 hashes, sent as an `HttpOnly`, `SameSite=Lax`
cookie (set `BROKERIUM_SECURE_COOKIES=true` behind HTTPS) or as `Authorization: Bearer`.
A new password or a disabled account ends the user's other sessions.
Every request other than GET must carry the `X-Brokerium-Request` header, which browsers do
not add to cross-site requests. Responses set a strict `Content-Security-Policy`,
`X-Frame-Options: DENY`, `X-Content-Type-Options: nosniff` and `Referrer-Policy: same-origin`.

After five failed logins for the same user from the same address, Brokerium refuses further
attempts for 30 seconds, doubling up to 15 minutes, and logs every failed login.

SSO, per-cluster roles and audit export are planned for
[Brokerium Enterprise](enterprise.md) (see the [roadmap](../ROADMAP.md)).

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
  so neither an operator nor a server can add lines to `server.properties`, the systemd unit or
  its environment file or pass other JVM options.
- On hosts the agent runs as the `brokerium-agent` account and Kafka as `kafka`; neither runs
  as root. The agent reaches Kafka in three ways:
  - sudo, with a rule in `/etc/sudoers.d/brokerium-agent` that allows `systemctl start`, `stop`
    and `restart` of `brokerium-kafka.service` and nothing else, not even `daemon-reload`;
  - the files it owns: its state with the client key (mode 0600) in `/var/lib/brokerium-agent`,
    `server.properties`, the Kafka releases in `/opt/brokerium/kafka` and the link `current`
    to the active one, and `kafka.env` with the heap options, which systemd reads and which
    pass the same checks as the configuration (only `-Xms` and `-Xmx`);
  - the `kafka` group: the data directory is `kafka:kafka` with mode 2770 and Kafka runs with
    `UMask=0007`, so the agent formats and wipes storage without root.

  The units and the sudo rule belong to root and are written only by the install script, so
  the agent cannot change which account Kafka runs as or what else systemd starts. It can act
  as the `kafka` account, since it installs the releases Kafka runs, but it cannot gain root.
  The agent unit uses `ProtectSystem=full`, `ProtectHome=yes` and `PrivateTmp=yes` and keeps
  `NoNewPrivileges=no`, which sudo needs.
- Hosts installed by earlier versions run the agent as root until the install command runs on
  them again (see [Installation](install.md#hosts-with-an-agent-running-as-root)). An agent
  running as root writes the Kafka unit itself; in lab containers the agent runs Kafka as its
  child process.
- The join token lives in `/etc/brokerium-agent.env` (mode 0600), which only systemd reads.
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

## Docker clusters

A server with the Docker socket controls the Docker host, and operators can start containers of
the lab node image there by creating Docker clusters. With the default
`BROKERIUM_DOCKER_PROVIDER=auto` the server offers Docker clusters only in the
[Docker installation](install.md#docker-clusters) and ignores a socket it finds in Kubernetes or
on a host; the Helm chart turns them off. Give the server the socket only for evaluations and
labs ([ADR 0019](../adr/0019-docker-clusters-for-docker-installations.md)).

## Reporting vulnerabilities

See [SECURITY.md](../../SECURITY.md).

## Checks in CI

`govulncheck`, `golangci-lint` (including gosec rules), dependency license checks for Go and
the dashboard, an SPDX SBOM per build and per release.

## Security review

The review before 0.1.0, with its fixes and the accepted risks (for example, viewers can read
message payloads and operators choose which addresses the server connects to):
[security-review-0.1.0.md](../security-review-0.1.0.md).
