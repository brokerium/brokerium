# ADR 0020: CI runs the jobs a change needs

- Status: accepted
- Date: 2026-10-20

## Context

On 2026-10-20 GitHub Actions ran 120 workflow runs in five hours. The `e2e` workflow ran 15 times
on pull requests (304 minutes, 20 on average) and 14 more times on pushes to `main` (136 minutes,
7 of them cancelled), each repeating the run of the pull request on the same tree. Every push to
`main` built and pushed four multi-architecture images with the tags `edge` and `sha-<commit>`.
Pull requests that changed only the dashboard or the Helm chart ran the whole lab, the rolling
upgrade scenario took up to 30 minutes when the download of a Kafka release from Apache stalled,
and the Enterprise features had no end-to-end scenario.

## Decision

- The `e2e` workflow runs on pull requests, nightly and on demand, not on pushes to `main`. A
  first job lists the files a pull request changes. The Community jobs skip changes that the
  scenarios do not exercise: documentation and Markdown, the dashboard (it has its own checks),
  Enterprise code, the Helm chart, the Docker Hub texts, the OpenAPI description, other
  workflows, issue and pull request templates and license files. The Enterprise job runs for
  changes of Go code, the scenarios, their build and the workflow. Scheduled and manual runs run
  every job.
- The scenarios run in three parallel jobs, each with only the lab it needs: `api` (beta and the
  small Docker cluster `ci-small` with agent certificates that live three minutes, then agents
  behind OpenResty), `docker-clusters` (the scenarios that create Docker clusters) and
  `enterprise` (beta and a server built with `-tags enterprise,licensetest`, so that the
  scenarios can sign license keys with the test key). Each Community scenario belongs to exactly
  one job, which the `api` job checks. The job `e2e`, the required check, passes when every other
  job succeeded or was skipped and fails when one failed or was cancelled.
- The agents of the Docker clusters download Kafka releases from a mirror on the runner
  (`BROKERIUM_KAFKA_MIRROR`): OpenResty serves archives that the workflow caches after checking
  their SHA-512 against Apache. The agents check the checksums from Apache themselves as well.
- The `CI` workflow publishes images for version tags, and the images of `main` as `edge` and the
  lab node image as `lab` nightly and when it is run by hand on `main`. It no longer publishes
  `sha-<commit>` tags. The SBOM of `main` is built on pushes to `main`, nightly and on manual
  runs; releases attach their own.
- The `go` and `web` jobs, the path filters of the `helm` and `e2e-strimzi` workflows, the CLA
  check and the Dependabot settings stay as they are.

## Consequences

- A change is tested end to end by its pull request. The ruleset does not require branches to be
  up to date before merging, so two pull requests that pass on their own are first tested
  together by the nightly run.
- `edge` and `lab` lag behind `main` by up to a day. Running the `CI` workflow on `main` by hand
  publishes them at once, for example before the `helm` workflow installs a chart change with
  `edge`. Images of single commits are gone; the `sha-<commit>` tags published so far stay.
- A new scenario has to be added to a job of `e2e.yml`, and a scenario that installs another
  Kafka release has to add it to the releases of the mirror, or its agents download from Apache.
- Changes of Go code exercise the license check and the message tools of Brokerium Enterprise end
  to end; release builds still refuse the `licensetest` tag.
