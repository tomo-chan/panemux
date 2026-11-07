# Agent Board: integration with agmsg

> Part of the current [Agent Board design](../agent-board.md).

## Integration with agmsg

Participation, identity selection, and reception belong to [agmsg's official workflow](https://github.com/fujibee/agmsg):
its SessionStart/SessionEnd hooks, delivery settings, `/agmsg join` / `actas`, and `spawn.sh`.
Follow agmsg's documentation for setup and lifecycle details; panemux does not duplicate those
procedures or provide its own automatic participation hook. Relay accepts valid names read
from the configured `agent_board.team` on each host, independently of panes or roster lookup.
Use `name@BoardID` for cross-host traffic and reply to the received sender with `--force`;
see the [plugin contract](relay.md#plugin-send-and-reply-contract). Panemux does not set up membership.

`internal/agmsg` implements the board-owned message and participation ports. It owns all script
contracts, path resolution, parsing, versions, and availability checks. Panemux uses agmsg's public scripts and never reads its database or team files. The required
contract is:

| Operation | Script contract |
|---|---|
| list teams | `api.sh get teams` |
| list members | `api.sh get teams <team> members` |
| read messages | `api.sh get teams <team> messages [--agent <name>] [--limit N] [--before-id <id>]` |
| send | `send.sh <team> <from> <to> <body> --force` |

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

Use agmsg's official participation workflow to choose names and configure reception; panemux
does not invoke `join.sh`, choose names or install a sender wrapper. Qualified senders are
forwarded copies or forgeries and never re-enter history, status or relay. Unknown unqualified
names are accepted on the host read through `api.sh`.

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
Read those constants for the current tested pair; [maintenance](../maintenance.md#bumping-the-pinned-agmsg-version) describes verified bump proposals.
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
matches the pin before `git archive --format=tar`. SHA comparison and the tar stream consume
only git stdout. The local runner captures stderr separately: successful warnings are not data,
and failed operations report their exit error and stderr. A verified cached commit can be used offline;
GitHub connectivity is needed only on a cache miss. Neither setup.sh, npx nor npm is used.

The archive travels over the shared input execution port to the chosen host. A fixed script extracts
it in a new mktemp directory, runs `bash ./install.sh` (Install) or `bash ./install.sh --update`
(Update) with stdin closed, and cleans only that temporary directory. The target does not need Git
or GitHub connectivity. Failures show the exit error and captured output, including timeouts;
`--recover` is never attempted automatically.

Tool management announces a pause before draining existing panemux calls. New sends fail promptly
with an updating error; paused status checks return 200 / unknown and overlapping writes 502.
Relay holds leases for the source batch and its destinations before changing history or cursors;
a paused destination keeps that batch retryable. Detection only admits individual availability probes; it holds no lease around terminal input.
A managed pause skips the check without a warning and remains retryable.
Installer attempts invalidate discovery caches, so successful Install can participate from the next
poll without restarting panemux. Pause ends on success, failure or cancellation.

The UI explains installer consent and asks for agents to restart afterwards. Panemux cannot stop
agent-owned scripts during replacement. The pinned installer can write outside `~/.agents/skills`,
including Claude commands, other agents' skills and Codex sandbox settings. It runs as the target
user; operator trust covers the fixed upstream code, verified through contract tests and human
release review, not a restricted installation sandbox. See [the security model](security-model.md).

## Automatic updating

`agent_board.agmsg_auto_update` defaults to false. With Agent Board enabled and this preference
on, panemux checks the local host and every configured SSH host in the background at startup.
Turning on **Update automatically (all hosts)** in any host menu saves the same global config
preference and starts those checks too. A re-enable received during an active run queues one
follow-up check for that host; overlapping requests coalesce and never run installers concurrently.
Adding an SSH host through the host settings checks that new host after config persistence succeeds,
if automatic updates are on. It does not recheck existing hosts or retry their retained failures.
Reopening a menu or collecting tasks does not retry a failed update. No periodic
monitor is added. Turning it off prevents pending updates from starting; an installer already
running is allowed to finish.

Only an installed release older than the version pin is updated, using the same prerequisite,
verified-source, archive and drain/pause path as manual Update. Eligibility is rechecked within
the update reservation and update-only intent is preserved through the adapter’s final probe,
so an external removal cannot turn an automatic Update into Install. Missing, newer, unknown and alternate installations are left alone.
Opting in never installs agmsg on an absent host and never downgrades it.

Menus show a check/update in progress and ask to reopen for the result, retain automatic failure
reasons (including prerequisites, acquisition, installer exit/output and timeouts), and report
successful automatic updates with agent-restart guidance. A queued confirmation keeps the notice
for its own host run. A later opt-in run does not inherit that success, and adding a host identity
clears results from any previous host with the same name, including late in-flight results.
Use the host's manual Update to retry;
a successful manual operation clears the retained automatic result. Restarting panemux or explicitly turning automatic updates off and on starts a new check. There is no automatic recovery invocation.

The global checkbox is available even on a host that cannot itself be updated: it controls all
hosts. Its consent explains that future automatic updates execute the trusted pinned installer
without another prompt, including writes outside the skill directory.

## Two panes in one project directory

Pane identity is always the pane ID, never the project path. Two same-type agents in one directory
would otherwise be discovered by agmsg as the same subscription set and could receive each other's
messages.

The adapter retains registry ID to transport type mappings for registration work. The mappings
cover exactly the agents declaring `board`, with valid, unique transport type names.
Panemux does not currently join or claim identities, replace watchers, or issue participation
instructions. Use agmsg's official identity and delivery workflow for same-project subscriptions;
detection alone does not establish identity isolation.

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
Relay discovery remembers confirmed absence; bootstrap retries presence on eligible ticks until confirmed for the pane session.
The host tool manager installs the pinned source and invalidates absent relay discovery after
an installer attempt. Successful Install is observed on the next presence check without restart.
