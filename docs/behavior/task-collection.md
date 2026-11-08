---
type: Specification
title: "Task collection and state"
description: "Task identity, host collection, state derivation, diagnostics, and agent availability."
---

# Task collection and state

Part of the [task dashboard](tasks.md).

## What a task is

A task is one agent session:

- **claude**: one Claude Code session, identified by its session ID.
- **codex**: one codex session, identified by its session ID — the UUID in its rollout's file name,
  `~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<session ID>.jsonl`. A running codex process that has
  no rollout yet (see [Codex sessions](#codex-sessions)) is a task identified by its pid. A process
  is interactive when its first positional argument is absent, a prompt, `resume` or `fork`; every
  other codex-cli subcommand (`exec`, `review`, `app-server`, `mcp`, `login` and the rest listed by
  `codex --help` of codex-cli 0.157.0) is not a task. Options that take a value are skipped with it,
  and `-i/--image` with every argument up to the next option, so `codex -i a.png review` is a task.

The [task API](task-api.md#get-apitasks) defines task fields; [records](task-records.md) and
[summaries](task-summaries.md) add operator metadata without changing observed state.

## Hosts and connections

- The hosts are the panemux host (reported with the name `""`) and the keys of `ssh_connections`.
  Hosts that exist only in `~/.ssh/config` are not collected from. Listing a `~/.ssh/config` host's
  name under `ssh_connections`, with no fields, makes it a dashboard host that connects as its
  `Host` block describes ([Defining connections](ssh.md#defining-connections-in-ssh_connections)).
  A name-only entry with no `Host` block of that name reports `ssh connection "<name>" has no host`
  as its error.
- The hosts can be added, edited and deleted from the dashboard's **Hosts…** dialog
  ([`/api/config/ssh-connections`](rest-api.md#apiconfigssh-connections)). A host added there is
  collected from at the next collection; an edited one has its connection dropped and a dial of it
  still in flight discarded, so the next collection dials it with the new details.
- Each SSH host gets **one** command connection shared by the task and agent boards, opened with the same dialer panes use
  (ProxyJump, ProxyCommand and `known_hosts` verification included) and reused by every later
  collection and Agent Board polling or sending. Each operation uses its own exec channel; cancelling
  one does not cancel another. It is never shared with a pane, and opening or closing panes does not affect it.
- A connection that fails while in use is dropped and dialed again on the next collection at once,
  which is also how a host restart is handled.
- A connection that could not be opened is not dialed again by ordinary collections for 60 seconds;
  the host reports the failure meanwhile. `POST /api/tasks/hosts/{name}/reconnect` skips the wait, and discards a dial of the host still in
  flight.
- A host whose connection is still being set up after 15 seconds reports `connecting`; the dial
  continues and serves a later collection.
- A collection that is still running after 15 seconds reports an error for that host. Its
  connection is kept when it still answers an SSH keepalive (within 5 seconds) and dropped
  otherwise, so a slow host is not redialed every collection. A host whose script takes longer than
  15 seconds every time therefore keeps reporting that error and never shows its tasks, and the
  script is started on it again at every collection.
- When a remote collection gives up, panemux closes that exec channel and sends no signal. Whether
  the script on the host stops at that point depends on the host and has not been checked. On the
  panemux host the script runs in a process group of its own, and the whole group — the script and
  every probe it started — is killed when the collection's time is up or the request is abandoned.
- A host removed from `ssh_connections` is refused by the execution port before another dial or
  command, even if OpenSSH still knows the name. Its connection is closed on the next collection. Every
  connection is closed when panemux shuts down.
- One host failing never hides another host's tasks.

## Collection

Collection runs only when the dashboard asks for it — every 10 seconds while the dashboard is on
screen and the page is visible, and on the Refresh button — or while the
[task event stream](task-events.md) has a subscriber, which observes the running tasks on every host
every 5 seconds. Nothing collects when neither asks.

The [event receiver](task-events.md#task-dashboard) owns precedence between live stream state
and dashboard collection state.

Each collection runs one fixed script per host (`sh -s`, with the script on stdin; see
[Task dashboard collection](../security/command-execution.md#task-dashboard-collection)). The script
reads only what the agents write themselves and what the host reports about its processes:

| Read | Used for |
|---|---|
| `~/.claude/sessions/*.json` | Running Claude Code sessions: `pid`, `sessionId`, `cwd`, `status`, `waitingFor`, `statusUpdatedAt`, `updatedAt`, `startedAt`, and the `version` that wrote it ([Agent availability](#agent-availability)) |
| `~/.claude/projects/*/*.jsonl` changed in the last 7 days (newest 100) | Stopped sessions, the first `"cwd"` recorded in each, and each log's modification time and size (what a summary is keyed on) |
| `ps -U <own uid> -o pid=,ppid=,command=` | Whether a state file's process is alive, running agents, and parent chains — the collecting user's processes only |
| `tmux list-panes -a -F '#{pane_pid} #{session_name}'` | Which tmux session an agent runs in |
| The working directory of each process that may be claude or codex | A running task's directory when no state file gives one |
| `PANEMUX_PANE_ID` in the environment of each process that may be claude or codex (`/proc/<pid>/environ` on Linux, `ps -E` on macOS) | The pane an agent outside tmux was started from ([below](task-navigation.md#the-pane-of-an-agent-outside-tmux)) |
| `ps -U <own uid> -o pid=,etime=,command=`, then each `codex` process's open files (`/proc/<pid>/fd`, or `lsof` where there is none) | How long each codex process has run, and the rollouts it holds open |
| For each rollout a codex process holds open: its modification time and size, the `cwd` of its first line, the last `task_*` or `turn_*` event other than `turn_diff` (the ones read are `task_started` / `task_complete` / `turn_aborted`) and the last `response_item` (with the `timestamp` that line starts with) in its final MiB, and the newest `thread_turns` row in `~/.codex/thread_history_1.sqlite` (with `sqlite3 -readonly`, when installed) | A running codex session's state |
| `~/.codex/sessions/*/*/*/rollout-*.jsonl` changed in the last 7 days whose first line's `originator` is `codex-tui` (the newest 100 of those), and that line's `cwd` and `cli_version` | Stopped codex sessions, and the codex version recorded ([Agent availability](#agent-availability)) |

The [event stream](task-events.md#lifecycle) uses the same live collection and state rules,
without the two stopped-session searches. Unreadable state is `unknown`, never a guessed wait.

The line protocol is `::panemux-tasks v2`. Common sections are `ps`, `tmux`, `cwd`, `env`;
agent sections are namespaced by [registry ID](../agent-registry.md): `claude/state`,
`claude/transcripts`, `codex/open`, `codex/permission`, `codex/rollouts`. The permission section
exists even without a hook, with no rows. Live sections precede `ps` so a process exiting between
probes is omitted. Missing requested sections or a missing terminator fail that host's collection;
login-shell output before the header is ignored.

Missing probes (tmux, agent directories, sqlite3 or compatible stat) produce no rows rather than
failing collection. Codex files are read under `$HOME/.codex`, ignoring a different `CODEX_HOME`:
open rollouts can still identify running sessions, but thread history and stopped sessions there
are not found.

## States

| State | Meaning | How it is decided |
|---|---|---|
| `wait` | Running and waiting for a person | Live claude process, `status: "waiting"`; `waitingFor` is shown as the reason |
| `busy` | Running and working | Live claude process, `status: "busy"` |
| `idle` | Running and waiting for the next instruction | Live claude process, `status: "idle"` |
| `run` | A codex process is running and has no session yet | A live interactive `codex` process holding no rollout ([Codex sessions](#codex-sessions)) |
| `unknown` | A session is running but its state cannot be read | A live claude process that no readable state file describes — its file is missing or [could not be read](#unreadable-state-files); a live claude process reporting another `status`; or a codex session whose `thread_turns` and rollout tail say nothing about its newest turn, or whose newest `thread_turns` status or last turn event is one this build does not know ([Agent availability](#agent-availability)) |
| `stop` | Nothing is handling the session | A conversation log with no live claude process for its session ID, or an interactive codex rollout that no live codex process holds open |

Claude process matching requires a program path component `claude` or `claude-code`, either
argv[0] or a node/bun/deno script, excluding `-p`/`--print`; merely referencing `~/.claude` is not
an agent. State files count only while their pid is such a process (`procStart` is unused).
Missing/reused pids leave stopped sessions discoverable by logs. `/resume` changes sessionId in
place: the previous session becomes stopped, the resumed session runs in the same process/file;
normal exit removes the file. Duplicate live state files keep the newest update.

An unreadable file is a diagnostic, never a task. A live process without readable state is
`unknown` with its process cwd, taking that directory's newest log as its own so the log is not
listed again as stopped. No observed state means done; only [records](task-records.md#done-and-labels)
say that. `stop` cannot distinguish normal exit, crash and restart.
Stopped tasks are the newest 50 logs per host across both agents, changed within 7 days, excluding
subagent logs (`<session>/subagents/*.jsonl`). State time is `statusUpdatedAt`, then `updatedAt`,
then `startedAt`; stopped tasks use log modification time. Host times are converted by age against
that host's own clock; future times become now.

## Unreadable state files

Dashboard and pane headers share the parser. Under `~/.claude/sessions`, a readable JSON object
requires positive integer `pid` and `sessionId` of letters, digits, `-`, `_`. Other fields are
optional; absent or wrong-typed values are treated as absent. Failures are classified in order:

| Reason (`reason`) | Shown as | Detail |
|---|---|---|
| `not_json` | Not valid JSON | The JSON parser's error, or `not a JSON object` for an array, a scalar or `null` |
| `invalid_pid` | pid missing or not positive | `pid is missing`, or `pid: ` and the value as written |
| `invalid_session_id` | sessionId missing or not a session ID | `sessionId is missing`, or `sessionId: ` and the value as written |

Report an unreadable `<pid>.json` only while its pid is a Claude process; ignore leftovers.
A filename without a pid is always reported. Its live process remains the pid-only `unknown` task
described above. Crash/forced-kill cleanup by Claude is unconfirmed.
The server logs unreadable/reason-change/recovery/disappearance once per host and full filename,
with quoted filename/detail; names differing beyond truncation remain distinct files.
The dashboard aggregates all hosts in one warning; [diagnostic UI](../ui-design/dashboard-hosts.md#host-tools-and-diagnostics)
owns presentation. Details and tab-separated Copy details include host, filename, reason/detail,
pid and location (unknown without a pid). The dialog auto-closes at zero files and does not reopen
on a later failure. API/log text replaces control/invisible format characters with U+FFFD and
cuts filenames/details to 128/120 characters.

To fix a file, open the named `~/.claude/sessions/<file>` on its host and compare `pid`,
`sessionId` (the matching `~/.claude/projects/*/<sessionId>.jsonl`), and optional state fields
against the parser rules above. A partial write (`not_json`, `unexpected end of JSON input`)
usually resolves on the next collection. A leftover with no live pid may be moved aside; its log
still supplies the stopped session. If every file breaks after an agent update, do not edit files
the agent rewrites: use *Copy details* to report the changed format for parser support.

## Agent availability

Each host's `availability` in [GET tasks](task-api.md#get-apitasks) records agent, capability,
status, reason/detail and newest observed version, with changes logged. Status rules are:

| Capability | Status / reason | When |
|---|---|---|
| any | `unknown` / `not_probed` | Nothing found out yet: no full collection of the host, or, for `launch` and `resume`, no probe over its current connection |
| `launch`, `resume` | `available` | The probe found the agent's CLI |
| `launch`, `resume` | `unavailable` / `cli_missing` | The probe found no CLI on `PATH` or a login shell's `PATH`; `collect` is left as it is |
| `launch`, `resume` | `unknown` / `probe_failed` | The probe failed, timed out or printed something else; the detail says what |
| `collect` | `available` | The last full collection read everything the agent wrote |
| `collect` | `degraded` / `unknown_format` | The last full collection found something of the agent's in a format this build does not know (below) |
| `collect` | `unknown` / `probe_failed` | The last full collection failed: the host could not be reached or its output could not be read. The detail is the host's `error`; what an earlier collection found is not reported until one succeeds again |

`auth_missing` is reserved and not emitted. Task actions come from declared adapters and the host
probe: undeclared/unknown agents offer no actions, and non-stopped tasks never offer resume.

| Action | Status / reason | When |
|---|---|---|
| `resume` | `unavailable` / `session_info_missing` | The session ID is not one the agent's resume accepts, or the session's working directory is not recorded |
| `resume` | as the host's `resume` above | Otherwise: `available`, `unavailable` / `cli_missing`, or `unknown` / `not_probed` or `probe_failed` |
| `summary` | `unavailable` / `session_info_missing` | The task has no session ID a summary can name (a codex one is a UUID), or its conversation log has not been found, which `POST /api/tasks/summary` refuses with `404` |
| `summary` | `available` | Otherwise. Whether summaries are on is `summaries_enabled`, not the action |

Resume/summary requests for undeclared capabilities or unknown agents return `400` regardless of
client input. The dashboard omits absent actions (Work explains that the agent does not summarize),
disables unavailable ones with the action detail, else a known cli_missing/session_info_missing
sentence, else raw reason; available/degraded/unknown remain offered and are checked again on use.
Unknown agents display `<agent> (unknown agent)` with only pane actions, no resume, summary, done or
labels, and an explanatory detail note. State notes come from `state_note`; stopped notes always
say no running process handles the session.

A current-connection `unavailable/cli_missing` launch/resume fails `409` before executing (resume
before recollection), with the launch script's fixed CLI-missing message, and immediately reprobes
so a later-installed CLI can be found on retry. Not-probed/probe-failed and degraded collection do
not block launch; the script checks the CLI. Unknown state never becomes stopped for resume,
and the agent's accepted session-ID rule still applies.

**The probe** runs asynchronously after a successful full dashboard collection, not an event,
resume or lookup collection. Only one runs per host; `GET /api/tasks` does not wait for it.
Until it answers, launch/resume retain the result for the current connection, or `not_probed`.
Results, including failures, last 5 minutes; the next full collection probes again after expiry,
and the first full collection on a new/reconnected connection probes immediately. Each probe has
5 seconds; timeout details say so. It looks up launch/resume CLIs on exec then login-shell `PATH`,
accepting absolute executable paths only ([probe security](../security/command-execution.md#task-agent-probe)).
Failure affects no collection, task or connection, and never checks or drops the connection.
Results arriving after shutdown, connection replacement or host removal are discarded. The server
logs failure/error changes and recovery, rather than every probe.

**An unknown format** is what an agent wrote that this build cannot read:

- **Structure that cannot be read**: a Claude Code state file that is not a JSON object or has no
  usable `pid` or `sessionId` ([Unreadable state files](#unreadable-state-files); a leftover of a
  process that is gone is not counted), and a `codex/open` or `codex/rollouts` row that cannot be
  read — a rollout name without a UUID, a time or size that is not a number, a row cut short.
  `ps`'s own `<pid>	<etime>` rows and blank lines are not counted.
- **A value outside the known set**: a Claude Code `status` other than `busy`, `waiting` and `idle`
  (a missing one is read as an older release's, not counted), a `thread_turns` status other than
  `inProgress`, `completed`, `interrupted` and `failed`, and a last turn event as above, each once
  per live session — a rollout open on two descriptors is one session, and a `codex/open` row
  printed once per descriptor is counted once. A live codex process whose `codex/open` row cannot be
  read is listed with its state `unknown`, not `run`.

Such a session is still listed, with its state `unknown`: no state, wait or wait signature is guessed
from it, so it notifies nothing. The agent's `collect` becomes `degraded` / `unknown_format`, with
a detail such as `unknown_status 1, malformed_row 2; e.g. status "paused" | …; versions 0.160.1`:
how many of each kind, the first 3 examples (each at most 120 characters, control characters
replaced), and up to 3 versions recorded, newest first. The version is Claude Code's state file
`version` and codex's `session_meta` `cli_version`; it is recorded, never compared with anything.
The server log says so when an agent's `collect` becomes degraded or its detail changes, and when it
is available again — not on every collection.

## Wait signature

A `wait` task carries `wait_signature`, an opaque identifier of the wait it is in, so a client can
tell a wait it has already seen from a new one. The same wait keeps its signature across
collections, page reloads and reconnects, and between `GET /api/tasks` and
the [task event stream](task-events.md#the-wait-id); a wait that begins after the previous one ended
gets another.

- It is a versioned SHA-256 (`w1-<hex>`) of the host, the agent, the session ID, when the wait began
  on the host's own clock as the agent recorded it, and the kind of wait. Clients compare it and never
  parse it.
- **Claude Code**: the state file's `statusUpdatedAt` (positive) and `waitingFor`. `updatedAt` and
  `startedAt` do not stand in for a missing `statusUpdatedAt`.
- **codex**: the `timestamp` the last `request_user_input` `response_item` line starts with, so a
  second question in the same turn is a new wait; for a permission request, the time the hook
  recorded it ([Codex permission requests](task-codex-permissions.md#codex-permission-requests)), so the next request in the
  same turn is a new wait.
- It never comes from panemux's clock, the collection time, the clock conversion, a rollout's
  modification time or a turn's start alone. A `wait` task without a valid wait start has no
  signature; one is never made up. Unknown or unreadable state is `unknown` or `run` as above, never a
  signed wait.

## Codex sessions

Codex sessions are identified through their open rollouts and newest turn records.

- **A codex process's session is the rollout it holds open.** The interactive `codex` process (the
  native binary; the node wrapper an npm install adds does not hold it) keeps its session's rollout
  open for writing. Codex creates the rollout only when the session gets its first instruction.
- **After `/new` or a `/resume` inside the TUI, the process holds two rollouts, and the one written
  last is its session.** Codex appends to a rollout as soon as it switches to it, and writes nothing
  to the other one after that. The descriptor number cannot be used: a `/resume` back to an earlier
  session reopens its rollout at a lower number. Two rollouts written in the same second go to the
  one created later (its file name starts with its creation time). The session switched away from is
  listed neither as that task nor as stopped while the process holds it open. Between `/new` and the
  new session's first instruction there is no new rollout, so the task still shows the previous
  session.
- **The state comes from the session's newest turn, and only while the process is alive.** When
  `sqlite3` is installed and `~/.codex/thread_history_1.sqlite` (codex-cli 0.157 and later) has a
  `thread_turns` row for the session, the row with the highest `rollout_ordinal` decides; otherwise
  the last `task_started`, `task_complete` or `turn_aborted` in the rollout's final MiB does. A
  `thread_turns` status this build does not know is not read past: the rollout's event does not stand
  in for it. Nor is a `task_*` or `turn_*` event other than those three and `turn_diff` that comes
  after them in the tail. Either makes the session `unknown` and is counted as an
  [unknown format](#agent-availability); a known `thread_turns` status still decides past an unknown
  event.

  | Newest turn | State | Since |
  |---|---|---|
  | `inProgress` / `task_started`, and the last `response_item` is a `request_user_input` call | `wait`, waiting for "a question from codex" | The rollout's last write |
  | `inProgress` / `task_started`, and the hook recorded a permission request of this turn that is not marked answered | `wait`, waiting for "a permission request from codex" | The record's time |
  | `inProgress` / `task_started`, otherwise | `busy` | The turn's start |
  | `completed`, `interrupted`, `failed` / `task_complete`, `turn_aborted` | `idle` | The rollout's last write |
  | Nothing, or a `thread_turns` status or last turn event this build does not know | `unknown` | — |

- **Approval waits require the permission hook** ([permission notifications](task-codex-permissions.md)).
  Without it, the turn remains `busy`: Codex files do not distinguish an approval from a running
  command. `request_user_input` is distinguishable because its call stays last until answered.
- **A turn that started before its process is a leftover, and the session is `idle`.** A codex
  killed mid-turn leaves that turn `inProgress` (and its rollout ending in `task_started`) for good,
  and resuming the session writes no new turn until it is given an instruction. A turn that started
  more than two seconds before the process did (from `ps`'s `etime`, to the second) is therefore not
  in progress. Without `etime` the turn is taken as it reads.
- **A codex process with no rollout is `run`, known by its pid.** Codex has no session of its own:
  it is waiting for its first instruction, held at a start-up screen — trusting the directory, a
  new-model notice, a usage-limit offer to switch models — or running its session in codex's shared
  daemon (below). Nothing on the host tells these apart, so the dashboard says so and points at the
  pane. Such a task cannot be marked done or labeled. A task started from the dashboard, whose first
  instruction is given on codex's command line and which runs with `--no-daemon`, stays here only
  while a start-up screen holds it.
- **Shared-daemon sessions have no discoverable pane.** A TUI without `-c` or `--no-daemon`
  (from codex-cli 0.157) uses `codex app-server`; the daemon holds the rollout and no host record
  ties it to the TUI. Each `codex-tui` session it holds is a separate task with location `daemon`,
  the daemon's pid and state derived above, using the daemon's age for the leftover check.
  The TUI itself is a pid-only `run` task, so one piece of work shows as two cards. The daemon can
  retain the rollout/writer lock after TUI exit, leaving its session `idle`, unavailable for Resume.
  Sessions for other clients such as the desktop app are excluded.
- **Stopped codex sessions are the TUI's.** A rollout's first line (`session_meta`) says where the
  session came from; only `"originator":"codex-tui"` is listed — the TUI's sessions, whether it wrote
  the rollout itself (`"source":"cli"`) or through the daemon (`"source":"vscode"`) — not `codex exec`
  (`codex_exec`), the desktop app or a subagent. The 100 the collection reads are counted after this
  filter, so a host where `codex exec` writes many rollouts does not push the TUI's out. Its working
  directory is that line's `cwd`, and the time it stopped is the rollout's modification time.
- Codex tasks with a collected rollout can be summarized by Codex ([Summaries](task-summaries.md#summaries)).
