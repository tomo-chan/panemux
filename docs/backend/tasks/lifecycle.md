---
type: Specification
title: "Task Launch, Records, Summaries, and Navigation"
description: "Task launch and resume, repository/issue metadata, done and label persistence, summary scheduling and storage, pane resolution, and temporary terminal attaches."
---

# Task Launch, Records, Summaries, and Navigation

Backend lifecycle contracts for starting and resuming tasks, repository/issue metadata and durable
done/label records, conversation summaries, pane location resolution, and temporary task/host
terminal attaches. Wire contracts live in [Task API](../../api/tasks.md); presentation lives in
[Task Dashboard UI](../../ui/tasks.md); security rules live in
[Task dashboard security](../../security/tasks.md).

## Starting and resuming tasks

### Starting a task

`New task` starts an agent declaring `launch` (`claude` or `codex`) in a detached tmux session on
the panemux host or an `ssh_connections` host without creating a pane or pane config.

| Field | Rule |
|---|---|
| Host | Panemux host (`""`) or `ssh_connections` key |
| Working directory | Existing absolute path passing [remote path validation](../../security/process-and-ssh.md#remote-path-arguments-ssh-working-directory) |
| Agent | Declares `launch`, in registry order (`unavailable` host capability disables Start; `unknown` defers to server) |
| Labels | Optional comma-separated values per [Done and labels](#done-and-labels) |
| First instruction | Required; trimmed, LF-normalized, `<= 32 KiB`, no NUL |

- **Claude**: runs `claude --session-id=<minted v4 UUID> -- <first instruction>` in
  `task-<first 8 UUID chars>` (refusing existing tmux names). Labels validate pre-launch and save
  immediately under `(host, claude, session ID)` (`records_error` reported if save fails while task
  stays running).
- **Host shell environment**: looks up `tmux` and CLI on exec then login-shell `PATH` (absolute
  executable files only). Absolute executable `$SHELL` named `bash`, `zsh`, or `sh` runs login +
  interactive (`sh` reads `~/.profile`); unset/relative/non-executable/other shells (`fish`, `tcsh`,
  `dash`, `ksh`, `mksh`) exec the agent directly. RC directory or positional-arg changes do not
  affect launch (even with `#` in CWD). The prompt travels via mode-`0600` temp file (`$TMPDIR` or
  `/tmp`, `panemux-task.XXXXXXXX`) read and deleted before rc runs (also deleted on tmux start
  failure). See [Task launch security](../../security/tasks.md#task-launch-and-resume).
- **Codex launch**: runs `codex --no-daemon -c check_for_update_on_startup=false -- <first
  instruction>` in `task-<8 random hex digits>` (initial response names tmux only). `codex --help`
  must advertise `--no-daemon` before creating tmux (else refuses with
  `"codex on the host is too old: it has no --no-daemon option"`). Codex's directory appends last to
  `PATH` after rc. Startup screens (trust directory, new model, usage limit) are left to the
  operator while the task stays pid-only `run`.
- **Deferred labels**: for any agent choosing its own session ID, validated labels wait in memory
  under `(host, agent, tmux session)` and merge onto existing labels on the first `GET /api/tasks`
  finding exactly one session of that agent there (a sibling pid-only process keeps them pending;
  other agents do not interfere). Dropped on restart, host removal, two sessions there (logged
  ambiguous), or collection `>= 1 min` after start finding none; write failure retries.
- **Working-directory suggestions**: unique collected `cwd`s on the chosen host with newest task's
  agent and time (running dirs labeled `now` ahead of stopped, newest `status_since` first; invalid
  paths excluded; existence checked on start; no separate storage).

### Resuming a task

Offered on stopped tasks of `resume`-declared agents with valid UUIDs (disabled with reason when
`unavailable`):

- Recollects the host first; only a stopped task for that agent with an existing, guarded recorded
  CWD resumes.
- Claude runs `claude --resume=<id>` in `task-<first 8 UUID chars>`; Codex runs
  `codex --no-daemon -c check_for_update_on_startup=false resume -- <id>` in
  `task-<last 8 UUID chars>` (avoiding UUIDv7 prefix collisions) using first-line rollout CWD and
  launch PATH handling. No instruction is sent; a mid-turn killed Codex session resumes `idle`.
- Both new tmux sessions and windows added to existing pane-recreated tmux sessions use the launch
  shell environment. Resume adds a window to an existing matching tmux session only when collection
  finds zero running tasks of any agent (including `unknown`) inside it; a running task refuses
  resume (new-task launch always refuses existing names).
- Resumes for the same `(host, session)` serialize within one panemux process (canceled callers
  abort waiting). Session ID and durable records (including `done`) remain unchanged.

## Metadata and records

### Repository, branch, pull request, issues, and references

Git metadata runs `git` on the task's host and `gh pr view` on the panemux host, cached `30 s` per
`(host, directory)` (abandoned requests stop `gh` without caching). Only running-task directories
query PRs (a stopped-only cache entry refreshes if a running task appears there). Stopped tasks
report only `repo` and `repo_url`, never a later checkout's branch/PR/issues/references.

- **Issues**: read from `closingIssuesReferences` in the same `gh pr view` call (`gh >= 2.72.0`;
  older `gh` returning `Unknown JSON field` retries with URL/number only, keeping the PR link).
  Non-HTTP(S) URLs and non-positive numbers are dropped; titles omitted.
- **Autolink references (`task_dashboard.autolinks`)**: scanned in branch name then PR title,
  replacing `<num>` in `url_template`. Numeric identifiers (`is_alphanumeric` omitted/false) stop at
  digits and reject trailing letters/digits; alphanumeric (`true`) consumes `A-Za-z0-9-` greedily.
  Exact-case prefix cannot be preceded by an ASCII letter/digit (`JIRA-418-retry-backoff` →
  `JIRA-418`; `xJIRA-418`, `JIRA-418a`, `jira-418` do not match). Deduplicated in order found;
  unconfigured prefixes (`UTF-8`, `CVE-…`) never link; no external tracker is queried.

### Done and labels

- Only tasks with a session ID carry records, keyed by `(host, agent, session_id)` (pid-only tasks
  cannot be marked done or labeled).
- Stored on the panemux host in `~/.config/panemux/tasks.json` (mode `0600`, atomic temp+rename,
  writes through symlinks including dangling targets). Empty records (`done: false`, no labels) are
  removed. Records persist across delisting/host removal (`PUT /api/tasks/records` can clear—never
  add—removed-host records).
- Only `collect`-declared agents receive records; records for unknown/non-collecting/alias IDs are
  validated (ID shape, session, labels, uniqueness), preserved untouched with unknown fields (never
  canonicalizing aliases), and omitted from responses until supported.
- A done task sits in the `Done` column only while stopped; resuming shows its live state while
  keeping the `done` flag until `Mark not done`.
- **Labels**: trimmed, deduplicated preserving insertion order, case-sensitive (`Docs` ≠ `docs`),
  max `32` chars, max `20` per task, rejecting control chars, Unicode `Cf` invisible format chars
  (including ZWJ emoji), and line/paragraph separators.
- **`known_labels`**: deduplicated union of all labels in `tasks.json` (including off-list tasks),
  sorted case-insensitively by code point (`Docs` before `docs`, including non-BMP code points,
  identical in Go and TypeScript); omitted when unreadable.
- Served from memory while mtime and size match last read/write; manual edits changing either are
  reloaded. Corrupt/unsupported files report `records_error`, list tasks without records, and refuse
  all writes until fixed (deleted file means no records).

## Summaries

Opt-in via `task_dashboard.summary.enabled: true` (default `false`). Claude uses `claude -p`, Codex
uses `codex exec` on the panemux host (never falling back across agents). Requires a summary
adapter, accepted session ID (UUID for Codex), and collected log (newest `100` per host within
`7 d`). Security/argv rules live in [Task summaries security](../../security/tasks.md#task-summaries);
cost trade-offs live in [Task decisions](../../decisions/tasks.md#task-summaries-bounded-input-persistence-cost-and-codex-runner-2026-09-262026-10-10-issues-258-312-352-353).

### Reading, scheduling, and output

- Fixed remote/local script reads whole log up to `2.25 MiB`, else first `256 KiB` + last `2 MiB`
  (Claude: `~/.claude/projects/*/<session ID>.jsonl`, duplicates pick newest mtime then larger size;
  accepts `user`/`assistant` `text` blocks excluding `isSidechain`, tools, thinking, attachments,
  cut lines). Excerpt budgets: `4 KiB` first user message, `24 KiB` recent messages (`<= 2 KiB`
  each). Secrets in text are unmasked; remote excerpts go to the panemux host account; only answers
  are saved. Zero supported conversation lines yields `unreadable` (no raw fallback, no CLI call
  until log changes).
- **Scheduling**: automatic on collection for `wait`/`idle` with changed log; `busy`/`unknown`
  keeps prior summary marked outdated; selecting a stopped/`unknown` task without a current summary
  requests one; Summarize button requests for any eligible task and retries failures (polling never
  retries same-log failures). Keyed by `(host, agent, session)` + `(mtime, size, Codex rollout
  name)`; unchanged excerpt SHA-256 + adapter reader/runner version reuses prior summary without a
  CLI call. Max `2` concurrent across hosts; `15 s` read timeout; `2 min` CLI timeout.
- **Retention & output**: expires `30 d` after last listing; LRU eviction beyond `1000`; removing a
  host deletes its summaries. Answers use conversation language: `1–2` sentences (`<= 1 KiB`) + up
  to `10` remaining-work items (`<= 300 B` each, truncated with `…`); zero remaining work on a
  ready, current answer flags `done_candidate` (never auto-marks done). Claude requests Haiku (if
  disallowed by org policy, keeps default-model answer, displays model, logs ID once or `unknown`).
  Missing CLI reports `<agent> CLI was not found on the panemux host` (no path); failures expose
  fixed messages only.

### Saved summaries and Codex specifics

- Persisted in `~/.config/panemux/task-summaries.json` (mode `0600`, atomic write-through symlink):
  metadata + answer + excerpt/version SHA-256 + log fingerprint (never logs/excerpts; never saves
  `pending`/`error`/`unreadable`; untouched when summaries are off; removed-adapter entries kept
  unshown until `30 d` expiry; last-listed timestamp saved at most once a day; manual edits while
  panemux runs are overwritten on the next save).
- Unusable files (invalid JSON, version `!= 1`, invalid entry) rename to
  `task-summaries.json.bad-<UTC time>` (up to `-1`..`-99` in the same second) and restart empty;
  unreadable/unmovable files or failed saves keep operating in memory and report `summaries_error`
  (older save finishing after a newer save is dropped).
- **Codex summaries**: `codex exec` inherits operator auth/model/settings/MCP/hooks in an empty temp
  dir with ephemeral session (not guaranteed tool-free). Reads only standard `~/.codex/sessions`
  (custom-root open rollouts fail summary reading), pinning collected basename/version (mtime →
  size → descending filename) without substitution. Empty/stripped messages do not consume message
  ID deduplication slots.

## Task navigation and temporary terminals

### Where a task runs

| `location.kind` | Meaning |
|---|---|
| `tmux` | Agent or ancestor is a tmux pane process (`tmux_session` named; `attachable` when matching `^[a-zA-Z0-9_.-]+$`) |
| `outside` | Live agent outside tmux (`pane_id` from process env if valid) |
| `daemon` | Codex session held by `codex app-server` (no discoverable pane) |
| `none` | Stopped task |

Browser pane matching checks `tmux`/`ssh_tmux` panes matching `(host, tmux_session)` for tmux tasks,
or `local`/`ssh` panes matching `(host, pane_id)` for outside tasks.

### The pane of an agent outside tmux

- `local` and `ssh` panes export `PANEMUX_PANE_ID` when the pane ID matches
  `^[A-Za-z0-9_.-]{1,128}$` (regardless of `url_open.browser_shim`), stripping any inherited
  `PANEMUX_PANE_ID` from local panes; `tmux`/`ssh_tmux` panes never set it and ignore inherited
  values. Plain `ssh` panes run an explicit command + login shell exec to export it.
- Collection reads only the agent's own process environment: `/proc/<pid>/environ` on Linux,
  `ps -E` on macOS (replayed against macOS 26.3.1, not verified live on macOS/SSH/older releases;
  strips separately read argv prefix, requires leading space and single ` PANEMUX_PANE_ID=` match up
  to next space; Apple binaries `/bin/sleep`, `/bin/zsh` and other users' processes expose no env;
  Claude/Codex/Node do; single-occurrence-inside-another-var and space truncation are accepted
  because IDs only match existing workspace panes of matching kind/host). Changing the variable
  after process start has no effect.

### Opening a task, view state, and temporary attaches

- **Open / Go to pane**: existing matched pane switches workspace and focuses; attachable tmux task
  without a pane creates a `tmux`/`ssh_tmux` pane at the right edge via **Open in workspace**
  (`POST /api/workspaces` → `PUT /api/workspaces/{id}` → `POST /api/sessions` → layout save,
  retrying completed steps without duplication; concurrent opens for the same tmux session create
  one pane). Stopped/unknown/unattachable/unmatched tasks do not open.
- **Type in pane (`POST /api/tasks/attach`)**: offered on `wait`/`idle` tasks in attachable tmux
  sessions only (never outside tmux—no second view on a pane PTY—never daemon tasks). Runs
  `tmux has-session -t =<name>` before `tmux attach-session -t =<name>` (never creates sessions);
  one attach per task (idempotent); served on `/ws/{session_id}` (hidden from `GET /api/sessions`);
  destroyed on `DELETE /api/tasks/attach/{session_id}` (including `pagehide` keepalive) or `10 s`
  after last WebSocket close / unattached creation; in-memory only; ends only the tmux client
  (tmux default `window-size latest` resizes to the most recently active client). Late attaches
  after popup close/switch are ended immediately; Retry/Reconnect calls `POST /api/tasks/attach`
  fresh (never restarts a pane).
- **Dashboard view state**: search, host/label filters, Group, Cards/Table, sort, and `Show done`
  persist in App's in-memory `TaskDashboardViewState` across workspace switches in one tab (leaving
  Tasks unmounts dashboard and closes temporary attaches). Reload resets transient filters while
  restoring Group (`None`/`Host`/`Label`/`Repository`), Cards/Table, and sort from versioned
  `localStorage` (defaults: Cards / None / State ascending; invalid/full storage falls back safely
  without overwriting). Filtering applies before grouping; sort is pure within groups (fixed-English
  case-insensitive natural order, board column order for State with Running/unknown tied, elapsed
  seconds without start-time fallback, missing last, task-ID tie-breaker; Elapsed defaults
  descending, others ascending). Missing stored host/label displays as All while retaining value
  across failures; notification jumps clear hiding filters and focus/scroll the task once.

### Opening a host

`ok` `ssh_connections` chips open a connection menu (`ssh` default or `ssh_tmux` requesting a fresh
name via `POST /api/hosts/session-name` sanitized to `[a-zA-Z0-9_.-]` + `-<8 hex>`). `Open` always
adds a new pane via **Open in workspace**; `Type in pane` creates a temporary host terminal via
`POST /api/hosts/terminal` (same `/ws/{session_id}`, hidden from `/api/sessions`, `10 s` grace,
`DELETE /api/hosts/terminal/{session_id}`, max `4` active per connection, isolated from task
attaches). Losing `ok` status closes the menu and shifts focus to `Reconnect` or dashboard; closing
an `ssh` host terminal logs out while `ssh_tmux` ends only its tmux client.
