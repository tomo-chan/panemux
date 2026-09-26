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
| `internal/session` | Provide one lifecycle interface for local PTY, SSH, local tmux, and tmux-over-SSH sessions. Optional capability interfaces expose CWD, Git context, port forwarding, and Agent Board operations only where supported. `CommandConn` is an SSH connection for short non-interactive commands, dialed with the same dialer panes use. |
| `internal/tasks` | Collect the task dashboard's agent sessions from the panemux host and every `ssh_connections` host with one fixed script, and own one reused `CommandConn` per host. |
| `internal/api` | Implement REST handlers and mount the route set. It is the single source of truth for API registration. |
| `internal/ws` | Bridge session bytes and control messages to terminal WebSockets and stream command-center events. |
| `internal/server` | Compose middleware, API routes, WebSocket routes, static assets, and SPA fallback into the production router. |
| `internal/portforward` | Maintain short-lived loopback listeners that forward callback traffic through a session's SSH connection. |
| `internal/board` | Invoke agmsg through its documented scripts, relay rows across configured hosts, and maintain the in-memory status/history view. |
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
| `usePaneUrlOpen` | Receive validated URL-open events and coordinate browser navigation/callback forwarding. |
| attention and notification hooks | Convert terminal activity and visibility changes into pane/workspace indicators and browser notifications. |
| Agent Board hooks and panels | Poll status/message APIs, stream command-center output, and present dashboard, palette, and history overlays. |
| `TaskDashboard` and `useTasks` | Poll `GET /api/tasks` while the task dashboard is shown, present tasks as a kanban by state, and match each task to the pane attached to its tmux session (`utils/taskBoard`). |
| Zod schemas | Runtime-validate structured success payloads and control frames for which schemas are defined. Generated TypeScript types derive from these schemas. |

## State and ownership

- The YAML config is the durable source for workspace layout and connection settings.
- The session manager owns live terminal sessions and replay buffers; the browser does not own
  process lifetime.
- Each terminal pane owns one browser-side terminal instance and WebSocket lifecycle.
- The backend resolves live Git/PR context from the active pane work directory and caches the
  result for the behavior-defined interval.
- The task dashboard keeps nothing but its per-host SSH connections: each collection reads the
  agents' own files on every host again. The pane a task belongs to is derived in the browser from
  the current workspaces.
- Agent Board's status/history cache is in memory. Relay cursors, bootstrap state, and command-center
  history/session state use dedicated persisted files.
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
command-center MCP client also uses bearer-authenticated board REST APIs. Only `internal/board`
invokes agmsg scripts. Full detail is in
[Agent Board architecture](agent-board/architecture.md).

### Task dashboard flow

While the dashboard is on screen, the browser polls `GET /api/tasks`. `internal/tasks` runs one
fixed script on every host at once — locally with `sh -s`, remotely over that host's reused
`CommandConn` — parses what it prints, and derives each task's state and tmux location. The API
handler adds each working directory's git and pull-request metadata, the issues the pull request
closes, and Jira links built from `task_dashboard.jira_url`. Opening a task creates or
focuses a `tmux` / `ssh_tmux` pane through the ordinary pane APIs. Full behavior is in
[Task dashboard](behavior/tasks.md).

### URL-open flow

Pane-side URL detection or the browser shim reports a URL to panemux. Ordinary URLs open in the
browser. Eligible loopback callback URLs may create a temporary local listener backed by SSH
`direct-tcpip`. Full behavior and constraints are in [Opening URLs from a pane](behavior/url-open.md)
and [URL-open security](security/url-open.md).

## Trust boundaries

- Core terminal routes assume a trusted deployment and are not an authenticated multi-user surface.
- `/api/board/*` and `/ws/board-command` are bearer-authenticated. The unauthenticated
  `GET /api/session-token` bootstrap route sits outside that subtree because it returns the bearer
  token itself; it accepts only requests whose remote address and `Host` are loopback. Non-loopback
  use of the authenticated routes also requires transport encryption.
- User-controlled values never select an arbitrary executable. Shell paths, tmux names, remote
  paths, and subprocess operands follow the per-sink rules in [Security design](security.md).
- Panemux runs no copy of itself on remote hosts. SSH-backed features use the existing SSH session
  and operator-installed remote tools; the task dashboard uses its own SSH connection per host and
  runs only a fixed script over it.
- The browser is untrusted input to Go handlers. Structured backend payloads with declared frontend
  schemas remain untrusted until validation succeeds; other input paths apply their own parsing and
  bounds checks.

## Deep dives

- [Behavior details](behavior/) — API, WebSocket, frontend, SSH, notifications, and URL-open contracts.
- [Security details](security/) — requirements grouped by execution and trust boundary.
- [Agent Board details](agent-board/) — messaging, relay, bootstrap, command center, and limitations.
- [UI design](ui-design.md) — workspace, pane, modal, attention, and Agent Board interactions.
- [Quality gateway](quality-gateway.md) — how architectural contracts are verified.
- [Decision log](DECISIONLOG.md) — tradeoffs and superseded approaches removed from this guide.
