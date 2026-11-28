---
type: Decision
title: "Engineering and maintenance decisions"
description: "Design rationale for documentation structure, screenshot capture, Codex permission hook maintenance, agmsg pin proposals, and release recovery."
---

# Engineering and maintenance decisions

Current procedures live in [Documentation index](../index.md), [CI and releases](../engineering/ci-and-releases.md),
and [Documentation screenshots](../engineering/screenshots.md).

## Documentation structure: current state, OKF bundle, and layer/cross-cutting layout (2026-09-23–2026-11-27, issues #518, #553)

Current-state contracts are kept separate from chronology and rejected alternatives so readers do
not reconstruct behavior from rollout narration. `docs/` is validated as an OKF v0.2 bundle
(`make check-docs-okf`) alongside link/anchor validation (`make check-docs-links`).

Following #518, one-decision-per-file splitting produced 104 small decision files across 9
subdirectories (114 files including indexes) plus large compatibility stubs (`DECISIONLOG.md`,
`quality-gateway/decisions.md`), and mixing concern-based and feature-based directories scattered
specifications across up to five trees. Issue #553 consolidates decisions into topic files under
`docs/decisions/`, removes compatibility stubs and parallel translations (standardizing on English),
and sets out follow-up reorganization (#555–#558) to eliminate double `docs/<topic>.md` +
`docs/<topic>/index.md` entrances and group specifications by layer and cross-cutting concern.

## Screenshots are generated from fixtures and checked by diff (2026-10-03, issue #285)

`make screenshots` captures `docs/images/*.png` through a dedicated Playwright config against
placeholder state (fake `HOME`, sample repo, private tmux socket, stub agmsg store, and a fixed
`/api/tasks` fixture so developer sessions are never exposed). PRs touching the presentation layer
must update `docs/images/` unless labeled `screenshots-exempt`; pixel-by-pixel CI diffs were
rejected because relative timestamps and font rendering vary across machines.

## Screenshot fixed paths and socket path bounds (2026-10-10, issues #328, #343)

macOS `tmp_cleaner` and `dirhelper` delete old files before empty directories, which previously
removed marker files inside fixed `/tmp` staging directories and broke later runs. Fixed paths
became symlinks into `$TMPDIR/panemux-screenshots` so ownership markers live at one run root, and
leftover empty-directory trees are removed safely with `rmdir` rather than refusing runs.

Because macOS limits Unix socket paths to 104 bytes and `$TMPDIR` resolves under `/private/var/folders/...`,
`screenshots-env_test.sh` checks the resolved socket path length and selects `$TMPDIR` when it fits
or `/tmp` otherwise, failing explicitly if neither fits.

## Codex approval waits via PermissionRequest hook (2026-10-15–2026-10-17, issues #294, #384, #396, #402)

- **Approximate observation**: Codex files do not record command-approval waits. A lightweight
  `PermissionRequest` hook writes one record per session, cleared by `Input given` or turn end
  (withdrawing the heavier PR #379 design that tried to prove human waiting and pin Codex versions).
- **Host-menu install**: A POSIX `sh` script embedded in panemux replaced shipping multi-target
  binaries. Differences in hook content drive `Update` (no version numbers or inter-instance locks);
  `~/.codex/hooks.json` is merged in Go and written through a quoted heredoc so hosts need no `jq`,
  while hook trust stays with the operator in Codex `/hooks`.
- **Opt-in per-host updates**: Automatic updates are opt-in per host (stored in
  `~/.config/panemux/codex-hook.json`), update-only (never installing where absent), and run at
  most once per process on first successful collection without retries.

## agmsg pin bump proposals (2026-10-31, issue #437; automated same-major merge 2026-11-28)

Verified real-install runs propose release/SHA and fixture updates through a GitHub App so
proposed PRs trigger normal CI (`GITHUB_TOKEN` PRs do not). Release tags are resolved once, the
ordering-regression fixture stays frozen, and token minting runs only after validating a data-only
artifact so upstream code never receives App secrets. Release-specific branches deduplicate even
closed PRs without overwriting human decisions.

To avoid manual merge backlogs while keeping repository-wide branch rulesets and `release-please`
unaffected, same-major (minor/patch) bump PRs receive the `agmsg-automerge` label at creation time
and are squash-merged by `.github/workflows/agmsg-automerge.yml` after a secretless read-only job
verifies the exact 9-file diff, provenance constants, and all 10 required checks (failing closed on
any pending or failing status). Enabling GitHub native auto-merge repository-wide was rejected
because it requires adding GitHub Actions status checks to the `main` ruleset, which would block
`release-please` PRs created with `GITHUB_TOKEN` (where Actions workflows do not trigger) and path-filtered
`ci.yml` PRs or force adding admin bypass rules. Major agmsg releases remain manual-review only, and
panemux release publication continues to require manual `release-please` PR merges.

## Release recovery uses original source with GNU coreutils (2026-11-02, PR #479)

Failed release drafts must rebuild from their recorded source SHA rather than silently picking up
later commits. Ubuntu 26.04's uutils `mkdir` accepted `EEXIST` without `-p`, breaking the Node
bootstrap's exclusive lock; keeping release builds on Ubuntu 24.04's GNU coreutils preserves
correct locking without ignoring cleanup errors or overlaying newer scripts on old release source.
See [Recovering a failed release build](../engineering/ci-and-releases.md#recovering-a-failed-release-build)
and [issue #480](https://github.com/tomo-chan/panemux/issues/480).
