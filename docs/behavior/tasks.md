# Behavior: task dashboard

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## Task Dashboard

The task dashboard lists every coding-agent session on every host panemux knows — the panemux host
itself and every `ssh_connections` entry — independently of panes. A pane is only the window used to
watch or answer a task, opened when the dashboard is asked to. The design is issue
[#252](https://github.com/tomo-chan/panemux/issues/252); this page covers what is built (its stage 1,
opening an agent outside tmux from issue [#254](https://github.com/tomo-chan/panemux/issues/254) and
on macOS hosts from issue [#263](https://github.com/tomo-chan/panemux/issues/263),
issue and reference links from issue [#255](https://github.com/tomo-chan/panemux/issues/255),
the done and label records of [#256](https://github.com/tomo-chan/panemux/issues/256), starting
and resuming tasks of [#257](https://github.com/tomo-chan/panemux/issues/257), the summaries of
[#258](https://github.com/tomo-chan/panemux/issues/258), and codex sessions of
[#264](https://github.com/tomo-chan/panemux/issues/264)).
The UI is described in [UI design's Task Dashboard](../ui-design.md#task-dashboard).

### What a task is

A task is one agent session:

- **claude**: one Claude Code session, identified by its session ID.
- **codex**: one codex session, identified by its session ID — the UUID in its rollout's file name,
  `~/.codex/sessions/YYYY/MM/DD/rollout-<time>-<session ID>.jsonl`. A running codex process that has
  no rollout yet (see [Codex sessions](#codex-sessions)) is a task identified by its pid. A process
  is interactive when its first positional argument is absent, a prompt, `resume` or `fork`; every
  other codex-cli subcommand (`exec`, `review`, `app-server`, `mcp`, `login` and the rest listed by
  `codex --help` of codex-cli 0.157.0) is not a task. Options that take a value are skipped with it.

A task carries its host, agent, session ID, working directory, state, the time it entered that
state, the reason it is waiting, its start time, its pid, where it runs, and the repository, branch
and pull request of its working directory, what a person recorded about it: whether it is done,
and its labels (see [Done and labels](#done-and-labels)), and, when summaries are enabled, a summary
of its conversation (see [Summaries](#summaries)).

### Hosts and connections

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
- Each SSH host gets **one** connection for the dashboard, opened with the same dialer panes use
  (ProxyJump, ProxyCommand and `known_hosts` verification included) and reused by every later
  collection. It is never shared with a pane, and opening or closing panes does not affect it.
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
- A host removed from `ssh_connections` has its connection closed on the next collection. Every
  connection is closed when panemux shuts down.
- One host failing never hides another host's tasks.

### Collection

Collection runs only when the dashboard asks for it — every 10 seconds while the dashboard is on
screen and the page is visible, and on the Refresh button — or while the
[task event stream](task-events.md) has a subscriber, which observes the running tasks on every host
every 5 seconds. Nothing collects when neither asks.

The dashboard shows a running task's `state`, `waiting_for`, wait and `status_since` from the task
event stream whenever the stream is live and has that task, so a change appears when it is published rather than at the
dashboard's next collection ([Task dashboard](task-events.md#task-dashboard)).

Each collection runs one fixed script per host (`sh -s`, with the script on stdin; see
[Task dashboard collection](../security/command-execution.md#task-dashboard-collection)). The script
reads only what the agents write themselves and what the host reports about its processes:

| Read | Used for |
|---|---|
| `~/.claude/sessions/*.json` | Running Claude Code sessions: `pid`, `sessionId`, `cwd`, `status`, `waitingFor`, `statusUpdatedAt`, `updatedAt`, `startedAt` |
| `~/.claude/projects/*/*.jsonl` changed in the last 7 days (newest 100) | Stopped sessions, the first `"cwd"` recorded in each, and each log's modification time and size (what a summary is keyed on) |
| `ps -U <own uid> -o pid=,ppid=,command=` | Whether a state file's process is alive, running agents, and parent chains — the collecting user's processes only |
| `tmux list-panes -a -F '#{pane_pid} #{session_name}'` | Which tmux session an agent runs in |
| The working directory of each process that may be claude or codex | A running task's directory when no state file gives one |
| `PANEMUX_PANE_ID` in the environment of each process that may be claude or codex (`/proc/<pid>/environ` on Linux, `ps -E` on macOS) | The pane an agent outside tmux was started from ([below](#the-pane-of-an-agent-outside-tmux)) |
| `ps -U <own uid> -o pid=,etime=,command=`, then each `codex` process's open files (`/proc/<pid>/fd`, or `lsof` where there is none) | How long each codex process has run, and the rollouts it holds open |
| For each rollout a codex process holds open: its modification time and size, the `cwd` of its first line, the last `task_started` / `task_complete` / `turn_aborted` and the last `response_item` (with the `timestamp` that line starts with) in its final MiB, and the newest `thread_turns` row in `~/.codex/thread_history_1.sqlite` (with `sqlite3 -readonly`, when installed) | A running codex session's state |
| `~/.codex/sessions/*/*/*/rollout-*.jsonl` changed in the last 7 days whose first line's `originator` is `codex-tui` (the newest 100 of those), and that line's `cwd` | Stopped codex sessions |

The task event stream's collection ([Task events](task-events.md#lifecycle)) runs the same script
without its two searches for stopped sessions: it reads every row above except the conversation logs
under `~/.claude/projects` and the rollouts under `~/.codex/sessions`, and lists the running tasks
only. The part both run is one shared constant, so a running task is found and its state decided the
same way by both.

Any probe that is missing on a host (no tmux, no `~/.claude`, no `~/.codex`, no `sqlite3`, BSD
`stat`) prints nothing rather than failing the collection. Codex's files are read under `$HOME/.codex`;
a `CODEX_HOME` set elsewhere is not followed (its running sessions are still found through their open
rollouts, but not their `thread_turns` or their stopped sessions). Text a login shell prints before the script's output is ignored. Output that
ends before its terminating marker — a connection dropped mid-run — fails that host's collection.

### States

| State | Meaning | How it is decided |
|---|---|---|
| `wait` | Running and waiting for a person | Live claude process, `status: "waiting"`; `waitingFor` is shown as the reason |
| `busy` | Running and working | Live claude process, `status: "busy"` |
| `idle` | Running and waiting for the next instruction | Live claude process, `status: "idle"` |
| `run` | A codex process is running and has no session yet | A live interactive `codex` process holding no rollout ([Codex sessions](#codex-sessions)) |
| `unknown` | A session is running but its state cannot be read | A live claude process that no state file describes; a state file that is not JSON or lacks a pid or a valid session ID; a live claude process reporting another `status`; or a codex session whose `thread_turns` and rollout tail say nothing about its newest turn |
| `stop` | Nothing is handling the session | A conversation log with no live claude process for its session ID, or an interactive codex rollout that no live codex process holds open |

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
  newest per host, claude's and codex's together.** Subagent logs (`<session>/subagents/*.jsonl`) are
  not sessions of their own.
- The time a task entered its state is `statusUpdatedAt`, falling back to `updatedAt` and then
  `startedAt`; for a stopped task it is the log's modification time. Every host time is converted by
  its age against that host's own clock, so a host whose clock is wrong does not shift the dashboard.
  A time ahead of the host's clock is treated as "now".

### Wait signature

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
  second question in the same turn is a new wait.
- It never comes from panemux's clock, the collection time, the clock conversion, a rollout's
  modification time or a turn's start alone. A `wait` task without a valid wait start has no
  signature; one is never made up. Unknown or unreadable state is `unknown` or `run` as above, never a
  signed wait.

### Codex sessions

Codex writes no file that ties a process to its session or says what it is doing (checked with
codex-cli 0.142.2 and 0.157.1 on macOS and 0.157.1 on Linux; the results are on
[#264](https://github.com/tomo-chan/panemux/issues/264)). The dashboard therefore reads what codex
does write:

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
  the last `task_started`, `task_complete` or `turn_aborted` in the rollout's final MiB does.

  | Newest turn | State | Since |
  |---|---|---|
  | `inProgress` / `task_started`, and the last `response_item` is a `request_user_input` call | `wait`, waiting for "a question from codex" | The rollout's last write |
  | `inProgress` / `task_started`, otherwise | `busy` | The turn's start |
  | `completed`, `interrupted`, `failed` / `task_complete`, `turn_aborted` | `idle` | The rollout's last write |
  | Nothing, or a `thread_turns` status this build does not know and no turn event | `unknown` | — |

- **Waiting for approval shows as `busy`.** Codex records nothing when it asks to run a command: the
  turn is in progress and the last item is the command's call, exactly as while the command runs.
  `request_user_input` — codex's question to the person, offered only in Plan mode — is told apart,
  because its call stays the last item until it is answered.
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
- **A session codex's shared daemon runs is a task of its own, which no pane can be opened for.**
  From codex-cli 0.157 a TUI started without `-c` or `--no-daemon` starts (or joins) a shared
  `codex app-server` daemon, and the daemon, not the TUI, holds the session's rollout. Nothing on
  the host ties the TUI's process to that session: not its descriptors (the TUI holds only a socket
  to the daemon), and not `~/.codex/logs_2.sqlite`, where the session's entries carry only the
  daemon's pid (checked with codex-cli 0.157.1 on Linux). Each TUI session the daemon holds
  (`originator` `codex-tui`) is therefore listed under its session ID, with its state read as above
  — the leftover check using the daemon's age — its location `daemon`, and the daemon's pid; the TUI
  itself is the `run` task above. One piece of work thus shows as two cards. The daemon keeps a
  session's rollout, and its writer lock, open after the TUI exits (for over a minute when checked),
  so the session stays listed there, `idle`, rather than as stopped, and is not offered `Resume`
  while it does. Sessions the daemon holds for other clients (the desktop app) are not listed.
- **Stopped codex sessions are the TUI's.** A rollout's first line (`session_meta`) says where the
  session came from; only `"originator":"codex-tui"` is listed — the TUI's sessions, whether it wrote
  the rollout itself (`"source":"cli"`) or through the daemon (`"source":"vscode"`) — not `codex exec`
  (`codex_exec`), the desktop app or a subagent. The 100 the collection reads are counted after this
  filter, so a host where `codex exec` writes many rollouts does not push the TUI's out. Its working
  directory is that line's `cwd`, and the time it stopped is the rollout's modification time.
- Codex tasks with a collected rollout can be summarized by Codex ([Summaries](#summaries)).

### Where a task runs

| `location.kind` | Meaning |
|---|---|
| `tmux` | The agent's process, or one of its ancestors, is a tmux pane's process; `tmux_session` names the session |
| `outside` | The agent is running, but not under tmux |
| `daemon` | A codex session run by codex's shared daemon; which pane shows it cannot be told ([Codex sessions](#codex-sessions)) |
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
- Collection reads the variable from the agent's own process environment, not a parent's: on Linux
  hosts from `/proc/<pid>/environ`, and on macOS hosts from `ps -E`. On other hosts nothing is
  reported and an agent outside tmux cannot be opened.
- The macOS reading has been tested only by replaying `ps -E` output recorded on macOS 26.3.1
  through a fake `ps`. It has not yet been run on a macOS host, over SSH to one, or on a macOS
  release before 26 (see the
  [decision log](../DECISIONLOG.md#reading-panemux_pane_id-on-macos-through-ps--e-2026-09-27-issue-263)).
- On macOS, `ps -E -p <pid> -o command=` prints the process's arguments, a space, and its
  environment's `NAME=value` entries joined by spaces. Collection removes the arguments
  `ps -p <pid> -o command=` prints from the front of that output, and reads the rest only when it
  starts with a space. It takes the value only when exactly one ` PANEMUX_PANE_ID=` is left, up to
  the next space. Two or more are not read, since one may be part of another variable's value; the
  agent then has no pane ID. An argument such as `PANEMUX_PANE_ID=…` is never read as the variable.
- When the agent has no `PANEMUX_PANE_ID` of its own and exactly one ` PANEMUX_PANE_ID=<id>` sits
  inside another variable's value, preceded by a space (which may follow a newline), macOS reads
  `<id>` as its pane ID; directly after a newline with no space it is not read. Linux reads
  neither. This is accepted: the value is only a claim, and the browser opens only a matching
  pane (see [Task dashboard collection](../security/command-execution.md#task-dashboard-collection)).
- Because `ps -E` does not delimit values, a macOS value containing a space is read up to that
  space, where Linux would reject it. Values panemux sets never contain one.
- macOS shows no environment for Apple's own binaries (`/bin/sleep`, `/bin/zsh`) or for another
  user's processes. An agent that is such a process reports no pane ID. claude, codex (its `node`
  wrapper and its native binary) and `node` show theirs (see the
  [decision log](../DECISIONLOG.md#reading-panemux_pane_id-on-macos-through-ps--e-2026-09-27-issue-263)).
- The environment is the one the process was started with: changing the variable afterwards inside
  a running agent has no effect.
- The value is untrusted, since any process of the user can set it. Collection keeps only a value
  matching the rule above, and the browser opens only a pane of the matching kind and host that the
  workspaces hold.

### Repository, branch, pull request, issues and references

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
only `repo` and `repo_url`, never `branch`, a pull request, issues or references.

- **Issues** are the ones the pull request closes: the same `gh pr view` call reads
  `closingIssuesReferences` along with the pull request's URL, number and title, so no further
  command runs. No pull request means no issues. An issue whose URL is not `http`/`https` or whose
  number is not positive is left out. Issue titles are not shown. The field needs `gh` 2.72.0 or
  later. An older `gh` refuses the whole call (`Unknown JSON field`), so the pull request is looked
  up again with only its URL and number, as a pane header does: the PR link stays, and the task has
  no issues and no references from the PR title.
- **References** are what `task_dashboard.autolinks` finds in the branch name and then the pull
  request title. Each entry works like a GitHub repository's autolink reference: its `key_prefix`
  followed by an identifier links to its `url_template` with every `<num>` replaced by the
  identifier. With `key_prefix: JIRA-` and `url_template: https://jira.example.com/JIRA-<num>`,
  `JIRA-123` links to `https://jira.example.com/JIRA-123`.
  - The identifier is digits, or with `is_alphanumeric: true` the letters `A`–`Z` in either case,
    digits and `-`, taking as many as follow the prefix. An alphanumeric identifier therefore runs
    on through a branch name's words: `TICKET-12-retry` gives `12-retry`.
  - The prefix matches exactly as written, case included, and not when an ASCII letter or digit
    comes directly before it. A numeric identifier does not match when a letter or digit follows
    it. `JIRA-418-retry-backoff` gives `JIRA-418`; `xJIRA-418`, `JIRA-418a` and `jira-418` give
    nothing.
  - Every reference is listed once, in the order found, with the text that matched (`JIRA-123`).
  - Only the configured prefixes match, so text that merely looks like a ticket key (`UTF-8`,
    `CVE-2024-45337`) is not linked unless its prefix is configured.
  - Without `task_dashboard.autolinks` there are no references. No issue tracker is ever contacted.

### Done and labels

A person records two things about a task on the dashboard: that it is **done**, and its **labels**.
Nothing about a process says whether a task's work is finished, so done is only ever what a person
set.

- **Only a task with a session ID can carry a record.** The record belongs to the host, the agent
  and the session ID together, so the same session ID on two hosts is two tasks. A codex process
  that has not started its session yet and a claude process no state file names are known only by a
  pid, which the host reuses once the process exits; they cannot be marked done or labeled. A codex
  session, running or stopped, can.
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

`New task` starts one claude or codex session on a host, in a detached tmux session of its own. No
pane is created: the task is opened from the dashboard like any other, when someone wants to watch
it.

| Field | Rule |
|---|---|
| Host | The panemux host or an `ssh_connections` key |
| Working directory | An absolute path with no shell metacharacters or control characters — the rule a pane's remote `cwd` follows ([Remote path arguments](../security/command-execution.md#remote-path-arguments-ssh-working-directory)) — that exists on the host |
| Agent | `claude` or `codex` |
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

A **codex** task starts the same way, with these differences:

- **Codex picks its own session ID**, once it has been given its first instruction; nothing on its
  command line sets it. The tmux session is named `task-` and eight random hex digits, and the
  response names only that tmux session.
- **It runs `codex --no-daemon -c check_for_update_on_startup=false -- <first instruction>`** in the
  working directory. The instruction follows `--`, so one that begins with `-` is still the
  instruction (codex reads an unguarded leading `-` as an option). `--no-daemon` keeps the session in
  the TUI's own process, where its rollout ties it to the task's tmux session, rather than in codex's
  shared daemon ([Codex sessions](#codex-sessions)). The update check is turned off because its
  prompt would hold the task; codex's configuration is not otherwise touched.
- **A codex without `--no-daemon` is refused.** Before anything starts, the launch reads
  `codex --help`; when it lists no `--no-daemon`, the host refuses with "codex on the host is too
  old: it has no --no-daemon option", and no tmux session is started. codex-cli 0.142.2 has no such
  option and exits 2 on it, so it would have ended the tmux session at once while the launch
  reported success; 0.157.1 has it. The version that added it has not been checked, so the help,
  not a version number, decides. A resume is refused the same way.
- **codex's own directory is added at the end of `PATH`.** An npm install's `codex` is a
  `#!/usr/bin/env node` script with node beside it, and a tmux server started from an SSH exec
  channel does not have that directory on its `PATH`. At the end, it supplies node when nothing
  earlier does and changes nothing else: the commands codex runs resolve as they would without it.
  The other side of that: **a `node` earlier on that `PATH` is the one that runs codex** — an old
  system `/usr/bin/node` on a host where codex was installed with nvm's node, say. Such a codex fails
  as it starts, and the tmux session ends; install codex with the `node` the exec channel's `PATH`
  finds first, or put its directory before that `node` in the login shell's `PATH`. The `codex
  --help` check above runs the same way, so a `node` that cannot run codex at all is reported as a
  codex too old.
- **Start-up screens are answered in the pane.** Codex can stop before the first instruction to ask
  whether to trust the directory, to announce a new model, or — once a usage limit is near — to
  offer a cheaper model. panemux does not answer them or write codex's configuration to avoid them.
  Until they are answered the task shows as `run` in its tmux session with no session
  ([Codex sessions](#codex-sessions)), and the detail panel says to open the pane.
- **The labels are recorded once codex's session is known.** They are checked before anything
  starts, held in panemux's memory under the host and the tmux session, and recorded — added to any
  labels the session already has — by the first `GET /api/tasks` that finds a codex session in that
  tmux session. They are dropped when panemux restarts before then, when the host is removed from
  `ssh_connections`, or when, a minute or more after the start, a collection of the host finds no
  task in that tmux session (codex quit at a start-up screen, or the session was killed). A record
  file that cannot be written keeps them for the next collection.
- **The dashboard selects the task in its tmux session**: as its process while it has no session,
  and again as its session once it has one.

Checked on Linux with a real tmux 3.4 and codex-cli 0.157.1 against a stand-in model server: a
dashboard start from a `PATH` without node, found through the login shell, stopped at the
directory-trust screen and was listed as `run` in its tmux session; once trusted, its instruction
`-h hello from the dashboard` was sent as the first message and the task was listed under its session,
`idle`, then `busy` during a command, then `wait` on a Plan-mode question. With `--no-daemon` added
and codex's directory at the end of `PATH`, a start from a `PATH` without node was again held by the
TUI itself (listed in its tmux session), the `PATH` its commands saw ended with codex's directory,
and a resume after `/exit` came back `idle` in `task-<last eight>`. A plain `codex` started in a pane
while the daemon ran showed as a `run` task in that pane and its session as a separate `idle` task
with location `daemon`.

Checked against a real tmux 3.4 with a stand-in for claude: an instruction holding a leading option,
command substitutions, quotes and newlines arrived as one argument after `--`, nothing in it ran, and
the temporary file was gone. **Not checked against a real Claude Code**: that the first instruction is
submitted as the first message, and that the state file names the minted session ID (the development
environment's claude stopped at its first-run screen). Scenario J21 is the manual check.

### Resuming a task

`Resume` is offered on a stopped claude or codex task whose session ID is a UUID.

- **The session must be listed as a stopped task of that agent on its host at that moment.** The host
  is collected again first; a session that is running, is another agent's, or is not listed there is
  not resumed. The ID passed to the agent therefore always came from the host's own conversation logs
  or rollouts.
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
- **Two resumes of the same task run one after the other.** A second resume of the same session on
  the same host waits until the first has started claude, and only then collects; it then finds that
  claude in the session and is refused, so one conversation never gets two claude processes from
  the dashboard. This covers one panemux process: two panemux instances, or a resume typed by hand on
  the host, are not coordinated with it. A resume whose request ends while it waits gives up.
- **The session ID is passed in the `=` form.** `--resume` takes an optional value, and a separate
  argument beginning with `-` would be read as an option; `--resume` also accepts a session title,
  which is why only a UUID is accepted.
- **The task keeps its session ID**, so its record is unchanged: a task marked done that is resumed
  stays marked done and shows the state it runs in, and returns to Done when it stops.
- A host restart stops tmux as well as claude, so resuming creates a new tmux session — or, when a
  pane was reconnected first, adds claude to the session that pane created.
- The dashboard selects the resumed task, and it shows as running once a collection finds it.
- **A codex task runs `codex --no-daemon -c check_for_update_on_startup=false resume -- <id>`** in the
  working directory its rollout's first line records, with codex's directory at the end of `PATH` as
  for a new task. The ID follows `--`: without it, `codex resume <id> '-h …'` prints its help and exits 0.
  `codex resume` also accepts a session name, which is why only a UUID is accepted. The tmux session
  is named `task-` and the **last** eight characters of the session ID: codex's IDs are version 7
  UUIDs, which begin with their creation time, so sessions started within a minute share their first
  eight. A resume sends no instruction; a codex session killed mid-turn is `idle` once resumed
  ([Codex sessions](#codex-sessions)). Checked on Linux: a session killed while busy was listed as
  stopped, resumed into `task-<last eight>`, and listed there as `idle`.

### Summaries

With `task_dashboard.summary.enabled: true` in `config.yaml`, each Claude or Codex task with a collected log gets a summary of what
it is doing and the work left, made by its own CLI on the panemux host. **Summaries are off by
default**, because they send conversation excerpts to the corresponding agent account on the panemux host.

```yaml
task_dashboard:
  summary:
    enabled: true
```

- **Which tasks.** A Claude or Codex task with a session ID whose conversation log the collection listed (the
  last 7 days, the newest 100 per host). Processes known only by a pid have no summary.
- **What is read.** The task's log, `~/.claude/projects/*/<session ID>.jsonl`, is read on its host by
  a fixed script run like the collection's ([Task summaries](../security/command-execution.md#task-summaries)).
  When the session has a log in more than one project directory, the one read is chosen as the
  collection orders them — newest first, then the larger of two changed in the same second — so it
  is the one whose modification time and size the summary is keyed on. The host sends the whole log when it is at most
  2.25 MiB, and otherwise its first 256 KiB and its last 2 MiB.
- **What Claude is given.** Each line is read as a JSON object, and only the text of the user's and
  the assistant's messages is kept: a line whose `type` is `user` or `assistant` and whose
  `message.content` is a string or holds `text` blocks, and which is not a subagent's
  (`isSidechain`). Tool calls, tool results, thinking, attachments and every other kind of line are
  never read, and a line cut by a byte limit is skipped. Of those messages claude is given the
  session's first user message (at most 4 KiB), which says what the task is, and the newest messages
  up to 24 KiB in all, each cut at 2 KiB, which say where it stands.
- **Where Claude text goes.** That text is sent to `claude -p` on the panemux host, so a remote host's
  conversation goes to the Claude account signed in on the panemux host, not the remote host's. It is
  not masked: a secret typed into the conversation, or quoted in a reply, is sent with it. Tool output
  — where file contents and command output sit — is not. Summaries are kept in panemux's memory only
  and are gone when it restarts.
- **A log that cannot be read.** When no line of a log has the supported conversation shape, the task's summary is
  `unreadable` and nothing is sent to its agent; the raw log is never sent instead. An agent release
  that changes the log's format shows up this way rather than as a wrong summary. Asking again reads
  nothing until the log changes, so the detail panel's button is disabled until then; once the log
  has changed the summary is marked outdated, the button is enabled again, and selecting a stopped
  or `unknown` task asks for it.
- **When a summary is made.**
  - A running task that waits for input or is idle — `wait` or `idle` — is summarized when a
    collection finds its log at a modification time and size it was not yet summarized at.
  - A `busy` task is not: its log changes all the time. Nor is an `unknown` one, whose state could
    not be read and which may be working. Its last summary is shown, marked outdated.
  - A stopped or `unknown` task is summarized when it is selected on the dashboard and has no
    current summary.
  - `Summarize` / `Summarize again` in the detail panel asks for any task that can have one.
  - A summary is cached separately by host, agent and session and reused while its log keeps the same modification time, size and (for Codex) rollout name, so the 10-second
    poll does not summarize again. A failed summary is not retried by the poll for the same log; the
    button retries it.
  - At most two summaries run at once, across hosts. Reading a log is limited to 15 seconds and one
    agent CLI run to 2 minutes.
  - A task that leaves the list takes its summary with it, and so does a host removed from
    `ssh_connections`. A host whose collection failed keeps what it had.
- **The answer.** A summary of one or two sentences (at most 1 KiB) and the remaining work, most
  immediate first (at most 10 items of 300 bytes), in the language of the conversation. A current,
  ready summary with nothing remaining makes the task a **done candidate**; that is only shown — done
  is still what a person records ([Done and labels](#done-and-labels)).
- **How claude runs.** `claude` is found on the panemux process's `PATH` and run in an empty
  temporary directory, without a shell, as
  `claude -p --session-id <minted UUID> --no-session-persistence --output-format=json --json-schema <schema> --strict-mcp-config --setting-sources "" --disable-slash-commands --disallowedTools=<every acting tool> -- <fixed instruction>`,
  with the excerpt on its standard input. The instruction tells it the excerpt is data to describe,
  not instructions. When claude fails, the task reports a fixed message (its exit status, a timeout,
  an answer that was not JSON or had no summary); nothing claude printed is passed on, since it can
  quote the conversation.

#### Codex summaries

Codex tasks use a separate `codex exec` summary runner and standard rollout reader;
Claude tasks retain the `claude -p` path described above. The Codex runner retains the
panemux host operator's authentication, default model and user/global/managed configuration.
It adds no profile or permission bypass and runs in an empty temporary working directory with
an ephemeral session. The bounded excerpt and a fixed summary-only instruction go on stdin,
and only the schema-checked final answer is read. CLI text on failure is discarded.
Inherited instructions, MCP and hooks may add model input, cost or permitted actions; the
summary-only prompt does not enforce a tool-free runtime. The runner uses Codex's non-interactive
exec behavior and reports a fixed error if the run cannot finish without intervention.
Flags were checked against Codex 0.160.0; older CLIs without those flags fail rather than receive
a different model or a permissions override.

The rollout reader keeps canonical user/assistant text from `response_item` messages, skipping
tool calls/results, reasoning, developer instructions, event mirrors and leading injected
AGENTS/environment wrappers. Message IDs prevent replay duplicates; identical distinct messages
remain. The first/recent budgets are the same as Claude's. The runner inherits its own global
instructions even though the source log's injected instructions are excluded from the excerpt.
The collection chooses a Codex rollout by modification time, numeric size, then descending
filename. Its collected filename and version are pinned for the read, and the reader checks that
version before and after reading and verifies session_meta.id. A changed or missing log fails
without substituting another file. Reading is limited to the standard `~/.codex/sessions` root;
an open rollout under a custom root can be collected, but its summary reports a read error
when the collected basename cannot be found under the standard root. PID-only tasks have no log and are excluded. Codex summaries
use the same wait/idle automatic, stop/unknown selected and busy manual-only rules as Claude.
Remote excerpts go to Codex on the panemux host, not to Claude or the remote account.
See [Codex summary runner and rollout reader](../security/command-execution.md#codex-summary-runner-and-rollout-reader).

### Opening a task

| Task | Result |
|---|---|
| In tmux, with a pane already attached | The pane's workspace becomes active and the pane takes focus |
| In tmux, no pane yet, attachable | A `tmux` (panemux host) or `ssh_tmux` (its connection) pane attaching to the session is added at the right edge of the active workspace, through the ordinary `POST /api/sessions` and layout save; the pane's `tmux new-session -A` attaches to the running session |
| In tmux, not attachable | Not opened; the reason is shown |
| Outside tmux, in a `local` / `ssh` pane the workspaces hold | That pane's workspace becomes active and the pane takes focus |
| Outside tmux, naming a pane no workspace holds, or no pane | Not opened; the reason is shown |
| Stopped | Not opened; a stopped claude or codex task offers `Resume` ([Resuming a task](#resuming-a-task)) |
| Unknown | Not opened |

Either way the dashboard closes and the pane is briefly outlined. Opening the same tmux session again
while its pane is still being created does not create a second pane.

**Type in pane.** A task waiting for input or idle can also be answered without leaving the
dashboard: `Type in pane` opens a popup over the board holding a real terminal on the task's tmux
session — the board's temporary attach below — rather than a log or a message form. It does not
switch workspaces or add a pane, and it neither starts nor resumes the agent.

| Task | Type in pane |
|---|---|
| Waiting for input or idle, in an attachable tmux session (a pane on it or not) | Offered on the card and in the detail panel, beside `Open` / `Go to pane` |
| Waiting for input or idle, in a session name a pane cannot attach to | Not offered; the detail panel says why |
| Waiting for input or idle, outside tmux | Not offered; the detail panel says why, and points to `Go to pane` when a workspace holds its pane. The board does not put a second view on a pane's PTY |
| Waiting for input or idle, run by codex's shared daemon | Not offered; the detail panel says why |
| Any other state | Not offered, and no reason is shown |

- **One attach per press.** The button reads `Connecting…` and is disabled from the press until the
  popup closes; the popup opens at once in `Connecting`. An attach that answers after the popup was
  closed — or after another task's popup was opened — is ended at once and never shown. The
  exception is a popup reopened on the same task: the server answers its request with that same
  attach, so the late answer is left for it.
- **Connection.** The header shows `Connecting`, `Connected`, `Disconnected` or `Failed`. The terminal
  takes keys only while `Connected`: before that, and after a disconnect, a failed request, a
  WebSocket that gave up, or a tmux client that exited (a session that ended — including one that
  ended between the server's check and the attach), it is dimmed and inert, and the popup says
  "Input is not being sent." with `Retry` or `Reconnect`. Both send `POST /api/tasks/attach` again
  and read its answer over a new WebSocket; a pane's `/api/sessions/{id}/restart` is never asked.
- **Bound to the task.** The popup stays on the task ID it was opened for. When a poll moves the task
  to another column it says "Moved to <column> on the board. This terminal stays on <task>.", and
  when the board stops listing it, that it no longer does; the terminal, its connection and focus do
  not change. The task's card carries a `Typing` tag while its popup is open.
- **With a workspace pane on the same session.** The popup is a second tmux client on that session:
  input from either reaches it, and the popup says that the window takes the size of the client that
  was used last.
- **Closing** (`Close`, `Cmd/Ctrl+Shift+Esc`, or `Escape` while focus is in the popup's header) sends
  `DELETE /api/tasks/attach/{session_id}`, which ends only the board's tmux client; the agent and
  its tmux session keep running. The dashboard leaving with the popup open ends it the same way:
  the layer shortcut unmounts it, and a reload or a closed tab sends the same `DELETE` from
  `pagehide` as a `keepalive` request, which outlives the page. Were that request lost, the server
  still destroys the attach once its last WebSocket has been closed for the grace period. A page
  that comes back from the back/forward cache with the popup open asks for the attach again, as
  `Retry` does. The board's selection, filters and scroll position are as they
  were, and focus returns to the button the popup was opened from (the same task's button where its
  card has moved to, or the dashboard when there is none).
- The popup's `Go to pane` / `Open` closes it and opens the task as above.

**The board's temporary attach.** What Type in pane uses: a terminal on a task's tmux session that
is not a pane. [`POST /api/tasks/attach`](#post-apitasksattach) opens a tmux client on the running
session, which the browser reads and writes over `/ws/{session_id}` like a pane's, and the layout
never holds it.

- It only attaches (`tmux attach-session -t =<name>`, an exact match) and never creates a session; a
  session that has ended fails the request. tmux reports a missing session only after its client has
  started, so the server first runs `tmux has-session -t =<name>` on the same host. A session that
  ends between that check and the attach still yields `201`; its client exits at once and the
  WebSocket shows tmux's message, and the attach is then destroyed like any other.
- One attach per task: opening the task again while it is open returns the same session, and two
  requests at once create one.
- It is destroyed by `DELETE /api/tasks/attach/{session_id}`, or 10s after the last WebSocket reading
  it closes, or 10s after it was created if none connected; a reload within that time reconnects to
  the same client. A client that has exited is replaced on the next request. Attaches are held in
  memory only and end with panemux.
- Destroying it ends only that tmux client; the tmux session and the agent keep running.
- panemux sets no tmux option for it. With tmux's default `window-size latest`, the window takes the
  size of the client that was used last and returns to the other client's size once the attach ends.

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
      "git": {
        "repo": "panemux",
        "repo_url": "https://github.com/example/panemux",
        "branch": "PAY-418-task-links",
        "pr_url": "https://github.com/example/panemux/pull/260",
        "pr_number": 260,
        "issues": [
          { "number": 255, "url": "https://github.com/example/panemux/issues/255", "repo": "example/panemux" }
        ],
        "autolinks": [{ "text": "PAY-418", "url": "https://jira.example.com/browse/PAY-418" }]
      },
      "labels": ["dashboard", "enhancement"],
      "done": true,
      "summary": {
        "state": "ready",
        "text": "Adding task summaries to the dashboard.",
        "remaining": ["Update the docs", "Run make check"],
        "summarized_at": "2026-09-25T11:58:00Z"
      }
    }
  ],
  "summaries_enabled": true
}
```

- `hosts` lists the panemux host first, then `ssh_connections` keys in name order. `status` is `ok`,
  `error` (with `error`) or `connecting`; `collected_at` is present when `status` is `ok`.
- `tasks` lists each host's running tasks in `id` order, then its stopped tasks newest first. It is
  `[]` when there are none.
- `id` is `local:<agent>:<key>` for the panemux host and `ssh:<host>:<agent>:<key>` for an SSH host,
  where the key is the session ID, `pid-<pid>` for a codex process with no session yet and for a
  claude process no state file names, or `state-file:<file name>` for a state file that could not be
  read.
- `session_id`, `cwd`, `waiting_for`, `status_since`, `started_at`, `pid`, `git` and
  `location.pane_id` are omitted when unknown. `waiting_for` is present only in the `wait` state.
- `wait_signature` is present only in the `wait` state, and only when the agent recorded when the
  wait began ([Wait signature](#wait-signature)).

- Within `git`, every field is omitted when empty. `issues[].repo` is the issue's `owner/name`,
  which can differ from the pull request's repository.
- `done` and `labels` are the task's record, omitted when it is not done or has no labels.
- `summaries_enabled` is `task_dashboard.summary.enabled`. `summary` is present only while it is
  true, and only for a task that has been summarized or is being summarized
  ([Summaries](#summaries)). Its `state` is `pending` (a summary is running or waiting to run),
  `ready`, `error` (with `error`) or `unreadable`. `text`, `remaining` and `summarized_at` are the
  last answer whenever there is one, whatever `state` says; `outdated` is true when the log has
  changed since that answer — or, for `unreadable` and `error`, since that attempt — and `done_candidate` when the answer is ready and current and lists
  nothing remaining. `remaining` is omitted when empty.
- `records_error` is present only when the record file could not be read; the tasks are then listed
  without records.
- The request answers `200` even when every host failed; failures are in `hosts`.
- Like every other route outside `/api/board/*`, it is not authenticated
  ([Current boundaries](../overview.md#current-boundaries)). Because it dials every host, it
  answers `403` to a request another site's page made: `Sec-Fetch-Site` of `cross-site` or
  `same-site`, or an `Origin` whose hostname and effective port differ from request Host. A request with
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

- Every field but `labels` is required in effect: `agent` must be `claude` or `codex`, and `cwd` and
  `prompt` follow [Starting a task](#starting-a-task). An unknown field is refused.
- Answers `201`:

  ```json
  { "id": "local:claude:0f0e0d0c-0b0a-4908-8706-050403020100", "session_id": "0f0e0d0c-0b0a-4908-8706-050403020100", "tmux_session": "task-0f0e0d0c", "labels": ["payment"] }
  ```

  `id` is the one `GET /api/tasks` lists the task under. `labels` is what was recorded, omitted when
  none were given; `records_error` is present instead when they could not be recorded. A codex task
  answers with `tmux_session` only, plus `pending_labels` — the labels held until its session is
  known — when it was given any:

  ```json
  { "tmux_session": "task-0a1b2c3d", "pending_labels": ["payment"] }
  ```
- `400` for a body, agent, label, directory or instruction that is not valid, `404` for a `host`
  that is not an `ssh_connections` key, `409` when the host refused — tmux or the agent missing, a codex
  without `--no-daemon`, the directory missing, a tmux session of that name existing, tmux failing, the temporary file not
  written, each with a fixed message — `502` when the host could not be reached or did not answer,
  and `403` for a cross-site request as for `GET /api/tasks`.

### `POST /api/tasks/resume`

Resumes a stopped claude or codex task:

```json
{ "host": "", "agent": "codex", "session_id": "01a0e2b9-d054-7cc2-9278-a5e25ebcc524" }
```

- `agent` is `claude` (the default when it is omitted) or `codex`.
- Answers `200` with `id`, `session_id` and `tmux_session` as above.
- `400` for a body that is not valid, an agent that is neither, or a `session_id` that is not a UUID,
  or a stopped session whose working directory is unknown or refused; `404` for an unknown `host`;
  `409` when the session is not a stopped task of that agent on the host, or the host refused as above; `502` when the host could not be
  collected or reached; `403` for a cross-site request.

### `POST /api/tasks/attach`

Opens the board's temporary attach ([Opening a task](#opening-a-task)) to a task's tmux session:

```json
{ "id": "local:claude:7c21e0a4" }
```

- `id` is a task ID from `GET /api/tasks`. The server collects that task's host again and takes the
  tmux session and host from it, never from the request.
- Answers `201` with the new attach, or `200` with the one already open for the task:

  ```json
  { "session_id": "board-0123456789abcdef", "tmux_session": "task-7c21" }
  ```

  The terminal is `/ws/{session_id}`. The session is not listed by `GET /api/sessions`.
- `400` for a body that is not valid or an empty `id`; `404` for a task the host no longer reports
  or an ID naming no host; `409` for a task outside tmux or in a session a pane cannot attach to;
  `502` when the host could not be collected or the attach failed (the session has ended, tmux or
  the SSH connection failed); `403` for a cross-site request.

`DELETE /api/tasks/attach/{session_id}` ends the attach at once and answers `204`; `404` for an ID
that is not a board attach (a pane's session included), and `403` for a cross-site request.

### `POST /api/tasks/summary`

Asks for one task's summary:

```json
{ "host": "", "agent": "codex", "session_id": "5d7e3a90-1b2c-4d3e-8f40-51627384a5b6" }
```

- `agent` accepts `claude` or `codex`; omitted means `claude` for older clients. Codex IDs must be UUIDs.
- Starts a summary unless the one for the log as it is now is ready or running; a failed one is
  retried. It does not collect: the session must be listed with a log by the host's last collection.
- Answers `202` at once with the task's `summary` as `GET /api/tasks` reports it (usually
  `pending`); the summary itself arrives with a later `GET /api/tasks`.
- `400` for a body that is not valid or a `session_id` that does not match `^[a-zA-Z0-9_-]+$`, `404`
  for a session the host's last collection did not list with a log (an unknown host included), `409`
  when summaries are disabled, `503` while panemux is shutting down, and `403` for a cross-site
  request as for `GET /api/tasks`.
