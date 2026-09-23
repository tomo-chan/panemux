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
   Its long-form testing references live in [docs/development/](docs/development/); DEVELOPMENT.md keeps the rule and links to the reasoning.
2. Read [docs/README.md](docs/README.md) to choose the overview, topic guide, or deep dive appropriate
   to the change.
3. Read [docs/overview.md](docs/overview.md) for the product purpose and current boundaries.
4. Read [docs/architecture.md](docs/architecture.md) for current component ownership and trust boundaries.
5. Read [docs/security.md](docs/security.md) before implementing changes that affect command execution, shell argument handling, SSH path handling, or `gosec` posture. It carries the rules that always apply and a map into [docs/security/](docs/security/), where each sink's requirements live.
6. Read [docs/behavior.md](docs/behavior.md) for runtime behavior, API behavior, and operational assumptions; per-surface detail is in [docs/behavior/](docs/behavior/).
7. Read [docs/ui-design.md](docs/ui-design.md) when changing frontend interaction or presentation.
8. Read [docs/maintenance.md](docs/maintenance.md) when touching CI, release automation, or GitHub workflow operations.
9. Read [docs/agent-board.md](docs/agent-board.md) before implementing cross-pane Claude
   messaging/status features (the `internal/board` package), then use its document map to reach the
   focused file under [docs/agent-board/](docs/agent-board/).
10. Read [docs/scenarios.md](docs/scenarios.md) when adding or changing a user-facing feature: it
   maps each use case to where it is verified, and every feature change is expected to update it.
11. Read [docs/quality-gateway.md](docs/quality-gateway.md) when changing what the test suite,
    coverage gates, or CI checks are responsible for. It defines the quality characteristics tests
    must protect and the current layered gates. The numbered design decisions and measurements are
    in [docs/quality-gateway/](docs/quality-gateway/).
12. Read [docs/DECISIONLOG.md](docs/DECISIONLOG.md) when the reason, sequence, or rejected alternative
    behind the current specification matters.

## Enforcement

Some of the rules in the documents above are checked automatically rather than
being left to memory. `.claude/` holds the agent-side half:

- `.claude/settings.json` wires two hooks. A `PostToolUse` hook checks the file
  you just edited (`gofmt -s`, and `go vet` on its package, or `tsc --noEmit`
  for frontend files). A `Stop` hook checks what the whole turn changed
  (formatting, plus the tests for the touched Go packages and the frontend
  tests related to the touched modules) and refuses to end the turn while any
  of it fails.
- Neither hook runs `make check` — that stays with `.githooks/pre-push` and CI.
  See decision D6 in [docs/quality-gateway/decisions.md](docs/quality-gateway/decisions.md).
- `.claude/agents/diff-reviewer.md` reviews a branch's diff in a fresh context.
  It does not block, and it is not meant to: see decision D5.
- `make test-hooks` tests the hooks themselves. It runs as part of `make test`
  and as its own CI step, since `ci.yml` invokes the suites directly rather
  than through `make test`.
- The hook tests use `jq` where they parse `settings.json` or a hook payload.
  It is not installed by `make install-deps`; without it those checks report
  themselves as **skipped** rather than passing or failing, and the hooks
  themselves degrade to a warning on stderr rather than blocking.

These are the checks; they do not replace reading the documents above.

## Document Map

Each entry below is an index. Where a topic outgrew one file, the detail sits in a directory of the
same name beside it, and the index's own document map routes by section name.

- Development workflow: [DEVELOPMENT.md](DEVELOPMENT.md) → [docs/development/](docs/development/)
- Documentation index and reader routes: [docs/README.md](docs/README.md)
- Product overview: [docs/overview.md](docs/overview.md)
- Current architecture: [docs/architecture.md](docs/architecture.md)
- Security requirements for implementation: [docs/security.md](docs/security.md) → [docs/security/](docs/security/)
- Behavior and API specification: [docs/behavior.md](docs/behavior.md) → [docs/behavior/](docs/behavior/)
- UI intent and interaction design: [docs/ui-design.md](docs/ui-design.md)
- CI and release maintenance: [docs/maintenance.md](docs/maintenance.md)
- Cross-pane Claude messaging design: [docs/agent-board.md](docs/agent-board.md) → [docs/agent-board/](docs/agent-board/)
- Use-case scenario coverage map: [docs/scenarios.md](docs/scenarios.md)
- Test quality characteristics and the gate design: [docs/quality-gateway.md](docs/quality-gateway.md) → [docs/quality-gateway/](docs/quality-gateway/)
- Design history and rejected alternatives: [docs/DECISIONLOG.md](docs/DECISIONLOG.md)
- TLA+ specifications for `internal/board`'s state machines: [spec/agentboard/README.md](spec/agentboard/README.md)

## Editing Rules

- Keep `AGENTS.md` short and index-oriented.
- Put day-to-day engineering workflow rules in `DEVELOPMENT.md`.
- Treat related documentation updates as part of every code change: inspect the document map and
  update all affected current-state documents in the same change. Follow
  [DEVELOPMENT.md's documentation rules](DEVELOPMENT.md#documentation-updates) for scope.
- Put enduring product, architecture, behavior, UI, and maintenance guidance under `docs/`.
- Keep topic guides focused on the current state; put chronology and superseded approaches in
  `docs/DECISIONLOG.md`.
- Put implementation-time security requirements in `docs/security.md` and treat it as required reading for security-sensitive changes.
- When a document outgrows one file, split it into `docs/<name>/` and leave `docs/<name>.md` as the
  entry point: orientation, the rules that always apply, and a document map keyed by the section
  names the file used to carry, so existing references keep resolving in one hop.
- When adding a new long-form document, add it to the index here so agents can discover it quickly.
