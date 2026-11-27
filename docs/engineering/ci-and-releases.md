---
type: Runbook
title: "CI and releases"
description: "GitHub Actions pinning, runner image constraints, Renovate schedules, model-checking and E2E trace jobs, and release recovery runbooks."
---

# CI and releases

For runtime version pins and local toolchain behavior, see
[Runtime and sandbox](runtime-and-sandbox.md). For agmsg compatibility CI and pin proposals, see
[agmsg compatibility maintenance](agmsg-maintenance.md).

## CI workflows and verification wiring

- **Explicit script and OKF jobs**: Because `ci.yml` and `scenarios.yml` do not invoke `make test`
  or `make check`, CI runs `make test-verify`, `make test-go-toolchain`, `make test-node-toolchain`,
  `make check-docs-okf`, and `make test-docs-okf` explicitly. `scripts/docsokf` is included in
  coverage, red-check, and mutation (its package name matches its directory for gremlins v0.6.0,
  with a thin CLI entrypoint in `scripts/docsokf/cmd`).
- **GitHub Actions pinning**: Pin every workflow `uses:` entry to a full commit SHA, never floating
  tags (`@v4`, `@v5`). Resolve tags to exact SHAs before committing; Renovate updates SHA pins in
  explicit reviewable PRs.
- **Dependency updates (Renovate)**: Weekly update PRs run before 09:00 Monday (`Asia/Tokyo`);
  monthly lockfile maintenance runs before 09:00 on the 1st. Covers Go modules, npm dependencies,
  GitHub Actions, and `GOLANGCI_LINT_VERSION` in `Makefile`. Keep automerge disabled unless policy
  changes.
- **Agent Board tools**: Instance-wide enablement and automatic-update settings live in
  `config.yaml`; `TestedAgmsgVersion` and `TestedAgmsgCommit` must stay paired
  ([agmsg maintenance](agmsg-maintenance.md#bumping-the-pinned-agmsg-version),
  [agmsg automatic updating](../backend/agent-board/agmsg.md#global-automatic-updating-post-apihostsagmsgauto-update)).

## Runner images

Workflows run on `ubuntu-latest` (Ubuntu 26.04), except `ci.yml` and `release-please.yml`'s
`goreleaser` job, which stay pinned to `ubuntu-24.04`:

- **Playwright Chromium**: Pinned Playwright 1.59.1 rejects Ubuntu 26.04 (`Playwright does not
  support chromium on ubuntu26.04-x64`; 1.62+ supports it). Do not add Playwright jobs on
  `ubuntu-latest` until upgraded ([#425](https://github.com/tomo-chan/panemux/issues/425)).
- **GNU coreutils `mkdir`**: Ubuntu 26.04's uutils `mkdir` 0.10.0 can report success to concurrent
  creators of a lock directory, breaking Node bootstrap's install lock and `test-node-toolchain` in
  GoReleaser's before-hook. Keep release builds on 24.04 until corrected ([#480](https://github.com/tomo-chan/panemux/issues/480)).
  (uutils multicall `sleep`/`mkdir` issues affecting unit tests were fixed in #423, so
  `coverage-blocks.yml`, `efficacy.yml`, and `mutation.yml` run on 26.04.)
- **`zsh` prerequisite**: Neither image includes `zsh`, required by `internal/tasks` login-shell
  launch tests under `CI=true` (#445). Every workflow running `internal/tasks` tests (`ci.yml`,
  `coverage-blocks.yml`, `efficacy.yml`, `mutation.yml`, and `release-please.yml`'s `goreleaser`
  job) must install `zsh` via `apt-get` first.

## Model-checking job

`.github/workflows/model-check.yml` is path-filtered (not a branch-protection required check) and
runs two formal-verification jobs outside `make check`:

- **`alloy`**: Checks every `docs/models/*.als` model and requires all `check` commands to return `UNSAT`.
- **`tlc`**: Runs Tier 2 (`make model-check`) over `spec/agentboard/*.tla` and verifies zero diff
  against committed transition tables under `internal/board/testdata/`
  ([State-machine model checking](testing.md#state-machine-model-checking)).

Both jars are version-pinned and SHA256-checksummed (`TLA_TOOLS_VERSION`, `TLA_TOOLS_JAR_SHA256`).
A TLC invariant violation indicates a spec or design defect; a non-empty table diff means the spec
and committed table drifted—regenerate with `make model-check-write` and commit (never hand-edit).
`make test-model-check` runs on every PR in `ci.yml` to verify the exporter offline.

## E2E failure traces

CI runs Playwright with one retry (`trace: 'on-first-retry'`). When `Test (E2E)` fails, CI uploads
`frontend/test-results/` and `frontend/playwright-report/` as the `e2e-traces` artifact (7-day
retention; no upload on pass or first-attempt-only flakes; warns rather than fails if directories
are absent):

```sh
gh run download <run-id> -n e2e-traces -D /tmp/e2e-traces
cd frontend && npx playwright show-report /tmp/e2e-traces/playwright-report
cd frontend && npx playwright show-trace /tmp/e2e-traces/test-results/<test>-retry1/trace.zip
```

## Release workflow

### Recovering a failed release build

Dispatch `release-please.yml` manually with `release_tag` after merging any workflow fix to `main`:

```sh
gh workflow run release-please.yml --ref main -f release_tag=v0.27.0
```

- Both push and manual recovery resolve the existing draft release inside the `goreleaser` job
  (`contents: write`), requiring matching name/tag, an empty asset list, and a full commit SHA in
  `target_commitish`. Both check out that recorded SHA, verify `HEAD`, refuse moved tags, and
  serialize per tag via concurrency group (a newer pending dispatch can supersede an older pending
  one; active uploads are not cancelled).
- The current workflow supplies runner setup; the recorded release SHA supplies source, runtime
  pins, GoReleaser config, and full `make check`. Recovery does not re-run release-please.
- Manual recovery passes `--draft`: inspect the four platform archives and `checksums.txt`, then
  publish manually (push releases publish automatically).
- If a prior run partially uploaded assets, confirm the release is still a draft and delete every
  asset (including `checksums.txt`) with `gh release delete-asset <tag> <asset-name>` before
  retrying; GoReleaser does not replace existing assets, and never move an already-published tag.

### release-please handling

- Never manually close a release-please PR; doing so leaves `release-please--branches--main` stale
  and causes future PRs to include historical commits.
- Version bumps update `.release-please-manifest.json` plus `extra-files` in
  `release-please-config.json` (`frontend/package.json` `$.version`, `frontend/package-lock.json`
  `$.version` and `$.packages[""].version`). Add any new version-bearing file to `extra-files`.

### Overriding a proposed version

Add label `release-as: x.y.z` to the open release PR before merging, or include `Release-As: x.y.z`
in a commit footer on `main`.

### Recovering from a stale internal branch

Close the incorrect release PR first, then reset the tracking branch:

```sh
gh pr close <release-pr-number>
git push origin origin/main:refs/heads/release-please--branches--main --force
```
