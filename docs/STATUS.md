# Status

Last updated: 2026-10-18

## Current phase

Phase 7: public release v0.1.0 (see [ROADMAP.md](ROADMAP.md)). Phases 0-6 are done; the
release is tagged once CI and the e2e workflow pass on `main`.

## Done

- **Phase 0, foundation:** public repository https://github.com/bvaliev/brokerium (PolyForm
  Shield 1.0.0, CLA with CLA Assistant, policies, ruleset "main protection" requiring the `go`,
  `web` and `cla` checks, admins bypass), GitHub project "Brokerium Roadmap"
  (https://github.com/users/bvaliev/projects/2), agent memory, ADRs, research notes, owner
  playbooks, Docker lab, runbooks validated by hand, code skeleton, CI (golangci-lint with
  gosec, govulncheck, buf lint, dependency licenses for Go and the dashboard, Redocly OpenAPI
  lint, Helm lint, SBOM, multi-arch image job).
- **Phase 1, dashboard:** cluster registry with connection profiles (PLAINTEXT, SASL, TLS,
  mTLS, MSK IAM), overview, cluster pages, health evaluation, quorum read straight from the
  controllers when brokers are down ([research](research/franz-go-controller-access.md)).
- **Phase 2, rebalancing:** operations engine, planner, throttled executor, manual moves,
  one-click balancing, agentless load sampler.
- **Phase 3, node lifecycle:** agent with join tokens and mTLS, Docker provider, create and
  delete clusters from the dashboard, add, drain, decommission and replace brokers, add and
  remove controllers, static-to-dynamic quorum migration, rolling restart, node adoption.
- **Phase 4, data plane:** topics, ACLs, SCRAM users, quotas, consumer and share groups,
  messages, users with roles, audit.
- **Phase 5, upgrades:** preflight checks (Java 17+, KAFKA-20979 for 4.1.0 to 4.3.1), one node
  at a time, explicit finalization; scenario 8 (4.3.1 to 4.4.0) automated.
- **Phase 6, Kubernetes and managed services:** Strimzi provider ([ADR 0012](adr/0012-strimzi-provider.md)),
  node pools in the dashboard, MSK IAM with the default credential chain, capability presets
  for Confluent Cloud and Aiven.
- **Phase 7, release work:** user guide (`docs/guide`), README with screenshots, OpenAPI 3.1,
  Helm chart validated in kind next to Strimzi, server and agent images, agent install script
  validated on systemd hosts, e2e workflow (beta and Docker clusters, on `main` and nightly),
  release workflow (binaries, checksums, SBOM, notes from `CHANGELOG.md`),
  [ADR 0013](adr/0013-distribution.md).
- **Security review** ([research/security-review-0.1.0.md](research/security-review-0.1.0.md)):
  login limits and bounded argon2id checks, equal timing for unknown users, session revocation
  on password changes, generic `/readyz` errors, strict CSP, checksum verification in the agent
  installer. Accepted risks are listed in the review.

## End-to-end coverage

All lab scenarios run as tests in `test/e2e` (build tag `e2e`); mapping in
[runbooks/README.md](runbooks/README.md).

| Test | Needs |
| --- | --- |
| `TestMultiClusterOverview`, `TestDataPlane`, `TestSecureConnections`, `TestUsersRolesAudit` | server, beta (`make lab-certs` first) and one more cluster |
| `TestScaleOutAndBalance`, `TestNodeLifecycle`, `TestMigrateStaticQuorum`, `TestRollingUpgrade` | `BROKERIUM_E2E_HEAVY=1`, the `bvaliev/brokerium-agent:lab` image |
| `TestControllersDirect` | `BROKERIUM_E2E_CONTROLLERS` (controller listeners reachable) |
| `TestStrimzi` | `deploy/lab/strimzi/strimzi.sh up`, `BROKERIUM_E2E_STRIMZI_KUBECONFIG=.data/kind.kubeconfig` |
| `TestAgentVMs` | `deploy/lab/vm/vm.sh up`, `BROKERIUM_E2E_VM=1 BROKERIUM_E2E_URL=http://localhost:18080` |

The e2e workflow in CI covers the first two rows (about 10 minutes on GitHub runners, also
nightly); the Strimzi and VM labs run locally before a release.

## Lab state

- Running: PostgreSQL, beta (registered as external, SASL and SSL listeners, authorizer),
  delta (Docker cluster with the share group `e2e-share`), the host server from `bin/`
  (log in `.data/server.log`). Docker Desktop does not restart PostgreSQL and beta after a
  restart: `docker compose -f deploy/lab/compose.yaml --profile beta up -d --wait postgres beta-c1 beta-b1`.
- Stopped: alpha, gamma, the containers of epsilon (its registration remains).
- Removed: the kind cluster of the Strimzi lab and the `kube-lab` registration, the VM lab,
  the Helm release.

## In progress

- Release v0.1.0: push to `main`, green CI and e2e, tag `v0.1.0` (issue #26).
- Container images in Docker Hub (issue #8): waiting for the `DOCKERHUB_TOKEN` secret.

## Next

1. After the token is set: check `bvaliev/brokerium-server` and `bvaliev/brokerium-agent` for
   both architectures and run the Docker and Helm quick starts against the published images.
2. Automatic renewal of agent certificates (valid for one year; must ship before 2027-10).
3. Publish the Helm chart (OCI on Docker Hub or GitHub Pages).
4. Run the Strimzi lab (kind) in the e2e workflow.
5. Commercial layer from the roadmap: SSO (OIDC), audit export, scheduled rebalancing.

## Owner tasks (manual)

- Create a Docker Hub access token (Read & Write) and store it:
  `gh secret set DOCKERHUB_TOKEN --repo bvaliev/brokerium`.
- Reserve the GitHub organization `brokerium` (free plan) to protect the name.
- Check and buy domains `brokerium.dev` / `brokerium.io`; then create a contact address for
  licensing and corporate CLAs.
- Trademark search (WIPO, EUIPO, USPTO) before the public launch.
- If employed: check the employment contract for IP clauses (see `docs/legal/sale-readiness.md`).

## Blockers

- Image publishing needs `DOCKERHUB_TOKEN`; until then the Docker and Helm quick starts need
  `make images`.
