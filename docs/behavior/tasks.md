# Behavior: task dashboard

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Task Dashboard

The task dashboard lists every coding-agent session on every host panemux knows — the panemux host
itself and every `ssh_connections` entry — independently of panes. A pane is only the window used to
watch or answer a task, opened when the dashboard is asked to. The design is issue
[#252](https://github.com/tomo-chan/panemux/issues/252); this page covers what is built (its stage 1,
and opening an agent outside tmux from issue [#254](https://github.com/tomo-chan/panemux/issues/254)).
The UI is described in [UI design's Task Dashboard](../ui-design.md#task-dashboard).

### What a task is

A task is one agent session:

- **claude**: one Claude Code session, identified by its session ID.
- **codex**: one running interactive `codex` process, identified by its pid. Codex's own session
  files are not read, and a codex process that has exited is not listed. A process is interactive
  when its first positional argument is absent, a prompt, `resume` or `fork`; every other codex-cli
  subcommand (`exec`, `review`, `app-server`, `mcp`, `login` and the rest listed by
  `codex --help` of codex-cli 0.157.0) is not a task. Options that take a value are skipped with it.

A task carries its host, agent, session ID, working directory, state, the time it entered that
state, the reason it is waiting, its start time, its pid, where it runs, and the repository, branch
and pull request of its working directory.

### Hosts and connections

- The hosts are the panemux host (reported with the name `""`) and the keys of `ssh_connections`.
  Hosts that exist only in `~/.ssh/config` are not collected from.
- Each SSH host gets **one** connection for the dashboard, opened with the same dialer panes use
  (ProxyJump, ProxyCommand and `known_hosts` verification included) and reused by every later
  collection. It is never shared with a pane, and opening or closing panes does not affect it.
- A connection that fails while in use is dropped and dialed again on the next collection at once,
  which is also how a host restart is handled.
- A connection that could not be opened is not dialed again by ordinary collections for 60 seconds;
  the host reports the failure meanwhile. `POST /api/tasks/hosts/{name}/reconnect` skips the wait.
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
- A host removed from `ssh_connections` has its connection closed on the next collection. Every
  connection is closed when panemux shuts down.
- One host failing never hides another host's tasks.

### Collection

Collection runs only when the dashboard asks for it — every 10 seconds while the dashboard is on
screen and the page is visible, and on the Refresh button. Nothing collects in the background.

Each collection runs one fixed script per host (`sh -s`, with the script on stdin; see
[Task dashboard collection](../security/command-execution.md#task-dashboard-collection)). The script
reads only what the agents write themselves and what the host reports about its processes:

| Read | Used for |
|---|---|
| `~/.claude/sessions/*.json` | Running Claude Code sessions: `pid`, `sessionId`, `cwd`, `status`, `waitingFor`, `statusUpdatedAt`, `updatedAt`, `startedAt` |
| `~/.claude/projects/*/*.jsonl` changed in the last 7 days (newest 100) | Stopped sessions, and the first `"cwd"` recorded in each |
| `ps -U <own uid> -o pid=,ppid=,command=` | Whether a state file's process is alive, running agents, and parent chains — the collecting user's processes only |
| `tmux list-panes -a -F '#{pane_pid} #{session_name}'` | Which tmux session an agent runs in |
| The working directory of each process that may be claude or codex | A running task's directory when no state file gives one |
| `PANEMUX_PANE_ID` in the environment of each process that may be claude or codex (`/proc/<pid>/environ`) | The pane an agent outside tmux was started from ([below](#the-pane-of-an-agent-outside-tmux)) |

Any probe that is missing on a host (no tmux, no `~/.claude`, BSD `stat`) prints nothing rather than
failing the collection. Text a login shell prints before the script's output is ignored. Output that
ends before its terminating marker — a connection dropped mid-run — fails that host's collection.

### States

| State | Meaning | How it is decided |
|---|---|---|
| `wait` | Running and waiting for a person | Live claude process, `status: "waiting"`; `waitingFor` is shown as the reason |
| `busy` | Running and working | Live claude process, `status: "busy"` |
| `idle` | Running and waiting for the next instruction | Live claude process, `status: "idle"` |
| `run` | A codex process is running; no finer state is known | Live `codex` process |
| `unknown` | A claude session is running but its state cannot be read | A live claude process that no state file describes; a state file that is not JSON or lacks a pid or a valid session ID; or a live claude process reporting another `status` |
| `stop` | Nothing is handling the session | A conversation log with no live claude process for its session ID |

- **A process is a claude process when its program has a path component named `claude` or
  `claude-code`**: argv[0], or the script a `node`, `bun` or `deno` interpreter runs. A process that
  only has a file under `~/.claude` as an argument (an editor, `tail`, a hook) is not one.
  `claude -p` / `claude --print` is not a task.
- **A state file describes a running session only while its pid is a claude process.** A file whose
  process has gone, or whose pid now belongs to another process (pids restart after a host
  reboot), is not listed as running; its session shows up as stopped through its conversation log
  instead. `procStart` is not used.
- **`/resume` inside a running Claude Code switches the task in place.** The process and its
  `<pid>.json` stay the same and the file's `sessionId` becomes the resumed session, so the session
  switched away from is listed as stopped and the resumed one as running where the process runs.
  A normal exit removes the state file.
- A state file that cannot be read is shown as `unknown` while the pid in its name (`<pid>.json`)
  is a claude process, dropped when that pid is not, and kept when the name carries no pid.
- **A running claude process that no state file names is shown as `unknown`, not `stop`**, with its
  process's working directory, so a Claude Code release that moved or stopped writing the state
  files does not make every running agent look stopped.
- A running `unknown` claude task is taken to be writing the newest conversation log in its working
  directory: that log is not listed again as a stopped task.
- Two live state files for one session ID keep the one updated most recently.
- `stop` covers a host restart, a crash and a normal exit alike; the dashboard does not tell them
  apart. Whether a task's work is finished cannot be read off a process, so there is no "done" state.
- **Stopped sessions are limited to conversation logs changed in the last 7 days, and to the 50
  newest per host.** Subagent logs (`<session>/subagents/*.jsonl`) are not sessions of their own.
- The time a task entered its state is `statusUpdatedAt`, falling back to `updatedAt` and then
  `startedAt`; for a stopped task it is the log's modification time. Every host time is converted by
  its age against that host's own clock, so a host whose clock is wrong does not shift the dashboard.
  A time ahead of the host's clock is treated as "now".

### Where a task runs

| `location.kind` | Meaning |
|---|---|
| `tmux` | The agent's process, or one of its ancestors, is a tmux pane's process; `tmux_session` names the session |
| `outside` | The agent is running, but not under tmux |
| `none` | No process is running for the task |

`attachable` is true when a `tmux` / `ssh_tmux` pane can attach to `tmux_session`, whose name must
match the pane's tmux session name rule (`^[a-zA-Z0-9_.-]+$`).

`pane_id` is present only for `outside`: the pane the agent was started from, as its environment
names it ([below](#the-pane-of-an-agent-outside-tmux)).

The pane that belongs to a task is found in the browser from the current workspaces:

- In tmux: a `tmux` pane for a task on the panemux host, or an `ssh_tmux` pane on the task's
  connection, whose `tmux_session` is the task's tmux session.
- Outside tmux: the pane whose ID is `pane_id`, and only when it is a `local` pane for a task on the
  panemux host or an `ssh` pane on the task's connection. Any other pane with that ID is not the
  task's pane.

### The pane of an agent outside tmux

- `local` and `ssh` panes start their shell with `PANEMUX_PANE_ID` set to the pane's ID, whatever
  `url_open.browser_shim` says. Every process started from that shell inherits it, including one
  that is later detached from the shell.
- Only a pane ID matching `^[A-Za-z0-9_.-]{1,128}$` is set; a pane with any other ID gets no
  variable, and its agents cannot be found from the dashboard. IDs panemux generates
  (`pane-<time>-<random>`) always match.
- A `PANEMUX_PANE_ID` panemux itself inherited (panemux run inside one of its own panes) is removed
  from every local pane, so a pane never carries another pane's ID.
- An `ssh` pane that would otherwise use the SSH shell request runs a command instead, to export the
  variable, and execs the login shell as the browser shim does
  ([Browser-open interception](url-open.md#browser-open-interception)).
- `tmux` and `ssh_tmux` panes do not set it. An agent under tmux is located through tmux, and a
  `PANEMUX_PANE_ID` it carries — inherited by whatever started the tmux server — is ignored.
- Collection reads the variable from the agent's own process environment, not a parent's, and only
  on Linux hosts (`/proc/<pid>/environ`). On other hosts nothing is reported and an agent outside
  tmux cannot be opened. On macOS, `ps -E` and `ps eww` did not show the variable (see the
  [decision log](../DECISIONLOG.md#opening-an-agent-outside-tmux-through-panemux_pane_id-2026-09-26-issue-254)).
- The environment is the one the process was started with: changing the variable afterwards inside
  a running agent has no effect.
- The value is untrusted, since any process of the user can set it. Collection keeps only a value
  matching the rule above, and the browser opens only a pane of the matching kind and host that the
  workspaces hold.

### Repository, branch and pull request

The git metadata of each task's working directory is resolved the way a pane header's is: `git` on
the task's host (locally, or over the host's dashboard connection) and `gh pr view` on the panemux
host, with a remote repository named from its origin URL. A lookup runs once per (host, directory),
is cached for 30 seconds, and is skipped for a host whose collection failed. A directory that is not
a repository, or cannot be inspected, has no `git` field. `gh` runs under the request's context, so
an abandoned request stops it. Nothing a request looked up is cached when that request was
abandoned, since its lookups were stopped rather than answered.

`gh pr view` runs only for a directory a running task uses. A directory only stopped tasks use is
looked up with `git` alone, and that cached result does not serve a running task that appears in
the directory within the 30 seconds: the directory is looked up again, with its pull request.

The metadata is the directory's **current** state. For a running task that is the branch it is
working on; for a stopped task it is whatever has been checked out since, so a stopped task reports
only `repo` and `repo_url`, never `branch` or a pull request.

### Opening a task

| Task | Result |
|---|---|
| In tmux, with a pane already attached | The pane's workspace becomes active and the pane takes focus |
| In tmux, no pane yet, attachable | A `tmux` (panemux host) or `ssh_tmux` (its connection) pane attaching to the session is added at the right edge of the active workspace, through the ordinary `POST /api/sessions` and layout save; the pane's `tmux new-session -A` attaches to the running session |
| In tmux, not attachable | Not opened; the reason is shown |
| Outside tmux, in a `local` / `ssh` pane the workspaces hold | That pane's workspace becomes active and the pane takes focus |
| Outside tmux, naming a pane no workspace holds, or no pane | Not opened; the reason is shown |
| Stopped or unknown | Not opened |

Either way the dashboard closes and the pane is briefly outlined. Opening the same tmux session again
while its pane is still being created does not create a second pane.

The dashboard and the workspaces are switched with the `← Tasks` and `Workspaces` buttons or with
`Cmd/Ctrl+Shift+<display.task_dashboard_shortcut>` (`S` unless configured), from either layer.

### `GET /api/tasks`

Collects from every host and returns:

```json
{
  "hosts": [
    { "name": "", "status": "ok", "collected_at": "2026-09-25T12:00:00Z" },
    { "name": "gpu-box", "status": "error", "error": "connect to gpu-box: dial tcp: i/o timeout" }
  ],
  "tasks": [
    {
      "id": "local:claude:7c21e0a4",
      "host": "",
      "agent": "claude",
      "session_id": "7c21e0a4",
      "cwd": "/workspace/user/panemux",
      "state": "wait",
      "waiting_for": "input needed",
      "status_since": "2026-09-25T11:57:00Z",
      "started_at": "2026-09-25T11:15:00Z",
      "pid": 101,
      "location": { "kind": "tmux", "tmux_session": "task-7c21", "attachable": true },
      "git": { "repo": "panemux", "repo_url": "https://github.com/example/panemux", "branch": "main" }
    }
  ]
}
```

- `hosts` lists the panemux host first, then `ssh_connections` keys in name order. `status` is `ok`,
  `error` (with `error`) or `connecting`; `collected_at` is present when `status` is `ok`.
- `tasks` lists each host's running tasks in `id` order, then its stopped tasks newest first. It is
  `[]` when there are none.
- `id` is `local:<agent>:<key>` for the panemux host and `ssh:<host>:<agent>:<key>` for an SSH host,
  where the key is the session ID, `pid-<pid>` for codex and for a claude process no state file
  names, or `state-file:<file name>` for a state file that could not be read.
- `session_id`, `cwd`, `waiting_for`, `status_since`, `started_at`, `pid`, `git` and
  `location.pane_id` are omitted when unknown. `waiting_for` is present only in the `wait` state.
- The request answers `200` even when every host failed; failures are in `hosts`.
- Like every other route outside `/api/board/*`, it is not authenticated
  ([Current boundaries](../overview.md#current-boundaries)). Because it dials every host, it
  answers `403` to a request another site's page made: `Sec-Fetch-Site` of `cross-site` or
  `same-site`, or an `Origin` that is neither the server's own nor a loopback origin. A request with
  neither header (not from a browser page) is served.

### `POST /api/tasks/hosts/{name}/reconnect`

Drops the named host's dashboard connection and any remembered connection failure, so the next
`GET /api/tasks` dials it at once. `name` is an `ssh_connections` key. Returns `204`, `404` for a
name that is not one, or `403` for a cross-site request as above.
