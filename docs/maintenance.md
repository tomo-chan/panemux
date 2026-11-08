---
type: Runbook
title: "Maintenance Guide"
description: "Maintenance procedures for toolchains, dependencies, CI, releases, and Agent Board tools."
---

# Maintenance Guide

This document captures repository maintenance rules that belong in durable project documentation rather than in the agent index.

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

Why this matters:

- it makes workflow execution reproducible
- it narrows supply-chain drift
- it keeps workflow changes explicit in code review

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

`.github/workflows/agmsg-contract.yml` runs Tier 2 of the [agmsg compatibility contract](agent-board/agmsg-contract.md#agmsg-compatibility-contract): `make test-agmsg-contract` against a real agmsg install, which the hermetic `make check` suite deliberately cannot do.

- **Daily (06:00 UTC / 15:00 JST)** scope resolves the newest stable `vN.N.N` tag with
  `git ls-remote --tags`, excluding `app-v*` and prereleases. Annotated tags use the peeled
  commit (`^{}`). All later checkout, installation and capture use that full SHA. Failure
  leaves the current pin intact and produces no proposal.
- **On every pull request** scope selects changes to `internal/agmsg/`,
  `scripts/agmsg_contract*`, `Makefile`, or this workflow. Relevant PRs run against
  `TestedAgmsgCommit` from the PR checkout; others report success without installation.
- **Manual dispatch** accepts a stable tag or full SHA, defaults to the pinned SHA, and never
  creates a PR. Tags are resolved once before installation.

The daily poll suits agmsg's release cadence (the measured median gap across 22 releases in
2026-06 through 2026-08 was 2.9 days). Successful verification is cached by **tag and commit
SHA**, so a moved tag is rechecked. Failed verification is never cached. A cached success still
retries proposal creation after missing credentials or transient GitHub failures. The proposal
job depends on the contract job succeeding and cannot run after contract failure.

The PR trigger has no `paths:` filter on purpose. A path-filtered workflow reports no status on the PRs it skips, and a required check that never reports blocks every one of them — hence the always-running `scope` job. The `contract` job likewise always runs and gates its expensive steps individually rather than skipping itself, so it always reports a real success or failure and never a `skipped` conclusion whose scoring branch protection would have to be trusted to get right.

That is what makes **`agmsg-contract / contract`** safe to mark as a required check in branch protection — which is how the contract's "a bump of the pin cannot merge unverified" requirement is actually enforced. Marking it required is a repository setting, not something this workflow can do for itself; until someone sets it, the job still runs and still reports, it just does not block.

### Bumping the pinned agmsg version

The pin is the pair `agmsg.TestedAgmsgVersion` (`internal/agmsg/agmsg_version.go`) and
`agmsg.TestedAgmsgCommit` (`internal/agmsg/tool.go`). After scheduled verification succeeds,
`scripts/agmsg_contract.py` prepares a `fix: bump pinned agmsg to vN.N.N` PR against main:

1. Update both constants to the verified version and SHA, only for a newer release.
2. Run `capture.sh` against the already checked-out SHA in an isolated HOME. This works on CI
   with sqlite3 and Python 3; no manual capture is needed. The new versioned directory contains
   raw JSONL, VERSION and COMMIT provenance. Project paths are sanitized to `/tmp/sample-project`.
3. Fixture tests derive their directory from TestedAgmsgVersion and expected IDs/timestamps from
   the captured data, so a bump changes no test code. Run the full agmsg and board packages.
   Preserve the original v1.2.0 capture for the ID-order regression, because fresh captures need
   not reproduce out-of-order UUIDs within one millisecond.
4. Create branch `automation/agmsg-vN.N.N` and its PR using a GitHub App token. PR CI rechecks
   the recorded SHA, independent of later tag movement.

One tag has at most one proposal: open, closed and merged PRs on that branch suppress duplicates.
A push that succeeded before PR creation failed is reused without force-pushing after its pin is
checked. Preflight skips existing proposals before capture and resumes a pushed branch without
rerunning upstream code. New tags receive
separate PRs; older proposals stay open and unchanged for human comparison and closure. Deliberately
closed proposals are not reopened. Scheduled runs are serialized by workflow concurrency. Moved
tags are reverified but do not rewrite existing proposals; investigate upstream retagging manually.
The automation never downgrades the pin or follows upstream main.

Review upstream changes, the version/SHA pair, captures and CI, then merge the proposal. Its `fix:`
title makes release-please propose a patch; publishing the draft remains manual. A failed canary
calls for investigation, not an unverified bump.

### GitHub App setup (repository owner)

Create a GitHub App with repository permissions **Contents: read and write** and
**Pull requests: read and write** (Metadata read is implicit), and install it on this repository.
Actions/Workflows write is unnecessary: proposals change only `internal/agmsg/`. Register the App
ID as repository Actions secret `AGMSG_BUMP_APP_ID` and its generated private key as
`AGMSG_BUMP_APP_PRIVATE_KEY`. App creation, installation and secret registration are manual.

`actions/create-github-app-token` mints a short-lived token scoped to this repository with those
two write permissions. Unlike GITHUB_TOKEN, its events trigger ordinary PR CI. Upstream installation
and capture run in a job with read-only repository permission and no App secrets. A separate fresh
publication runner checks out trusted code and accepts only one bounded JSON artifact containing
the three JSONL captures, VERSION and COMMIT. It checks names, regular-file status, sizes, JSON
shape and provenance against the independently resolved tag/SHA; no uploaded source, scripts,
paths, hooks or executable modes are applied. Trusted code writes the constants and data, then
tests the full agmsg and board packages before minting the token. Scope needs contents/PR read;
other jobs need only contents read until the App token. If either secret is absent, the workflow
reports no proposal and succeeds after contract verification.
The automation changes no App settings or secrets.

### Running it locally

```sh
make test-agmsg-contract AGMSG_PATH=/tmp/agmsg-home/.agents/skills/agmsg
make test-agmsg-automation
```

For the first command, clone agmsg into a temporary directory, check out the desired full SHA,
and install with a throwaway HOME and cleared XDG/agent configuration overrides. Never install
into the developer's own skills/settings. sqlite3 and an explicit installation are required;
otherwise the contract skips. The second command uses Python 3 and offline fake process boundaries
and temporary git repositories.

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

### Recovering a failed release build

The `release-please` workflow also accepts a manual `release_tag`, for example
`gh workflow run release-please.yml --ref main -f release_tag=v0.27.0`. Recovery
has a main-ref condition to prevent accidental dispatch from another branch;
this is not a security boundary against writers who can edit a branch's workflow.
Use it after the workflow fix has merged to main.

Both push and recovery resolve the draft inside the GoReleaser job, using
`contents: write` because draft visibility requires push access. They require a
draft whose name and tag match the requested tag, whose asset list is empty, and
whose `target_commitish` is a full commit SHA. Both check out that recorded SHA,
even when release-please creates a draft for an earlier merge than the current
push, and verify HEAD against it before creating a local tag. Both refuse an
existing tag that points elsewhere; neither path moves tags. Metadata lookup,
validation and upload share a per-tag concurrency group, preventing simultaneous
uploads to that draft without blocking builds of other tags or release-please.
GitHub keeps one active and one pending job per group: a newer job can replace
an older pending recovery. Check a dispatch's outcome and dispatch again if it
was superseded; active uploads are not cancelled.

Recovery does not run release-please again or change the proposed next version.
The current workflow
provides runner setup, while the selected release commit provides the source,
runtime pins, GoReleaser configuration and full `make check` gate.

Manual recovery passes `--draft`: after GoReleaser succeeds, inspect the draft's
four platform archives and `checksums.txt`, then publish it. The push path retains
the default `release --clean` behavior, which publishes after a successful build;
the manual inspection/publication step applies to recovery only.
A recovery failure leaves the draft unpublished; inspect the failed check before
retrying. Leftover assets are rejected before checkout, dependency setup or tests.
If an upload partially succeeded,
remove every asset left on that draft (including `checksums.txt`) with
`gh release delete-asset <tag> <asset-name>` before retrying: the release
configuration does not replace existing assets, and mixing builds would leave
inconsistent checksums. Confirm it is still a draft before deleting any assets.
Do not move an already published tag.

### release-please handling

- Never manually close a release-please PR.
- Closing it leaves the `release-please--branches--main` tracking branch stale.
- On the next push to `main`, release-please may recreate a PR with incorrect release notes that include historical commits.

### Overriding a proposed version

If release-please proposes the wrong version:

- add the `release-as: x.y.z` label to the existing release PR before merging, or
- include `Release-As: x.y.z` in the footer of a commit message on `main`

### Recovering from a stale internal branch

If the internal release-please branch becomes stale:

```sh
gh pr close <release-pr-number>
git push origin origin/main:refs/heads/release-please--branches--main --force
```

Close the incorrect PR first, then reset the tracking branch.

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
when updating the tested upstream source. The automatic pin-bump workflow belongs to #437.

Host management owns prerequisite checks, drain/pause and background automatic updates at startup,
on opt-in and after a new host is persisted. Automatic results are process-local; failures remain visible for manual Update,
with no periodic retry. The global checkbox uses config persistence; Codex hook preferences continue
to use their per-host store. See [agmsg integration](agent-board/agmsg-integration.md#automatic-updating).
