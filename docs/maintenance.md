---
type: Runbook
title: "Maintenance Guide"
description: "Maintenance procedures for toolchains, dependencies, CI, releases, and Agent Board tools."
---

# Maintenance Guide

Use the [maintenance index](maintenance/index.md) for agmsg and release operating procedures.

## Runtime toolchains

Keep `go.mod` and `.node-version` as the sole exact Go and Node version sources.
Go workflows use `go-version-file: go.mod`; Node workflows use
`node-version-file: .node-version`. Local Makefile initialization, recipes, hooks, npm tasks,
and fixture/gate scripts use the common runtime shell, including the selected SDK's bundled
npm. Node 24.21.0 LTS ships npm 11.19.0 according to the
[official release metadata](https://nodejs.org/dist/index.json). It has official support through
2028-04-30 according to the [release schedule](https://github.com/nodejs/Release/blob/main/schedule.json).
Node 20's support ended on 2026-04-30; upgrading the runtime does not change npm dependencies.

Do not add parallel version pins or require per-command environment overrides. The Node
selector uses existing exact SDKs or bootstraps a verified official archive; its local cache
is excluded from source control. CI runs both runtime regression suites explicitly because
its unit suites do not use `make test`. Verify those suites and `make check` when changing
pins. See [the development contract](development/runtime.md) for supported
platforms, installation, direct npm behavior, and failure semantics.

## Local verification scripts

CI runs `make test-verify` with the sandbox-safe script suites, because its Go
and frontend jobs do not invoke `make test`. Keep the fixed verification entry
points and original gates aligned; their saved local base must never replace
CI's pull-request base. User-managed approval setup and remaining capability
limits are documented in [Codex verification](development/codex-verification.md).

The OKF bundle checker and its Go tests run explicitly in `ci.yml` and the documentation job in
`scenarios.yml`, since those workflows do not invoke `make check`. `scripts/docsokf` is a normal
Go package included in coverage, red-check, and mutation. Keep its pre-push selection aligned
with bundle Markdown and checker changes.
Its package name matches its directory because gremlins v0.6.0 resolves per-mutant test
packages through that match; the thin CLI entrypoint lives in `scripts/docsokf/cmd`.

## GitHub Actions Pinning

- Pin GitHub Actions to full commit SHAs, not floating tags such as `@v4` or `@v5`.
- When updating workflows, resolve the intended tag to its exact commit SHA first, then commit the SHA-pinned reference.
- Renovate manages scheduled GitHub Actions updates for this repository. Keep workflow `uses:` entries SHA-pinned so Renovate can advance them as explicit reviewable changes.

SHA pins make workflow execution reproducible, prevent a moved tag from changing what runs
(supply-chain drift), and keep every change explicit in review.

## Runner Images

Workflows run on `ubuntu-latest`, now Ubuntu 26.04, except `ci.yml` and the release
workflow's `goreleaser` job, which are pinned to `ubuntu-24.04`. CI installs
Playwright's Chromium, and the pinned Playwright (1.59.1) refuses that
platform: `Playwright does not support chromium on ubuntu26.04-x64`. Playwright 1.62 and later know
it.

Return `ci.yml` to `ubuntu-latest` once Playwright is updated to a version that installs Chromium on
Ubuntu 26.04 and the job passes there (issue #425). Until then, add no new job that installs
Playwright on `ubuntu-latest`.

Release builds use GNU coreutils because Ubuntu 26.04's uutils `mkdir` 0.10.0 can
report success to both concurrent creators of a lock directory. That breaks the
Node bootstrap's exclusive install lock and fails `test-node-toolchain` in the
GoReleaser before hook. Keep the release job on 24.04 until the replacement runner
has a corrected `mkdir` and passes the concurrent Node selector tests and full
release quality gate. Recovery uses this runner setup even for older release
source commits; no source override or skipped check is needed. Developer machines
with the affected `mkdir` still need a conforming GNU or corrected uutils
implementation for concurrent Node acquisition; portable locking is tracked in
[issue #480](https://github.com/tomo-chan/panemux/issues/480).

Ubuntu 26.04 also ships uutils coreutils, one multicall binary behind `/usr/bin/sleep`, `mkdir` and
the rest. It refuses to run under another name, and its `mkdir` leaves the path out of its error.
The Go tests that depended on either pass on 26.04 since issue #423; `coverage-blocks.yml`,
`efficacy.yml` and `mutation.yml` run them there.

Neither image has zsh, which the task launch tests run the agent through (issue #445). Every
workflow that runs `internal/tasks`'s tests — `ci.yml`, `coverage-blocks.yml`, `efficacy.yml` and
`mutation.yml`, plus the `goreleaser` job in `release-please.yml` whose before hook runs
`make check` — installs it with `apt-get` first: with `CI` set, those tests fail rather than skip
when a shell they need is missing, so a new such workflow needs the same step.

## Dependency Updates

- Renovate is the repository's scheduled dependency update mechanism.
- Weekly update PRs run before 9am on Monday in the `Asia/Tokyo` timezone.
- Monthly lockfile maintenance runs before 9am on the first day of the month.
- Renovate covers Go modules, frontend npm dependencies, GitHub Actions, and the `GOLANGCI_LINT_VERSION` pin in `Makefile`.
- Review Renovate PRs like any other dependency bump and keep automerge disabled unless project policy changes.

## The agmsg Compatibility Contract Job
See [agmsg compatibility maintenance](maintenance/agmsg-contract.md).

### Bumping the pinned agmsg version
See [agmsg compatibility maintenance](maintenance/agmsg-contract.md#bumping-the-pinned-agmsg-version).

### GitHub App setup (repository owner)
See [agmsg compatibility maintenance](maintenance/agmsg-contract.md#github-app-setup-repository-owner).

### Running it locally
See [agmsg compatibility maintenance](maintenance/agmsg-contract.md#running-it-locally).

## The Model-Checking Job

`.github/workflows/model-check.yml` holds two jobs, both validating formal models and neither part of `make check`.

- **`alloy`** checks every `docs/models/*.als` model and requires all of its `check` commands to come back `UNSAT`.
- **`tlc`** runs Tier 2 of the [state-machine model checking](agent-board/model-checking.md#state-machine-model-checking) split introduced by issue #168: `make model-check`, which runs TLC over `spec/agentboard/*.tla` and then diffs the transition table it exports against the copy committed under `internal/board/testdata/`.

The workflow is path-filtered — it runs only when a spec, an exported table, the export scripts or the workflow itself changes — so it is deliberately **not** a candidate for a required check in branch protection, unlike `agmsg-contract / contract`. A path-filtered workflow reports no status at all on the pull requests it skips.

`tla2tools.jar` is pinned to a release tag and checksummed, for the same reason the Alloy jar is: a model checker that silently changed version is a model check nobody can reproduce. Bumping it means changing both `TLA_TOOLS_VERSION` and `TLA_TOOLS_JAR_SHA256`, and re-running `make model-check` — a TLC version that explores the state space differently would show up as a table diff.

### What a failure means

- **TLC reports a violated invariant or property.** The spec is wrong, or the design it describes is. Read the counterexample trace TLC prints; it is a concrete sequence of actions.
- **The transition table diff is non-empty.** The spec and the committed table have drifted. Tier 1 — `internal/board/ledger_conformance_test.go`, inside `make check` — replays the real Go implementation against the **committed** table, so a spec change that never reaches it silently weakens every hermetic run. Regenerate and commit:

```sh
curl -fsSL -o /tmp/tla2tools.jar \
  https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar
TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check-write
```

Never hand-edit a file under `internal/board/testdata/*-transitions.json`; the next `make model-check-write` overwrites it, and the diff it produces is the behavioral change a reviewer reads.

### The hermetic half

`make test-model-check` (inside `make check`, and run by `ci.yml` on every pull request) tests the exporter this job depends on, against committed dot fixtures in `scripts/testdata/model-check/` — no JDK, no jar. It is what catches an exporter that would write a table smaller than the `.cfg` asked for, which is the failure that makes Tier 1 check less while still reporting green. `python3` is optional for it the way `jq` is for `make test-hooks`: without it those checks report themselves as skipped.

## Release Workflow
See [Release maintenance](maintenance/releases.md).

### Recovering a failed release build
See [Release maintenance](maintenance/releases.md#recovering-a-failed-release-build).

### release-please handling
See [Release maintenance](maintenance/releases.md#release-please-handling).

### Overriding a proposed version
See [Release maintenance](maintenance/releases.md#overriding-a-proposed-version).

### Recovering from a stale internal branch
See [Release maintenance](maintenance/releases.md#recovering-from-a-stale-internal-branch).

## E2E Failure Traces

CI runs Playwright with one retry, and `playwright.config.ts` records a trace on that first retry
(`trace: 'on-first-retry'`). When the `Test (E2E)` step of `ci.yml` fails, the next step uploads
`frontend/test-results/` (the traces) and `frontend/playwright-report/` (the HTML report) as the
`e2e-traces` artifact, kept for 7 days. Nothing is uploaded when the step passes.

- Download it from the failed run's summary page, or with `gh run download <run-id> -n e2e-traces`.
  The upload drops the common `frontend/` prefix, so the download holds `test-results/` and
  `playwright-report/` at its top level.
- Open the HTML report with `npx playwright show-report <download-dir>/playwright-report` from
  `frontend/`; a failed test's entry links its retry's trace.
- Open one trace directly with
  `npx playwright show-trace <download-dir>/test-results/<test>-retry1/trace.zip` from `frontend/`.
- If neither directory exists when the step fails (for example, the web server never started), the
  upload step leaves a warning annotation on the run rather than failing the job.
- A test that failed only on its first attempt and passed on the retry fails nothing, so it leaves no
  artifact; a retry that also failed is what a trace exists for.

## Related Documents

- Developer workflow and PR rules: [../DEVELOPMENT.md](../DEVELOPMENT.md)
- Architecture and security design: [architecture.md](architecture.md)
- Runtime behavior and operational assumptions: [behavior.md](behavior.md)

## Agent Board tools (#435)

Enablement, mode and the agmsg automatic-update preference are instance-wide in config.yaml.
The agmsg adapter pins both `TestedAgmsgVersion` and `TestedAgmsgCommit`; acquisition verifies the
exact commit and archives it through the shared input execution lane. Keep the pair consistent
when updating the tested upstream source. See [pin proposal maintenance](maintenance/agmsg-contract.md#bumping-the-pinned-agmsg-version).

Host management owns prerequisite checks, drain/pause and background automatic updates at startup,
on opt-in and after a new host is persisted. Automatic results are process-local; failures remain visible for manual Update,
with no periodic retry. The global checkbox uses config persistence; Codex hook preferences continue
to use their per-host store. See [agmsg integration](agent-board/agmsg-integration.md#automatic-updating).
