@DEVELOPMENT.md
@docs/security.md

# AGENTS.md

This file is an index for coding agents working in this repository. Do not duplicate long-form guidance here; follow the linked documents.

## Setup

Before building or running tests, always run:

```sh
make install-deps
```

This installs npm packages, downloads Go modules, and configures the repo-local Git hooks. Required once per environment.

## Read This First

1. Read [DEVELOPMENT.md](DEVELOPMENT.md) for build, test, branch, and PR workflow rules.
   This includes the path-sanitization rule for tests, fixtures, PR bodies, PR comments, review replies, and the mandatory `make check` completion rule.
2. Read [docs/overview.md](docs/overview.md) for the product purpose and current boundaries.
3. Read [docs/architecture.md](docs/architecture.md) for implementation structure and security design.
4. Read [docs/security.md](docs/security.md) before implementing changes that affect command execution, shell argument handling, SSH path handling, or `gosec` posture.
5. Read [docs/behavior.md](docs/behavior.md) for runtime behavior, API behavior, and operational assumptions.
6. Read [docs/ui-design.md](docs/ui-design.md) when changing frontend interaction or presentation.
7. Read [docs/maintenance.md](docs/maintenance.md) when touching CI, release automation, or GitHub workflow operations.
8. Read [docs/agent-board.md](docs/agent-board.md) before implementing cross-pane Claude
   messaging/status features (the `internal/board` package). It is a design document — check its
   status note before treating anything in it as shipped behavior.
9. Read [docs/scenarios.md](docs/scenarios.md) when adding or changing a user-facing feature: it
   maps each use case to where it is verified, and every feature change is expected to update it.

## Document Map

- Development workflow: [DEVELOPMENT.md](DEVELOPMENT.md)
- Product overview: [docs/overview.md](docs/overview.md)
- Architecture and security rationale: [docs/architecture.md](docs/architecture.md)
- Security requirements for implementation: [docs/security.md](docs/security.md)
- Behavior and API specification: [docs/behavior.md](docs/behavior.md)
- UI intent and interaction design: [docs/ui-design.md](docs/ui-design.md)
- CI and release maintenance: [docs/maintenance.md](docs/maintenance.md)
- Cross-pane Claude messaging design: [docs/agent-board.md](docs/agent-board.md)
- Use-case scenario coverage map: [docs/scenarios.md](docs/scenarios.md)

## Editing Rules

- Keep `AGENTS.md` short and index-oriented.
- Put day-to-day engineering workflow rules in `DEVELOPMENT.md`.
- Put enduring product, architecture, behavior, UI, and maintenance guidance under `docs/`.
- Put implementation-time security requirements in `docs/security.md` and treat it as required reading for security-sensitive changes.
- When adding a new long-form document, add it to the index here so agents can discover it quickly.
