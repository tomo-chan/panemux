# Decision Log

This log records *why* a consequential design or implementation choice was made, what it replaced,
and the sequence in which related choices changed. The rest of `docs/` describes the current state.

Newest entries appear first within each topic. Dates and pull requests are included when repository
history identifies them. A decision remains here after its consequences are reflected in the
current specification; superseded decisions are retained and labelled as such.

## Documentation structure

### Current-state guides and historical decisions are separate (2026-09-23)

Top-level topic documents had accumulated three different kinds of information: current rules,
deep implementation detail, and a narrative of issues and rollout phases. Readers looking for the
current contract had to infer it from phrases such as "landed", "earlier revision", and "Phase 3".

The documentation now has three reading levels: [overview](overview.md), concise topic guides, and
same-topic deep dives. This log owns chronology, rejected alternatives, and migration reasoning.
The structure and writing rules are indexed in the [documentation index](README.md).

## Architecture

### Domain config and file context use separate types (2026-09-20, issue #66, PR #240)

`config.Data` is the serializable domain model and preserves user-facing YAML section order.
`config.Config` embeds it and adds load/save context such as file and SSH-config paths. The earlier
single type mixed persisted data with runtime context; a separate serialization struct then had to
repeat every config section and silently dropped a section when that copy was not updated.

### Filesystem globals use named seams (2026-09-10 through 2026-09-13, PRs #224, #231, #234)

Home-directory, cache-directory, and persistent-write behavior had been overridden independently by
callers or manipulated through process-wide environment variables in tests. The repository adopted
`internal/homedir`, `internal/cachedir`, and `internal/fileops` as one named seam per global
capability. The separation is deliberate: home and cache lookup have different platform semantics,
and atomic file replacement has different safety properties from either lookup.

These seams make failure paths injectable and give lint a single allowed location for direct OS
lookups. They are not safe for parallel substitution; tests that override them remain non-parallel.

### Loopback callbacks use a panemux-host forward (2026-08-23, PR #177)

CLI login flows in remote panes open `localhost` callback URLs on the SSH host, while the browser
runs on the panemux host. Panemux chose an on-demand local listener backed by SSH `direct-tcpip`
instead of exposing a general proxy or requiring users to preconfigure `ssh -L`. The scope remains
loopback-only and the listener exists only for the callback flow.

## Runtime and API contracts

### Command-center terminal frames carry non-fatal warnings (2026-09-11 through 2026-09-13, PRs #228 and #234)

A failed history write was first represented as `error` followed by `done`, violating the protocol's
one-terminal-frame rule; limiting the warning to successful turns then hid it whenever the query
also failed. `warnings` now rides whichever single terminal frame ends the turn. Malformed stream
output cancels the subprocess before its remaining output is drained, preventing the drain from
holding the busy flag and client error until the full timeout.

### Layout responses are normalized before frontend parsing (2026-09-03, PR #198)

Valid hand-written config can omit `direction`/`children` or put a lone pane at the layout root,
while the browser contract requires a normalized recursive node. The API now fills missing keys,
relocates a lone root pane into its implied child, and validates that pane exactly like every other
pane. The normalized form is persisted only on the next explicit save.

### Command-center prompts use a panemux-owned history entry (2026-08-17)

The Claude stream contains `stream_event`, `system`, `assistant`, and `result` frames but does not
repeat the prompt that produced them. Deriving history from the captured stream alone therefore
made each turn unreadable. Panemux now writes the prompt first as `panemux_prompt`, a type the CLI
does not emit, then appends the captured subprocess frames. A subprocess failure still records the
prompt; a request that never starts a subprocess does not create a turn.

## Task dashboard

### Dashboard hosts are listed by name; details come from `~/.ssh/config` (2026-09-29, issue #272)

The dashboard collects only from `ssh_connections`, which kept a host that lives in `~/.ssh/config`
off it unless every connection detail was copied into YAML. An `ssh_connections` entry can now be
just a name: its details come from the `~/.ssh/config` `Host` block of that name, and each field the
entry does set overrides the block's value for that setting. Field-by-field merging was chosen over
using the `Host` block only when the entry is empty, so that one setting (a different `user`, say)
can be changed without copying the rest. The consequence is that an entry that already set `host`
and `user` now also takes `IdentityFile` and `Port` from a `Host` block of the same name when it leaves
those unset; before, a name in both sources used only the YAML entry.

The route is the exception. An entry that sets its own `host` takes neither `ProxyJump` nor
`ProxyCommand` from the block. The first version of the merge inherited both, and review of PR #274
found two consequences: a block's `ProxyJump` that panemux cannot follow (`user@host`, a list, `none`)
failed an entry that had connected on its YAML values alone, and the block's `ProxyCommand` ran through
`/bin/sh -c` with the YAML `host` substituted for `%h` — a data flow from `config.yaml` into a shell
that had not existed. Parsing the other `ProxyJump` forms was the alternative; it was not taken because
the route a block describes belongs to the host it names, and the setups this change is for (a
name-only entry over a block whose `ProxyJump` is an alias) keep it either way. The same review found
that a `ProxyJump` cycle recursed until the stack overflowed, which the merge had made reachable from
fully specified entries too; the chain is now carried along and a cycle is an error.

The collection scope itself is unchanged: widening it to every `~/.ssh/config` host was rejected in
issue #272, since that file commonly lists hosts that have nothing to do with agents. The same change
fixed validation adding the `~/.ssh/config` hosts to the config's own `ssh_connections` map whenever
that map was non-empty — which made every such host a dashboard host and wrote it into config.yaml on
the next save. Entries a config already had written that way are not removed by the fix: they stay
dashboard hosts until deleted from `ssh_connections` by hand. Issue #272's proposal of a UI to edit `ssh_connections` was not built: listing a name
is now the whole edit.

### Codex sessions: state, starting and resuming (2026-09-27, issue #264)

Stage 1 listed a codex task as a running process known only by its pid, and stage 2 started and
resumed claude only, because codex's files had not been checked. They were checked on real codex-cli
0.142.2 and 0.157.1 on macOS (by the operator, with a stand-in model server for the states a
usage-limited account could not reach, and a real model on 0.157.1) and 0.157.1 on Linux (in the
implementation session, with a stand-in model server); the results are the comments on #264. The
operator decided the approach from them, and the design points the Linux check raised were put to
the operator before implementation:

- **A codex process's session is the rollout it holds open.** Codex writes no `<pid>.json` and no
  state value; the TUI process (not the npm node wrapper, not the 0.157 app-server daemon) keeps its
  rollout open, found through `lsof` on macOS and `/proc/<pid>/fd` on Linux. Reading codex's
  `logs_2.sqlite` (whose `process_uuid` names the pid) was the alternative; it is an internal trace
  log.
- **The state is the newest turn — `thread_turns.status` in `thread_history_1.sqlite` where
  available, otherwise the rollout's last turn event — and only while the process is alive.** A
  killed turn stays `inProgress` forever, so a dead process is always `stop`. Waiting for command
  approval is `busy`, since codex records nothing for it on either platform; a pending
  `request_user_input` call is `wait`. The operator chose `sqlite3` as the primary source and the
  rollout as the fallback for hosts without `sqlite3` and codex before 0.157.
- **The rollout's last line is not its last record of interest.** On Linux 0.157.1 every waiting and
  running turn ended with `token_usage_record` after the call (the macOS observation had the call
  last), so the script keeps the last turn event and the last `response_item` separately rather than
  reading the final line.
- **A turn older than its process is a leftover.** Found in the Linux check: a session killed
  mid-turn and resumed without an instruction still had its killed turn as the newest one, in
  `thread_turns` and in the rollout, so it read as working while codex sat idle. A process's start
  (from `ps`'s POSIX `etime`) now bounds which turns can be in progress. Stage 1 rejected `procStart`
  for claude because its meaning was unknown; `etime` is ps's own elapsed time.
- **After `/new` or a TUI `/resume`, the rollout written last is the session** (operator's choice
  among the options raised). The Linux check switched A → B → A inside one process: the newest
  modification time matched the current session at every step, while the highest descriptor number
  was wrong after returning to A, which codex reopens at a lower number.
- **A codex task held at a start-up screen is `run`, known by its pid, with a note to open the pane**
  (operator's choice). Showing it as `wait` would also flag a codex just started from a shell and
  sitting at its prompt, and telling the two apart by whether the command line carries a prompt was
  the rejected third option. panemux does not answer the screens or write codex's configuration to
  avoid them; only the update prompt is turned off, with `-c check_for_update_on_startup=false`.
- **A new codex task's labels are held until its session is collected** (operator's choice). Codex
  has no option to set its session ID — the Linux check found none in `codex --help` — so the ID is
  known only after the first instruction. Holding the labels in memory keyed by tmux session was
  chosen over hiding the labels field for codex and over waiting up to 15 seconds in the request,
  which would lose them exactly when a start-up screen holds the task.
- **Stopped codex sessions are the TUI's and share the cap with claude** (operator's choice): 50
  stopped tasks per host across both agents, keeping stage 1's "50 per host". The first version kept
  `source` `cli` only; review of PR #271 found that a TUI running through codex's shared daemon writes
  `source` `vscode` (with `originator` `codex-tui`), so those sessions were never listed. The operator
  chose `originator` `codex-tui`, which keeps `codex exec` (`codex_exec`) out, and the script now counts
  its 100 after that filter (the first version cut at 100 first, so 100 newer `codex exec` rollouts
  hid every TUI session).
- **A session codex's shared daemon runs is a task of its own** (operator's choice, from review of
  PR #271). From codex-cli 0.157, `codex` started without `-c` or `--no-daemon` runs its session in a
  shared `codex app-server` daemon, which holds the rollout — and keeps holding it, with its writer
  lock, after the TUI exits. The implementation session's Linux check had always passed `-c`, which
  keeps the session in the TUI, so it never saw the daemon. Nothing ties the TUI to the session: the
  TUI holds only a socket, and `logs_2.sqlite` records the session under the daemon's pid. The
  alternatives were pairing a TUI with a daemon session of the same working directory when only one
  matched (rejected: a guess that goes wrong when an exited session lingers in the daemon) and only
  documenting the limit. The chosen form shows the session's real state under a `daemon` location
  that offers no pane, beside the TUI's `run` card, and never offers `Resume` on a session the daemon
  still holds.
- **The dashboard's own starts pass `--no-daemon`** (operator's choice), so their sessions stay tied
  to their tmux session. That `-c` alone did the same was an observation, not a documented rule.
- **A codex without `--no-daemon` is refused** (operator's choice, from the second review of PR
  #271). codex-cli 0.142.2 exits 2 on the option, which left the dashboard reporting a start whose
  tmux session had already ended. The alternatives were passing `--no-daemon` only where the help
  lists it (older versions have no daemon to avoid) and only documenting a minimum version. The
  check reads the help rather than comparing versions, since the version that added the option was
  not checked.
- **`--` before codex's prompt and its resumed session ID, and UUIDs only.** Checked on both
  platforms: without `--`, a prompt beginning with `-` is an option, and `codex resume <id> '-h …'`
  prints help and exits 0; `codex resume` also takes a session name, which codex sets automatically
  from the first instruction with a real model.
- **A codex resume's tmux session is named from the ID's last eight characters.** Codex IDs are
  version 7 UUIDs, whose first eight characters are a timestamp shared by sessions started within a
  minute.
- **codex's own directory is added at the end of `PATH`.** Found in the Linux check: an npm
  install's `codex` is a node script, and started from a `PATH` without node it failed with
  `env: 'node': No such file or directory`. The first version put the directory first; review of PR
  #271 showed that this also moved every command codex and its model run ahead to that directory (a
  `python3` beside codex won). The operator chose the end, which still supplies node. The second
  review showed the other side — a `node` earlier on the `PATH` runs codex instead — and the operator
  chose to document it rather than run codex with the `node` beside it, which would depend on how
  the npm package is laid out and break a native codex that happens to have a `node` next to it.
- **Codex tasks are not summarized.** The summary reads claude's log format; a rollout is another.

### Stage 1: a dashboard of agent sessions, independent of panes (2026-09-25, issue #252)

The design, its stages and its open questions are agreed in issue #252. The choices stage 1 made
while being built:

- **Claude Code's own files are read as they are.** `~/.claude/sessions/<pid>.json` and
  `~/.claude/projects/*/<session>.jsonl` are not a published format. They are the only record of a
  session that does not depend on panemux having started it, which is the point of the feature, so
  the dependency is accepted and made to fail visibly: every field is optional, an unreadable state
  file stays on the board as `unknown` instead of disappearing, and only the first `"cwd"` of a log
  is read. Codex has no equivalent that has been checked, so codex tasks are running processes and
  nothing more (issue #252, open question 5). *Superseded by the issue #264 entry above: codex's
  rollouts are read.*
- **Liveness is "the pid is alive and its program is claude", and `procStart` is not used.** A
  state file's pid alone is not enough, because pids restart after a host reboot. Claude
  Code 2.1.282's `procStart` matched field 22 of `/proc/<pid>/stat` (clock ticks since boot) for the
  one process checked on Linux, while the observation recorded in the issue did not match (425
  against 419), so how it is computed is still unknown and nothing relies on it. "Its program" is a
  path component named `claude` or `claude-code` in argv[0], or in the script `node`/`bun`/`deno`
  runs, because how Claude Code shows in `ps` depends on how it was installed — a native binary
  under `…/claude/versions/<version>`, or `node` running the npm package. The first version matched
  `claude` anywhere in the command line; review found that an editor or `tail` with a file under
  `~/.claude` as its argument then passed for a waiting agent.
- **A running claude process with no state file is `unknown`, not `stop`** (review of PR #253).
  Building running tasks only from state files meant a Claude Code release that moved those files
  would have shown every running agent as stopped. Such a process is taken to be writing the newest
  log in its directory, so that session is not listed twice.
- **Codex's interactive sessions are recognized by subcommand**, from `codex --help` of codex-cli
  0.157.0: no subcommand (a prompt), `resume` and `fork` are interactive, every other subcommand is
  not. The first version excluded any process with an `exec` token anywhere, which hid a prompt
  containing the word and listed `codex app-server`.
- **Only the collecting user's processes are listed** (`ps -U`), so on a shared host another
  user's agents are not shown as one's own tasks.
- **A slow collection keeps its connection** while the connection answers an SSH keepalive. The
  first version dropped the connection on any timeout, so a host whose collection took longer than
  15 seconds was redialed every 10 seconds and never showed a result. Keeping the connection stops
  the redialing only: a host whose script alone takes longer than 15 seconds every time still never
  shows a result, and its script is started again at every collection. What the remote script does
  once panemux closes the exec channel (no signal is sent) has not been checked.
- **The task routes refuse cross-site requests.** They stay unauthenticated like the rest of
  `/api/*`, but `GET /api/tasks` is the first GET with a heavy side effect — dialing every host — so
  a page on another site must not be able to trigger it with an `<img>`.
- **A stopped task reports its repository but not its branch or PR**: git metadata is read from the
  directory as it is now, which says nothing about the branch a stopped session worked on. So
  `gh pr view` runs only for a directory a running task uses (review of PR #253); the first version
  ran it for every directory and threw the stopped-only results away. A lookup a request abandoned
  is not cached, so an aborted `gh pr view` does not hide a running task's PR for 30 seconds.
- **The local collection is killed as a process group** (review of PR #253). The first version
  relied on `exec.CommandContext` killing `sh` alone; a probe the script had started kept its
  stdout open, so the collection outlived its 15 seconds until that probe finished.
- **`/resume` and exit were checked on a real machine** (macOS, 2026-09-26; the Claude Code
  version was not recorded). `/resume` keeps the same process and the
  same `<pid>.json`, and rewrites that file's `sessionId` to the resumed session (`updatedAt`
  moves with it) — so the switched-from session shows as stopped and the switched-to one as running
  in the same place, as issue #252 expected. `/exit` removes both `<pid>.json` and its `.key` file,
  so a normal exit leaves no state file behind; the liveness check still guards against the files a
  crash or a host restart can leave.
- **One fixed script per host, fed on stdin.** Every host runs the same constant script through
  `sh -s`: one round trip per collection, identical parsing for local and remote, no remote value
  ever quoted into a command, and nothing that depends on the remote login shell being POSIX. The
  alternative — one exec per probe, as the pane header's lookups do — would cost several round trips
  per host every 10 seconds.
- **The pane a task belongs to is found in the browser, not by the API.** The browser already holds
  the workspaces, so a pane the dashboard has just created is matched at once rather than after the
  next collection, and the API has no layout state to keep consistent with it.
- **Stopped sessions are the last 7 days, at most 50 per host.** Decided for stage 1 while it was
  built; conversation logs are never deleted, so without a bound every past session would be
  listed. Open question 1 still decides retention and recording for stage 2.
- **panemux opens on the workspaces**, as before, rather than on the dashboard the issue's mockup
  starts on.
- **The layers switch with `Cmd/Ctrl+Shift+S`, and the letter is configurable** as
  `display.task_dashboard_shortcut` (open question 6). `S` was chosen for stage 1 while it was
  built; the environment it was built in could not reach a list of Chrome's own shortcuts. On a real,
  non-headless browser on macOS (2026-09-26), `Cmd+Shift+S` switched layers and was not taken by the
  browser; `Ctrl+Shift+S` on Linux or Windows was not checked there. It is a config setting rather
  than a per-browser one so every browser on one panemux behaves the same; `K` and `B` are refused
  because the palette and the board already use them. `GET /api/display` reports the effective
  letter so the default lives in one place, and an unset value is never written back.
- **The UI text is English**, like the rest of panemux's interface, although the issue's mockup is
  written in Japanese.

### Opening an agent outside tmux through `PANEMUX_PANE_ID` (2026-09-26, issue #254)

Stage 1 located an agent only through tmux, so one running directly in a `local` or `ssh` pane could
not be opened. Issue #254 chose to have those panes export `PANEMUX_PANE_ID` to their shell, the way
the browser shim exports `BROWSER`, and to read it back from the agent's environment at
collection. The choices made while building it:

- **Only IDs matching `^[A-Za-z0-9_.-]{1,128}$` are exported.** Pane IDs have no charset rule in the
  config. Exporting any ID would have put arbitrary text into an SSH pane's remote command, relying
  on quoting alone; limiting it keeps the value inert in a shell and lets collection apply the same
  rule to what it reads back. Every ID panemux generates matches, so only a hand-written config ID
  can lose the feature.
- **SSH panes export it whether or not the browser shim is enabled.** An `ssh` pane that used the
  SSH shell request now runs a command that execs the login shell, the change the shim already
  made. Exporting it only with the shim would have left agents in those panes impossible to open,
  and `AcceptEnv` on the server cannot be relied on for `Setenv`.
- **Remote commands with setup are handed to `/bin/sh`.** sshd runs an exec request with the
  user's login shell. The first version of this change prepended `PANEMUX_PANE_ID=…; export …` to
  the command, which `fish` rejects as a syntax error and `tcsh` half-runs, so an `ssh` pane on
  such a host exited at once, and `url_open.browser_shim: false` no longer avoided it (review of
  PR #260). The shim's own setup had always failed the same way on those hosts. The whole script is
  now passed as `exec /bin/sh -c '<one line>'`, with the shim's body as octal escapes because
  `tcsh` expands the `!` of `#!/bin/sh` even inside single quotes. Checked on Linux (2026-09-26)
  against a real sshd with `fish` 3.7.0 and `tcsh` login shells, with and without the shim, for
  panes with and without `cwd`. Exporting through `exec env PANEMUX_PANE_ID=… "$SHELL"` was
  rejected because it left the shim broken, and keeping the SSH shell request when the shim is
  disabled because those panes would have had no pane ID.
- **The browser matches the ID against the panes it holds**, as it already did for tmux sessions,
  rather than the server checking it against the config. The value only claims a pane, so it opens
  one only when that pane is a `local` pane (panemux host) or an `ssh` pane on the task's
  connection.
- **Only the agent's own environment is read, and at first only on Linux** (`/proc/<pid>/environ`).
  On macOS (2026-09-26), a `sleep 120` started with `PANEMUX_PANE_ID=test-1` was listed by
  `ps -E -p <pid> -o command=` and by `ps eww -p <pid> -o command=` as `sleep 120` alone, without
  the variable. No other way to read another process's environment on macOS was verified, so
  macOS hosts reported no pane ID rather than relying on an unchecked method. Issue #263 later
  found that `/bin/sleep` was an exception (below).
- **Process ancestry was not used instead.** Matching an agent's parent chain against a local
  pane's shell pid would work without reading environments on the panemux host, but the pid of an
  `ssh` pane's remote shell is not known to panemux, and an agent that detached from its shell
  would be lost; the environment survives both.

### Issue links and autolinked references (2026-09-26, issue #255)

- **An issue is one the task's pull request closes**, read from `gh pr view --json
  closingIssuesReferences` in the same `gh` call that already found the pull request, so the
  dashboard runs no extra command and still looks up only directories a running task uses. That
  the field exists and what it exports (`id`, `number`, `url`, and the repository's `name` and
  owner `login`; the query asks for the first 100) was checked in the source of gh 2.101.0
  (`api/export_pr.go`, `api/query_builder.go`), not against a real pull request: the environment it
  was built in had no working GitHub token. Whether an issue linked by hand in the pull request's
  sidebar, rather than by a closing keyword, is included has not been checked either. Issue titles
  are not shown, since that export carries none.
- **An older `gh` keeps the PR link** (review of PR #261). `closingIssuesReferences` first appears
  in gh 2.72.0 (it is absent from `api/query_builder.go` at v2.71.2), and gh refuses a whole call
  that names a field it does not know, before contacting GitHub. The first version therefore lost
  the task's PR link on an older `gh` while the pane header, which asks for `url,number` only,
  still showed it. A call refused with `Unknown JSON field` is now repeated with `url,number`. Only
  that refusal is retried: a branch without a pull request also makes `gh` fail, and is the common
  case, so a retry on any failure would double the `gh` runs.
- **References use the shape of GitHub's autolink references** (`task_dashboard.autolinks`:
  `key_prefix`, `url_template` with `<num>`, `is_alphanumeric`; review of PR #261, at the
  author's direction). The first version had a Jira site (`task_dashboard.jira_url`) and found keys
  by shape, `[A-Z][A-Z0-9_]+-[1-9][0-9]*`. The shape alone also matched `UTF-8` and `SHA-256` in a
  title and cut `CVE-2024-45337` to `CVE-2024`. A `task_dashboard.jira_projects` allowlist was added
  next and then replaced: a list of prefixes, each with its own URL template, is the allowlist and
  the site together, is not tied to Jira or to its `/browse/` path, and is configured the way
  GitHub already asks for it. Only the configuration's shape follows GitHub; panemux does not read
  a repository's autolink settings, which GitHub's API ties to repository administration permission
  (stated for GitHub Apps in its REST documentation; not checked for a user's own token).
  Dropping a key followed by `-<digit>` was also considered, and rejected: it would have fixed only
  the `CVE-2024` case and hidden a real key in a branch such as `PAY-418-2-retry`.
- **Where GitHub's documentation is silent, panemux chose** (its own documentation and API
  descriptions say what `<num>` may contain and that prefixes may not overlap, nothing more): the
  prefix matches case included and not directly after an ASCII letter or digit, a numeric
  identifier not directly before one, and an omitted `is_alphanumeric` means digits only. The last
  is because an alphanumeric identifier includes `-` and so runs on through a branch name's words
  (`TICKET-12-retry` gives `12-retry`); GitHub's own default for the field was not found.
- **References are linked, never queried.** No tracker API is called and no ticket title is
  fetched, so panemux holds no tracker credentials. The setting is a new top-level `task_dashboard`
  section rather than part of `display`, since it says where links point rather than how anything
  looks. A `url_template` must be `https`, because it becomes the address of a link the operator
  clicks, and `<num>` must come after the host, so an identifier cannot change where a link goes.
- **A template's host and port are held to what the browser parses** (review of PR #261).
  `url.Parse` accepted `https://jira.example.invalid:99999`, `https://ex<ample.com` and
  `https://xn--/`, which the browser's `new URL()` refuses; one such link made the browser reject
  the whole task list. Go has no WHATWG URL parser, so the check is narrower than the browser
  instead of equal to it: an IP or ASCII letters, digits and `-`, no punycode label (there is no
  decoder in the module to check one with), no numeric last label unless the host is an IPv4
  address, and a port 1–65535. `testdata/autolink-url-validation.json` is read by the Go test and
  by the frontend's schema test, so a URL Go accepts that the browser does not fails a test.

### Reading `PANEMUX_PANE_ID` on macOS through `ps -E` (2026-09-27, issue #263)

Issue #254 left macOS hosts without a pane ID because `ps -E` had not shown the variable for
`/bin/sleep`. A check on macOS 26.3.1 with SIP enabled (2026-09-27, claude 2.1.283, codex-cli
0.142.2 and 0.157.1) found that `/bin/sleep` was the exception, not the rule:

- `ps -E`, `ps eww` and `sysctl` `KERN_PROCARGS2` all showed the variable for claude (a native
  Mach-O binary), codex's `node` wrapper and its native binary, `node -e`, and a `sleep` equivalent
  compiled locally.
- Apple-signed OS binaries (`/bin/sleep`, `/usr/bin/tail`, `/bin/zsh`) showed no environment at all:
  the kernel returns none for them.
- `KERN_PROCARGS2` refused root's processes (`EINVAL`). What the setuid-root `ps -E` shows for
  another user's process was not checked; collection only reads the user's own processes.
- codex 0.157's resident daemon (`codex app-server --managed-daemon`, started by launchd) has no
  pane ID because it was not started from a pane; the process writing an interactive session is
  the TUI, which has one.

The choices made from that (decided by the user in issue #263):

- **`ps -E`, read by the fixed `sh -s` script.** `KERN_PROCARGS2` delimits entries with NULs and
  would be exact, but it needs a compiled helper or an interpreter to call `sysctl`; the macOS
  `sysctl` command does not expose it (`unknown oid`). A helper does not fit a script that is one
  constant run with `sh -s`, so it was rejected.
- **The arguments are removed from the front.** `ps -E` joins the arguments and the `NAME=value`
  entries with spaces, so a process whose argument is `PANEMUX_PANE_ID=…` would otherwise supply
  the value. Removing what `ps -p <pid> -o command=` prints from the front was checked on macOS to
  tell the two apart; output that does not start with the arguments is not read.
- **Exactly one ` PANEMUX_PANE_ID=` or nothing.** Another variable's value can contain a space and
  `PANEMUX_PANE_ID=` (`AAA='x PANEMUX_PANE_ID=evil'`), which `ps -E` prints indistinguishably from
  the real entry. Taking the first or the last occurrence was rejected because which one is right
  cannot be told from the output; the agent then has no pane ID.
- **One occurrence inside another variable's value is read, and this is accepted** (review of
  PR #269, decided by the user). With no real `PANEMUX_PANE_ID`, `AAA='x PANEMUX_PANE_ID=pane-b'`
  or a value with a newline before ` PANEMUX_PANE_ID=pane-b` is reported as pane `pane-b` on macOS,
  where Linux reports nothing. It is accepted because only someone who can start an agent as the
  same user outside a pane can plant it — and such a process can set `PANEMUX_PANE_ID` directly —
  the effect is at most a `Go to pane` that focuses another existing `local`/`ssh` pane of that
  host, and no command runs. Rejected alternatives: skipping the reading whenever the environment
  part contains a newline would stop only the newline form, not the space form, and would lose the
  pane of a legitimate agent whose environment holds a multi-line value; reading
  `KERN_PROCARGS2` through `perl` or another interpreter would be exact, but reverses the decision
  above not to go beyond the fixed `sh -s` script, and whether macOS 26 provides a usable
  interpreter was not checked.
- **The same rule as Linux** (`^[A-Za-z0-9_.-]{1,128}$`) is applied to the value, read up to the next
  space. The shell also prints it only when it consists of those characters, so a newline in it
  cannot start a row of the output.
- **The macOS reading is chosen by `uname -s` being `Darwin`**, not by `/proc` being absent, so
  another system without `/proc` does not run a `ps -E` whose meaning there was not checked.
- Apple's own binaries and other users' processes reporting no pane ID is documented as a limit
  rather than worked around.

The script's macOS branch is tested on Linux by replaying the `ps -E` output recorded in the issue
through a fake `ps` and `uname`; it has not yet been run on a real macOS host.

### Stage 2: done and labels are recorded on the panemux host (2026-09-26, issue #256)

Issue #252's open question 1 left where done and labels are kept, and for how long, to this stage.
The operator decided the four points below before implementation; the rest was chosen while it was
built.

- **One file, `~/.config/panemux/tasks.json`, beside panemux's other state files** — decided with
  the operator. Keeping it out of `config.yaml` keeps a record save from racing a layout save and
  keeps a dotfiles-managed config free of per-session data.
- **Records are kept until a person clears them, and a task off the list is not shown** — decided
  with the operator. The 7-day, 50-per-host listing bound stays as stage 1 set it; a record whose
  session left it is neither deleted nor turned into a card built from the record alone, and applies
  again if the session is listed again. A rejected alternative was to show such records in the Done
  column, which would have meant cards with none of what the collection knows.
- **A task marked done that runs again shows its real state** — decided with the operator. The
  record is not cleared automatically: that would have made `GET /api/tasks` write, and a task that
  stops again returns to Done without being marked a second time. Always showing it in Done was the
  other alternative, rejected because it would hide a task that is waiting for input.
- **Only tasks with a session ID can carry a record** — decided with the operator. A pid is reused
  after its process exits, so a record keyed by one would move to an unrelated process.
- **Done is a field, not a state.** Issue #252's column table gives Done a `done` state; the API
  keeps `state` as what the host reported and adds `done`, so a running task marked done still says
  what it is doing, and the column rule ("done and stopped") lives in one place in the browser.
- **The record's key is host, agent and session ID.** A session ID is unique only on its own host.
- **`PUT /api/tasks/records` replaces the whole record.** The dashboard sends what it shows. A
  collection that was already running when a record was saved had read the records before it, so
  the browser drops that answer rather than let it put the old record back for up to 10 seconds.
- **A file panemux cannot read is never overwritten.** It is reported in `records_error`, tasks are
  listed without records, and writes fail until the file is fixed, so a file from a newer panemux or
  a broken hand edit is not silently replaced by an empty set.
- **A file edited by hand is read again** (review of PR #262). The first version read the file once
  and served memory afterwards, so a hand edit made while panemux ran was hidden and then undone by
  the next save — and editing the file is the only way to clear the record of a task that has left
  the list. The file's modification time and size are compared before each use; an edit that keeps
  both is not seen, which was accepted over re-reading the file on every 10-second poll.
- **A symlinked record file is written through** (review of PR #262), the way `config.yaml` is.
  `AtomicWrite` alone replaces a link with a regular file, which would have moved the records out of
  a dotfiles repository without saying so.
- **Clearing a record does not need its host to be configured** (review of PR #262). The first
  version refused every request for a host no longer in `ssh_connections`, which left that host's
  records impossible to clear through the API. Adding a record still needs a configured host.
  Removing the records when a host is removed from the config was the alternative; it was not taken
  because it would tie the record file to the config's save path.
- **Label limits** (32 characters, 20 labels, no control characters) were chosen while it was built,
  to keep a label a short tag on a card rather than free text. Invisible format characters and line
  and paragraph separators were added to the refused set in review of PR #262: they let a label look
  empty, look identical to another label, or reorder the text after it. That also refuses emoji
  joined with a zero-width joiner, which was accepted as the cost of a simple rule; invisible
  characters outside those categories (a Hangul filler, a braille blank) still pass.
- **The catch-all rows are keyed in a `Map`** (review of PR #262). Keying them apart from label
  names first used an object literal, which made a label named `__proto__` crash the dashboard when
  rows were split by label, and `constructor` or `toString` show as an empty catch-all row.

### Stage 2: starting and resuming claude tasks from the dashboard (2026-09-26, issue #257)

Issue #252's open questions 1 (what resuming does) and 2 (the security design of a new command path)
were decided with the operator before implementation, from a proposal checked against claude 2.1.283
and tmux 3.4 in the development environment. The operator decided:

- **claude only.** Codex tasks are known only by a pid, so they cannot carry labels, and a codex
  process that exited is not listed, so there is nothing to resume; codex could not be checked
  either, since it was not installed. Codex support is issue #264. *Superseded by the issue #264
  entry above.*
- **The tmux session ends with claude.** Keeping a login shell in it (so the last output stays
  readable) was the alternative; ending it makes the task list as stopped again, which is what the
  board is for.
- **A resumed task keeps its record.** A task marked done that is resumed stays marked done and shows
  its running column, as #256's rules already say; clearing done on resume was the alternative.
- **Starting a task opens no pane**; the dashboard waits for the task to be listed and selects it.
- **The working directory is typed**, not only picked from directories tasks already use.
- **A tmux session name that already exists is an error**, not an attach or a replacement.
- **A real Claude Code run is a manual scenario (J21)**: that the first instruction becomes the first
  message, and that the state file names the minted ID, could not be observed here.

The security design, also agreed before implementation:

- **The prompt never enters a command line a shell parses.** A remote start must cross the remote
  login shell, unlike the command center's `exec.CommandContext` argv, so the launch reuses the
  collection's shape — a fixed script on `sh -s` stdin — and carries the directory and the prompt in
  heredocs with a quoted, randomly tagged terminator. Escaping values into a command string was the
  rejected alternative: it makes safety depend on a quoting routine being right for every shell.
- **The prompt travels through a temp file, not tmux's arguments.** Found while verifying: a tmux
  server keeps the arguments of the client that started it as its own process arguments for its
  whole life, so a prompt passed there would stay readable in `ps` long after the task.
- **`--` before the prompt, and `--resume=<id>` in the `=` form.** Command-center history showed a
  prompt starting with `-` being parsed as an option; for `--resume`, verified that
  `claude --resume --version` prints the version, because its value is optional. Session IDs are
  therefore UUIDs only (`--resume` also accepts a title), panemux mints the ID for a new task with
  `--session-id` (so its labels can be recorded at once), and a resume uses only an ID the host
  lists as a stopped claude task at that moment.

Chosen while it was built:

- **claude is found through the login shell when `PATH` lacks it.** An SSH exec channel's `PATH`
  rarely holds a per-user install such as `~/.local/bin`; only an absolute path to an executable is
  run.
- **Host refusals are fixed codes** (`no-tmux`, `no-cwd`, `no-claude`, `tmux-exists`,
  `tmux-failed`, `prompt-file`) mapped to fixed messages, so no host output reaches the response.
- **Labels are checked before the task starts**, and a start whose labels could not be written still
  answers `201` with `records_error`, since the task is already running.
- **A launch during a running collection collects again when it finishes.** That collection began
  before the task existed, and skipping the launch's own collection left the task unlisted until the
  next poll.
- **The working directory is not given to tmux's `-c`** (review of PR #265). tmux expands `-c` as a
  format, and a directory holding `#` silently became the home directory while the launch reported
  success. The fixed `sh -c` inside the session changes to it instead; escaping `#` as `##` was the
  alternative, rejected because it depends on tmux's format rules staying as they are.
- **A resume adds a window to a session of the task's name that no agent runs in** (review of PR
  #265, decided with the operator). A pane opened on the task attaches with `new-session -A`, so
  recreating it after claude exited left a shell session of that name, and every later resume was
  refused with no way to clear it from the dashboard. Rejected alternatives: replacing the pane's
  window with `respawn-pane -k` (kills whatever the shell was running), a different session name
  (the open pane would not show claude, and the shell session lingers), and changing how task panes
  attach (a config-schema change to every `tmux`/`ssh_tmux` pane).
- **Resumes of one (host, session) are serialized inside panemux** (review of PR #265, decided with
  the operator). Reusing a session is decided from the collection a resume makes before launching,
  so two overlapping resumes could both find the session free and start two `claude --resume` on one
  conversation. Checking the session's panes for claude inside the script, just before
  `new-window`, was the alternative; it was not taken because walking the process tree in shell is
  fragile across `ps` implementations and still leaves a gap. Two panemux processes are not
  coordinated.
- **Each resume in flight is tracked per task** (review of PR #265). One tracked ID was replaced by a
  second resume, re-enabling the first task's button while its request still ran.

### Stage 3: summaries of a task's work and what is left (2026-09-26, issue #258)

Issue #252's open questions 3 (depending on the conversation log's format) and 4 (what reaches
`claude -p`), and when summaries are made again, were decided with the operator before
implementation, from facts checked in the development environment (Claude Code 2.1.283, one real
conversation log of 75 lines and 605 KB, and real `claude -p` runs):

- In that log, `attachment` lines held 349 KB and tool results most of the rest; the text of the
  user's and the assistant's messages was 3.6 KB in all. One line was 130 KB. One attachment's kind
  was `credential_org`. The session's first instruction was at the top of the file.
- `claude -p` with the excerpt on stdin and `--json-schema` answered in `structured_output`. One
  summary cost about US$0.03 with the CLI's default model and with `claude-haiku-4-5` alike (most of
  it output tokens and the CLI's own ~6k-token system prompt) and took 4.6 to 22 seconds.

The operator decided:

- **The log is interpreted as little as possible, and a format it does not understand is not
  summarized** (open question 3). The Agent Board's principle is that a private transcript format is
  not part of a contract; the dashboard already reads the log's first `"cwd"` (stage 1), and a
  summary cannot be made without reading the conversation. So the dependency is limited to "each line
  is a JSON object; `type` `user`/`assistant`; `message.content` a string or `text` blocks", and a
  log in which no line has that shape reports `unreadable` rather than falling back to raw text.
  Rejected: passing the log's raw tail without interpreting it (no format dependency, but the text
  would have been almost all tool output and attachments, the part most likely to hold secrets), and
  interpreting it with a raw-text fallback (a format change would silently start sending tool output).
- **claude is given the first user message (4 KiB) and the newest 24 KiB of conversation text**
  (open question 4); the host sends at most the log's first 256 KiB and last 2 MiB for that. Rejected:
  the newest text alone (a summary without the task's goal) and 64 KiB (more tokens and more
  exposure for little gain).
- **Summaries are off unless `task_dashboard.summary.enabled` is set**, the conversation text is not
  masked (no masking can be relied on), and summaries are kept in memory only. The behavior document
  says what is and is not sent. Rejected: on by default.
- **Running tasks that are not working are summarized by the poll, stopped ones when selected**, a
  summary is reused while the log keeps its modification time and size, a busy task is not
  summarized again until it stops working, at most two run at once, and one run is limited to 2
  minutes. Rejected: summarizing every task automatically (up to 50 stopped tasks per host, about
  US$1.50 at first sight of a busy host) and summarizing only on a button.

Chosen while it was built:

- **The log's size joins its modification time as the summary's key.** The collection reported only
  the modification time, in whole seconds; a log written twice in one second would have kept a stale
  summary. The size is a fourth field of the transcript rows, so an older row without it still parses.
- **claude's tools are denied with the command center's list, not removed with `--tools ""`.**
  Checking whether an empty `--tools` leaves the CLI with no tools needed its tool listing, which the
  development environment's permission policy refused; the model's own claim that it still had Bash
  was not evidence either way. `--disallowedTools` is the denial the command center verified.
- **The argv is the command center's otherwise** — no setting sources, strict MCP config, slash
  commands disabled, a minted session ID — plus `--no-session-persistence`, so summaries leave no
  session files under the panemux host's `~/.claude`. The shipped argv was run once against claude
  2.1.283: a Japanese conversation came back summarized in Japanese, and the temporary directory was
  removed.
- **A failure shows a fixed message.** claude's own output can quote the conversation.
- **A failed summary is not retried by the poll for the same log.** Retrying every 10 seconds would
  spend tokens on a failure that is likely to repeat (claude not signed in, say); the detail panel's
  button retries it.
- **A done candidate is an answer with an empty `remaining` list**, not a separate verdict from
  claude, so the candidate and the list shown under it cannot disagree.
- **The card's title button no longer also selects through the card's click handler**, which ran for
  every click and would have asked for a stopped task's summary twice.
- **An `unknown` task is summarized only when asked** (review of PR #268, decided with the operator).
  The first version summarized `unknown` with `wait` and `idle` on the poll. But `unknown` includes a
  running session reporting a status this release does not know, which may be working with its log
  growing at every poll — so a new Claude Code status would have started `claude -p` every 10
  seconds, the failure mode spending money exactly when the format changed. Summarizing it once and
  then treating it like `busy` was the alternative.
- **The fetch picks a session's log the way the collection does** (review of PR #268). The collection
  keys a summary on the first of a session's logs after `sort -rn` over `<mtime> <size> <path>`, while
  the fetch script first read the first one the shell's glob found, and then the newest by mtime
  alone, so a session with logs in two project directories — changed in the same second, in the
  second version — could get a summary of the other log recorded as current. The script now orders
  them with the same line and the same `sort`. Rejecting a fetch whose size differs from the
  collected one was the alternative; a running task's log grows between the two, so it would have
  failed ordinary summaries.
- **A log that cannot be read keeps a disabled button** (review of PR #268, decided with the operator).
  The server does not read such a log again until it changes, so the button did nothing; hiding it
  was the alternative, rejected so the panel still shows that summarizing is what cannot be done.
  The first version kept it disabled even after the log changed, and with `unknown` off the poll
  nothing could summarize such a task again. An `unreadable` or `error` summary is now marked
  outdated once the log changes after that attempt, which enables the button and makes selecting a
  stopped or `unknown` task ask again (decided with the operator). Always enabling the button, with a
  tooltip saying nothing happens until the log changes, was the alternative.

## Agent Board

### Compatibility is checked against a real agmsg release (2026-08-23, PR #176)

Fixture tests could not detect false assumptions about `watch.sh` output or the shape of event-log
message IDs. A second contract tier now installs the pinned agmsg version in CI and runs daily
against the latest release. Its first live run found that treating IDs as numbers prevented the
relay cursor from advancing; the current cursor contract treats IDs as opaque ordered values from
the returned stream.

The design borrows Pact's consumer-owned contract principle, but not Pact tooling: agmsg is a CLI
dependency without an HTTP/message provider verification loop or broker. Direct Go tests against
the installed scripts verify the actual boundary with less machinery.

### Contract incidents favor behavior over environment and wording (2026-08-23 onward, PR #176)

The live contract first asserted a `watch.sh` diagnostic that is absent on its successful path; it
now observes message delivery. Its first CI run also depended on finding an agent in the test
process ancestry, so locks looked live locally and stale on runners. Tests now set agmsg's documented
`AGMSG_AGENT_PID` override and verify that a dead owner can be replaced.

The scheduled canary later failed when agmsg v1.3.1 changed a diagnostic sentence while preserving
the behavior Agent Board needs. The assertion was narrowed to the stable fact that the dropped pair
is named. The canary remains daily because the measured median between 22 releases from v1.0.2
through v1.2.2 was 2.9 days; a weekly sample could span several releases and obscure the cause.

### Version coverage normalizes install provenance (2026-08, PR #176)

An installed `VERSION` may be a bare release, a `v`-prefixed tag, or `git describe` provenance.
Literal comparison to the tested pin falsely warned on supported installs. Panemux now normalizes
those forms and treats later patches on the tested major/minor line as covered. This is a warning-noise
policy, not a semver promise; the real-install contract remains authoritative. Patch tolerance also
avoids forcing pin churn at agmsg's observed multi-day release cadence.

### Usage remains outside the Agent Board status contract (2026-08)

Account-wide usage belongs to the agent provider. Per-pane usage may still be derived by summing the
documented `usage` field already stored in each pane's transcript: that is direct field reading, not
inference from private state. If added, it belongs to panemux's existing pane-inspection surface,
not the cooperative agmsg status schema.

### The own-send ledger is the model-checking pilot (2026-09, PR #243, issue #168)

The ledger was chosen before `Relay.processRow` because it is the smallest self-contained state
machine and had already suffered a multiset-versus-set bug. It established the TLA+, generated
transition graph, and Go conformance tiers at low cost. `Relay.processRow` is the intended next
model; dynamic executor selection is a search-and-retry algorithm better suited to fuzz or property
tests.

### Same-project panes claim an agmsg actas lock (2026-08-22, PR #171)

agmsg's default watcher resolves every identity registered for one project and agent type. Two
Claude panes in the same directory therefore subscribed to each other's messages even though their
board identities were distinct. Bootstrap now follows agmsg's own template: join, claim the pane ID
against the Claude session ID, then run `watch.sh` with the pane ID as its narrowing argument. A
held lock is reported instead of disturbing the owning watcher.

### Dashboard capability is independent from command-center capability (2026-08-22, PR #171)

The dashboard can be enabled without the command center and vice versa. A single
`command_center_enabled` flag could not represent both cases, so `/api/session-token` also returns
`agent_board_enabled`. The dashboard remains read-only; broadcast composition stays in the command
center/API surface.

### Command center pins its own execution context (2026-08-22, PR #171)

Live CLI verification showed that plain `claude -p` can report the ambient interactive session ID,
and inherited settings can broaden an `--allowedTools` policy. The command center now mints and
persists its own session ID, disables ambient setting sources, uses an empty per-query directory,
passes its instructions explicitly, restricts MCP configuration, and combines an allowlist with an
explicit acting-tool denylist. The current security contract and evidence are in
[security/command-center.md](security/command-center.md).

### Browser clients obtain the board token from a dedicated bootstrap endpoint (2026-08-14, PR #170)

The original command-center design covered how its subprocess received a bearer token but not how
the bundled frontend learned a randomly generated token. `GET /api/session-token` was added as the
bootstrap contract. It is deliberately outside the authenticated `/api/board/*` subtree and is
limited to the local/trusted deployment model documented in [security/auth.md](security/auth.md).

### Command-center subprocess arguments are structurally separated (2026-08-14, PR #170)

Adversarial testing against the real `claude` CLI found two independent argv defects: a
dash-prefixed prompt could be parsed as a flag, while ordinary prompts failed because of variadic
flag placement. The runner now constructs a fixed option segment, uses the CLI's option terminator,
and keeps the prompt in the operand position. Exact argv-shape tests are intentionally retained as
a security contract. See [security/command-center.md](security/command-center.md).

### Command center uses panemux's API, not agmsg scripts (2026-08, PR #162)

Direct script execution would have made the LLM responsible for shell-safe composition and would
have limited it to one host's agmsg state. A narrow MCP server exposes only board status, message
history, and broadcast, backed by panemux's authenticated REST API. `internal/board` remains the
only package that invokes agmsg scripts.

### The own-send ledger authenticates sentinel-attributed rows (2026-08, PR #162)

`send.sh --force` does not authenticate the free-text `from` value, so trusting every row from the
reserved `_system` identity allowed impersonation. Panemux now accepts such a row only when it
matches a recent send recorded by panemux itself. Model checking later formalized this multiset
behavior; see [agent-board/model-checking.md](agent-board/model-checking.md).

### Remote message bodies use an encoded wrapper argument (2026-08, PR #162)

agmsg's `send.sh` accepts the body as a positional argument and has no stdin write contract. Panemux
therefore base64-encodes the body, allowlists the encoded alphabet, quotes every
`RunBoardCommand` argument, and uses a fixed remote wrapper to decode immediately before `send.sh`.
Team and agent identifiers use a narrower direct allowlist. The security argument and its explicit
CodeQL-verification caveat live in [security/agent-board.md](security/agent-board.md).

### Agent Board uses agmsg only (2026-08-08, PR #162)

Supersedes an early dual-backend design with a panemux-owned SQLite protocol for Claude panes.
Maintaining two protocols added no capability: agmsg already supplied the messaging contract and
interoperability with multiple agent types. Panemux therefore owns relay/cache behavior but no
message schema or database.

Claude Code's native cross-session messaging was also evaluated and rejected: it is Claude-only,
does not fit arbitrary SSH hosts, and exposes no documented process API that panemux can call.

## Quality gateway

The quality gateway has a denser numbered record. [quality-gateway/decisions.md](quality-gateway/decisions.md)
contains decisions D1–D12 in decision order, including evidence and rejected alternatives. The
rollout sequence is retained there rather than in the current [quality-gateway guide](quality-gateway.md).

Key milestones were:

| Date | Change | Decision |
|---|---|---|
| 2026-08-28 | Real-router tests, wider coverage scope, agent hooks, red-check, and scenario checks | D1–D6 |
| 2026-08-30 | Diff-scoped block coverage and mutation measurement | D7–D8 |
| 2026-09-03 | Go-produced contract fixtures and accessibility ceilings | D10–D11 |
| 2026-09-15 | Mutation findings became a blocking gate | D9 |
| 2026-09-21 | TLA+ transition export plus Go conformance replay | D12 |
