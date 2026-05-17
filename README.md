# panemux

**Browser-based terminal multiplexer** — split your terminal into multiple panes, each connecting to a local shell, remote SSH host, or tmux session, all rendered in your browser via xterm.js.

[![CI](https://github.com/tomo-chan/panemux/actions/workflows/ci.yml/badge.svg)](https://github.com/tomo-chan/panemux/actions/workflows/ci.yml)
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go 1.24](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://golang.org)
[![Releases](https://img.shields.io/github/v/release/tomo-chan/panemux)](https://github.com/tomo-chan/panemux/releases)

---

## Features

- **Four pane types** — `local` (shell), `ssh` (remote), `tmux` (local session attach), `ssh_tmux` (SSH → tmux)
- **Recursive split layout** — nest horizontal and vertical splits to any depth
- **Workspace tabs** — define multiple layouts and switch between them with tabs on any edge
- **Drag-to-resize** — drag dividers in the browser to adjust pane sizes
- **Drag-to-move** — drag a pane by its header handle to move it to a workspace edge, another pane edge, or a divider target
- **In-browser layout editing** — split, close, resize, add terminals, move panes, and manage workspaces directly from the main UI with immediate persistence
- **`~/.ssh/config` integration** — reference any host alias from `~/.ssh/config` directly as a `connection` without duplicating entries in YAML
- **Session resilience** — tmux sessions are auto-created when absent; exited panes show a Restart button to reconnect without reloading
- **xterm.js rendering** — full-featured terminal emulation with Unicode and colour support
- **Single binary** — Go backend embeds the compiled frontend; no separate web server needed
- **YAML config** — declare your entire layout and SSH connections in one file; defaults to `~/.config/panemux/config.yaml`

---

## Installation

### Pre-built binary

```sh
curl -fsSL https://raw.githubusercontent.com/tomo-chan/panemux/main/install.sh | sh
```

Or with options:

```sh
./install.sh --repo tomo-chan/panemux --version v0.2.0 --install-dir ~/.local/bin
```

### Build from source

Requirements: **Go 1.24+**, **Node.js 20+**

```sh
git clone https://github.com/tomo-chan/panemux.git
cd panemux
make install-deps   # npm install + go mod download
make build          # outputs bin/panemux
```

---

## Quick start

```sh
# Run with defaults (loads ~/.config/panemux/config.yaml if it exists, otherwise a single local shell)
./bin/panemux

# Load a specific config file
./bin/panemux --config config.yaml

# Override port and open Chrome automatically
./bin/panemux --port 9090 --open
```

Then open [http://localhost:8080](http://localhost:8080) in your browser.

---

## Using panemux

### Daily workflow

1. Start `panemux` and open it in your browser.
2. Click a workspace tab to switch between layouts such as development, operations, or production.
3. Work inside each terminal pane exactly like a normal shell, SSH session, or tmux attach session.
4. Rearrange panes or add terminals directly from the main UI when you need to evolve the layout.

### Workspace tabs

- Each workspace has its own independent layout tree and set of panes.
- Click a tab to switch workspaces without restarting the underlying sessions.
- If a hidden workspace needs attention, its tab flashes until you open it.
- You can add a workspace, rename it inline, delete it, or move the tab bar to the top, bottom, left, or right directly from the workspace bar.

Common uses:

- Keep local development shells in one workspace and production SSH panes in another.
- Put a dedicated tmux-based workspace on one tab and short-lived local shells on another.

### Pane interactions

- **Resize panes** by dragging the split divider between siblings.
- **Split a pane** from its header controls to create a new local pane beside it.
- **Add a terminal** from the workspace bar to create a blank local pane or clone an existing pane's settings before choosing where it should be inserted.
- **Move a pane** by dragging the handle in its header; drop it on a workspace edge to create a new outer split, or on another pane edge / divider to insert it there.
- **Close a pane** from its header controls; the layout collapses automatically.
- **Restart a pane** with the on-screen button if the underlying session exits.
- **Open a linked PR** from the pane header when the current Git branch already has a GitHub pull request. If an agent such as Codex or Claude is actively working in a separate `git worktree`, panemux prefers that agent process's current worktree and falls back to the pane's own working directory after the agent exits.
- **Open VS Code** from supported panes using the pane header action.

### Notifications and attention prompts

- panemux watches terminal output for agent confirmation prompts such as approval or proceed requests.
- Codex permission menus such as MCP tool allow prompts are detected too.
- When one is detected, the pane frame is highlighted.
- If the pane belongs to an inactive workspace, that workspace tab is highlighted too.
- Browser notifications are shown after notification permission has been granted only when the prompt is not currently visible to the user, and clicking one switches to the matching workspace.
- panemux remembers the last browser-notified prompt per pane, so refreshes, reconnects, and maximize toggles do not re-notify the same prompt replay.
- Notification permission is requested on the first browser interaction, instead of waiting for the first prompt.

### Choosing pane types

- Use `local` for ordinary shells on the same machine as the panemux server.
- Use `ssh` when you want one remote shell per pane.
- Use `tmux` when you want a pane to reattach to a persistent local tmux session.
- Use `ssh_tmux` when you want a persistent tmux session on a remote host.

### SSH and tmux usage

- A pane with `connection: my-host` can use either a named `ssh_connections` entry or a `Host my-host` entry from `~/.ssh/config`.
- `tmux` and `ssh_tmux` panes automatically create the target tmux session if it does not already exist.
- Set `cwd` on `local`, `ssh`, or `ssh_tmux` panes when you want the shell to start in a specific directory.
- In the pane settings dialog, `Working Directory` can be chosen from a browsable directory tree for both local and SSH-backed panes. Hidden directories are available through a toggle.

---

## Configuration

The default config path is `~/.config/panemux/config.yaml` (created automatically on first save). Copy `config.example.yaml` as a starting point:

```yaml
server:
  port: 8080
  host: "127.0.0.1"

# Named SSH connections (optional — hosts from ~/.ssh/config are also usable directly)
ssh_connections:
  prod-web:
    host: "192.168.1.10"
    port: 22
    user: "deploy"
    key_file: "~/.ssh/id_ed25519"

# Workspace tabs, each with its own recursive layout tree
workspaces:
  active: dev
  tab_position: top           # top | bottom | left | right
  items:
    - id: dev
      title: "Development"
      layout:
        direction: horizontal # horizontal | vertical
        children:
          - size: 50          # percentage (siblings must sum to 100)
            pane:
              id: "local-main"
              type: local     # local | ssh | tmux | ssh_tmux
              shell: "/bin/zsh"
              cwd: "~/development"
              title: "Dev Shell"
          - size: 50
            pane:
              id: "ssh-prod"
              type: ssh
              connection: prod-web
              title: "Prod Web"
    - id: ops
      title: "Operations"
      layout:
        direction: horizontal
        children:
          - size: 100
            pane:
              id: "ops-shell"
              type: local
              title: "Ops Shell"
```

Older config files with a top-level `layout:` are still accepted. When the config is next saved, panemux migrates that layout into a `default` workspace and writes the `workspaces:` format.

The workspace bar is always available for workspace actions. Newly added workspaces start with a single local terminal pane, become active immediately, and are saved to the config. The same bar also exposes `+ Terminal`, inline workspace rename, workspace delete with confirmation, and tab-position controls. Switching the active workspace and changing `tab_position` are persisted so the same workspace and tab placement are restored after restart. Agent confirmation prompts, including Codex MCP allow menus, can mark inactive workspace tabs and trigger browser notifications after notification permission has been granted. Clicking the notification switches to the relevant workspace.

### Pane types

| Type | Description |
|------|-------------|
| `local` | Local shell process (`shell`, `cwd` optional) |
| `ssh` | SSH connection — name from `ssh_connections` or a `~/.ssh/config` host alias |
| `tmux` | Attach to a local tmux session (`tmux_session`); created automatically if absent |
| `ssh_tmux` | SSH to a host, then attach to a tmux session; created automatically if absent |

### SSH connections

Connections can be defined in two ways:

**In the YAML config** under `ssh_connections` — supports `host`, `user`, `port`, `key_file`, `password`, and `known_hosts_file`.

**Via `~/.ssh/config`** — any non-wildcard `Host` entry is automatically available as a `connection` name. `HostName`, `User`, `Port`, and `IdentityFile` are read from the file. This lets you reuse your existing SSH config without duplicating it in YAML.

When the same name appears in both, `ssh_connections` takes precedence.

Authentication is attempted in order: configured `key_file` → configured `password` → default key files (`~/.ssh/id_ed25519`, `~/.ssh/id_rsa`, `~/.ssh/id_ecdsa`).

---

## Development

### Prerequisites

- Go 1.24+
- Node.js 20+

### Setup

```sh
make install-deps   # first time: npm install + go mod download
```

### Dev servers

```sh
make dev-backend    # Go backend on :8080
make dev-frontend   # Vite dev server on :5173 (proxies /api and /ws to :8080)
```

### Quality gate

```sh
make check   # lint + test + coverage (must pass before build)
```

Individual commands:

```sh
go test ./... -v -race           # Go tests
cd frontend && npm test          # Frontend tests
make test-e2e                    # Playwright E2E for browser-only rendering regressions
make coverage-go                 # Go coverage (≥ 80 % required)
cd frontend && npm run coverage  # Frontend coverage (≥ 80 % required)
go vet ./...                     # Go lint
cd frontend && npx tsc --noEmit  # TypeScript type check
```

---

## Contributing

1. Fork the repository and create a feature branch.
2. Make your changes — write tests first, confirm they fail, then implement.
3. Run `make check` and ensure all checks pass.
4. Open a pull request against `main` with a description of what and why.

Please keep pull requests focused. One logical change per PR makes review faster and history cleaner.

---

## License

[MIT](LICENSE) — Copyright (c) 2026 tomo-chan
