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

## Reporting vulnerabilities

See [SECURITY.md](../../SECURITY.md).

## Checks in CI

`govulncheck`, `golangci-lint` (including gosec rules), dependency license checks for Go and
the dashboard, an SPDX SBOM per build and per release.

## Security review

The review before 0.1.0, with its fixes and the accepted risks (for example, viewers can read
message payloads and operators choose which addresses the server connects to):
[security-review-0.1.0.md](../security-review-0.1.0.md).
