# Behavior: task dashboard

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Task Dashboard

The task dashboard lists every coding-agent session on every host panemux knows — the panemux host
itself and every `ssh_connections` entry — independently of panes. A pane is only the window used to
watch or answer a task, opened when the dashboard is asked to. The design is issue
[#252](https://github.com/tomo-chan/panemux/issues/252); this page covers what is built (its stage 1,
the done and label records of [#256](https://github.com/tomo-chan/panemux/issues/256), and starting
and resuming tasks of [#257](https://github.com/tomo-chan/panemux/issues/257)).
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
and pull request of its working directory, and what a person recorded about it: whether it is done,
and its labels (see [Done and labels](#done-and-labels)).

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
  apart. Whether a task's work is finished cannot be read off a process, so no state means "done":
  done is what a person records ([Done and labels](#done-and-labels)), and it never changes `state`.
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

The pane that belongs to a task is found in the browser from the current workspaces: a `tmux` pane
for a task on the panemux host, or an `ssh_tmux` pane on the task's connection, whose
`tmux_session` is the task's tmux session.

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

### Done and labels

A person records two things about a task on the dashboard: that it is **done**, and its **labels**.
Nothing about a process says whether a task's work is finished, so done is only ever what a person
set.

- **Only a task with a session ID can carry a record.** The record belongs to the host, the agent
  and the session ID together, so the same session ID on two hosts is two tasks. A codex task and a
  claude process no state file names are known only by a pid, which the host reuses once the process
  exits; they cannot be marked done or labeled.
- **The records live in `~/.config/panemux/tasks.json` on the panemux host**, for every host's
  tasks, written through a temp file and a rename with mode `0600`. A symlink at that path is
  written through — its target gets the new contents and the link stays a link — as `config.yaml`
  is, including one whose target does not exist yet. The file holds only tasks that carry
  something: a task marked not done with no labels is removed from it.
- **Records are kept until a person clears them.** A task that leaves the list — its conversation
  log deleted, older than 7 days, or beyond the 50 newest — keeps its record, which is not shown
  anywhere and applies again if the session is listed again. Such a record is cleared by editing the
  file, which can be done while panemux runs (below). A host removed from `ssh_connections` keeps
  its records too; `PUT /api/tasks/records` still clears them, but adds none.
- **A task marked done is in the Done column only while it is stopped.** One that runs again (a
  `/resume`, `claude --resume`, or the dashboard's `Resume`) shows the state it is in, still marked done, and returns to Done
  when it stops. The record is not cleared automatically; `Mark not done` clears it.
- **Labels** are trimmed, repeats dropped, and kept in the order they were added. A label is at most
  32 characters, and has no control characters, no invisible format characters (zero-width spaces,
  direction overrides — Unicode category Cf) and no line or paragraph separators; a task carries at
  most 20. Emoji joined with a zero-width joiner (a family emoji, say) are refused with them, since
  the joiner is itself a format character. Case matters: `Docs` and `docs` are two labels.
- The file is served from memory while its modification time and size are what panemux last read
  or wrote, and read again when they change, so an edit made by hand while panemux runs is seen by
  the next request and is not undone by the next save. An edit that keeps both the same (within the
  filesystem's timestamp resolution, at the same size) is not noticed. A file that cannot be read —
  not JSON, another format version, an invalid entry — is reported by `GET /api/tasks` as
  `records_error`, the tasks are listed without records, and every write fails until the file is
  fixed, so a file panemux does not understand is never replaced. A deleted file is no records.

### Starting a task

`New task` starts one claude session on a host, in a detached tmux session of its own. No pane is
created: the task is opened from the dashboard like any other, when someone wants to watch it.

| Field | Rule |
|---|---|
| Host | The panemux host or an `ssh_connections` key |
| Working directory | An absolute path with no shell metacharacters or control characters — the rule a pane's remote `cwd` follows ([Remote path arguments](../security/command-execution.md#remote-path-arguments-ssh-working-directory)) — that exists on the host |
| Agent | `claude` only. Codex tasks are known only by a pid, so they could carry no labels ([#264](https://github.com/tomo-chan/panemux/issues/264)) |
| Labels | Optional, comma-separated in the form, under the rules of [Done and labels](#done-and-labels) |
| First instruction | Required. Surrounding blank space is dropped and line endings become LF; at most 32 KiB after that, and no NUL |

- **panemux mints the session ID** (a version 4 UUID) and runs
  `claude --session-id=<id> -- <first instruction>` in the working directory, so the task — and its
  labels — are known before claude has written anything. The instruction is claude's single
  argument after `--`: one that begins with `-` is still the instruction. Slash commands are not
  disabled; the session is the operator's own.
- **claude is started in the working directory by the command inside the tmux session**, not by
  tmux's `-c`: tmux expands `-c`'s value as a format, so a directory holding `#` (`#S`, `##`) would
  have become a different path, and tmux starts in the home directory when that path does not
  exist — while reporting success.
- **The tmux session is named `task-` and the first eight characters of the session ID.** If a tmux
  session of that name already exists on the host, the task is not started, and the existing
  session is neither attached nor replaced.
- **When claude exits, its tmux session ends**, and the task is listed as stopped.
- **The labels are recorded right after the task starts**, under the host, `claude` and the new
  session ID. They are checked before anything starts, so an invalid label refuses the whole request.
  If the record file cannot be written the task is still running; the response says why its labels
  were not recorded, and the dashboard shows it.
- **The host needs tmux and claude.** tmux receives the command as separate arguments (tmux 2.0 and
  later; verified with tmux 3.4). claude is looked up on the `PATH` of the `sh` that runs the launch,
  and, when it is not there, through the user's login shell (`$SHELL -lc 'command -v claude'`), since
  an SSH exec channel's `PATH` rarely includes a per-user install such as `~/.local/bin`. Only an
  absolute path to an executable file is run.
- **The first instruction reaches claude through a file, not a command line.** It is written to a
  mode-`0600` temporary file on the host (`$TMPDIR`, else `/tmp`, named `panemux-task.XXXXXXXX`),
  which the command inside the tmux session reads and deletes before claude starts; if tmux fails to
  start the session, the launch deletes it. It is never part of the SSH command, of a string any
  shell parses, or of tmux's arguments. A tmux server keeps the arguments of the command that started
  it as its own process arguments for as long as it runs, so an instruction passed there would stay
  visible in `ps`. It is claude's own argument, though, so anyone who can list claude's process
  arguments on the host can read it while claude runs.
- After a start, the dashboard selects the task once a collection lists it. claude writes the state
  file the collection reads only once it is running, so that can take until the next poll (10 s).

Checked against a real tmux 3.4 with a stand-in for claude: an instruction holding a leading option,
command substitutions, quotes and newlines arrived as one argument after `--`, nothing in it ran, and
the temporary file was gone. **Not checked against a real Claude Code**: that the first instruction is
submitted as the first message, and that the state file names the minted session ID (the development
environment's claude stopped at its first-run screen). Scenario J21 is the manual check.

### Resuming a task

`Resume` is offered on a stopped claude task whose session ID is a UUID.

- **The session must be listed as a stopped claude task on its host at that moment.** The host is
  collected again first; a session that is running, is not claude's, or is not listed there is not
  resumed. The ID passed to claude therefore always came from the host's own conversation logs.
- **It runs `claude --resume=<id>`** in the working directory that session's conversation log
  records, in a new detached tmux session named like a new task's: `task-` and the first eight
  characters of the session ID. A session whose log records no working directory, or one the
  remote-path rule refuses, is not resumed.
- **A tmux session of that name that no agent runs in gets claude as a new window.** A pane opened
  on the task attaches with `tmux new-session -A`, so when that pane is recreated after claude has
  exited — a `Reconnect`, or the automatic reconnect after an SSH drop — it creates a session of the
  task's name holding a shell. When the collection the resume makes finds no running task (claude,
  codex, or one whose state could not be read) inside that session, claude is started as a new window
  of it: the shell's window is left as it is, and the attached pane shows claude. When a running task
  is inside it, the resume is refused, as a new task is refused whenever the name is taken.
- **The session ID is passed in the `=` form.** `--resume` takes an optional value, and a separate
  argument beginning with `-` would be read as an option; `--resume` also accepts a session title,
  which is why only a UUID is accepted.
- **The task keeps its session ID**, so its record is unchanged: a task marked done that is resumed
  stays marked done and shows the state it runs in, and returns to Done when it stops.
- A host restart stops tmux as well as claude, so resuming creates a new tmux session — or, when a
  pane was reconnected first, adds claude to the session that pane created.
- The dashboard selects the resumed task, and it shows as running once a collection finds it.

### Opening a task

| Task | Result |
|---|---|
| In tmux, with a pane already attached | The pane's workspace becomes active and the pane takes focus |
| In tmux, no pane yet, attachable | A `tmux` (panemux host) or `ssh_tmux` (its connection) pane attaching to the session is added at the right edge of the active workspace, through the ordinary `POST /api/sessions` and layout save; the pane's `tmux new-session -A` attaches to the running session |
| In tmux, not attachable | Not opened; the reason is shown |
| Outside tmux | Not opened; the reason is shown |
| Stopped | Not opened; a stopped claude task offers `Resume` ([Resuming a task](#resuming-a-task)) |
| Unknown | Not opened |

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
      "git": { "repo": "panemux", "repo_url": "https://github.com/example/panemux", "branch": "main" },
      "labels": ["dashboard", "enhancement"],
      "done": true
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
- `session_id`, `cwd`, `waiting_for`, `status_since`, `started_at`, `pid` and `git` are omitted when
  unknown. `waiting_for` is present only in the `wait` state.
- `done` and `labels` are the task's record, omitted when it is not done or has no labels.
- `records_error` is present only when the record file could not be read; the tasks are then listed
  without records.
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

### `PUT /api/tasks/records`

Replaces the record of one task:

```json
{ "host": "", "agent": "claude", "session_id": "7c21e0a4", "done": true, "labels": ["dashboard"] }
```

- `host` is `""` for the panemux host or an `ssh_connections` key; `agent` is `claude` or `codex`;
  `session_id` must match `^[a-zA-Z0-9_-]+$`. Every field is replaced: a request without `labels`
  clears them. An unknown field is refused.
- Answers `200` with the record as stored — labels normalized, both `done` and `labels` always
  present. `400` for a body or record that is not valid (the reason is in the body), `404` for a
  `host` that is not an `ssh_connections` key — except on a request that clears the record
  (`done` false and no labels), which is accepted for any host so a removed host's records can be
  cleared — `403` for a cross-site request as above, and `500` when the file cannot be read or
  written; nothing is changed then.
- It does not collect: the dashboard applies the answer to the task at once, and ignores a
  `GET /api/tasks` that was already running when the record was saved.

### `POST /api/tasks`

Starts a task:

```json
{ "host": "", "agent": "claude", "cwd": "/workspace/user/project", "prompt": "Fix the flaky test", "labels": ["payment"] }
```

- Every field but `labels` is required in effect: `agent` must be `claude`, and `cwd` and `prompt`
  follow [Starting a task](#starting-a-task). An unknown field is refused.
- Answers `201`:

  ```json
  { "id": "local:claude:0f0e0d0c-0b0a-4908-8706-050403020100", "session_id": "0f0e0d0c-0b0a-4908-8706-050403020100", "tmux_session": "task-0f0e0d0c", "labels": ["payment"] }
  ```

  `id` is the one `GET /api/tasks` lists the task under. `labels` is what was recorded, omitted when
  none were given; `records_error` is present instead when they could not be recorded.
- `400` for a body, agent, label, directory or instruction that is not valid, `404` for a `host`
  that is not an `ssh_connections` key, `409` when the host refused — tmux or claude missing, the
  directory missing, a tmux session of that name existing, tmux failing, the temporary file not
  written, each with a fixed message — `502` when the host could not be reached or did not answer,
  and `403` for a cross-site request as for `GET /api/tasks`.

### `POST /api/tasks/resume`

Resumes a stopped claude task:

```json
{ "host": "", "session_id": "5d7e3a90-1b2c-4d3e-8f40-51627384a5b6" }
```

- Answers `200` with `id`, `session_id` and `tmux_session` as above.
- `400` for a body that is not valid or a `session_id` that is not a UUID, or a stopped session whose
  working directory is unknown or refused; `404` for an unknown `host`; `409` when the session is not
  a stopped claude task on the host, or the host refused as above; `502` when the host could not be
  collected or reached; `403` for a cross-site request.
