# Agent Board: integration with agmsg

> Part of the current [Agent Board design](../agent-board.md).

## Integration with agmsg

`internal/agmsg` implements the board-owned message and participation ports. It owns all script
contracts, path resolution, parsing, versions, and participation text. Panemux uses agmsg's public scripts and never reads its database or team files. The required
contract is:

| Operation | Script contract |
|---|---|
| list teams | `api.sh get teams` |
| list members | `api.sh get teams <team> members` |
| read messages | `api.sh get teams <team> messages [--agent <name>] [--limit N] [--before-id <id>]` |
| send | `send.sh <team> <from> <to> <body> --force` |
| join | `join.sh <team> <agent-id> <agent-type> <project-path> --force` |

`api.sh` returns JSONL oldest-first and does not mark messages as read. Message IDs are opaque
strings: they are matched for equality only, never parsed, sorted, or compared across hosts.

### Incremental reads

agmsg has backward pagination (`--before-id`) but no forward `after`/`since` operation. Each relay
poll therefore reads the newest bounded window and selects rows after the previous cursor in the
order returned by agmsg.

If the cursor is absent from that window, the whole window is treated as new instead of assuming an
order that agmsg does not guarantee. This can replay rows. If more than the configured limit arrives
between polls, older overflow rows can instead be lost. Both are accepted limits of the external
contract; cross-host send failures have the separate best-effort semantics in
[Relay](relay.md#startup-and-recovery).

### Board writes

Every board write uses `send.sh --force`. The reserved `_system` identity and panes on other hosts
are not necessarily members of the local agmsg team, so normal roster validation cannot support
status reports or cross-host delivery. Agents may still use ordinary roster-checked agmsg commands
for conversations unrelated to Agent Board.

Because `--force` also makes `from` forgeable, the relay must validate senders. A `_system` row is
trusted only when it matches panemux's own-send ledger; the string alone grants no authority.

Panes join directly through `join.sh` with the pane ID as `agent_id`. Direct script calls avoid
agmsg's agent-specific `/agmsg` versus `$agmsg` command prefixes and keep pane addressing stable
across Agent Board.

### Discovery and paths

Panemux detects agmsg by checking `~/.agents/skills/agmsg/scripts/api.sh`. It must not use or invoke the
`agmsg` npm bootstrap command. The host menu can install or update the pinned source as described below. A missing installation
disables board integration for that host without affecting terminal service.

The installation location is fixed at `~/.agents/skills/agmsg`. Panemux expands the home directory
before remote shell quoting:

- locally, against the local user's home directory;
- remotely, against the remote user's home determined through the shared host execution port.

Remote board commands run in a non-login, usually non-interactive shell. agmsg's dependencies
(`bash`, `node`, and `sqlite3`) must therefore be available on that shell's `PATH`; availability in
an interactive profile is insufficient.

### Version coverage

`agmsg.TestedAgmsgVersion` and `agmsg.TestedAgmsgCommit` pin the tested release and source commit.
The initial pair is `1.2.0` / `3c537a51bb76a23e312e7c27770df4a58ba794ff`.
Startup compares normalized installed releases with the pinned release; every different release,
including a newer patch, warns without blocking messaging. Missing VERSION remains unknown.
Only whitespace and a leading v are normalized. Describe builds, dirty trees and prereleases
are unknown and warn: they cannot be ordered safely against the pin and are never changed.
VERSION is not proof of installed source integrity.

### Manual Install / Update

With Agent Board enabled, each task-board host menu shows agmsg. Missing copies offer Install,
older releases offer Update, and equal releases show Installed with their version. Newer releases
are kept unchanged with a warning: no downgrade is offered. Unreadable or unparseable versions show
Not checked. Another named installation (a `.agmsg` marker under another skill directory) refuses
installation/update and asks the operator to consolidate it; v1.2.0 otherwise selects the first install.

Before running, the shared tool manager checks the target's `bash`, `sqlite3`, `tar`, `mktemp` and
`node`, plus pinned-source availability on the panemux host. Dependencies are never installed.
Panemux uses `git` to fetch only the fixed SHA from `https://github.com/fujibee/agmsg.git` into
cachedir's `panemux/agmsg/<SHA>` bare repository. It verifies that the resolved commit exactly
matches the pin before `git archive --format=tar`. A verified cached commit can be used offline;
GitHub connectivity is needed only on a cache miss. Neither setup.sh, npx nor npm is used.

The archive travels over the shared input execution port to the chosen host. A fixed script extracts
it in a new mktemp directory, runs `bash ./install.sh` (Install) or `bash ./install.sh --update`
(Update) with stdin closed, and cleans only that temporary directory. The target does not need Git
or GitHub connectivity. Failures show the exit error and captured output, including timeouts;
`--recover` is never attempted automatically.

Tool management announces a pause before draining existing panemux calls. New sends fail promptly
with an updating error; paused status checks return 200 / unknown and overlapping writes 502.
Relay holds leases for the source batch and its destinations before changing history or cursors;
a paused destination keeps that batch retryable. Bootstrap holds a lease through its PTY write.
Installer attempts invalidate discovery caches, so successful Install can participate from the next
poll without restarting panemux. Pause ends on success, failure or cancellation.

The UI explains installer consent and asks for agents to restart afterwards. Panemux cannot stop
agent-owned scripts during replacement. The pinned installer can write outside `~/.agents/skills`,
including Claude commands, other agents' skills and Codex sandbox settings. It runs as the target
user; operator trust covers the fixed upstream code, verified through contract tests and human
release review, not a restricted installation sandbox. See [the security model](security-model.md).

Instance-wide automatic updating is not currently executed. `agmsg_auto_update` is persisted but inactive, and
this PR exposes no automatic-update checkbox for agmsg.

## Two panes in one project directory

Pane identity is always the pane ID, never the project path. Two same-type agents in one directory
would otherwise be discovered by agmsg as the same subscription set and could receive each other's
messages.

The adapter owns each agent's transport type and onboarding differences (`internal/agmsg/agent_adapters.go`).
At package initialization it checks that every agent declaring `board` has exactly one entry, that
no other agent has one, and that transport type names are valid and unique. Tests cover the same
matrix. Session detection returns registry IDs; conversion happens only inside the adapter. For
`claude-code`, the only adapter that claims an identity, bootstrap follows agmsg's act-as protocol:

1. join with the pane ID;
2. claim that identity with the current Claude session ID;
3. when monitor delivery is active, run a watcher restricted to that pane ID.

If another live session holds the identity, onboarding stops and reports the conflict. The claim is
still made for `turn` and `off`, but those modes do not start a watcher.

This protection is currently verified only for `claude-code`; the other five share an adapter that
writes the common steps alone, and agent types in the same project
directory can still cross-receive. Claims are made only during bootstrap, so restarting an agent
inside the same unchanged pane does not automatically claim again. These bounds are documented in
[Known limitations](limitations.md#known-limitations).

## Agent type mapping

| Agent ID | agmsg type |
|---|---|
| `claude` | `claude-code` |
| `codex` | `codex` |
| `cursor` | `cursor` |
| `gemini` | `gemini` |
| `grok-build` | `grok-build` |
| `opencode` | `opencode` |

Board process patterns retain the detection behavior checked against agmsg v1.1.13's
`type.conf` `detect_proc` keys in 2026-08. This provenance belongs to the integration, while the
registry owns transport-independent process matching. Explicit-only types (antigravity, copilot,
hermes) and the desktop agmsg-app are not detected as board agents.

The public path override has been removed. The adapter owns the fixed default location.
Relay discovery remembers confirmed absence; bootstrap rechecks presence on eligible ticks.
Installation and invalidating an absent relay discovery after Install belong to #435's follow-up.
