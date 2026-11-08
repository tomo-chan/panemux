---
type: Specification
title: "Behavior: REST API"
description: "REST endpoints, request and response contracts, and failure behavior."
---

# Behavior: REST API

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## REST API

Every API route uses the [shared browser request guard](../security/auth.md#shared-browser-request-guard)
before authentication or handler side effects, including GET, mutations and OPTIONS. Another
loopback port or hostname is refused with `403`; browser requests without Origin require
a matching Referer and consistent Fetch Metadata. Requests without browser headers remain
supported for CLI clients. Vite uses its same-origin proxy, preserving Host and Origin;
direct cross-port browser requests to the backend are refused.

### A mutation that fails changes nothing

Every route that changes the config persists it before answering, and each one applies the same rule
when that step fails: **the config panemux is still running is the config the error says was not
saved.**

Each config mutation takes a snapshot and restores it if persistence fails. Session lifecycle
ordering preserves the same rule:

- A route that **destroys** a session — `DELETE /api/sessions/{id}`, `DELETE /api/workspaces/{id}` —
  persists the config change first and tears the session down only once the write has succeeded. A
  `500` therefore leaves a pane that still works and a request the operator can retry.
- A route that **creates** sessions — `POST /api/workspaces` — closes and unregisters the ones it had
  already created when a later pane or the write itself fails, so a failed attempt leaves neither a
  workspace whose panes have no session nor sessions no workspace lists.
- `POST /api/sessions/{id}/restart` creates the replacement session before it touches anything, so a failure
  leaves the existing session registered and servable.

Validation failures (`400`, `422`) and unknown IDs (`404`) never reach the mutation at all.

### `GET /api/layout`

Returns the current layout tree as JSON.

Every response layout node carries `direction` and `children`. Missing direction is normalized;
missing children become `[]`. A root `pane` with no children becomes its single layout child and
is validated like any other pane: missing type or an undefined SSH connection fails startup with
the equivalent child-pane error. Normalization does not repair invalid values such as direction.

A node containing both `pane` and children is preserved as written, including the root `pane` in
the response and frontend schema, without prepending or rescaling siblings. Zod requires direction
and children for the whole workspaces response; undeclared fields would be stripped and lost on
later saves. The normalized shape is persisted on the next layout save, never rewritten on read.

### `PUT /api/layout`

Accepts a layout JSON document, validates it, updates in-memory state, and persists it when possible.

- `400`: invalid JSON
- `422`: structurally invalid layout
- `200`: accepted and returned

The request is normalized, then `~/` paths are expanded, then it is validated, stored and echoed,
matching config load order. Expansion follows normalization so a relocated root pane's `cwd`
is expanded too. The echoed response has normalized direction/children and passes the frontend
schema. The same applies to `PUT /api/workspaces/{id}/layout`.

### `GET /api/workspaces`

Returns the active workspace ID, `tab_position`, `vertical_bar_width`, and all workspace layouts.

### `PUT /api/workspaces/tab-position`

Accepts `{ "tab_position": "top" | "bottom" | "left" | "right" }`, validates it, persists the workspace config, and returns the updated workspace response.

- `400`: invalid JSON
- `422`: invalid tab position
- `500`: unable to save the config
- `200`: updated and returned

### `PUT /api/workspaces/vertical-bar-width`

Accepts `{ "vertical_bar_width": <int> }`, validates the shared vertical workspace-bar width in pixels, persists the workspace config, and returns the updated workspace response.

- `400`: invalid JSON
- `422`: invalid width
- `500`: unable to save the config
- `200`: updated and returned

### `GET /api/sessions`

Returns a list of active sessions with `id`, `type`, `title`, and `state`. The task dashboard's
temporary attaches ([`POST /api/tasks/attach`](task-api.md#post-apitasksattach)) and host terminals
([`POST /api/hosts/terminal`](task-api.md#post-apihoststerminal)) are not listed.

### `POST /api/sessions`

Creates a session from a `PaneConfig` payload, provided the pane ID does not already exist.

- `400`: invalid JSON
- `409`: duplicate session ID
- `422`: invalid pane config
- `201`: session created

Current product use: the frontend uses this endpoint when the user splits a pane or uses the pane-header quick-add buttons to create a default local pane to the right or below. It remains a narrow pane-lifecycle API, not a general provisioning layer.

### `DELETE /api/sessions/{id}`

Removes the pane from the layout, collapses redundant parent splits, normalizes sibling sizes,
persists the result, then closes the session and returns `204`.

- `404`: no session is registered for `id`
- `500`: the layout could not be saved. The session is left registered and open and the pane is left
  in the layout, so the request can be retried — see [A mutation that fails changes
  nothing](#a-mutation-that-fails-changes-nothing).
- `204`: the pane is gone and its session is closed

### `POST /api/sessions/{id}/restart`

Recreates the session for an existing pane from its config (e.g. after an SSH connection was lost). The
replacement session is created first; the old session is only removed and swapped out once the new one
starts successfully.

- `404`: no pane config exists for `id`
- `409`: a restart for this `id` is already in progress. Session creation can block on a real SSH
  dial, so concurrent restart requests for the same pane are serialized instead of each building a
  session independently and racing to swap it in.
- `500`: session creation failed (e.g. SSH dial/handshake error). The pane's prior session, if any,
  remains registered and servable — it is not removed on failure — so `/ws/{id}` and `/git-info` keep
  working against it instead of 404ing until a future restart succeeds.
- `200`: the new session replaced the old one (or was created fresh if none existed)

SSH transport dialing retries transient failures (including DNS errors) a bounded number of times
with short backoff. Only TCP/ProxyJump/ProxyCommand connection setup is retried, never handshake
or authentication failure. One wall-clock deadline covers the entire ProxyJump chain and every
attempt's timeout shrinks to the remaining budget.

Once connected, each SSH handshake (version exchange, key exchange and authentication) gets at
most 30 seconds and no more than the remaining dial budget; it does not open a fresh window per
hop. Panemux enforces this even where transport deadlines are unsupported or ineffective
(ProxyJump channels and ProxyCommand pipes), so a hung bastion cannot block reconnect indefinitely.
A timed-out handshake returns `500`, like any other handshake failure.

### `POST /api/sessions/{id}/open-url`

Accepts `{ "url": "<http(s) URL>" }` and prepares the panemux host for a URL the browser is about to
open on that pane's behalf. It does not open the browser itself: the browser panemux should drive is
the one already showing the dashboard, so the frontend opens the tab and this endpoint only
publishes the URL's loopback callback port on this host.

- `400`: invalid JSON body
- `404`: no session exists for `id`
- `409`: the loopback port cannot be bound on the panemux host — another pane already forwards it,
  or an unrelated local process holds it
- `422`: the URL is missing, unparseable, or uses a scheme other than `http`/`https`
- `500`: the forward could not be established for another reason
- `200`: request understood; the body reports what happened

Response:

```json
{ "url": "https://example.com/auth?...", "forwarded": true, "port": 51789 }
```

`forwarded` is `false`, with a human-readable `reason` and no `port`, when nothing needed forwarding:
the URL carries no loopback callback port, or the pane's shell already runs on the panemux host
(`local` and `tmux` panes).

A `409` is reported rather than worked around. An OAuth provider matches the registered
`redirect_uri` exactly, so rewriting the callback to a different local port would break the login it
is meant to complete.

### `POST /api/sessions/{id}/preview`

Accepts `{ "path": "<the path as the terminal showed it>" }`, resolves it from the session's working
directory and its agent's worktrees, and registers the HTML or Markdown file it names for the
[split preview](preview.md). For an `ssh` or `ssh_tmux` session — a pane, or a task's attach — the
path is resolved on, and the file read from, the session's SSH host.

Accepts `{ "url": "<a localhost URL>" }` instead for an `ssh` or `ssh_tmux` session: the URL's address
and port on the session's host are relayed to a port of their own on `127.0.0.1` of the panemux host
([Localhost URLs of an SSH host](preview.md#localhost-urls-of-an-ssh-host)).

- `400`: invalid JSON body; a path that is not `.html`, `.htm`, `.md` or `.markdown`; a URL that is
  not an http or https URL of `localhost`, `127.0.0.1` or `[::1]`; a URL for a session on the panemux
  host; or an `ssh`/`ssh_tmux` session that names no SSH connection
- `403`: the file is outside the session's working directory and its agent's worktrees — an absolute
  path elsewhere, a `../` that leaves them, or a symbolic link that leads out of them (the reason names
  the directories allowed)
- `404`: no session exists for `id`, or no file exists at the path (the reason names where it looked)
- `413`: the file is larger than 10 MiB
- `422`: the session's working directory and its agent's worktrees are all unknown
- `502`: the session's SSH host cannot be reached, or offers no SFTP subsystem for a path, or nothing
  answers at the URL's address on it
- `200`: the preview to show, or the files to choose from

Errors carry their reason as plain text, which the preview shows. A file to show:

```json
{ "status": "ready", "kind": "file", "url": "/preview/<token>/mockups/index.html", "path": "/workspace/user/project/mockups/index.html", "host": "" }
```

A relayed URL, with `path` the URL as clicked:

```json
{ "status": "ready", "kind": "url", "url": "http://127.0.0.1:<relay port>/app", "path": "http://localhost:5173/app", "host": "devbox" }
```

Several files a relative path may name, absolute, in the order the directories were searched; the
chosen one is sent back as `path`:

```json
{ "status": "choose", "candidates": ["/workspace/user/project/docs/design.md", "/workspace/user/project-wt/docs/design.md"], "host": "" }
```

`host` is `""` for the panemux host, otherwise the session's `ssh_connections` name.

### `GET /preview/{token}/*`

Serves a registered preview and the files under the same root. It sits outside `/api` and its
request guard, since the page it serves runs in an opaque origin and all of its own requests are
cross-site; the token is what grants access. Missing tokens and files, directories, `..` and paths or
symbolic links leading outside the root answer `404`; an HTML or Markdown file over 10 MiB answers
`413`. Markdown is answered rendered as HTML. Headers and policies are in
[security/preview.md](../security/preview.md).

### `GET /api/tasks`, `POST /api/tasks/hosts/{name}/reconnect`, `PUT /api/tasks/records`, `POST /api/tasks`, `POST /api/tasks/resume`, `POST /api/tasks/summary`, `POST /api/tasks/input-given`, `GET`/`POST /api/tasks/codex-hook`, `POST /api/tasks/codex-hook/auto-update` and `POST`/`DELETE /api/tasks/attach`

Task payloads, response fields, status codes and failure semantics are authoritative in the
[task REST API](task-api.md). Runtime rules are linked from the [task dashboard guide](tasks.md).

### `GET` and `POST /api/hosts/agmsg`

Manual pinned agmsg state and installation/update for the panemux host or a configured SSH host,
available only with Agent Board enabled. See [Manual agmsg host tool](host-tools.md#manual-agmsg-host-tool).

### `POST /api/hosts/session-name` and `POST`/`DELETE /api/hosts/terminal`

Generated session names and temporary host terminals: see the
[host terminal API](task-api.md#post-apihostssession-name) and
[host navigation](task-navigation.md#opening-a-host).

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
- `422`: validation error (name, hostname, or user missing; name contains invalid characters; hostname, user, or identity_file contains a control character such as a line break; port out of range)
- `500`: unable to read or write `~/.ssh/config`
- `201`: host appended

`name` must match `^[a-zA-Z0-9_.\-]+$`. `hostname`, `user`, and `identity_file` must not contain a control character: each is written as the rest of one line, and a line break would add a directive of its own ([security](../security/ssh-execution.md#ssh-proxycommand)). `port` defaults to 0 (omitted from the written block) when not specified. `identity_file` is optional.

### `/api/config/ssh-connections`

Lists and edits `ssh_connections` in `config.yaml`: the hosts the
[task dashboard](task-collection.md#hosts-and-connections) collects from, which panes can also use
([SSH](ssh.md#defining-connections-in-ssh_connections)). This is not `~/.ssh/config`, which
`POST /api/ssh-config/hosts` writes. All four routes refuse a request another site's page made with
`403`, as the task routes do, and every change is written to `config.yaml` through the same
whole-file atomic write as the workspace routes; a write that fails answers `500`
`failed to save ssh_connections` and leaves the running config as it was. Error bodies are
`{"error": "…"}` and name a field, never its value.

An entry, as every route returns it:

```json
{
  "name": "build-box", "host": "build.example.com", "user": "deploy", "port": 2222,
  "key_file": "/home/deploy/.ssh/id_ed25519", "known_hosts_file": "/home/deploy/.ssh/known_hosts",
  "has_password": true, "in_ssh_config": false, "panes": ["build"]
}
```

`host`, `user`, `port`, `key_file` and `known_hosts_file` are omitted when unset. The password is
never returned: `has_password` says whether one is saved. `in_ssh_config` is whether `~/.ssh/config`
has a `Host` block of the same name, whose values fill in the fields the entry leaves unset.
`panes` is the IDs of the `ssh` and `ssh_tmux` panes, in every workspace, that use the entry.

- `GET /api/config/ssh-connections` — `200` `{"connections": [entry, …]}`, sorted by name.
- `POST /api/config/ssh-connections` — adds an entry. Body: `name` and any of `host`, `user`, `port`,
  `key_file`, `known_hosts_file`, `password`. `201` with the entry; `400` for invalid JSON or an
  unknown field; `409` when the name is already an entry; `422` for a missing or malformed name
  (`^[a-zA-Z0-9_.\-]+$`), `clear_password`, or a field error (below).
- `PUT /api/config/ssh-connections/{name}` — replaces the entry's fields with the body's: a field
  left out is removed. The password is the exception: an empty or absent `password` keeps the saved
  one, a non-empty one replaces it, and `clear_password: true` removes it. `name` in the body, if
  sent, must be the entry's own; an entry cannot be renamed. `200` with the entry; `404` for an
  unknown name; `400`/`422` as for `POST`, and `422` for `password` together with `clear_password`.
  The task dashboard's connection to the host is dropped, and a dial of it still in flight is
  discarded, so the next collection dials it with the new details.
- `DELETE /api/config/ssh-connections/{name}` — `204`; `404` for an unknown name; `409` when, without
  the entry, a pane's connection or another dashboard host would no longer resolve, naming them. That
  covers a pane that uses the entry by name with no `~/.ssh/config` `Host` block of that name to fall
  back to, and a pane or host whose `~/.ssh/config` `ProxyJump` goes through the entry.

`{name}` is URL-escaped, so an entry written by hand in `config.yaml` with a character such as `@`,
`:` or `/` in its name can be edited and deleted.

Field errors (`422`): `port` outside 1–65535 (0 or absent means unset); a control character in
`host`, `user`, `key_file` or `known_hosts_file`; a `key_file` or `known_hosts_file` that is neither
absolute nor `~/`-relative; and no `host` when `~/.ssh/config` has no `Host` block of the entry's
name to take it from, or cannot be read. A leading `~/` in the two paths is expanded on save, as it
is when `config.yaml` is loaded.

### `GET /api/display`

Returns display preferences such as header/status-bar visibility, and `task_dashboard_shortcut`: the
effective task dashboard shortcut letter, upper-cased, `S` when the config does not set one.

The global agmsg preference uses `POST /api/hosts/agmsg/auto-update` with only an enabled boolean,
not a host selector. Tool responses include its global value and retained automatic results; see
[global agmsg automatic updates](host-tools.md#global-agmsg-automatic-updates).
