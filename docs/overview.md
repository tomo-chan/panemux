# Application Overview

PaneMux is a browser-based terminal workspace. One server process renders multiple local, SSH, and
tmux sessions in a resizable split layout and serves the browser UI that controls them.

Use this page for the product-level picture. Continue to [Architecture](architecture.md) for system
structure, [Behavior specification](behavior.md) for runtime contracts, or the
[documentation index](README.md) to choose another topic.

## What it provides

- Recursive split-pane workspaces defined in YAML and editable from the browser.
- Four terminal backends: `local`, `ssh`, `tmux`, and `ssh_tmux`.
- Raw terminal streaming over WebSocket, including resize and lifecycle control messages.
- Workspace tabs, layout persistence, pane creation/removal, and pane-level Git/PR context.
- Browser notifications and attention indicators for terminal activity.
- Loopback OAuth callback forwarding for CLI login flows running in SSH-backed panes.
- Optional Agent Board status aggregation, cross-pane messaging, and a command center.
- A self-contained CLI binary with the React frontend embedded at build time.

## System at a glance

```text
config.yaml
    |
    v
Go server -------- REST APIs -------- browser workspace
    |                                      |
    +-- session manager                    +-- React layout and controls
    |     +-- local PTY                    +-- xterm.js terminal panes
    |     +-- SSH shell                    +-- Agent Board overlays
    |     +-- local tmux
    |     `-- tmux over SSH
    |
    `------------ WebSocket terminal streams ---------^
```

Each terminal pane owns one session and one WebSocket connection. Binary frames carry terminal
bytes; text frames carry control messages. The Go process also serves the built frontend, so the
backend and UI are released together.

## Operating model

- Configuration supplies reproducible startup state. Runtime edits are persisted when panemux was
  launched with a writable config path.
- Sessions are isolated behind one interface, so a pane failure does not stop other panes or the
  server.
- Local and SSH-backed panes expose the same browser interaction model even when process control
  and filesystem inspection happen on different hosts.
- Agent Board is additive. Terminal sessions continue to work when agmsg is absent or board
  integration is disabled.

## Current boundaries

- Panemux is intended for a local machine or otherwise trusted network. Core terminal REST and
  WebSocket routes do not implement user authentication. Agent Board operations under
  `/api/board/*` and `/ws/board-command` use a bearer token; the unauthenticated
  `GET /api/session-token` bootstrap route returns that token only after loopback remote-address and
  `Host` checks. These controls do not turn the whole server into an Internet-facing multi-user
  service.
- Panemux does not terminate TLS. Non-loopback Agent Board deployments require a TLS-terminating
  reverse proxy; see [Auth token and transport encryption](security/auth.md).
- Loopback URL forwarding assumes the browser and panemux server run on the same host. A browser on
  another machine cannot receive a listener bound to the panemux host's loopback interface.
- Chrome is the validated browser for terminal themes that depend on Powerline private-use glyphs.
- Release archives target macOS and Linux. Windows use is through WSL2 rather than a native package.
- Runtime pane creation is intentionally narrower than the full configuration format and is mainly
  used by browser pane splitting.

## Where to continue

- [Architecture](architecture.md) — components, ownership, and trust boundaries.
- [Behavior specification](behavior.md) — startup, configuration, APIs, and browser behavior.
- [Security design](security.md) — implementation requirements by sensitive sink.
- [UI design](ui-design.md) — interaction and presentation rules.
- [Decision log](DECISIONLOG.md) — why the present design replaced earlier approaches.
