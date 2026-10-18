# Security review for v0.1.0

Review date: 2026-10-18

Scope: `brokerium-server` (REST API, dashboard, agent gateway), `brokerium-agent`, the agent
install script, container images and the Helm chart. Method: review of the attack surfaces in
the code, `golangci-lint` with gosec, `govulncheck`, `pnpm audit`, and checks against a running
server in the lab.

## Fixed during the review

| Finding | Risk | Fix |
| --- | --- | --- |
| Login attempts were not limited. Checking the password of an existing user (for example `admin`) allocates 64 MiB for argon2id, so parallel requests could exhaust the server's memory. | Password guessing, denial of service | After 5 failed logins for the same user from the same address, logins are refused for 30 s, doubling up to 15 min (HTTP 429 with `Retry-After`). At most 4 password checks run at once. Failed logins are logged. |
| Unknown usernames were rejected without a password check, so response times revealed which accounts exist. | Account enumeration | Unknown users are checked against a dummy hash; both cases take the same time (0.41 s in the lab). |
| A password reset did not end the user's existing sessions. | A stolen session survives a password reset | A new password or a disabled account deletes the user's other sessions; the session making the change stays. |
| `/readyz` returned the database error text to unauthenticated callers. | Disclosure of database host and user | Generic message; details go to the server log. |
| No Content-Security-Policy. | Larger impact of a future XSS bug | Scripts only from the server, no plugins, no framing (`frame-ancestors 'none'`). The dashboard was checked for violations after sign-in, on the overview and on cluster pages. |
| The install script did not verify the downloaded agent binary. | Tampered binary from a compromised mirror or download | Binaries from GitHub releases are checked against the release's `SHA256SUMS`; custom URLs can pass `BROKERIUM_AGENT_SHA256`. |
| gosec findings: unchecked integer conversions of ids and ports, the CA pinning callback of the agent. | Wrong node ids, weaker TLS verification code path | Fixed earlier in the release (commit 0b787d9b); gosec now runs in CI. |

## Reviewed without changes

- **CSRF:** every state-changing request needs the `X-Brokerium-Request` header; the session
  cookie is `HttpOnly` and `SameSite=Lax`.
- **Sessions:** random tokens stored as SHA-256 hashes, 12-hour lifetime, disabled users are
  rejected on every request.
- **Authorization:** roles are enforced per route on the server; the viewer role cannot change
  anything.
- **Join tokens:** stored hashed, single use, valid for one hour, and bound to the server CA
  fingerprint, so a token cannot be replayed against another server.
- **Agent identity:** taken from the verified client certificate; every connection looks the
  node up, so deleting a node revokes its access at once.
- **Agent commands:** a closed set of typed commands, no shell.
- **Stored credentials:** sealed with AES-256-GCM and never returned by the API; kubeconfigs with
  exec plugins are rejected.
- **HTTP server:** JSON bodies are limited to 4 MiB, `ReadHeaderTimeout` is set.
- **Images and chart:** distroless images running as UID 65532; the chart sets
  `runAsNonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation: false`, drops all
  capabilities and uses the `RuntimeDefault` seccomp profile.
- **Dependencies:** `govulncheck` reports no vulnerabilities in code that Brokerium calls;
  `pnpm audit` reports none for the dashboard.

## Accepted risks and limitations of 0.1.0

- **Viewers read message payloads:** the viewer role reads everything, including messages.
  Give it only to people who may see the data.
- **Operators choose network destinations:** testing a connection, registering a cluster or
  adding a kubeconfig makes the server connect to the given address. Restrict the server's
  egress with network policies where this matters.
- **Agent certificates are valid for one year** and are not renewed automatically yet.
  Renewal is planned for 0.2.0, well before the first certificates expire.
- **The join token is visible in the process list** of the host while `install.sh` runs; it is
  single use and expires after an hour.
- **The Docker provider is for labs:** a server with access to a Docker socket lets operators
  start lab node containers. Do not give the server a Docker socket in production.
- **HTTPS is terminated in front of the server** (ingress or proxy); set
  `BROKERIUM_SECURE_COOKIES=true` there. The agent port 8443 should be reachable only by agents.
- **No SSO and no per-cluster roles** in the free edition ([ADR 0011](../adr/0011-editions.md)).
