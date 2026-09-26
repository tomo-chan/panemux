# Behavior Specification

## Startup Sequence

1. Parse `--config`, `--open`, and `--port`.
2. Load config: if `--config` is given, load that file; otherwise try `~/.config/panemux/config.yaml`; if that file does not exist, use the built-in default config with `~/.config/panemux/config.yaml` as the save path.
3. Override the configured port if `--port` is set.
4. Create the in-memory session manager.
5. Traverse every configured workspace layout and create each pane session, including panes in
   inactive workspaces.
6. Start enabled Agent Board relay/bootstrap and command-center services.
7. Start the HTTP server and serve the embedded frontend.
8. On `SIGINT` or `SIGTERM`, shut down background services, the server, and all sessions.

If a configured session fails to start, the server logs a warning and continues booting other sessions.

## Configuration Rules

The YAML config defines:

- `server.host`, `server.port`, and the Agent Board `server.auth_token`
- `ssh_connections`, which are also the hosts the task dashboard collects agent sessions from
- `workspaces`, including the active workspace, tab position, vertical bar width, and each
  workspace's recursive layout
- optional `display` settings, including `display.task_dashboard_shortcut`
- optional `url_open` settings
- optional `agent_board` and `command_center` settings

Legacy top-level `layout` is accepted at load time and normalized to one `default` workspace. The
next save writes the current `workspaces` shape.

Layout rules:

- `direction` must be `horizontal` or `vertical`
- sibling `size` values must sum to `100` within a small tolerance
- pane IDs must be globally unique across workspaces
- `ssh` and `ssh_tmux` panes must reference a defined SSH connection
- `tmux` and `ssh_tmux` panes must define `tmux_session`
- workspace IDs must be unique and `workspaces.active` must name an existing workspace when set
- `workspaces.tab_position` must be `top`, `bottom`, `left`, or `right`

`display.task_dashboard_shortcut` rules:

- one letter `A`–`Z` in either case, naming the `Cmd/Ctrl+Shift+<letter>` shortcut that switches
  between the task dashboard and the workspaces
- `K` and `B` are refused, since the command palette and the Agent Board dashboard use them
- omitted means `S`; `GET /api/display` always reports the effective letter in upper case, and an
  omitted value is never written back on save

`url_open` rules:

- `url_open.browser_shim` gates browser-open interception (see "Opening URLs from a pane")
- it is a tri-state: omitted means enabled, and an omitted block is never written back into an
  operator's config file on save

Path behavior:

- `~/` in SSH key paths and pane working directories is expanded at load time

## Document map

This document keeps the whole-process behavior: startup, configuration, operational assumptions, and
distribution. Per-surface behavior lives in [`docs/behavior/`](behavior/), grouped by area:

| Sections | Document |
|---|---|
| SSH Connections | [ssh.md](behavior/ssh.md) |
| Agent Attention Notifications | [notifications.md](behavior/notifications.md) |
| REST API | [rest-api.md](behavior/rest-api.md) |
| Agent Board REST API; `GET /api/session-token` | [board-api.md](behavior/board-api.md) |
| WebSocket Protocol; Command Center WebSocket Protocol | [websocket.md](behavior/websocket.md) |
| Frontend Runtime Behavior; Pane Git and PR metadata | [frontend.md](behavior/frontend.md) |
| Opening URLs from a Pane | [url-open.md](behavior/url-open.md) |
| Task Dashboard; `GET /api/tasks`; `POST /api/tasks/hosts/{name}/reconnect`; `PUT /api/tasks/records` | [tasks.md](behavior/tasks.md) |

## Operational Assumptions

- The app is designed for local or otherwise trusted usage.
- Long-lived WebSocket connections are expected, so HTTP write timeout is disabled.
- The server serves the SPA with fallback to `index.html` for non-asset routes.
- Browser support is not uniform for terminal glyph rendering. Chrome is the validated browser for prompt themes that use Powerline private-use glyphs.
- oh-my-zsh `agnoster` uses Powerline glyphs such as `` and ``. Correct rendering depends on both the browser and locally installed Powerline-compatible fonts.

## Distribution and Installation

- Releases are distributed as versioned `.tar.gz` archives through GitHub Releases.
- The release archives contain the embedded-frontend CLI binary plus reference files such as `config.example.yaml`.
- macOS installation is expected through `install.sh`, which downloads the correct archive from GitHub Releases and installs the binary into a user-local bin directory by default.
- Windows installation is supported through WSL2 by using the Linux release archive and the same shell-based installer flow.
- The release pipeline builds the frontend first, then cross-compiles the Go binary so the shipped executable already contains `frontend/dist`.
- Repository automation is Makefile-first: CI and release workflows call `make` targets instead of duplicating raw `npm` and `go` command sequences in workflow steps.

Example installation flow:

```sh
curl -fsSL https://raw.githubusercontent.com/OWNER/REPO/main/install.sh | bash -s -- --repo OWNER/REPO
```

Example version-pinned installation:

```sh
MST_REPO=OWNER/REPO MST_VERSION=v1.2.3 ./install.sh
```

## Related Documents

- Product overview: [overview.md](overview.md)
- Architecture and security design: [architecture.md](architecture.md)
- Security requirements for implementation: [security.md](security.md)
- UI intent: [ui-design.md](ui-design.md)
- Agent Board design: [agent-board.md](agent-board.md)
- Developer workflow rules: [../DEVELOPMENT.md](../DEVELOPMENT.md)
