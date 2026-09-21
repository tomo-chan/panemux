# Behavior: SSH connections

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## SSH Connections

### Defining connections in `ssh_connections`

Each entry under `ssh_connections` in the YAML config has the following fields:

| Field | Required | Description |
|---|---|---|
| `host` | yes | Hostname or IP address |
| `user` | yes | Remote username |
| `port` | no (default 22) | SSH port |
| `key_file` | no | Absolute path to a private key, or `~/…`, which is expanded at load time. A path that is still relative when the key is read is refused — see [security/command-execution.md](../security/command-execution.md#ssh-private-key-paths-and-an-unresolvable-home-directory) |
| `password` | no | Password for password-based authentication |
| `known_hosts_file` | no (default `~/.ssh/known_hosts`) | Path to known\_hosts file for host-key verification. Absolute or `~/…`, refused when still relative at the read, for the same reason as `key_file` |

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

Pane settings in the frontend expose a directory browser for `cwd`. Local and local tmux panes browse the local filesystem; `ssh` and `ssh_tmux` panes browse the selected SSH connection's remote filesystem. The browser lists directories only and hides dot-directories by default unless the user enables the hidden-directory toggle.

For local `tmux` panes, `cwd` is passed to `tmux new-session` via `-c` and, like `ssh_tmux`, only takes effect when tmux creates a brand-new session; attaching to an already-running session of the same name keeps that session's existing working directory.

Persistence behavior:

- layout and workspace changes are persisted immediately when a save path is available
- pane resize, split, close, quick-add create, move, workspace add/delete/rename, active-workspace changes, `tab_position` changes, and vertical workspace-bar width changes all follow the same immediate-save path
- when no `--config` is given, the default save path is `~/.config/panemux/config.yaml`; the directory is created automatically on first save
- pane maximize state is frontend-local, tracked per workspace, and restored when the user switches away from a workspace and then returns
