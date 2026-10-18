# ADR 0013: Distribution and installation

- Status: accepted
- Date: 2026-10-15

## Context

Brokerium is installed by operators of Kafka clusters on Kubernetes, on virtual machines and on
laptops for evaluation. The agent has to reach hosts that may have no container runtime, and
releases must be verifiable.

## Decision

- **Releases** are created from `v*` tags by `.github/workflows/release.yml`: static binaries
  of `brokerium-server` (Linux and macOS, amd64 and arm64, dashboard embedded) and
  `brokerium-agent` (Linux amd64 and arm64), `SHA256SUMS`, an SPDX SBOM and the notes of the
  matching `CHANGELOG.md` section.
- **Images** `bvaliev/brokerium-server` and `bvaliev/brokerium-agent` are built for
  `linux/amd64` and `linux/arm64` by the CI workflow: `edge` and `sha-<commit>` from `main`,
  `<major>.<minor>.<patch>`, `<major>.<minor>` and `latest` from release tags. They run as
  UID 65532 on distroless bases.
- **Kubernetes:** the Helm chart lives in `deploy/helm/brokerium`. It runs one replica with the
  `Recreate` strategy (one operations engine), keeps the generated sealing key across upgrades
  and can bundle a single PostgreSQL for evaluations.
- **Virtual machines:** the server serves `install.sh` at `/api/v1/agent/install.sh`. The
  script downloads the agent binary of a release (or `BROKERIUM_AGENT_URL` for air-gapped
  sites), creates the `kafka` account and installs `brokerium-agent.service`. Kafka itself runs
  as `brokerium-kafka.service` under the `kafka` account, so restarting or upgrading the agent
  never stops Kafka.
- **End-to-end tests** run in GitHub Actions on every push to `main` and nightly against beta and
  Docker clusters created by the Docker provider. Kubernetes and VM scenarios need local labs
  (`deploy/lab/strimzi`, `deploy/lab/vm`) and run before releases.

## Consequences

- Image publishing depends on the `DOCKERHUB_TOKEN` repository secret (a Docker Hub access
  token with Read & Write) and the `DOCKERHUB_USERNAME` variable; without the token, CI skips
  the images job.
- Release tags are not marked as pre-releases: the installer downloads from
  `releases/latest`, which GitHub resolves only to full releases.
- The Helm chart is not yet published to a chart repository; it installs from a checkout.
