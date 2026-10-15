# Status

Last updated: 2026-10-15

## Current phase

Phase 0: Foundation (see [ROADMAP.md](ROADMAP.md)); next item is the code skeleton and CI.

## Done

- Toolchain pinned with mise (`.mise.toml`): Go 1.27.2, Node 24.22.0, pnpm 12.11.2,
  golangci-lint 2.14.0, buf 1.73.0, protoc-gen-go 1.36.12, protoc-gen-go-grpc 1.6.2.
- Docker Desktop resources raised to 6 CPUs and 16 GB.
- Public repository https://github.com/bvaliev/brokerium: PolyForm Shield 1.0.0, CLA, CCLA,
  policies, templates, CODEOWNERS, CLA Assistant (signatures on `cla-signatures`), Dependabot,
  security settings, ruleset "main protection" (admins bypass).
- GitHub project "Brokerium Roadmap" (https://github.com/users/bvaliev/projects/2): 17 labels,
  8 phase milestones, issues #1-#26. Helpers in `.data/` (git-ignored): `create_roadmap.py`,
  `set_status.py <Status> <issue...> [--close]`.
- Agent memory: AGENTS.md, `.cursor/rules`, ADR 0001-0010, roadmap, research notes
  (`docs/research/`), owner playbooks (`docs/legal/`).
- Lab (`deploy/lab`): clusters alpha (dynamic, `--initial-controllers`), beta (dynamic,
  `--standalone`), gamma (static, stock entrypoint), PostgreSQL on port 55432, `lab.sh` helpers.
- Runbooks validated by hand on Kafka 4.3.1: scale-out with throttled reassignment, controller
  quorum 3 -> 5 -> 3, broker replacement (new id and same id), decommission with cordon, gamma
  static -> dynamic migration. Findings in `docs/research/lab-findings.md`.

## Lab state after the runbooks

- alpha: controllers 1-3 (4 and 5 stopped, removed from the quorum); brokers 101, 102, 104, 105
  (empty, uncordoned), 106; 103 unregistered. Leaders unbalanced on purpose for the planner.
- gamma: dynamic quorum now; must be started with `-f compose.gamma-dynamic.yaml`.
- `deploy/lab/lab.sh reset` returns everything to a clean state.

## Next

1. Code skeleton: server, agent, dashboard, OpenAPI, proto, migrations (issue #7).
2. CI and container images (issue #8; needs `DOCKERHUB_TOKEN`).
3. Phase 1 dashboard (issues #9-#14).

## Owner tasks (manual)

- Create a Docker Hub access token (Read & Write) and store it:
  `gh secret set DOCKERHUB_TOKEN --repo bvaliev/brokerium`.
- Reserve the GitHub organization `brokerium` (free plan) to protect the name.
- Check and buy domains `brokerium.dev` / `brokerium.io`; then create a contact address for
  licensing and corporate CLAs.
- Trademark search (WIPO, EUIPO, USPTO) before the public launch.
- If employed: check the employment contract for IP clauses (see `docs/legal/sale-readiness.md`).

## Blockers

None.
