# Status

Last updated: 2026-10-15

## Current phase

Phase 0: Foundation (see [ROADMAP.md](ROADMAP.md)).

## Done

- Toolchain pinned with mise (`.mise.toml`): Go 1.27.2, Node 24.22.0, pnpm 12.11.2,
  golangci-lint 2.14.0, buf 1.73.0, protoc-gen-go 1.36.12, protoc-gen-go-grpc 1.6.2.
- Docker Desktop resources raised to 6 CPUs and 16 GB.
- Public repository https://github.com/bvaliev/brokerium with PolyForm Shield 1.0.0, CLA, CCLA,
  policies, issue and PR templates, CODEOWNERS, CLA Assistant workflow (signatures on the
  `cla-signatures` branch), Dependabot.
- Repository settings: Discussions on, squash merges only, branch deletion on merge, private
  vulnerability reporting, secret scanning with push protection, Dependabot alerts and security
  updates, approval required for Actions from all external contributors, ruleset "main protection"
  (PR with code-owner review, no force push, no deletion; repository admins bypass).
- Repository variable `DOCKERHUB_USERNAME=bvaliev`.
- Agent memory: AGENTS.md, `.cursor/rules`, ADR 0001-0010, roadmap.

## In progress

- Legal playbooks in `docs/legal/` and research notes in `docs/research/`.

## Next

1. GitHub project "Brokerium Roadmap" with labels, milestones and issues.
2. Docker lab (alpha, beta, gamma) and manual validation of the scenarios.
3. Code skeleton, CI and container images.

## Owner tasks (manual)

- Create a Docker Hub access token (Read & Write) and store it:
  `gh secret set DOCKERHUB_TOKEN --repo bvaliev/brokerium`.
- Reserve the GitHub organization `brokerium` (free plan) to protect the name.
- Check and buy domains `brokerium.dev` / `brokerium.io`; then create a contact address for
  licensing and corporate CLAs.
- Trademark search (WIPO, EUIPO, USPTO) before the public launch.

## Blockers

None.
