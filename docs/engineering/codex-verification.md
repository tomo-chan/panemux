---
type: Runbook
title: "Codex verification"
description: "Fixed Codex verification commands, approval scope, checkout-local caches, logs, and PR base setup."
---

# Codex verification

Use fixed verification commands in a trusted checkout so approval prefixes stay stable whenever
logs, caches, or PR comparison refs change (issue #334):

```sh
make verify-install-deps
make verify-test-go
make verify-lint-go
make verify-check
sh scripts/verify.sh --set-base origin/main
make verify-efficacy
make verify-coverage-blocks
make verify-mutation
```

Run dependency setup before tests. Each `verify-*` target runs the underlying target, records
combined stdout/stderr in a unique `.cache/verify/<target>.<unique>/output.log`, and prints exit
status plus log location (with bounded head/tail/first-error excerpts on failure). Go build and npm
caches relocate to checkout-local `.cache/go-build/` and `.cache/npm/` (overriding `GOCACHE` and
`npm_config_cache`, while leaving `TMPDIR`, Go module/SDK caches, tool binaries, and permissions
untouched).

The wrapper accepts only the seven targets above or `--set-base <ref>`. It executes trusted
repository code (including dependency lifecycle scripts and test subprocesses), so approving it
approves that workload rather than creating a sandbox against untrusted repos. Never approve broad
`make`, `sh`, `bash -c`, or arbitrary Python prefixes.

## First-time user setup

Follow official Codex documentation ([Agent approvals and security](https://learn.chatgpt.com/docs/agent-approvals-security),
[Rules](https://learn.chatgpt.com/docs/rules),
[Configuration basics](https://learn.chatgpt.com/docs/config-file/config-basic)). Repository
scripts must never edit user approval settings or install allow rules.

Keep `sandbox_mode = "workspace-write"` and `approval_policy = "on-request"` unless organization
policy specifies otherwise. Save only specific verification prefixes (e.g., `make verify-check`) in
Desktop approval dialogs or `~/.codex/rules/default.rules`:

```python
prefix_rule(
    pattern = ["make", "verify-check"],
    decision = "allow",
    match = ["make verify-check"],
    not_match = ["make check", "make verify-mutation"],
)
```

Because prefix rules match trailing arguments and other checkouts, agents must issue only the plain
commands above and users should test candidate rules with
`codex execpolicy check --pretty --rules <file> -- make verify-check` before restarting Codex.
`verify-install-deps` includes network acquisition and hook setup (broader than local checks).
More restrictive rules and managed policies always win; if permission is denied, report the limit
and stop without switching executors to bypass it. Full access is not required (`approval_policy =
"never"` rejects actions needing approval rather than enabling blocked checks). This repository
installs neither `.codex/config.toml` nor `.codex/rules/`.

## Why a prompt can still be necessary

| Cause | Handling |
|---|---|
| Go build/npm/lint cache writes | Relocated to `.cache/go-build/`, `.cache/npm/`, `.cache/golangci-lint/`, and `.cache/runtimes/`. |
| Go modules/SDKs or pinned tools | Shared module/SDK caches and binaries may prompt on initial download/upgrade. |
| Worktree and Git metadata | `.git` pointers, worktree creation, hooks, commits, and pushes keep their own approval scope. |
| Localhost bind/connect | HTTP/WS tests require local networking; use purpose-specific grants where policy permits (Claude Code's `allowLocalBinding` does not configure Codex). |
| PTY, tmux sockets, `ps`, `dscl` | Capability probes skip locally and fail in CI; approve an explicit run if needed locally. |
| Shell redirection / env prefixes | Advanced shell syntax (`>`, `TMPDIR=...`, `MUTATION_BASE=...`) breaks prefix matching; wrapper handles logs/caches/bases internally. |
| External or destructive ops | Fetch, GitHub writes, cleanup, and deployment remain outside verification rules. |

## PR base and underlying gate contracts

Set the PR base once per checkout with `sh scripts/verify.sh --set-base <ref>` (stored atomically
in `.cache/verify/base-ref`; supply parent branch for stacked PRs). The wrapper never guesses or
fetches; fetch remote refs separately. Every run re-resolves the saved ref, verifies shared history
with `HEAD`, and prints resolved base and merge-base commits. Missing/invalid bases fail before the
gate runs without overwriting a prior valid base. Explicit `EFFICACY_BASE`/`MUTATION_BASE`/
`COVERAGE_BLOCKS_BASE` override the saved ref for that invocation; new worktrees start with no
saved base.

Underlying contracts remain unchanged: bare `make efficacy` defaults to `origin/main`, bare
`make mutation` requires `MUTATION_BASE`, bare `make coverage-blocks` without a base reports
unexecuted blocks without failing (while `make verify-coverage-blocks` always enforces the diff
gate), and CI supplies the true PR base.

## TMPDIR, socket length, and logs

Do not prepend `TMPDIR=/tmp` to ordinary checks; scripts use explicit `mktemp` templates under
`${TMPDIR:-/tmp}` and fail closed on allocation errors. The short-path constraint applies only to
tmux Unix sockets (`frontend/e2e/tmux-env.sh` checks resolved paths including macOS `/private`
against 103 usable bytes on macOS / 107 on Linux). Screenshot helper tests reuse `tmux_short_dir`
(`TMPDIR` if short enough, else `/tmp`). Production E2E/screenshot runs with an unusually long
`TMPDIR` require choosing a shorter permitted `TMPDIR` before launch; see
[Documentation screenshots](screenshots.md#socket-and-process-isolation).

## Local acceptance checklist

After setup, verify in both the main checkout and a feature worktree: no repeat prompt for saved
allowed commands, unique per-run log directories, nonzero exit on real failures, no user-cache
writes for relocated caches, non-main/missing base handling, and explicit reporting of any skipped
capabilities. Unit tests (`make test-verify`) cover wrapper scope, logs, statuses, base errors, and
worktree isolation, but cannot verify client approval UIs or managed policies.
