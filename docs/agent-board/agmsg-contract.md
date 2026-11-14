---
type: Specification
title: "Agent Board: agmsg compatibility contract"
description: "The supported agmsg interface, compatibility guarantees, and upgrade verification contract."
---

# Agent Board: agmsg compatibility contract

> Part of the current [Agent Board design](../agent-board.md).

## agmsg compatibility contract

Agent Board reads through `api.sh`, sends through `send.sh`, and uses the pinned `install.sh` for
managed Install / Update. Sending and installation are not covered by agmsg's read-API compatibility
promise. The consumer contract must detect drift before users encounter silent delivery failure.
Compatibility tests also exercise agmsg's official participation workflow; panemux does not invoke
its join or delivery setup scripts.

Every panemux invocation of an agmsg script sets the fixed environment value
`AGMSG_SELF_NAME=off` on the execution host, for local and SSH execution alike.
This includes reads, sends, and managed Install / Update. Board sends act for the
message's `from`, not the calling session: agmsg must not assign that name to the
caller's pane or type `/rename` into it. The fixed value overrides inherited settings;
`--force`, sender/recipient arguments, and message bodies retain their transport contract.

The fixed execution wrapper also removes `BASH_ENV` and `ENV`, so child Bash startup
files cannot turn naming back on. It removes the pinned built-in drivers' pane markers
(`TMUX`, `TMUX_PANE`, `HERDR_ENV`, `HERDR_SOCKET_PATH`, `HERDR_PANE_ID`,
`ORCA_TERMINAL_HANDLE`, `CLAUDE_CODE_ENTRYPOINT`) and `CODEX_THREAD_ID`, which can
recover a Codex pane. Without a caller pane, upstream self-rename returns before
reading or writing a rename mark: `off` alone would leave a durable skip mark that
could suppress the sender's next enabled action. HOME, PATH and agmsg storage settings
remain available. A new terminal driver's identity mechanism must be reviewed when
changing the supported agmsg version.

The contract verifies observable behavior, not log wording or private storage:

- joining assigns the requested pane identity;
- sending with `--force` preserves sender, recipient, and arbitrary body text;
- script execution disables the self-naming family even when inherited settings enable it;
- child Bash startup cannot restore naming, and Board sends leave the sender's next
  enabled rename action available without deleting or changing existing agent marks;
- reads return the required JSONL fields in source order with opaque IDs;
- reads do not mark messages as consumed;
- identity claims and pane-specific watchers isolate same-project Claude sessions;
- dead claim owners can be replaced;
- exact release/tag normalization is accepted; non-release provenance warns and is not ordered.

### Tier 1: hermetic

Every `make check` run verifies panemux's command construction and parses captured JSONL fixtures
from the tested agmsg release selected by `fixtureDir` in `internal/agmsg/agmsg_fixture_test.go`.
The original `internal/agmsg/testdata/agmsg-v1.2.0/` capture also protects the historical ID-order regression.
Parser, cursor, command-wrapper, availability, and real-install contract tests live in
`internal/agmsg`; the relay and own-send transition tests remain in `internal/board`. Fixtures are regenerated from a verified release commit SHA, never
hand-authored. This tier catches regressions in panemux without requiring agmsg, a network, or
external state.

`TestAgmsgBashStartupAndPaneIsolation` uses a Bash-shebang observer across sh, bash,
and zsh login shells, local and simulated SSH execution, and send/read/install/update.
The observer verifies the expected HOME, exact post-login PATH, and storage settings;
a unique inert PATH entry distinguishes preservation from a child shell's defaults.
The data-only [preserved-settings.env](../../internal/agmsg/testdata/self-name/preserved-settings.env)
fixture pins exact values for the 86 retained AGMSG/AGMSGD configuration inputs
identified in the pinned public-source audit, including the age executable override,
plus the case-sensitive platform input `SystemRoot` (87 fixture settings).
HOME/PATH retain their dedicated assertions. The fixture loader also requires the
`SystemRoot` sentinel independently of the observer's fixture-driven expectations.

The source inventory is pinned to agmsg v1.7.1, commit
`f5898f49eb3d3c079c446dab4bca715b832da313`. The configuration/startup/pane inventory
contains 129 names: 118 preserved and 11 removed or overridden (the ten unset names
above plus `AGMSG_SELF_NAME=off`). The 888-entry ledger recorded in panemux commit
`07e0e32b` gains one configuration input, `SystemRoot`: 889 entries, comprising
118 preserved, 11 removed/overridden, and the original 760 out-of-scope candidates.
These ledger totals do not count every lowercase implementation local or lexical
false positive from the broader extraction.

| Name | Classification | Pinned upstream evidence | Reason |
|---|---|---|---|
| `SystemRoot` | Preserve | `scripts/daemon.sh:474`; `scripts/lib/windows-elevation.sh:29-30` | Windows system-directory fallback used to locate native system executables; distinct from `SYSTEMROOT`. |
| `agmsgd_lock_sql_PID` | Out of scope; excluded from configuration counts | `scripts/daemon.sh:141-142` | Bash creates this PID for the named `coproc agmsgd_lock_sql`; it is operation state, not inherited configuration. |

To repeat the case-complete audit, scan the pinned public `scripts/` tree
(including `scripts/lib/`; this release has no top-level `lib/`) and `install.sh`.
Install-time inputs are in scope: `AGMSG_FORCE_WINDOWS` is preserved and read only
at `install.sh:750`, so scanning `scripts/` alone finds 117 of the 118 preserved
inputs. Match shell names
with `[A-Za-z_][A-Za-z0-9_]*`, including `$name`, `${name}`, `${!name}` and `${#name}`;
scan Node `process.env`, environment-object property and literal bracket reads,
Python `environ`/`getenv`, PowerShell `$env:`, and the driver manifest detect,
detect-fallback, session and forwarded-environment keys as well. Compare exact,
case-sensitive names with the prior ledger, then inspect definitions and call sites
to separate inherited inputs from shell/JavaScript/jq locals, interpreter values,
and generated operation state. Dynamic manifest reads retain the prior audit's
classification. Verify public file bytes against the pinned Git blobs; no source
execution or private store inspection is needed. This expanded audit finds one
additional inherited configuration input, `SystemRoot`; lowercase and mixed-case
locals and generated state do not add configuration inputs. Unlisted extension
inputs remain inherited by default; this inventory is not an environment allowlist.

The broad source search, run from that public source root, is:

```sh
rg -n -g '!*.md' \
  -e '\$\{?[!#]?[A-Za-z_][A-Za-z0-9_]*' \
  -e '(process\.env|environment|env)\.' \
  -e 'environ|getenv|\$env:|process\.env\[' \
  -e '^(detect|detect_fallback|session_env|spawn_forward_env|session_name_self_source)=' \
  scripts install.sh
```

Fixtures are data only: split each non-comment line at its first `=`, preserve the
literal remainder (including an empty value), and read a final line even without LF.
The observer also checks variable presence, so deleting an explicitly empty value
must fail. `TestSettingsFixtureGoAndBashAgree` covers both line endings, empty and
delimiter-only values, embedded equals and literal shell metacharacters.

### Tier 2: real installation

`make test-agmsg-contract AGMSG_PATH=...` runs the same contract against an installed agmsg through
panemux's real local client.

CI runs it:

- on relevant pull requests against the PR's `agmsg.TestedAgmsgCommit`;
- on a schedule against the latest agmsg release.

The scheduled canary proposes a version/SHA pin and fixture bump only after success.
The App-authenticated `fix:` PR must pass ordinary CI and be reviewed before merging; draft
release publication remains manual. See [maintenance](../maintenance/agmsg-contract.md#bumping-the-pinned-agmsg-version).
The job remains outside `make check` because it installs and executes an external project. Local
runs skip when no explicit agmsg path is supplied.

Assertions must describe facts Agent Board depends on. Exact diagnostic sentences, timing based on
fixed sleeps, and incidental process ancestry are not contract behavior.

`TestAgmsgContract_BoardSendDoesNotSuppressLaterRename` reads only the public upstream
self-rename function, with fake driver and mark dependencies. It verifies that a Board
send leaves no mark or keystroke, followed by a successful enabled sender action.
It does not copy or read the installed instance's private store.

The workflow reads the version from `internal/agmsg/agmsg_version.go` and commit from
`internal/agmsg/tool.go`, and selects pull requests touching `internal/agmsg/`,
`scripts/agmsg_contract*`, `Makefile`, or the workflow itself. The package is included in
the Go coverage gate and in the fixture-agent registry consistency check.
