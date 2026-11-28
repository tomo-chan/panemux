---
type: Specification
title: "Task Collection, States, and Codex Permission Hook"
description: "Task identity, host pooling, collection protocol, state derivation, unreadable-state diagnostics, agent availability, wait signatures, Codex sessions, and Codex permission hook."
---

# Task Collection, States, and Codex Permission Hook

The task dashboard lists coding-agent sessions on the panemux host and every `ssh_connections` host
independently of panes; a pane is only the window used to watch or answer a task, and observed
state never indicates whether work is done (done and labels are human-set records). Wire payloads
live in [Task API](../../api/tasks.md); dashboard presentation lives in
[Task Dashboard UI](../../ui/tasks.md); command-safety rules live in
[Task dashboard security](../../security/tasks.md).

## What a task is

A task is one coding-agent session:

- **Claude (`claude`)**: identified by its Claude Code session ID (or `pid-<pid>` if a live process
  has no readable state file).
- **Codex (`codex`)**: identified by its session ID—the UUID in
  `~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<session ID>.jsonl`—or `pid-<pid>` while a live
  interactive process holds no rollout yet. A process is interactive when its first positional
  argument is absent, a prompt, `resume`, or `fork`; every other `codex-cli` subcommand (`exec`,
  `review`, `app-server`, `mcp`, `login`, and the rest listed by `codex --help` of `codex-cli
  0.157.0`) is non-interactive. Value-taking flags skip their value and `-i`/`--image` consumes up
  to the next option (`codex -i a.png review` is a task).

## Hosts and connections

- Hosts are the panemux host (`""`) and `ssh_connections` keys (hosts only in `~/.ssh/config` are
  not collected; name-only entries inherit their `Host` block, or report
  `ssh connection "<name>" has no host` if missing; see
  [SSH connections](../workspace/sessions.md#defining-connections-in-ssh_connections)).
- Entries are editable via [`/api/config/ssh-connections`](../../api/workspace.md#apiconfigssh-connections).
  After create/edit/delete, the key's pooled connection and pending dial are discarded under the
  config lock before Board observation invalidation so recreated keys never reuse stale targets;
  unchanged entries and failed saves keep their connections.
- Each SSH host shares **one** command connection between the task and agent boards (using the same
  dialer as panes: `ProxyJump`, `ProxyCommand`, `known_hosts`), never shared with panes; each
  operation runs on its own exec channel so cancellation is isolated.
- Connections failing in use drop and redial on the next collection; failed dials back off for
  `60 s` (skipped by `POST /api/tasks/hosts/{name}/reconnect`, which also discards an in-flight
  dial).
- A dial still running after `15 s` reports `connecting` while continuing in the background. A
  collection running over `15 s` reports a host error, keeping the connection if an SSH keepalive
  answers within `5 s` and dropping otherwise. On remote timeout panemux closes the exec channel
  without sending a signal (whether the remote script exits is host-dependent and unverified); on
  the panemux host the script runs in its own process group and is killed as a group on timeout or
  cancellation.
- Removed hosts are refused by the execution port before dialing even if OpenSSH knows them;
  collection prunes removed hosts; shutdown closes all connections. One failing host never hides
  another host's tasks.

## Collection

Collection runs every `10 s` while the dashboard is visible (plus manual Refresh) or every `5 s`
for live tasks while `/ws/tasks/events` has a subscriber; nothing collects otherwise. Live stream
precedence is owned by [Task events](events.md#task-dashboard).

Each collection runs one fixed `sh -s` script per host
([Task collection security](../../security/tasks.md#task-collection)) reading only agent-written
files and own-user process tables:

| Read | Purpose |
|---|---|
| `~/.claude/sessions/*.json` | Live Claude sessions: `pid`, `sessionId`, `cwd`, `status`, `waitingFor`, `statusUpdatedAt`, `updatedAt`, `startedAt`, `version` |
| `~/.claude/projects/*/*.jsonl` changed in last `7 d` (newest `100`) | Stopped Claude sessions, first `"cwd"`, log mtime & size |
| `ps -U <own uid> -o pid=,ppid=,command=` | Live process liveness, agent detection, parent chains |
| `tmux list-panes -a -F '#{pane_pid} #{session_name}'` | Tmux session mapping |
| Working directory of candidate `claude`/`codex` PIDs | Fallback CWD when no state file supplies one |
| `PANEMUX_PANE_ID` in candidate process env (`/proc/<pid>/environ` Linux, `ps -E` macOS) | Pane lookup for agents outside tmux ([below](lifecycle.md#the-pane-of-an-agent-outside-tmux)) |
| `ps -U <own uid> -o pid=,etime=,command=` + open files (`/proc/<pid>/fd` or `lsof`) | Codex process age and open rollouts |
| Each open Codex rollout (mtime, size, first-line `cwd`, last non-`turn_diff` `task_*`/`turn_*` event, last `response_item` with `timestamp` in final `1 MiB`) + newest `thread_turns` row in `~/.codex/thread_history_1.sqlite` via `sqlite3 -readonly` | Live Codex session state |
| `~/.codex/sessions/*/*/*/rollout-*.jsonl` changed in last `7 d` with `originator == "codex-tui"` (newest `100`), first-line `cwd` & `cli_version` | Stopped Codex sessions and version |

Line protocol: `::panemux-tasks v2`. Common sections: `ps`, `tmux`, `cwd`, `env`; agent sections
namespaced by [registry ID](../agent-registry.md): `claude/state`, `claude/transcripts`,
`codex/open`, `codex/permission` (emitted even without a hook), `codex/rollouts`. Live sections
precede `ps` so exiting processes are omitted. Missing requested sections or terminator fail that
host's collection; login-shell output before the header is ignored. Missing probes (`tmux`, agent
dirs, `sqlite3`, compatible `stat`) emit zero rows rather than failing. Codex files are read under
`$HOME/.codex`, ignoring custom `CODEX_HOME` (open rollouts still identify live sessions, but
thread history and stopped sessions outside `$HOME/.codex` are not found).

## States

| State | Meaning | Derivation |
|---|---|---|
| `wait` | Running, waiting for input | Live Claude `status: "waiting"` (`waitingFor` reason), or Codex waiting on `request_user_input` / unanswered permission hook |
| `busy` | Running and working | Live Claude `status: "busy"`, or live Codex `inProgress`/`task_started` without a wait |
| `idle` | Running, awaiting next instruction | Live Claude `status: "idle"`, or live Codex completed/aborted/leftover turn |
| `run` | Live Codex process with no rollout yet | Interactive `codex` holding no rollout ([Codex sessions](#codex-sessions)) |
| `unknown` | Live process with unreadable/unknown state | Live Claude with missing/unreadable state or unknown `status`; live Codex with no turn info or unknown turn status/event |
| `stop` | No live process handling session | Conversation log / `codex-tui` rollout with no live process |

- Claude matching requires a path component `claude` or `claude-code` (`argv[0]` or `node`/`bun`/
  `deno` script) excluding `-p`/`--print`. State files count only while their `pid` matches such a
  process (`procStart` unused); duplicate live files keep the newest update; `/resume` switches
  `sessionId` in place (previous becomes stopped).
- Unreadable state files are diagnostics, never tasks; a live process without readable state
  becomes `unknown` with process CWD, claiming that directory's newest log so it is not duplicated
  as stopped.
- Stopped tasks are capped at the newest `50` logs per host across both agents within `7` days,
  excluding subagent logs (`<session>/subagents/*.jsonl`). `stop` does not distinguish clean exit,
  crash, or host reboot.
- State time uses `statusUpdatedAt` → `updatedAt` → `startedAt` (mtime for stopped), converted by
  age against the host's own clock (future times clamped to now).

## Unreadable state files

Dashboard and pane headers share `session.ParseClaudeSessionState`: under `~/.claude/sessions`, a
valid JSON object requires positive integer `pid` and `sessionId` matching `^[a-zA-Z0-9_-]+$`
(other fields optional; wrong types ignored). Classification order:

| `reason` | Label | Detail |
|---|---|---|
| `not_json` | Not valid JSON | JSON parser error, or `not a JSON object` for array/scalar/`null` |
| `invalid_pid` | pid missing or not positive | `pid is missing`, or `pid: <raw>` |
| `invalid_session_id` | sessionId missing or not a session ID | `sessionId is missing`, or `sessionId: <raw>` |

Unreadable `<pid>.json` is reported only while `<pid>` is a live Claude process (leftovers ignored;
crash/kill cleanup by Claude is unconfirmed); filenames without a pid are always reported. The
server logs unreadable/reason-change/recovery/disappearance once per `(host, full filename)` (names
differing beyond truncation stay distinct). API/log text replaces control/invisible format chars
with `U+FFFD` and truncates filenames to `128` chars and details to `120` chars. Presentation lives
in [Host tools and diagnostics](../../ui/tasks.md#host-tools-and-diagnostics).

To fix a file: inspect `~/.claude/sessions/<file>` against `pid` and `sessionId` (matching
`~/.claude/projects/*/<sessionId>.jsonl`). Partial writes (`unexpected end of JSON input`) usually
self-heal on the next poll; leftovers with no live pid can be moved aside (their log keeps the
stopped session); if an agent update breaks every file, use *Copy details* to report the format
change rather than hand-editing files the agent rewrites.

## Agent availability

Each host's `availability` in [`GET /api/tasks`](../../api/tasks.md#get-apitasks) tracks `(agent,
capability, status, reason, detail, observed_version)` with changes logged (`auth_missing` is
reserved and not emitted):

| Capability | Status / reason | Condition |
|---|---|---|
| any | `unknown` / `not_probed` | No full collection yet, or (`launch`/`resume`) no probe on current connection |
| `launch`, `resume` | `available` | Probe found agent CLI |
| `launch`, `resume` | `unavailable` / `cli_missing` | Probe found no CLI on exec or login-shell `PATH` (`collect` unaffected) |
| `launch`, `resume` | `unknown` / `probe_failed` | Probe failed, timed out, or printed unexpected output |
| `collect` | `available` | Last full collection parsed everything the agent wrote |
| `collect` | `degraded` / `unknown_format` | Last full collection saw unparseable structure or unknown status/event |
| `collect` | `unknown` / `probe_failed` | Last full collection failed (host unreachable/unreadable; detail is host error) |

Task actions derive from declared adapters and host probes (undeclared/unknown agents offer no
actions; non-stopped tasks never offer `resume`; requests for undeclared capabilities or unknown
agents return `400`):

| Action | Status / reason | Condition |
|---|---|---|
| `resume` | `unavailable` / `session_info_missing` | Session ID rejected by resume adapter, or CWD unrecorded |
| `resume` | host `resume` status | Otherwise (`available`, `unavailable`/`cli_missing`, `unknown`/`not_probed` or `probe_failed`) |
| `summary` | `unavailable` / `session_info_missing` | No valid session ID (UUID for Codex), or conversation log not found |
| `summary` | `available` | Otherwise (`summaries_enabled` is separate from the action) |

- Cached `cli_missing` refuses launch/resume with `409` before execution (before recollection on
  resume) using the launch script's fixed message and immediately triggers a fresh probe.
  `not_probed`, `probe_failed`, or degraded collection never blocks launch.
- **CLI probe**: runs asynchronously after a successful full dashboard collection (one per host;
  `GET /api/tasks` does not wait), `5 s` timeout, `5 min` TTL (immediate on new/reconnected
  connection), looking up absolute executable paths on exec then login-shell `PATH`
  ([Task agent probe](../../security/tasks.md#task-agent-probe)). Probe failures never affect
  collection or connections; stale results after shutdown/reconnect/host removal are discarded.
- **Unknown formats**: counted per live session (deduplicated across multiple FDs for one rollout),
  covering unreadable Claude JSON / malformed `codex/open` or `codex/rollouts` rows (live Codex
  with malformed open row becomes `unknown`, not `run`) and unknown Claude `status` (omitted
  accepted as older release) / `thread_turns` status (outside `inProgress`, `completed`,
  `interrupted`, `failed`) / tail turn events. Detail summarizes counts, up to `3` sanitized
  `120`-char examples, and up to `3` newest observed versions (`version` / `cli_version`, never
  compared).

## Wait signature

A `wait` task carries opaque `wait_signature` (`w1-<SHA-256 hex>`) over `(host, agent, session_id,
agent-recorded wait start on host clock, wait kind)`—consistent across collections, reloads,
reconnects, `GET /api/tasks`, and `/ws/tasks/events` ([The wait ID](events.md#the-wait-id)).

- **Claude**: positive `statusUpdatedAt` + `waitingFor` (`updatedAt`/`startedAt` never substitute).
- **Codex**: `timestamp` prefix of the last `request_user_input` `response_item` line, or hook
  timestamp for permission requests—so a second question or permission request in the same turn gets
  a fresh signature.
- Never derived from panemux clock, collection time, rollout mtime, or turn start alone; a `wait`
  without a valid wait start omits `wait_signature`.

## Codex sessions

- **Open rollout = session**: the native interactive `codex` binary (not its npm node wrapper) holds
  the session rollout open for writing after the first instruction. After `/new` or `/resume` in
  TUI, two rollouts remain open; the one written last wins (same-second tie goes to the later
  creation timestamp in the filename; FD numbers are never used). Between `/new` and the next prompt
  no new rollout exists so the prior session remains displayed.
- **Turn state**: when `sqlite3` is installed and `~/.codex/thread_history_1.sqlite` (`codex-cli`
  0.157+) has a `thread_turns` row, the highest `rollout_ordinal` row decides; otherwise the last
  `task_started`/`task_complete`/`turn_aborted` in the rollout's final `1 MiB` decides. Unknown
  `thread_turns` statuses or unknown `task_*`/`turn_*` tail events (other than `turn_diff`) are
  never fallen through and yield `unknown` (counted as `unknown_format`; a known `thread_turns`
  status still decides past an unknown tail event).

| Newest turn | State | Since |
|---|---|---|
| `inProgress` / `task_started` + last `response_item` is `request_user_input` | `wait` (`"a question from codex"`) | Rollout last write |
| `inProgress` / `task_started` + unanswered hook permission record for this turn | `wait` (`"a permission request from codex"`) | Record timestamp |
| `inProgress` / `task_started` otherwise | `busy` | Turn start |
| `completed`, `interrupted`, `failed` / `task_complete`, `turn_aborted` | `idle` | Rollout last write |
| Nothing or unknown status/event | `unknown` | — |

- **Leftover turn guard**: a turn starting more than `2 s` before its process (via `ps` `etime`) is
  a leftover from a mid-turn kill and classifies as `idle` (without `etime`, taken as read).
- **PID-only `run`**: interactive `codex` holding no rollout (awaiting first prompt, startup screen,
  or running in the shared daemon); cannot be marked done or labeled. Dashboard launches pass
  `--no-daemon` so they stay here only while held at a startup screen.
- **Shared daemon (`codex app-server`)**: TUI without `--no-daemon` (`codex-cli` 0.157+) delegates
  the rollout to the daemon, appearing as a separate `location.kind == "daemon"` task using the
  daemon's PID/age (TUI itself shows as pid-only `run`). The daemon may hold the rollout after TUI
  exit (`idle`, unresumable). Non-TUI clients (desktop app) are excluded.
- **Stopped Codex sessions**: filtered to `"originator":"codex-tui"` (covering `"source":"cli"` and
  `"source":"vscode"`, excluding `codex_exec`, desktop app, and subagents) before applying the
  `100`-rollout collection cap.

## Codex permission requests

A Codex `PermissionRequest` hook (`internal/codexhook/codex_permission_hook.sh`, POSIX `sh`) records
approval waits so sessions enter `wait` and trigger [notifications](events.md). Installed per host
from the host menu and trusted manually in Codex `/hooks`. Host tool updates share
[update coordination](../agent-board/agmsg.md#update-coordination); security rules live in
[Codex permission hook security](../../security/tasks.md#codex-permission-hook).

- **Host menu check (`GET /api/tasks/codex-hook`)**:

| Host state | Display |
|---|---|
| No `~/.codex` and no `codex` on `sh -s` `PATH` | Omitted (local menu states no Codex found) |
| Neither hook file nor `hooks.json` entry | `Not installed` + `Install` |
| Exact embedded hook, executable, and entry present | `Installed` |
| Older/edited/non-executable hook or partial setup | `Update` |
| Invalid `hooks.json` structure or `> 1 MiB` | `Cannot install:` + reason (no button) |
| Check failed (connection/EOF) | `Not checked:` + reason |

- **`Install` / `Update`**: writes embedded hook to `~/.local/bin/panemux-codex-permission-hook.sh`
  (`0755`) and adds a `PermissionRequest` entry (`timeout: 1`) to `~/.codex/hooks.json` if absent,
  preserving other keys/hooks and literal `&`, `<`, `>` with 2-space indentation and sorted top-level
  keys. Prepares a temp file in `~/.codex` before copying in place (preserving symlinks/mode; read-only
  reports `~/.codex/hooks.json is not writable` untouched; partial copy keeps recovery temp path;
  hook `> 1 MiB` shows `Update` unread; invalid `hooks.json` writes neither file). Panemux never
  trusts or checks `/hooks` trust. Manual installs at another path show `Not installed` and must
  remove duplicate entries manually if later installed via menu.
- **`Update automatically`**: persisted per host in `~/.config/panemux/codex-hook.json`
  (`{"version":1,"auto_update":["","gpu-box"]}`, mode `0600`; unreadable files report error, disable
  checkbox, and are never overwritten). When enabled on an `Update` host, updates automatically on
  host-menu open, immediately when turned on while `Update` is shown, and once in the background on
  first successful collection after startup (never auto-installs `Not installed`, never updates
  Codex itself, runs at most once per host per process; automatic update failures are not retried
  until restart, and a successful manual `Update` clears the message).
- **Hook record & collection**: hook writes `<session ID> <turn ID> <unix seconds>` (hex/dash IDs
  only, no command/conversation text) to `~/.local/state/panemux/codex-permission/<session ID>`,
  prunes files `> 7 d` old, prints nothing, and exits `0`. `codex/permission` matches the active
  turn by `task_started` turn ID (or record time `>=` turn start if `task_started` left the final
  `1 MiB`). `Input given` (`POST /api/tasks/input-given`) records answered `wait_signature` in memory
  only (per host; next request in the same turn waits again).
- **Known limits**: Codex invokes the hook even before auto-approving a command and writes nothing
  when the operator answers in the pane, so the wait remains until turn end or `Input given`.

### Manually verifying a permission request

1. Install the hook on the target host and trust it in Codex `/hooks`.
2. Start Codex with `sandbox_mode = "workspace-write"` and `approval_policy = "on-request"` (no
   `approval_policy = "never"` profile; writes inside checkout or `/tmp` do not prompt).
3. Ask Codex to run `curl -sI https://example.com` (or `touch ~/panemux-approval-test` if network is
   allowed, then remove it).
4. Confirm `Waiting for input`, press `Input given` to clear, confirm a second request in the same
   turn waits again, and confirm ending the turn clears it automatically
   ([scenarios J51 and J53](../../scenarios.md#j-the-task-dashboard)).
