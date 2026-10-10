# Codex verification

Use fixed verification commands in a trusted checkout to avoid changing the shell
command whenever logs, caches, or PR comparison refs change (issue #334):

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

Run dependency setup before tests. Each `verify-*` target runs the corresponding
original target, saves stdout and stderr together in a unique
`.cache/verify/<target>.<unique>/output.log`, and prints completion with the child
exit code and log location. A failure prints bounded head/tail excerpts and context around the first
recognized error/failure/panic diagnostic; inspect the named
log if these do not identify the cause. Go's build cache and npm's cache live in
this checkout's ignored `.cache/go-build/` and `.cache/npm/`. These commands
replace inherited values of `GOCACHE` and `npm_config_cache`; they leave `TMPDIR`,
Go module/SDK caches, tool locations, and permission settings unchanged.

The wrapper accepts only the seven fixed targets above, or `--set-base <ref>`;
it cannot run a supplied command, script, or make options. It executes trusted
repository code, including dependency lifecycle scripts and test subprocesses.
An approval for it is consequently an approval for that workload, not a
security boundary against a malicious checkout. Do not approve broad `make`,
`sh`, `bash -c`, or arbitrary Python prefixes.

## First-time user setup

Use the official [Agent approvals and security](https://learn.chatgpt.com/docs/agent-approvals-security),
[Rules](https://learn.chatgpt.com/docs/rules), and
[Configuration basics](https://learn.chatgpt.com/docs/config-file/config-basic)
as the configuration authority. Repository scripts must not edit the user's
approval settings or silently install allow rules.

Keep `sandbox_mode = "workspace-write"` and `approval_policy = "on-request"`
unless your organization specifies otherwise. Commands inside the sandbox need
no escalation; approval rules apply to commands requested outside it. In the
Desktop approval dialog, save only the particular verification prefix you
intend to reuse, such as `make verify-check`. The CLI also supports user-managed
`~/.codex/rules/default.rules`, loaded at startup:

```python
prefix_rule(
    pattern = ["make", "verify-check"],
    decision = "allow",
    match = ["make verify-check"],
    not_match = ["make check", "make verify-mutation"],
)
```

This is a prefix, not an exact-command or working-directory restriction: trailing
make variable assignments/options can also match it, and another checkout can
supply a different Makefile. Only use it with reviewed commands and trusted
code. An agent should always issue the plain command shown above, without extra
operands. Add the other individual prefixes only if you want those workloads
approved too. `verify-install-deps` includes network acquisition and hook setup;
its permission scope is broader than an already-installed local check.

Test your candidate rule with `codex execpolicy check --pretty --rules <file> --
make verify-check` and a nonmatching command before installing it, then restart
Codex. When multiple rules match, the most restrictive decision wins. Saved
allow rules do not override forbidden rules, managed requirements, or an
approval rejection. If permission is denied, report the limitation and stop the
dependent operation; do not switch executors to bypass it.

Trusted project `.codex/config.toml` and `.codex/rules/` are supported, but this
repository installs neither. CLI overrides rank above project configuration,
then selected profiles, user configuration, cloud-managed defaults, system
configuration, and built-in defaults. Managed requirements constrain these
values rather than being overridden by project settings. Work Cloud/local
executor policy is separate; these Codex instructions do not establish its
permissions. `approval_policy = "never"` suppresses prompts by rejecting actions
that need approval; it does not make blocked checks work. Full access is not a
prerequisite for this workflow.

## Why a prompt can still be necessary

| Cause | Handling |
|---|---|
| Go build/npm/lint cache writes | The wrapper relocates build/npm caches; the existing lint target uses `.cache/golangci-lint/`. Runtime downloads already use `.cache/runtimes/`. |
| Go modules/SDKs or pinned tool acquisition | Shared Go module/SDK caches and tool binaries remain shared. Initial or upgraded downloads/writes may need approval; install before offline validation. |
| Worktree and Git metadata | A checkout can be writable while its `.git` pointer and shared Git directory are protected. Worktree creation, hooks, commits and pushes retain their own approval scope. |
| Localhost bind/connect | HTTP/WebSocket tests need local networking. A filesystem grant alone cannot enable it; use the allowed sandbox/network settings or approve the individual workload if policy permits. |
| PTY, tmux sockets, `ps`, `dscl` | Existing capability probes skip unavailable integration tests locally and fail under CI. Read skips as a coverage limitation. Approve an explicit workload if these checks must run locally. |
| Log redirection and environment prefixes | Codex conservatively treats advanced shell syntax as a whole shell invocation. `make check > file.log 2>&1`, `TMPDIR=/tmp make check`, and `MUTATION_BASE=origin/main make mutation` need not match the saved plain-make prefix. Internal log/cache/base handling keeps the outer command stable. |
| External service or destructive operations | Fetch, GitHub writes, cleanup and deployment are separate operations; verification rules do not authorize them. |

Use a purpose-specific network grant only where supported by your client and
managed policy; do not enable all sandbox networking merely to avoid localhost
prompts. The Claude Code `allowLocalBinding` repository setting does not configure
Codex. There is no repository-side promise of zero prompts across all clients or
managed policies. Test the actual saved rules on the actual client.

## PR base and existing gate contracts

Set the actual PR base once in each checkout with `--set-base`; it atomically
saves a ref name in `.cache/verify/base-ref`. For a stacked PR, supply its parent
branch; for a non-main default branch, supply that branch's tracking ref. The
wrapper does not guess from `origin/main`, `origin/HEAD`, or the current branch,
and does not fetch or query GitHub. Fetch the appropriate remote ref separately
and keep it current. Ref presence does not prove freshness. Each diff run prints
the resolved base commit and merge base so the comparison can be audited.

An existing commit and shared history with HEAD are required both when setting
the base and when running a gate. Missing/deleted refs, non-commit refs, missing
history and an unset base fail before the check runs. The saved ref is resolved
again on every run. Invalid settings never overwrite the previous saved base.
An explicit `EFFICACY_BASE`, `MUTATION_BASE`, or `COVERAGE_BLOCKS_BASE` overrides
the saved value for its gate; prefer saving the actual PR base to repeating shell
assignments. A new worktree has no saved base, even if another checkout has one.

The underlying contracts are unchanged:

- Bare `make efficacy` defaults to `origin/main`.
- Bare `make mutation` requires `MUTATION_BASE` (or script `--base`).
- Bare `make coverage-blocks` without a base reports all unexecuted blocks and
  does not fail for them. `make verify-coverage-blocks` always supplies a valid
  base and runs the diff gate. `--summary` still wins over base for the coverage
  percentage target's summary.
- CI supplies each gate's base from the pull request, including stacked PRs.
  Local saved settings do not replace that contract or weaken the diff scope.

## TMPDIR and logs

Do not set `TMPDIR=/tmp` for ordinary checks. Existing scripts use an explicit
`mktemp` template under `${TMPDIR:-/tmp}` and stop on allocation failure. The
verification log itself uses a unique directory under the checkout; runs never
truncate another run's log. No user-supplied log path is executed or interpolated
into a shell command. Success stays short so verbose Go/npm output does not
consume the conversation; the full diagnostic evidence remains on disk. Await
the execution tool's session completion to obtain its status; periodic log tails
are unnecessary. `make` may translate a failing recipe's exit code to its own
nonzero status, but the wrapper preserves the status of its child `make`.

The tracked Makefile and launch scripts do not prepend `TMPDIR=/tmp` to ordinary
verification. The existing recommendation in DEVELOPMENT.md is conditional
on the screenshot tmux path being too long. The generic Playwright launcher
uses TMPDIR for its build cache, config staging and build lock without forcing
`/tmp`; these are separate from the socket length calculation.

The short-path requirement concerns tmux sockets, not all temporary files.
`frontend/e2e/tmux-env.sh` computes `<root>/tmux-<uid>/default` and checks the
OS limit (103 usable bytes on macOS, 107 on Linux). Its E2E root is unique under
TMPDIR; screenshots use `$TMPDIR/panemux-screenshots/tmux`. The usual macOS
TMPDIR fits the production fixture, but nested test isolation directories or a
longer configured path can exceed the limit. Screenshot helper tests exercise
root calculation separately and give the real tmux start/stop/teardown helpers
short private fixture roots inside the original TMPDIR; ordinary temporary
files retain their original location. For that explicit
E2E/screenshots run only, select a shorter permitted TMPDIR before launching the
fixture; do not apply it to unit checks. Fixed screenshot staging paths
`/tmp/sample-project`, `/tmp/panemux-screenshots-agmsg`, and the screenshot lock
are a separate capture contract, protected by markers and a lock. Keep issue
#315's isolation and fail-fast behavior; no new fixed shared socket root or
permission fallback is introduced here.

## Acceptance on a local client

After setup, repeat the plain commands above in a trusted checkout and a feature
worktree, using their own saved PR base. Confirm no repeat prompt for the same
saved allowed command, different per-run logs, nonzero status on a real failure,
and no user-cache writes for relocated caches. Also test a non-main/parent base,
a missing base, and a deliberately unavailable capability. Record any skipped
checks and managed-policy restrictions. Unit tests verify the wrapper's command
scope, logs, statuses, base failure behavior, and worktree isolation; they cannot
simulate Desktop's approval UI or establish a different user's policy.
