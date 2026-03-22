# Behavior Specification

## Startup Sequence

1. Parse `--config`, `--open`, and `--port`.
2. Load config: if `--config` is given, load that file; otherwise try `~/.config/panemux/config.yaml`; if that file does not exist, use the built-in default config with `~/.config/panemux/config.yaml` as the save path.
3. Override the configured port if `--port` is set.
4. Create the in-memory session manager.
5. Traverse the configured layout and create each pane session.
6. Start the HTTP server and serve the embedded frontend.
7. On `SIGINT` or `SIGTERM`, shut down the server and close all sessions.

If a configured session fails to start, the server logs a warning and continues booting other sessions.

## Configuration Rules

The YAML config defines:

- `server.host` and `server.port`
- `ssh_connections`
- `layout`
- optional `display` settings

Layout rules:

- `direction` must be `horizontal` or `vertical`
- sibling `size` values must sum to `100` within a small tolerance
- pane IDs must be unique
- `ssh` and `ssh_tmux` panes must reference a defined SSH connection
- `tmux` and `ssh_tmux` panes must define `tmux_session`

Path behavior:

- `~/` in SSH key paths and pane working directories is expanded at load time

## SSH Connections

### Defining connections in `ssh_connections`

Each entry under `ssh_connections` in the YAML config has the following fields:

| Field | Required | Description |
|---|---|---|
| `host` | yes | Hostname or IP address |
| `user` | yes | Remote username |
| `port` | no (default 22) | SSH port |
| `key_file` | no | Path to private key; `~/` is expanded at load time |
| `password` | no | Password for password-based authentication |
| `known_hosts_file` | no (default `~/.ssh/known_hosts`) | Path to known\_hosts file for host-key verification |

Example:

```yaml
ssh_connections:
  prod-web:
    host: 192.168.1.10
    user: deploy
    key_file: ~/.ssh/id_ed25519
  bastion:
    host: bastion.example.com
    user: ops
    key_file: ~/.ssh/id_ed25519
    known_hosts_file: ~/.ssh/known_hosts
```

### Using `~/.ssh/config` hosts

Panes can reference host aliases from `~/.ssh/config` directly in the `connection` field without duplicating them under `ssh_connections`. The following fields are read from each non-wildcard `Host` block:

- `HostName` — hostname or IP (defaults to the alias name if omitted)
- `User` — remote username
- `Port` — port number (defaults to 22 if omitted)
- `IdentityFile` — path to private key; `~/` is expanded at session creation time

Wildcard entries (`Host *`, `Host *.example.com`) are skipped.

`ssh_connections` takes precedence over `~/.ssh/config` when the same name appears in both.

### Authentication

When establishing an SSH connection, the following auth methods are attempted in order:

1. Key file specified in `key_file` (if present)
2. Password specified in `password` (if present)
3. Default key files in order: `~/.ssh/id_ed25519`, `~/.ssh/id_rsa`, `~/.ssh/id_ecdsa`

Host-key verification uses `known_hosts_file` if configured, or `~/.ssh/known_hosts` by default. If the known\_hosts file does not exist, the connection is refused (the app does not silently accept unknown hosts).

### SSH pane fields

| Field | Types | Required | Description |
|---|---|---|---|
| `connection` | `ssh`, `ssh_tmux` | yes | Name from `ssh_connections` or `~/.ssh/config` |
| `cwd` | `ssh`, `ssh_tmux` | no | Remote working directory; executes `cd {cwd} && exec $SHELL` |
| `tmux_session` | `ssh_tmux` | yes | Remote tmux session name to attach or create |

`tmux_session` must match `^[a-zA-Z0-9_.-]+$`.

`cwd` is validated against `validRemotePath` before use: absolute paths only, no shell metacharacters (`;|&$` + "`" + `'"<>(){}[]!`), no control characters. See *Security Design* in `architecture.md`.

Persistence behavior:

- `PUT /api/layout` updates in-memory layout always
- file persistence happens only when edit mode is ON (`PUT /api/edit-mode {"editMode":true}`)
- when no `--config` is given, the default save path is `~/.config/panemux/config.yaml`; the directory is created automatically on first save

## REST API

### `GET /api/layout`

Returns the current layout tree as JSON.

### `PUT /api/layout`

Accepts a layout JSON document, validates it, updates in-memory state, and persists it when possible.

- `400`: invalid JSON
- `422`: structurally invalid layout
- `200`: accepted and returned

### `GET /api/sessions`

Returns a list of active sessions with `id`, `type`, `title`, and `state`.

### `POST /api/sessions`

Creates a session from a `PaneConfig` payload, provided the pane ID does not already exist.

- `400`: invalid JSON
- `409`: duplicate session ID
- `422`: invalid pane config
- `201`: session created

Current product use: the frontend uses this endpoint when the user splits a pane and needs a new local session. It should be treated as a narrow lifecycle API, not a general provisioning layer.

### `DELETE /api/sessions/{id}`

Closes the session, removes its pane from the layout, collapses redundant parent splits, normalizes sibling sizes, and returns `204`.

### `GET /api/ssh-connections`

Returns a sorted list of all known SSH connection names — the union of names defined in `ssh_connections` (YAML) and non-wildcard hosts from `~/.ssh/config`. Names present in both sources are deduplicated, with the YAML entry taking precedence.

Response:

```json
{ "names": ["bastion", "prod-web"] }
```

### `GET /api/ssh-config/hosts`

Returns all non-wildcard hosts from `~/.ssh/config` with full field details.

- `500`: unable to read `~/.ssh/config`
- `200`: list of host records

Response:

```json
{
  "hosts": [
    { "name": "bastion", "hostname": "bastion.example.com", "user": "ops", "port": 22, "identity_file": "~/.ssh/id_ed25519" }
  ]
}
```

Fields `port` and `identity_file` are omitted when not set in `~/.ssh/config`.

### `POST /api/ssh-config/hosts`

Appends a new `Host` block to `~/.ssh/config`.

Request body:

```json
{ "name": "my-server", "hostname": "192.168.1.5", "user": "deploy", "port": 22, "identity_file": "~/.ssh/id_ed25519" }
```

- `400`: invalid JSON
- `409`: a host with the same name already exists in `~/.ssh/config`
- `422`: validation error (name, hostname, or user missing; name contains invalid characters; port out of range)
- `500`: unable to read or write `~/.ssh/config`
- `201`: host appended

`name` must match `^[a-zA-Z0-9_.\-]+$`. `port` defaults to 0 (omitted from the written block) when not specified. `identity_file` is optional.

### `GET /api/display`

Returns display preferences such as header/status-bar visibility.

## WebSocket Protocol

Endpoint: `GET /ws/{sessionID}`

Connection behavior:

- `404` if the session ID does not exist
- initial text frame is a JSON status message with `type: "status"` and `state: "connected"`

Frame behavior:

- binary frame from browser to server: raw terminal input bytes
- binary frame from server to browser: raw terminal output bytes
- text frame from browser to server: JSON control message

Supported control messages:

```json
{ "type": "resize", "cols": 120, "rows": 40 }
```

Resize messages with zero dimensions are ignored. Invalid JSON control frames are ignored rather than terminating the session.

When the backend session reaches EOF, the handler emits:

```json
{ "type": "status", "state": "exited" }
```

## Frontend Runtime Behavior

### Initial page load

```text
Browser loads SPA
  -> GET /api/layout
  -> GET /api/display
  -> validate JSON with Zod
  -> render recursive split tree
  -> each TerminalPane opens /ws/{sessionID}
```

### Terminal I/O round trip

```text
User types in xterm.js
  -> browser sends binary WebSocket frame
  -> session.Write(...)
  -> shell/SSH/tmux produces output
  -> session.Read(...)
  -> backend sends binary WebSocket frame
  -> xterm.js writes bytes to the terminal
```

### Selection and copy behavior

- Terminal text can be selected with the mouse using xterm.js standard selection behavior.
- If text is selected, `Cmd+C` or `Ctrl+C` copies the current selection instead of sending terminal input.
- If no text is selected, `Cmd+C` or `Ctrl+C` is left to normal terminal behavior, so shell interrupts still work.
- This interaction is currently validated in Chrome.

### Resize and layout updates

- `ResizeObserver` triggers terminal fit logic when pane size changes.
- The browser sends a `resize` control message with current cols/rows.
- Dragging split dividers updates layout percentages in memory.
- Layout persistence is debounced by 500 ms before `PUT /api/layout`.

### Split and close semantics

- Splitting a pane creates a new local pane, creates a backend session through `POST /api/sessions`, then rewrites the layout tree so the original and new panes each receive `50%` under a new split node.
- The original pane keeps its current visible terminal contents when split; it must not go blank or reset to a fresh prompt while the new sibling pane is created.
- Closing a pane calls `DELETE /api/sessions/{id}`, removes the pane from the tree, collapses parents with a single child, and renormalizes sizes to total `100`.

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
