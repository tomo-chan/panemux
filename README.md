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
- **Drag-to-resize** — drag dividers in the browser to adjust pane sizes
- **Edit mode** — lock/unlock toggle controls whether layout changes are saved to disk; edits are always applied in-memory
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

## Configuration

The default config path is `~/.config/panemux/config.yaml` (created automatically on first save). Copy `config.example.yaml` as a starting point:

```yaml
server:
  port: 8080
  host: "127.0.0.1"

# Named SSH connections referenced by layout panes
ssh_connections:
  prod-web:
    host: "192.168.1.10"
    port: 22
    user: "deploy"
    key_file: "~/.ssh/id_ed25519"

# Recursive layout tree
layout:
  direction: horizontal       # horizontal | vertical
  children:
    - size: 50                # percentage (siblings must sum to 100)
      pane:
        id: "local-main"
        type: local           # local | ssh | tmux | ssh_tmux
        shell: "/bin/zsh"
        cwd: "~/development"
        title: "Dev Shell"
    - size: 50
      direction: vertical     # nested split
      children:
        - size: 60
          pane:
            id: "ssh-prod"
            type: ssh
            connection: prod-web   # key from ssh_connections
            title: "Prod Web"
        - size: 40
          pane:
            id: "tmux-local"
            type: tmux
            tmux_session: "work"   # existing tmux session name
            title: "Tmux Work"
```

### Pane types

| Type | Description |
|------|-------------|
| `local` | Local shell process (`shell`, `cwd` optional) |
| `ssh` | SSH connection defined in `ssh_connections` |
| `tmux` | Attach to a local tmux session (`tmux_session`); created automatically if absent |
| `ssh_tmux` | SSH to a host, then attach to a tmux session; created automatically if absent |

### Edit mode

By default, layout changes (drag-resize, close) are applied in-memory only. Click the lock icon in the bottom-right corner to enable **edit mode**, which persists changes back to the config file. Disable edit mode to explore layouts without touching the file.

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
