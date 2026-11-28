---
type: Specification
title: "Startup, Configuration, Sessions, and Pane Metadata"
description: "Startup sequence, YAML configuration rules, operational assumptions, distribution, SSH connections, and pane Git/PR worktree resolution."
---

# Startup, Configuration, Sessions, and Pane Metadata

Whole-process startup, YAML configuration validation, operational assumptions, distribution, SSH
connection resolution, and live pane Git/PR metadata. Wire contracts live in
[Workspace API](../../api/workspace.md); browser interaction lives in
[Workspace UI](../../ui/workspace.md); security rules live in
[Local process, tmux, and SSH execution](../../security/process-and-ssh.md).

## Startup sequence

1. Parse `--config`, `--open`, and `--port`.
2. Load config: `--config` if given, otherwise `~/.config/panemux/config.yaml`; if absent, use the
   built-in default config with `~/.config/panemux/config.yaml` as the save path.
3. Override configured port when `--port` is set.
4. Create the in-memory session manager.
5. Traverse every configured workspace layout and start each pane session, including panes in
   inactive workspaces.
6. Start enabled Agent Board relay/bootstrap and Command Center services.
7. Schedule enabled global agmsg automatic-update checks across hosts, then start the HTTP server
   and serve the embedded frontend.
8. On `SIGINT` or `SIGTERM`, shut down background services, HTTP server, and all sessions.

If a configured session fails to start, panemux logs a warning and continues booting remaining
sessions.

## Configuration rules

The YAML config defines `server` (`host`, `port`, `auth_token`), `ssh_connections` (shared by both
boards through the host execution port; name-only entries inherit from `~/.ssh/config`),
`workspaces` (active ID, `tab_position`, `vertical_bar_width`, recursive layouts), optional
`display`, `url_open`, `task_dashboard` (`autolinks`, `summary.enabled` defaulting to `false` and
written back only when set), `agent_board`, and `command_center`. Legacy top-level `layout` is
accepted at load time, normalized to one `default` workspace, and saved in `workspaces` shape on the
next write. `~/` in SSH key paths and pane working directories is expanded at load time.

- **Layout rules**: `direction` is `horizontal` or `vertical`; sibling `size` values sum to `100`
  within a small tolerance; pane IDs are globally unique across workspaces; `ssh`/`ssh_tmux` panes
  reference a defined SSH connection; `tmux`/`ssh_tmux` panes define `tmux_session`; workspace IDs
  are unique; `workspaces.active` (when set) names an existing workspace; `workspaces.tab_position`
  is `top`, `bottom`, `left`, or `right`.
- **`display.task_dashboard_shortcut`**: one ASCII letter `A`–`Z` (any case) for
  `Cmd/Ctrl+Shift+<letter>`; `K` and `B` are refused (reserved for command palette and Agent Board).
  Omitted defaults to `S` (`GET /api/display` reports upper case) and is never written back on save.
- **`url_open.browser_shim`**: tri-state gating browser-open interception; omitted means enabled and
  is never written back on save.
- **`task_dashboard.autolinks`**: GitHub-style `{key_prefix, url_template, is_alphanumeric}` list
  matching branch names and PR titles ([Metadata and records](../tasks/lifecycle.md#repository-branch-pull-request-issues-and-references)):
  - `key_prefix`: required, no whitespace, no prefix overlap with another entry (`TICKET` and `TICK`
    cannot coexist).
  - `url_template`: absolute `https` URL with a host and `<num>` after the host; query/fragment
    allowed; credentials, whitespace, and control characters forbidden.
  - Host: IP address or dot-separated ASCII letter/digit/`-` labels; punycode (`xn--`) refused;
    numeric final label (`example.123`) must be an IPv4 address; optional port `1–65535` (guaranteeing
    browser `URL` compatibility so no single link breaks task-list validation).
  - `is_alphanumeric` omitted means digits only; omitted `autolinks` block is never written back.

## Operational assumptions and distribution

- Intended for local or otherwise trusted usage. Long-lived WebSockets are expected, so HTTP write
  timeout is disabled. Non-asset routes fall back to `index.html`.
- Chrome is the validated browser for Powerline private-use glyphs (such as oh-my-zsh `agnoster`
  ``, ``), which depend on both browser support and locally installed Powerline-compatible fonts.
- GitHub Releases ship versioned `.tar.gz` archives containing the embedded-frontend CLI and
  `config.example.yaml`. macOS installs via `install.sh` into a user-local bin directory; Windows
  uses the Linux archive under WSL2 ([Installation](../../../README.md#installation)). `install.sh`
  accepts `MST_REPO=OWNER/REPO MST_VERSION=v1.2.3 ./install.sh`. Release automation calls Make
  targets ([CI and releases](../../engineering/ci-and-releases.md)).

## SSH connections

### Defining connections in `ssh_connections`

Connection keys must be non-empty and contain only Unicode letters, combining marks, numbers, or
`._-+`, preserving exact spelling without normalization (rejecting whitespace, `@`, control
characters, and other punctuation—matching BoardID addresses like `worker@ssh:開発+1`). Config load
validates every key even with Agent Board disabled and fails startup with an error quoting the key
and allowed characters; API create/edit share the exact validator (the Hosts dialog delegates
character validation to the server). This rule applies to `ssh_connections` keys, not unrelated
OpenSSH `Host` aliases.

An entry can be name-only (inheriting all settings from the matching `~/.ssh/config` `Host` block);
every field is optional and overrides the `Host` block field by field:

| Field | Overrides | Description |
|---|---|---|
| `host` | `HostName` | Hostname or IP address |
| `user` | `User` | Remote username |
| `port` | `Port` | SSH port (`22` when neither sets one) |
| `key_file` | `IdentityFile` | Absolute path or `~/…` (expanded at load; still-relative paths refused at read, see [SSH private key paths](../../security/process-and-ssh.md#ssh-private-key-paths-and-an-unresolvable-home-directory)) |
| `password` | — | Password for password-based authentication |
| `known_hosts_file` | — | Host-key verification file (default `~/.ssh/known_hosts`; absolute or `~/…`, refused if still relative at read) |

`ProxyJump` and `ProxyCommand` have no `ssh_connections` fields: an entry leaving `host` unset takes
both from its `Host` block; setting `host` replaces that host and takes neither (skipping
`ProxyJump` resolution entirely; see [SSH `ProxyCommand`](../../security/process-and-ssh.md#ssh-proxycommand)).
An entry with neither `host` nor a matching `Host` block fails on connect with
`ssh connection "<name>" has no host`. An unreadable `~/.ssh/config` (including when home directory
cannot be resolved, since relative lookup is forbidden) counts as having no `Host` block, so entries
with explicit `host` still connect while name-only entries include the read error. Saving writes
name-only entries as `name: {}` and omits unset fields.

`ssh_connections` is also the host list for [task collection](../tasks/collection.md#hosts-and-connections)
and Agent Board; hosts present only in `~/.ssh/config` work in panes but are not collected until
added under `ssh_connections`. Entries are editable via the Hosts dialog
([`/api/config/ssh-connections`](../../api/workspace.md#apiconfigssh-connections)): saved passwords
are never returned, empty password keeps the existing one, entries cannot be renamed, and deletion
is refused while any pane or another dashboard host (including via `ProxyJump`) depends on it without
a `~/.ssh/config` fallback.

### Using `~/.ssh/config` hosts

Panes may reference non-wildcard `Host` aliases from `~/.ssh/config` directly (`Host *` and
`Host *.example.com` are skipped), reading `HostName` (defaults to alias), `User`, `Port` (default
`22`), `IdentityFile` (`~/` expanded at session creation), `ProxyJump` (single alias only—no
`user@host`, comma list, or `none`; cycles refused as `proxy jump cycle: a -> b -> a`), and
`ProxyCommand`. Matching `ssh_connections` fields override `Host` block fields one by one.

### Authentication and SSH pane fields

Authentication order: (1) configured `key_file` or `Host` `IdentityFile`, (2) configured `password`,
(3) default keys `~/.ssh/id_ed25519`, `~/.ssh/id_rsa`, `~/.ssh/id_ecdsa`. Host-key verification uses
`known_hosts_file` or `~/.ssh/known_hosts` and refuses connections if the file does not exist.

| Field | Types | Required | Description |
|---|---|---|---|
| `connection` | `ssh`, `ssh_tmux` | yes | Name from `ssh_connections` or `~/.ssh/config` |
| `cwd` | `ssh`, `ssh_tmux` | no | Remote working directory (`cd {cwd} && exec $SHELL`), validated by `validRemotePath` ([Remote path arguments](../../security/process-and-ssh.md#remote-path-arguments-ssh-working-directory)) |
| `tmux_session` | `ssh_tmux` | yes | Remote tmux session matching `^[a-zA-Z0-9_.-]+$` |

Frontend Pane Settings exposes a directory-only browser (dot-directories hidden unless toggled) for
local/local-tmux and remote SSH filesystems. For local `tmux` panes, `cwd` passes via `-c` and, like
`ssh_tmux`, applies only when creating a new tmux session. New `ssh_tmux` sessions on tmux 3.3+
receive browser-open shim settings ([New remote tmux sessions](preview-and-urls.md#new-remote-tmux-sessions)).
Frontend persistence rules live in
[Resize, persistence, and layout operations](../../ui/overview.md#resize-persistence-and-layout-operations).

## Pane Git and PR metadata

### Worktree resolution

- Pane headers display live Git/PR context resolved from the pane's active working context (subject
  to the 30-second server cache): normal panes use the pane CWD; local `tmux` and `ssh_tmux` panes
  use only the active tmux pane.
- When an interactive `claude` or `codex` process (matched by Task rules among `workdir`-declared
  agents; see [Detection rules](../agent-registry.md#detection-rules)) works in a sibling Git
  worktree of the same repository, panemux prefers that worktree over the pane base directory.
  Non-interactive runs (`claude -p`/`--print`, `codex exec`/`e`/`review`/`login`/`mcp-server`/
  `app-server`/`apply`, respecting value-taking options and `-i`/`--image`) never override metadata.
- Resolution runs per session (`internal/session`'s `Workdir` capability: `locate` then `workdirs`,
  including subagents), falling back to the newest differing descendant process CWD only when the
  session resolver yields nothing.
- **Codex log order**: newest `exec_command` `response_item.payload.arguments.workdir`, then
  `turn_context.cwd`, then `session_meta.cwd` (compare all three before changing order in future
  Codex releases).
- **Claude session & transcript resolution**: reads `~/.claude/sessions/<pid>.json` with the shared
  exact-case parser ([Unreadable state files](../tasks/collection.md#unreadable-state-files)); the
  first matching `pid` decides (wrong-typed `cwd`/`sessionId` is skipped; invalid `sessionId` or
  missing `cwd` falls back). Project directory keys replace path separators and dots with `-`
  (`/workspace/user/my.project` → `-workspace-user-my-project`, pinned by
  `TestClaudeProjectDirName_ObservedEncoding`).
  - **Fallback probe**: if a local transcript is missing at the derived path, panemux lists
    `~/.claude/projects` once and checks one level deep in name order, logging a miss once per
    session (silent if no transcript exists). Remote panes run
    `ls -1 ~/.claude/projects/*/<sessionId>.jsonl 2>/dev/null | head -n 1` only when the derived
    directory yields nothing, caching answers (including empty, never transport failures) for `30 s`
    per host and session, validating the returned path with `validRemotePath` and session-ID match,
    and resolving the project directory so `<sessionId>/subagents/*.jsonl` subagent transcripts are
    read too (returning the already-derived directory is not treated or logged as an encoding
    change).
  - **Claude signal precedence**: latest `Bash` `cd ... &&` target → latest top-level entry `cwd`
    (`user`/`assistant`/`attachment`/`system`) → latest non-auxiliary tool file path
    (`Read`/`Edit`/`Write`/etc.) or file-history snapshot path. Every subagent transcript under
    `<sessionId>/subagents/*.jsonl` is evaluated with the same rule and no recency cutoff; no Claude
    `hooks` or `statusLine` setup is required.
- **Sticky preference & divergence**: if lookup fails or returns nothing (including after agent
  exit), previously preferred distinct sibling worktrees that remain valid for the pane's current
  repo stay preferred; otherwise panemux falls back to the pane's directory. A successful lookup
  replaces the remembered set (if none is a distinct sibling worktree, it clears immediately without
  reusing old entries). Multiple distinct sibling worktrees are deduplicated by repo root and each
  gets an independent GitHub PR lookup ([Pane header](../../ui/workspace.md#multiple-worktrees));
  VS Code open uses the first.
- SCP-style SSH origins (`git@alias:owner/repo.git`) consult `~/.ssh/config` `Host` aliases for both
  browser link hostname and GitHub PR repo host before falling back to the raw host token.

### Polling and caching

- Local/`tmux` panes inspect locally; `ssh`/`ssh_tmux` panes run equivalent remote Git inspection
  (requiring remote **Git 2.31+** for `git rev-parse --path-format=absolute --git-common-dir`; older
  Git degrades to no Git info).
- Visible pane headers fetch immediately on visibility and refresh every `10 s` while both tab and
  pane are visible (paused when hidden behind maximize; immediate refresh on restore).
- The server caches each session's `git-info` response for `30 s` (cleared on session delete or
  recreate). Explicit refresh triggers (pane click/focus, tab restore, VS Code open) skip frontend
  requests only when an answer is `< 10 s` old.
- Workspace-summary polling (`/api/sessions` + Git metadata) is frontend-only and best-effort every
  `10 s` while visible without mounting hidden terminals.
- Unchanged Codex/Claude logs reuse the last parsed worktree result by file fingerprint (checking
  remote file metadata first before transferring full `jsonl`).
