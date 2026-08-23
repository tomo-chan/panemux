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
- **Agent Board** — one dashboard for what every pane's coding agent is doing, and a command palette to message them ([setup](#agent-board))

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
- When the bar is on the left or right, you can drag its inner edge to resize it. That shared vertical width is persisted with the workspace settings.
- The workspace bar also shows per-workspace pane status at a glance. Each tab includes a compact pane-name summary, and each workspace can expose pane detail cards with connection state, SSH host alias, repository, branch, and PR number.
- When the bar is on the top or bottom, pane detail cards appear as a hover/focus overlay attached to the workspace tab so the terminal area keeps its height.
- When the bar is on the left or right, pane detail cards stay expanded inline under each workspace tab so the workspace-to-pane grouping remains visible without extra pointer travel.

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
- **Open a linked PR** from the pane header when the current Git branch already has a GitHub pull request. For local panes, panemux can prefer the live worktree of an interactive `codex` or `claude` session, including resumed Codex sessions, and keeps the last valid sibling worktree pinned after the agent exits until a newer valid context is detected.
- **Open VS Code** from supported panes using the pane header action. Like pane Git/PR metadata, this can prefer the live worktree of an interactive `codex` or `claude` session and keeps the last valid sibling worktree pinned after the agent exits until a newer valid context is detected.

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
- In `tmux` and `ssh_tmux` panes, plain drag continues to follow tmux mouse behavior. Use `Option` + drag on macOS or `Shift` + drag on Linux and Windows to force browser-side text selection.
- Set `cwd` on `local`, `ssh`, or `ssh_tmux` panes when you want the shell to start in a specific directory.
- In the pane settings dialog, `Working Directory` can be chosen from a browsable directory tree for both local and SSH-backed panes. Hidden directories are available through a toggle.
- Pane headers resolve Git and PR metadata from the live working context, including interactive `codex` and `claude` worktrees for both local and SSH-backed panes. When a valid sibling worktree was detected recently, panemux keeps that worktree pinned after the agent exits until the pane moves to a different repository context. `tmux` and `ssh_tmux` use the currently active tmux pane only.

---

## Agent Board

Agent Board gives you one view of what every coding agent in your panes is doing, and one place to
message them. Two independent pieces, either of which can be used without the other:

- **Dashboard** — each pane's self-reported status (state, repo, branch, PR, summary), plus the
  cross-pane message history. Opens from the **Agent Board** button or `Cmd/Ctrl+Shift+B`.
- **Command center** — a Spotlight-style palette (`Cmd/Ctrl+Shift+K`) where you ask in plain language:
  *"which panes are blocked?"*, *"tell every pane the branch is frozen"*.

Full design lives in [docs/agent-board.md](docs/agent-board.md).

### Prerequisites

**[agmsg](https://github.com/fujibee/agmsg) must already be installed** on every host whose panes join
the board — including remote hosts for `ssh`/`ssh_tmux` panes. panemux is tested against **agmsg
1.2.0**; it reads each host's `VERSION` at startup and logs a warning for anything else, without
blocking. agmsg promises compatibility only for reading through `scripts/api.sh`, while panemux also
depends on `send.sh`, `join.sh` and `delivery.sh`, so a different version may work or may misbehave. panemux never installs, updates, or
manages agmsg; it only detects an existing installation by looking for `scripts/api.sh` under the
configured path. If it isn't there, panemux logs one warning naming the host and the path it looked in,
and skips that host — panes there simply stay off the board, and nothing else about them changes.

The command center additionally needs the `claude` CLI on the machine running panemux. It does **not**
need agmsg.

Remote hosts run these scripts over the SSH exec channel, which does not source `.bashrc`/`.profile`.
Whatever agmsg needs (`bash`, `node`, `sqlite3`) must be on the non-interactive `PATH` — a common
surprise when agmsg was installed under `nvm`/`asdf` in an interactive session.

### Configuration

```yaml
server:
    host: 127.0.0.1
    # Required for the command center. Leave empty and panemux generates one on
    # first run, storing it in ~/.config/panemux/token (never in this file).
    auth_token: ""

command_center:
    enabled: true            # default false

agent_board:
    team: panemux                      # shared agmsg team for all board-enabled panes
    agmsg_path: ~/.agents/skills/agmsg # ~ is expanded per host, including remote hosts

workspaces:
    items:
        - id: default
          title: Default
          layout:
            direction: horizontal
            children:
                - pane:
                    id: api          # this id becomes the pane's agmsg identity
                    type: local
                    agent_board:
                        enabled: true
                        mode: monitor  # monitor (default) | turn | both | off
                  size: 50
```

Pane ids become board addresses, so give them names you will recognize (`api`, `web`, `infra`) rather
than `pane-1`. `_system` is reserved and rejected at config validation.

You do not have to edit YAML for this: **Pane Settings** in the pane header has a *Join the agent
board* checkbox and, once ticked, a *Message delivery* picker for the mode. Changes there are saved
to `config.yaml` like any other pane setting.

### How a pane joins

Joining is automatic — you do not run anything by hand.

1. Start your agent in the pane as usual (`claude`, `codex`, `cursor-agent`, `gemini`, `grok`, or
   `opencode`). panemux polls every 5s and needs to see it on two consecutive polls.
2. panemux confirms agmsg exists at `agmsg_path` on that pane's host.
3. It writes a **one-time instruction into the pane's terminal**, asking the agent to run agmsg's
   `join.sh` using the pane id as its agmsg agent id, and from then on to send board messages with
   `send.sh` and to self-report its status periodically.

You will see that instruction appear in the pane, and the agent's replies to it. That is expected —
it is written into the terminal the same way your own keystrokes are, so nothing happens mid-command
that you cannot see. It is written once per pane; panemux remembers which panes are done across
restarts.

The pane id is used deliberately as the agmsg agent id: every board address assumes `from`/`to` are
pane ids, so an agent that picks its own name breaks addressing.

### Delivery mode, and the one setup step it needs

`agent_board.mode` decides whether messages *reach* a pane's agent. It is worth understanding before
you pick a value, because the default is the quiet one:

| mode | Writes into your repository | Broadcasts reach the agent |
|---|---|---|
| `monitor` (default) | no | **no** — they sit in agmsg until the agent looks |
| `turn` / `both` | yes, one file | yes |

With `monitor`, panemux does not run agmsg's `delivery.sh` at all, so no delivery hooks exist and the
board is effectively read-only: panes report their status and you can see it, but a broadcast is not
pushed to anyone. Choose `turn` or `both` if you want the messaging half to work.

`turn` and `both` have the agent run agmsg's `delivery.sh`, and **agmsg** — not panemux — writes a
hook file into the pane's project directory. The path is agmsg's own per-type convention
(`scripts/drivers/types/<type>/type.conf`, `hooks_file=`), and agmsg deliberately rejects any
non-project-relative value, so it cannot be redirected to a user-scope location:

| agent type | file agmsg writes |
|---|---|
| claude-code | `.claude/settings.local.json` |
| codex | `.codex/hooks.json` |
| gemini | `.agent/rules/agmsg.md` |
| opencode | `.opencode/rules/agmsg.md` |
| cursor | `.cursor/rules/agmsg.mdc` |
| grok-build | `.grok/rules/agmsg.md` |

These are local, machine-specific files — none of them belong in version control. Rather than editing
`.gitignore` in every repository you work in, set a global exclude file once per machine:

```sh
git config --global core.excludesFile ~/.gitignore_global
cat >> ~/.gitignore_global <<'EOF'
.claude/settings.local.json
.codex/hooks.json
.agent/rules/agmsg.md
.opencode/rules/agmsg.md
.cursor/rules/agmsg.mdc
.grok/rules/agmsg.md
EOF
```

Repeat that once on each host that runs `ssh`/`ssh_tmux` panes. Writes are idempotent — each
`delivery.sh set` strips agmsg's own hook entries before re-adding them — but they persist after the
pane closes, and panemux never reverts them, including when a pane later sets
`agent_board.enabled: false`. Removing them is done through agmsg (`delivery.sh set off`), outside
panemux.

### Checking that it worked

Open the dashboard. A pane appears there only after its agent has actually sent a status report, so
give it a moment after the agent starts.

Nothing showing up? In order of likelihood:

- **agmsg isn't where panemux looked.** The startup log carries one
  `no agmsg installation at "<path>" on host "<host>"` warning per affected host. Confirm
  `<agmsg_path>/scripts/api.sh` exists there.
- **The agent wasn't detected.** Headless invocations are deliberately ignored (`claude -p`,
  `codex exec`), so the agent must be running interactively in the pane.
- **The agent hasn't reported yet.** Status is entirely self-reported; panemux computes nothing. A
  card older than five minutes is dimmed and marked `stale`.
- **Remote `PATH`.** See the prerequisites above.

### What the command center can and cannot do

It has exactly three tools: read board status, read message history, and send messages to panes. It
has **no shell, no filesystem access, and no network access** — it cannot write code, run tests, or
open pull requests, and it is launched in a way that enforces this rather than relying on it being
asked nicely (see [docs/security.md](docs/security.md#command-center-subprocess-execution)).

A message it sends is an ordinary message to the receiving agent, not a pre-authorized command. That
agent's own confirmation behavior still applies, so "sent" is not "done".

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
  vertical_bar_width: 280     # shared width in px when tab_position is left/right
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

The workspace bar is always available for workspace actions. Newly added workspaces start with a single local terminal pane, become active immediately, and are saved to the config. The same bar also exposes inline workspace rename, workspace delete with confirmation, a `+` workspace action, and tab-position controls. When the bar is vertical, its width is shared across all workspaces and is also persisted. It also acts as a cross-workspace operational overview: pane detail cards can be selected to focus that pane, and those same cards can be dragged onto another workspace to move the pane there. Switching the active workspace, changing `tab_position`, and resizing the vertical workspace bar are persisted so the same navigation chrome is restored after restart. Agent confirmation prompts, including Codex MCP allow menus, can mark inactive workspace tabs and trigger browser notifications after notification permission has been granted. Clicking the notification switches to the relevant workspace.

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
make install-deps   # first time: npm install + go mod download + repo-local pre-push hook setup
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

`make install-deps` also configures the tracked `.githooks/pre-push` hook, which runs `make check`
before every `git push`.

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
4. Push only after the local `pre-push` hook passes.
5. Open a pull request against `main` with a description of what and why.

Please keep pull requests focused. One logical change per PR makes review faster and history cleaner.

---

## License

[MIT](LICENSE) — Copyright (c) 2026 tomo-chan
