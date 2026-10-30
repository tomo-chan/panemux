# Architecture

This guide is the concise map of the current system. It identifies component ownership and the
boundaries a change must preserve. Runtime contracts live in [Behavior specification](behavior.md),
security requirements in [Security design](security.md), and historical rationale in the
[Decision log](DECISIONLOG.md).

## System structure

Panemux is one Go process with an embedded React application. The Go side owns configuration,
processes, remote connections, persistence, HTTP APIs, and WebSocket transport. The browser owns
layout rendering, terminal emulation, interaction state, and presentation.

```text
                         Go process
  config ---------------------------------------------------+
    |                                                       |
    v                                                       v
  startup -> session manager -> PTY / SSH / tmux       embedded SPA
                 |                                         |
                 +------ REST + WebSocket server -----------+
                                      |
                                      v
                                browser frontend
```

## Backend component map

| Component | Current responsibility |
|---|---|
| `main.go` and root helpers | Parse options, load config, construct dependencies, start sessions and optional subsystems, serve, and shut down. |
| `internal/config` | Load, normalize, validate, and persist YAML. `Data` is the serializable domain model; `Config` adds file and lookup context. |
| `internal/agent` | Register every coding agent panemux knows: its stable ID, display name, aliases, the process rules that detect it, and the capabilities it declares. Imports nothing else from panemux. It declares; the packages that use a capability implement it. See [Agent registry](agent-registry.md). |
| `internal/session` | Provide one lifecycle interface for local PTY, SSH, local tmux, and tmux-over-SSH sessions. Optional capability interfaces expose CWD, Git context, port forwarding, and agent detection only where supported. Holds the per-session Workdir capability (`workdirAdapters`, one adapter per agent that declares `workdir`) and the one parser of Claude Code's `~/.claude/sessions` files, both of which `internal/tasks` uses too. `CommandConn` is an SSH connection for short non-interactive commands, dialed with the same dialer panes use. |
| `internal/host`, `internal/hostexec` | Define host identity and fixed-script execution, including a separate arbitrary stdin stream; own local and shared per-host SSH adapters, lazy dialing, cooldown and reconnect. |
| `internal/hosttools` | Manage tool status, installation and Codex hook automatic-update settings and background checks; define the `Tool` port (`Check` / `Install`), optional adapter prerequisites, and shared status. Coordinate per-tool/host call admission, update notification and draining. Consume successful task-collection notifications and own cancellation and joining of tool checks. |
| `internal/codexhook` | Implement the tool port using the shared host execution port; own the embedded permission hook, fixed check/install scripts and hooks.json parsing. |
| `internal/tasks` | Collect the task dashboard's agent sessions from the panemux host and every `ssh_connections` host with one fixed script, composed at startup from a collect adapter per agent, through the shared host execution port; probe each host's agent CLIs with a second fixed script and keep what can be used per host and agent, including what the collection could not read (`Availability`); list the registered agents with their adapters' notes (`Agents`) and decide which actions each task offers and why one cannot be taken (`TaskActions`, `StateNote`); keep the done and label records in `~/.config/panemux/tasks.json` (`RecordStore`); start and resume claude and codex tasks in detached tmux sessions with one fixed launch script over the same connection (`Launch`, `Resume`), holding a new codex task's labels until its session is collected (`PendingLabels`); read a task's conversation log with a third fixed script and summarize it with its own agent CLI on the panemux host, through a summary adapter per agent (`summaryAdapters`), keeping the summaries in `~/.config/panemux/task-summaries.json` (`SummaryStore`) and reusing one while the excerpt it was made from is unchanged (`Summaries`, `RequestSummary`). |
| `internal/taskevents` | Observe the running tasks on every host through `internal/tasks`'s lightweight collection while the task event stream has a subscriber, each host on its own cycle; keep what was last observed, publish each difference as an ordered event, assign unsigned waits their `wait_id`, and fan the events out to subscribers without waiting on any one of them. It decides nothing about notifications or panes. |
| `internal/api` | Implement REST handlers and mount the route set. It is the single source of truth for API registration. |
| `internal/ws` | Bridge session bytes and control messages to terminal WebSockets, stream command-center events, and serve the task event stream (`/ws/tasks/events`). |
| `internal/requestsecurity` | Guard every API and WebSocket before side effects using browser Origin, Referer and Fetch Metadata evidence. |
| `internal/server` | Compose middleware, API routes, WebSocket routes, static assets, and SPA fallback into the production router. |
| `internal/preview` | Resolve a clicked HTML or Markdown path against a session's working directories, refusing any file outside them, register the file under a random token for the server's lifetime, and serve it and the files under its root sandboxed in an opaque origin, rendering Markdown. For an SSH session the same is done on the host over SFTP (`Remote`, `ResolveRemote`, `Registry.OpenRemote`), and a clicked localhost URL of the host is relayed to a port of its own on `127.0.0.1` (`Relays`). |
| `internal/portforward` | Maintain short-lived loopback listeners that forward callback traffic through a session's SSH connection. |
| `internal/board` | Define messaging and participation ports, validate senders, relay rows across configured hosts, and maintain the in-memory status/history view. |
| `internal/agmsg` | Implement board messaging and participation through agmsg scripts, including path discovery, versions, parsing, agent type mapping, and the pinned-source tool adapter (cache, archive transfer, installer and prerequisites). |
| `internal/commandcenter` | Run one headless Claude query at a time, persist its session/history state, and stream structured events. |
| `internal/boardmcp` | Expose the three board operations to the command-center subprocess through a narrow stdio MCP server backed by panemux REST APIs. |
| `internal/homedir`, `internal/cachedir` | Centralize OS directory lookups and provide scoped test substitutions. |
| `internal/fileops` | Centralize atomic persistent writes and injectable filesystem operations. |

## Frontend component map

| Component | Current responsibility |
|---|---|
| React application shell | Load initial state, render workspace navigation and overlays, and coordinate top-level actions. |
| `useLayout` | Own the normalized recursive layout tree, optimistic edits, and persistence requests. |
| `TerminalPane` and `useTerminal` | Own xterm.js setup, fitting, addons, pane input, replay sequencing and suppression, pending terminal messages, and terminal lifecycle. |
| `useWebSocket` | Own the pane connection, reconnect behavior, send-if-open transport, and validation of structured text control frames. |
| `PreviewSplit` and `usePreview` | Split a workspace pane's or the Type in pane popup's body into the terminal and a preview of a clicked HTML or Markdown path (`utils/preview`) or a localhost URL, own the divider and its per-pane / per-task ratio, and hide the preview when the terminal disconnects. |
| `usePaneUrlOpen` | The one opener for a terminal's printed links, OSC 8 links and browser-shim requests: hold the ones from terminal output for approval, and open each once — in a tab with its callback forward, or a localhost URL on the panemux host in the preview. |
| task event store (`useTaskEvents`, `utils/taskEvents`) | Hold one `/ws/tasks/events` connection per tab and the tasks and hosts it reports, validated and in `seq` order; reconnect from a fresh snapshot on any gap or close, 2 to 30 seconds apart with no limit. |
| attention and notifications (`App`, `utils/taskEvents`) | Project the task event store and visibility changes into pane/workspace indicators, browser notifications and the task dashboard's state column, matching tasks to panes with `utils/taskBoard` and recording the `wait_id`s each tab notified, and those whose attention a person cleared, in its session storage. |
| Agent Board hooks and panels | Poll status/message APIs, stream command-center output, and present dashboard, palette, and history overlays. |
| `TaskDashboard` and `useTasks` | Poll `GET /api/tasks` while the task dashboard is shown, present tasks as a kanban by state, save done and labels through `PUT /api/tasks/records`, start and resume tasks through `POST /api/tasks` and `POST /api/tasks/resume` (`NewTaskDialog`), and match each task to the pane attached to its tmux session, or to the `local` / `ssh` pane its agent's `PANEMUX_PANE_ID` names (`utils/taskBoard`). Type in pane opens and ends the board's temporary attach through `POST`/`DELETE /api/tasks/attach` (`useTaskInput`) and shows it in `TaskInputPopup` / `TaskTerminal`. A host chip opens `HostConnectDialog`: `Open` adds an `ssh` / `ssh_tmux` pane (`paneConfigForHost`), and Type in pane opens a host terminal through `POST`/`DELETE /api/hosts/terminal` (`useHostTerminal`) shown in `HostTerminalPopup`; an `ssh_tmux` session name comes from `POST /api/hosts/session-name`. The menu's `HostTools` lists tools through the shared `HostToolItem`; `codexHookTool` supplies Codex-specific copy and actions. It reads and installs the host's codex permission hook through `GET`/`POST /api/tasks/codex-hook`, and turns updating it by itself on or off through `POST /api/tasks/codex-hook/auto-update`; `agmsgTool` adds manual pinned Install / Update through `/api/hosts/agmsg` when Agent Board is enabled; the panemux host's chip opens its tool items without connection actions. |
| Zod schemas | Runtime-validate structured success payloads and control frames for which schemas are defined. Generated TypeScript types derive from these schemas. |

## State and ownership

- The YAML config is the durable source for workspace layout and connection settings.
- The session manager owns live terminal sessions and replay buffers; the browser does not own
  process lifetime.
- Each terminal pane owns one browser-side terminal instance and WebSocket lifecycle.
- The backend resolves live Git/PR context from the active pane work directory and caches the
  result for the behavior-defined interval.
- The API handler owns the shared `internal/hostexec` pool for its process-service lifetime; closing
  or replacing the task consumer does not close an injected pool. The pool owns one command
  connection per SSH host, shared by both boards and separate
  from pane sessions. The task dashboard keeps the labels of codex tasks started but not
  yet collected (in memory), and, on the panemux host, the done and
  label records a person set (`~/.config/panemux/tasks.json`) and, while summaries are enabled, the
  task summaries (`~/.config/panemux/task-summaries.json`, kept 30 days after a task was last listed);
  everything else is read from the agents' own files on every host again at each collection. The pane a task belongs to is derived in the browser from
  the current workspaces.
- The task event stream's last observation, `epoch`, `seq` and unsigned wait IDs are in memory and
  last until the server restarts. Which waits a tab has already notified is in that tab's
  session storage; the server keeps no per-viewer state.
- Agent Board's status/history cache is in memory. Relay cursors, bootstrap state, command-center
  history/session state, and the task dashboard's done and label records and summaries use dedicated
  persisted files.
- Host tool management owns Codex hook settings in `~/.config/panemux/codex-hook.json` and the
  per-process automatic-update outcomes. It shares the handler-owned execution pool and closes
  its background checks before that pool closes. Task collection reports success through a generic
  callback and has no tool installation or automatic-update state.
- Structured success payloads and control frames with declared schemas are parsed through Zod.
  Terminal binary frames and some API error bodies use separate handling paths.

## Main flows

### Terminal flow

Browser keystrokes travel over the pane WebSocket to the session. Session output returns as binary
frames and is written to xterm.js. Resize messages update both the terminal emulator and underlying
PTY or remote terminal dimensions.

### Configuration flow

Startup loads and normalizes YAML into `config.Data`, adds runtime file context in `config.Config`,
and validates cross-references before sessions start. Browser layout/workspace mutations go through
REST handlers and are persisted atomically when a save path is available.

### Agent Board flow

Board-enabled agents report through agmsg. `internal/board` polls configured hosts, validates and
relays rows, and updates `BoardCache`. The browser obtains the token from the loopback-only
bootstrap endpoint and sends it to the board REST and command-center WebSocket routes; the
command-center MCP client also uses bearer-authenticated board REST APIs. Only `internal/agmsg`
invokes agmsg scripts, through `host.Runner`; board logic uses its message ports. Full detail is in
[Agent Board architecture](agent-board/architecture.md).

### Task dashboard flow

While the dashboard is on screen, the browser polls `GET /api/tasks`. `internal/tasks` runs one
fixed script on every host at once — locally with `sh -s`, remotely over that host's reused
`CommandConn` — parses what it prints, and derives each task's state and location: its tmux
session, or for an agent outside tmux the pane ID its environment carries, which `local` and `ssh`
panes export to their shell as `PANEMUX_PANE_ID`. The API handler adds each working directory's git
and pull-request metadata, the issues the pull request closes, the references
`task_dashboard.autolinks` finds in the branch name and PR title, and each task's done and label
record from `tasks.RecordStore`; `PUT /api/tasks/records` replaces one record. Opening a task
creates or focuses a `tmux` / `ssh_tmux` pane through the ordinary pane APIs, or focuses the
`local` / `ssh` pane an agent outside tmux runs in. `POST /api/tasks/attach` instead opens a
temporary tmux client on a task's session that the layout never holds: the handler finds the task
with a fresh collection of its host (`tasks.Service.FindTask`), registers an attach-only
`tmux attach-session -t =<name>` session with the session manager under a `board-` ID served by
`/ws/{id}`, and removes it on `DELETE` or once no WebSocket has read it for 10s. `POST /api/hosts/terminal` registers a host terminal the same way — an `ssh` / `ssh_tmux` session on an `ssh_connections` entry, built by `session.CreateFromConfig`, new on every request and marked as a host terminal so that each route's `DELETE` ends only its own kind. The dashboard's
Type in pane popup (`TaskInputPopup`) reads it through `TaskTerminal`, which uses the same
`useTerminal` as a pane but asks for no `/api/sessions/{id}/restart` on a disconnect: it opens the
attach again instead. `POST /api/tasks` and `POST /api/tasks/resume`
run a second fixed script the same way — `sh -s` with the script on stdin — which starts the agent
in a detached tmux session. The script is composed at startup from a launch and a resume adapter per
agent (`internal/tasks/launch_adapters.go`), each a fixed fragment under the case of its agent and
mode; an agent that declares neither is refused before anything runs, and so is one whose CLI the
host's probe found missing. A resume first collects the host again to confirm the session is stopped
there, and a start records its labels in `tasks.RecordStore` — when the launch names no session ID
(codex picks its own once started), `tasks.PendingLabels` holds them in memory under the host, the
agent and the tmux session, and the first `GET /api/tasks` that finds exactly one session of that
agent in that tmux session records them. When
`task_dashboard.summary.enabled` is set, `GET /api/tasks` also attaches each task's summary and starts
the ones that are due, in the background and at most two at a time: a third fixed script reads the
task's conversation log on its host, and the matching agent CLI on the panemux host summarizes the conversation
text extracted from it; `POST /api/tasks/summary` asks for one.
The task event stream observes with the lightweight collection: the same script without its
searches for stopped sessions (`attentionScript`), the running tasks only, and none of the handler's
git, record, label or summary work. Both give a waiting task the same `wait_signature`, derived from
the wait start the agent recorded on its host. Full behavior is in
[Task dashboard](behavior/tasks.md).

### Task event flow

While at least one browser tab holds `/ws/tasks/events`, `internal/taskevents` observes every host
with the lightweight collection, each host 5 seconds after its previous observation finished, and
publishes the difference from what it last observed: tasks added, changed and removed, and hosts'
statuses, each numbered by `seq` within the server's `epoch`. A new subscriber first gets a snapshot.
A host that fails publishes only its status; its tasks stay as last observed. Observation stops
when the last subscriber leaves, keeping what it observed. The server publishes every state change and decides
nothing about it: each tab's store feeds the task dashboard's states, the pane and workspace
attention and the browser notifications, and the tab matches tasks to panes itself. Full behavior is
in [Task events](behavior/task-events.md).

```text
 hosts ──attentionScript──▶ internal/tasks ──▶ internal/taskevents ──▶ /ws/tasks/events
                                               (model, diff, seq,        │ one per tab
                                                fan-out)                 ▼
                                                     task event store ──▶ dashboard states
                                                                      ├─▶ pane / workspace attention
                                                                      └─▶ browser notifications
```

### URL-open flow

Pane-side URL detection or the browser shim reports a URL to panemux. Ordinary URLs open in the
browser. Eligible loopback callback URLs may create a temporary local listener backed by SSH
`direct-tcpip`. Full behavior and constraints are in [Opening URLs from a pane](behavior/url-open.md)
and [URL-open security](security/url-open.md).

New `ssh_tmux` sessions on tmux 3.3 or newer receive shim settings through session-local `-e`
operands, with a fixed initial-pane bootstrap for PATH and the configured shell/command. Their shim
uses pane-local tmux passthrough to deliver the same OSC event as an ordinary
SSH pane; existing sessions and the tmux server's environment are not reconfigured.

### Preview flow

A path the terminal shows as an HTML or Markdown link is sent to
`POST /api/sessions/{id}/preview`, which resolves it on the session's host and returns a
`/preview/<token>/…` address. `PreviewSplit` frames that address beside the terminal; Open in new tab
opens the same address. A localhost URL on the panemux host needs no server step: `usePaneUrlOpen`,
the one opener for printed links, OSC 8 links and browser-shim requests, hands it to `usePreview`,
and `PreviewSplit` frames the URL in its own origin, inside a `srcdoc` document whose policy keeps the frame on loopback.
For an `ssh` or `ssh_tmux` session the API's `previewHosts` keeps one `preview.Remote` per connection
a session was dialed with (`session.PreviewTarget`), connected with `session.DialPreviewConn` — the
pane dialer, its own connection — and files are read over its SFTP subsystem. A localhost URL
`usePreview` is handed for such a session is sent as `url`; `preview.Relays` gives the host's address
and port a listener on `127.0.0.1:0` and returns that address, which `PreviewSplit` frames as it
frames a localhost page. Behavior is in [the split preview](behavior/preview.md), and the isolation in
[preview security](security/preview.md).

## Trust boundaries

- Core terminal routes assume a trusted deployment and are not an authenticated multi-user surface.
- Every API and WebSocket uses the [shared browser request guard](security/auth.md#shared-browser-request-guard)
  before handler side effects. Another loopback port is refused; browser requests without Origin
  require a matching Referer and consistent Fetch Metadata. CLI requests without browser headers
  remain supported. CORS and development proxies use this same authority boundary.
- `/ws/tasks/events` is unauthenticated like `/ws/{sessionID}`. Because opening it makes every host
  be dialed, it refuses cross-site requests before upgrading, as the task routes under `/api/tasks` do.
- `/api/board/*` and `/ws/board/command` are bearer-authenticated. The unauthenticated
  `GET /api/session-token` bootstrap route sits outside that subtree because it returns the bearer
  token itself; it accepts only requests whose remote address and `Host` are loopback. Non-loopback
  use of the authenticated routes also requires transport encryption.
- User-controlled values never select an arbitrary executable. Shell paths, tmux names, remote
  paths, and subprocess operands follow the per-sink rules in [Security design](security.md).
- Panemux runs no copy of itself on remote hosts. The task and agent boards share a per-host
  command connection through `internal/host` and `internal/hostexec`, running trusted fixed scripts
  with variable values in positional arguments. Terminal panes keep their own SSH sessions. The
  split preview keeps its own SSH connection per host too, and runs no command over it: it reads files
  over SFTP and opens direct-tcpip channels to the host's loopback.
- Previewed HTML and Markdown are untrusted content. They are served under `/preview/<token>/` in a
  sandboxed opaque origin, so their requests to `/api/*` and `/ws/*` are cross-site and refused; the
  token, issued only by the guarded API, is what grants access to a file
  ([preview security](security/preview.md)). A relayed localhost page of an SSH host runs in the
  origin of its relay port on `127.0.0.1`, another loopback port, which the request guard refuses.
- The browser is untrusted input to Go handlers. Structured backend payloads with declared frontend
  schemas remain untrusted until validation succeeds; other input paths apply their own parsing and
  bounds checks.

## Deep dives

- [Behavior details](behavior/) — API, WebSocket, frontend, SSH, notifications, and URL-open contracts.
- [Security details](security/) — requirements grouped by execution and trust boundary.
- [Agent registry](agent-registry.md) — agent IDs, aliases, detection rules, capabilities, and session keys.
- [Agent Board details](agent-board/) — messaging, relay, bootstrap, command center, and limitations.
- [UI design](ui-design.md) — workspace, pane, modal, attention, and Agent Board interactions.
- [Quality gateway](quality-gateway.md) — how architectural contracts are verified.
- [Decision log](DECISIONLOG.md) — tradeoffs and superseded approaches removed from this guide.

Agent Board enablement and delivery mode are instance-wide. Its relay accepts all configured
pane identities on their owning hosts; bootstrap supplies a synchronized participation snapshot
for the authenticated status API. Dashboard membership is based on detected agents or reports.
The agmsg installation location is fixed per host. Host tool management coordinates manual
Install / Update with board call leases and discovery revisions; automatic updating is not currently executed.
