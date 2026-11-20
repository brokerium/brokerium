# ADR 0022: Third-party notices in every artifact

- Status: accepted
- Date: 2026-11-20

## Context

MIT, BSD and Apache-2.0 require their copyright and permission notices in every copy, binaries
included, and Apache-2.0 also requires the attribution notices of a dependency's NOTICE file. Up
to 0.3.1 the images carried only the `LICENSE` and `NOTICE` of Brokerium, and the release binaries
came without any notices. `NOTICE` referred to the SBOM, which names no license for most packages
and contains no license texts. The NOTICE files of gRPC, the AWS SDK and other Apache-2.0
dependencies were not reproduced anywhere.

## Decision

- `make notices` runs `internal/tools/notices`, which writes `THIRD_PARTY_NOTICES` at the
  repository root. It lists the Go standard library, every module that `go list -deps` finds in
  the released builds (the Community and Enterprise server for Linux and macOS, the agent for
  Linux) and every production npm package of the dashboard (`pnpm licenses list --prod`), each
  with its version, its license and the full text of its license and notice files. Go licenses
  are identified with `github.com/google/licensecheck`, npm licenses come from `package.json`. A
  package without a license file gets the standard text of the license it declares.
- The generator also rewrites the section "Notices of third-party components" at the end of
  `NOTICE`, which reproduces the notice files of the components; the text above it is edited by
  hand.
- Both files are committed. The server embeds `LICENSE`, `NOTICE` and `THIRD_PARTY_NOTICES`, and
  `ee/LICENSE` in Enterprise builds, and serves them to signed-in users at `GET /api/v1/notices`
  for the Licenses page of the dashboard. The images copy them to `/licenses`; the lab node image
  also keeps the `LICENSE`, `NOTICE` and `licenses/` of Apache Kafka in `/opt/kafka`. Releases
  attach them and list them in `SHA256SUMS`.
- The `go` job regenerates both files and fails a pull request whose files are out of date.
  Pull requests of Dependabot only get a warning: Dependabot cannot run `make notices`, and a
  commit that a workflow pushes with its `GITHUB_TOKEN` does not start the required checks again,
  so their minor and patch updates would never merge on their own. The images job (through an
  artifact of the `go` job) and the release workflow build with freshly generated files, so
  published artifacts always carry current notices.

## Consequences

- A pull request that changes dependencies runs `make notices`.
- After a Dependabot update `main` can carry outdated files until the next pull request, which
  fails until it regenerates them; published images and releases are not affected.
- A dependency without a license file whose license has no standard text in
  `internal/tools/notices/licenses` stops the generator until the text is added.
