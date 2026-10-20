# Contributing to Brokerium

Thank you for helping make Brokerium better. The project is developed by a single
maintainer, Bulat Valiev, who reviews every change and decides what is merged.
The fastest way to influence Brokerium is to tell us what you need.

## Ask for what you want

- **Bug reports:** open an [issue](https://github.com/brokerium/brokerium/issues/new/choose)
  using the bug template. Include the Brokerium version, the Kafka version, the
  cluster type (VM, Kubernetes, managed) and steps to reproduce.
- **Feature requests and ideas:** open an issue with the feature template or start a
  [discussion](https://github.com/brokerium/brokerium/discussions). Describe the
  problem you are solving; the maintainer may implement it in a different way.
- **Security issues:** never open a public issue. Follow [SECURITY.md](SECURITY.md).

## Pull requests

Pull requests are welcome, with a few rules:

1. **Sign the CLA.** The CLA Assistant bot asks you to sign the
   [Contributor License Agreement](CLA.md) on your first pull request. Pull requests
   without a signed CLA are not reviewed. Companies sign the [Corporate CLA](CCLA.md).
2. **Discuss large changes first.** Open an issue before investing in a big change.
3. **Keep pull requests small and focused**, with tests for new behavior.
4. **Only original work.** Do not paste code from Stack Overflow, blogs or other
   projects unless its license is compatible and you name the source. If you used AI
   tools, review the result and mention it in the pull request description.
5. **The maintainer decides.** A pull request may be declined, or the maintainer may
   implement the idea independently instead of merging it. Only the maintainer merges.
6. **No changes to Enterprise code.** Pull requests that change `ee/` or `web/src/ee/` are
   not accepted; that code is under the [Brokerium Commercial License](ee/LICENSE).
   Issues about it are welcome.

Every change to `main`, including the maintainer's own, goes through a pull request that
passes the required checks (`go`, `web`, `cla`, `e2e`) and is merged as a single squashed
commit.

GitHub Actions do not run on pull requests from new contributors until the
maintainer approves them.

## Development setup

The toolchain is pinned in [.mise.toml](.mise.toml).

```bash
brew install mise          # or see https://mise.jdx.dev
mise install               # Go, Node.js, pnpm, golangci-lint, buf
make lab-up                # local Kafka clusters in Docker
make dev                   # Brokerium server and dashboard in development mode
make test                  # unit tests
```

See the [development guide](docs/development.md) for the repository layout, all commands and
the lab.

## Conventions

- Go code is formatted with `gofmt` and checked with `golangci-lint`.
- The dashboard is TypeScript with strict mode; run `pnpm lint` and `pnpm test` in `web/`.
- Every source file starts with the SPDX header
  `SPDX-License-Identifier: LicenseRef-PolyForm-Shield-1.0.0`.
- Commit messages use the imperative mood: "Add broker drain operation".
- Changes that affect Kafka operations must respect the safety rules in
  [docs/adr/0008-operations-engine.md](docs/adr/0008-operations-engine.md).
