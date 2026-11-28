---
type: Specification
title: "agmsg Integration, Host Tools, and Compatibility Contract"
description: "Official agmsg workflow integration, agent type mapping, host tool coordination, manual/automatic agmsg updates, and Tier 1/2 compatibility contracts."
---

# agmsg Integration, Host Tools, and Compatibility Contract

`internal/agmsg` implements `internal/board`'s messaging and participation ports, and
`internal/hosttools` coordinates manual and automatic updates for both agmsg and the Codex
permission hook. Security rules live in
[Agent Board security](../../security/agent-board.md#agmsg-acquisition-and-installation); pin-bump
maintenance lives in [agmsg maintenance](../../engineering/agmsg-maintenance.md).

## Integration with agmsg

Participation, identity selection, and delivery setup belong to
[agmsg's official workflow](https://github.com/fujibee/agmsg) (SessionStart/SessionEnd hooks,
`/agmsg join` / `actas`, `spawn.sh`); panemux does not invoke `join.sh`, choose names, or install
a sender wrapper. Panemux calls only agmsg's public scripts and never reads its database or team
files:

| Operation | Script contract |
|---|---|
| List teams | `api.sh get teams` |
| List members | `api.sh get teams <team> members` |
| Read messages | `api.sh get teams <team> messages [--agent <name>] [--limit N] [--before-id <id>]` |
| Send | `send.sh <team> <from> <to> --body - --force` (decoded body on stdin) |

- **Incremental reads**: `api.sh` returns JSONL oldest-first without consuming messages, using
  opaque equality-only IDs. Because agmsg offers backward pagination (`--before-id`) without a
  forward `after` cursor, each relay poll reads the newest bounded window and selects rows after its
  cursor. A missing cursor treats the whole window as new (possible replay); bursts exceeding the
  limit drop oldest overflow rows.
- **Board writes**: every write uses `send.sh --force` because `_system` and remote names are not
  local roster members. Because `--force` makes `from` forgeable, relay sender validation and the
  own-send ledger remain mandatory ([Sender validation](relay.md#addresses-forwarding-and-sender-validation)).
- **Discovery and fixed path**: detected at `~/.agents/skills/agmsg/scripts/api.sh` (expanded
  against local or remote user home; public path override removed; `npx`/`npm` bootstrap never
  used). Remote commands run in non-login, non-interactive shells, so `bash`, `node`, and `sqlite3`
  must be on that shell's `PATH`. Relay caches confirmed absence until a managed installer attempt;
  bootstrap retries presence on eligible ticks until confirmed for the pane session.
- **Version coverage**: `agmsg.TestedAgmsgVersion` and `agmsg.TestedAgmsgCommit` pin the tested
  pair. Startup normalizes only whitespace and a leading `v` and warns on any mismatch (including a
  newer patch) without blocking messaging. Missing `VERSION`, describe builds, dirty trees, and
  prereleases are treated as unknown, warn, and are never overwritten. `VERSION` does not prove
  source integrity.

## Two panes in one directory and agent type mapping

Pane identity is always the pane ID rather than the project directory so two same-type agents in one
directory do not collapse into one subscription set (use agmsg's official workflow for same-project
isolation). `internal/agmsg/agent_adapters.go` maps exactly the registry's `board`-declared agents
to unique transport type names (validated at startup and in tests).

### Agent type mapping

| Agent ID | agmsg type |
|---|---|
| `claude` | `claude-code` |
| `codex` | `codex` |
| `cursor` | `cursor` |
| `gemini` | `gemini` |
| `grok-build` | `grok-build` |
| `opencode` | `opencode` |

Board process patterns retain detection behavior checked against agmsg v1.1.13's `type.conf`
`detect_proc` keys in 2026-08. Explicit-only types (`antigravity`, `copilot`, `hermes`) and desktop
`agmsg-app` are not auto-detected.

## Host tool coordination and agmsg management

### Update coordination

`internal/hosttools` manages both the Codex permission hook and agmsg over the shared host execution
port (notified by successful task collections;
[Codex hook routes](../../api/tasks.md#get-and-post-apitaskscodex-hook-post-apitaskscodex-hookauto-update)
and agmsg routes share this layer):

- Updates announce a pause for `(tool, host)` and drain in-flight calls before running adapter
  prerequisites and installing.
- During a pause, new consumer calls and overlapping writes fail immediately with an updating error
  (`502`), while status checks return `200` / `unknown` without invoking the tool; other tools and
  hosts remain unaffected.
- Failed prerequisites report every failed name and reason (dependencies are never installed
  automatically). Relay holds leases for source and destination batches before mutating history or
  cursors (paused destination keeps the batch retryable); bootstrap admits individual availability
  probes silently without holding a lease around terminal input.
- Success, failure, and cancellation all lift the pause; installer attempts invalidate discovery
  caches so successful Install takes effect on the next poll without restarting panemux.

### Manual Install / Update (`GET` and `POST /api/hosts/agmsg`)

Available only when `agent_board.enabled` is true (otherwise `404`; unknown hosts return `404`;
guarded by the shared browser request guard; no request selects source or arguments):

- `GET /api/hosts/agmsg?host=<ssh_connections-key>` (empty = local) returns
  `{status, version?, error?, auto_update, auto_updated?, auto_error?}` where `status` is
  `missing`, `installed`, `outdated`, `newer`, `invalid`, or `unknown`. Missing offers `Install`,
  older offers `Update`, equal shows `Installed`, newer warns without downgrade, unreadable/describe/
  dirty shows `Not checked`, and an alternate `.agmsg` installation under another skill directory is
  refused asking the operator to consolidate it.
- `POST /api/hosts/agmsg` (`{"host": "..."}`) checks target prerequisites (`bash`, `sqlite3`, `tar`,
  `mktemp`, `node`) and local pinned-source availability, then fetches only `TestedAgmsgCommit` via
  `git` from `https://github.com/fujibee/agmsg.git` into cachedir `panemux/agmsg/<SHA>`, verifies
  the exact commit SHA on stdout (capturing stderr separately so git warnings never corrupt data
  while failures keep stderr; verified cache works offline), streams `git archive --format=tar` over
  the shared stdin execution port, extracts in a new `mktemp` dir, runs `bash ./install.sh` or
  `bash ./install.sh --update` with stdin closed, and removes only that temp dir (target needs no
  Git/GitHub access; `--recover` is never auto-invoked; overlapping/failing writes return `502`).
- Installer runs as the target user and can write outside `~/.agents/skills` (Claude commands,
  other agents' skills, Codex sandbox settings); panemux cannot stop running agent scripts during
  replacement, so UI displays consent and asks agents to restart afterward.

### Global automatic updating (`POST /api/hosts/agmsg/auto-update`)

- Controlled by `agent_board.agmsg_auto_update` (default `false`). `POST /api/hosts/agmsg/auto-update`
  accepts only `{"enabled": true|false}` (no host selector; `404` when Agent Board disabled; `400`
  on unknown fields or non-boolean; `500` on config save failure leaving in-memory state unchanged)
  and returns `{"auto_update": <bool>}`.
- Triggers background checks across local and `ssh_connections` hosts at startup, on off-to-on
  opt-in, and on persisted SSH host addition (checking only the new host; no periodic timer; menu
  reopen and task collection never retry retained failures). Overlapping requests coalesce and never
  run installers concurrently; a re-enable during an active run queues one follow-up check; turning
  off cancels pending checks while letting an active installer finish.
- **Update-only**: rechecks eligibility inside the reservation and preserves update-only intent
  through the final probe so only older installed releases update (missing, newer, unknown, and
  alternate installs stay untouched). Retained results stay scoped to the host run (cleared by
  successful manual Update, restart, opt-in toggle, or host re-addition).

## agmsg compatibility contract

Sending (`send.sh`) and installation (`install.sh`) sit outside agmsg's read-API stability promise,
so panemux enforces a two-tier consumer contract verifying observable behavior (not log wording or
private storage):

- **Self-naming suppression**: every local and SSH agmsg invocation (reads, sends, Install/Update)
  sets `AGMSG_SELF_NAME=off`, unsets `BASH_ENV` and `ENV` (blocking Bash startup overrides), and
  unsets built-in terminal driver markers (`TMUX`, `TMUX_PANE`, `HERDR_ENV`, `HERDR_SOCKET_PATH`,
  `HERDR_PANE_ID`, `ORCA_TERMINAL_HANDLE`, `CLAUDE_CODE_ENTRYPOINT`, `CODEX_THREAD_ID`) so Board
  sends never rename caller panes or leave a durable skip mark blocking the sender's next enabled
  action while keeping `HOME`, `PATH`, and agmsg storage settings intact. Review new drivers when
  bumping the pinned version.
- **Body contract**: NUL (`U+0000`) rejected before execution (no silent alteration; broadcast
  cancels ledger entry and returns no delivery; relay logs and keeps original under best-effort
  cursor). Option-like text (`--force`, `--body`, `--body-file`, `--`, `--help`), shell syntax,
  Unicode, and embedded newlines pass as stdin via `--body - --force`
  (`TestAgmsgContract_OptionLikeBodiesRoundTrip`). Trailing newlines follow pinned upstream
  normalization (`TestAgmsgContract_TrailingNewlinesAreNormalized`).
- **Behavioral assertions**: join assigns requested identity; `--force` preserves qualified names
  (`@`, `:`, `.`) and data bodies; self-naming stays disabled without suppressing future enabled
  renames; reads preserve source order with opaque IDs and never mark consumed; identity claims and
  watchers isolate same-project sessions and replace dead owners; exact tag/release normalization
  works while non-release provenance warns and is unordered.

### Tier 1: hermetic (`make check`)

Verifies command construction, escaping, parser, cursor, availability, and captured JSONL fixtures
(`fixtureDir` in `internal/agmsg/agmsg_fixture_test.go`, plus `agmsg-v1.2.0/` ID-order regression
capture and `agmsg-v1.7.1/get_qualified_messages.jsonl` from commit
`f5898f49eb3d3c079c446dab4bca715b832da313`, never hand-authored). Covers SSH key characters
(`TestAgmsgContract_SSHKeyCharactersSendAndHistory`, `TestPaneRelaySSHKeySendAndReply`), E2E
`send.sh` fixture (`--body -` and positional seeding), NUL rejection across direct/lazy/leased
clients, and Bash startup/pane isolation (`TestAgmsgBashStartupAndPaneIsolation` across `sh`,
`bash`, `zsh` login shells).

- **Environment audit ledger (pinned to agmsg v1.7.1, `f5898f49eb3d3c079c446dab4bca715b832da313`)**:
  data-only [preserved-settings.env](../../../internal/agmsg/testdata/self-name/preserved-settings.env)
  pins exact values for `86` `AGMSG`/`AGMSGD` inputs plus case-sensitive `SystemRoot` (`87` fixture
  settings; `HOME`/`PATH` asserted separately; `TestSettingsFixtureGoAndBashAgree` verifies parsing
  including empty values, embedded `=`, metacharacters, and missing final LF). Full ledger: `889`
  entries (`118` preserved including install-time `AGMSG_FORCE_WINDOWS` at `install.sh:750` and
  `SystemRoot`, `11` removed/overridden, `760` out-of-scope candidates; not an allowlist—unlisted
  extension inputs stay inherited).

| Name | Classification | Pinned upstream evidence | Reason |
|---|---|---|---|
| `SystemRoot` | Preserve | `scripts/daemon.sh:474`; `scripts/lib/windows-elevation.sh:29-30` | Windows system-directory fallback distinct from `SYSTEMROOT` |
| `agmsgd_lock_sql_PID` | Out of scope | `scripts/daemon.sh:141-142` | Bash-generated `coproc` PID operation state, not inherited config |

Audit search from the pinned public source root (`scripts/` including `scripts/lib/`, plus
`install.sh`):

```sh
rg -n -g '!*.md' \
  -e '\$\{?[!#]?[A-Za-z_][A-Za-z0-9_]*' \
  -e '(process\.env|environment|env)\.' \
  -e 'environ|getenv|\$env:|process\.env\[' \
  -e '^(detect|detect_fallback|session_env|spawn_forward_env|session_name_self_source)=' \
  scripts install.sh
```

### Tier 2: real installation (`make test-agmsg-contract`)

Runs against an explicit isolated pinned installation (`AGMSG_PATH=...`, copied before use, never
the operator's live store; skips locally when unset). Exercises
`TestAgmsgContract_QualifiedIdentifiersRoundTrip` and
`TestAgmsgContract_BoardSendDoesNotSuppressLaterRename` (public upstream self-rename function only,
no private store reads). CI runs Tier 2 on PRs touching `internal/agmsg/`, `scripts/agmsg_contract*`,
`Makefile`, or the workflow against `agmsg.TestedAgmsgCommit`, and daily against latest release
(proposing a pin bump PR on success; see
[agmsg maintenance](../../engineering/agmsg-maintenance.md#bumping-the-pinned-agmsg-version)).
`internal/agmsg` is included in the Go coverage gate and fixture-agent registry consistency checks.
