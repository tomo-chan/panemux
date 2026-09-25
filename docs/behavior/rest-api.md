# Behavior: REST API

> Part of the [behavior specification](../behavior.md). That document carries startup, configuration, and operational assumptions.

## REST API

### A mutation that fails changes nothing

Every route that changes the config persists it before answering, and each one applies the same rule
when that step fails: **the config panemux is still running is the config the error says was not
saved.**

This has to be arranged rather than assumed. `Config.write()` serializes the whole in-memory config,
so a route cannot save first and mutate afterwards — it mutates, then writes. A route that returned
`500` and left its mutation in place therefore did two things the operator could not see: it kept
running a config it had just reported as unsaved, and it left that change to be persisted by the next
successful write from *any other* route, so a failed workspace deletion could land in `config.yaml`
when an unrelated rename succeeded minutes later.

Each route now takes a `config.Snapshot()` before it mutates and restores it when the write fails
(issue [#204](https://github.com/tomo-chan/panemux/issues/204)). Sessions are the other half of the
same rule, and the ordering follows from it:

- A route that **destroys** a session — `DELETE /api/sessions/{id}`, `DELETE /api/workspaces/{id}` —
  persists the config change first and tears the session down only once the write has succeeded. A
  `500` therefore leaves a pane that still works and a request the operator can retry.
- A route that **creates** sessions — `POST /api/workspaces` — closes and unregisters the ones it had
  already created when a later pane or the write itself fails, so a failed attempt leaves neither a
  workspace whose panes have no session nor sessions no workspace lists.
- `POST /api/sessions/{id}/restart` is the shape the other routes were brought in line with: it
  creates the replacement session before it touches anything, so a failure leaves the existing
  session registered and servable.

Validation failures (`400`, `422`) and unknown IDs (`404`) never reach the mutation at all.

### `GET /api/layout`

Returns the current layout tree as JSON.

Every layout node in a response carries both `direction` and `children`, whatever the config file
says. `config.yaml` may omit either — and may write a single-pane workspace as `layout: {pane: ...}`
with no children at all, which validation accepts — but `normalizeLayoutNode` fills in the direction,
substitutes an empty array for absent children, and moves a root `pane` into the one child it means
before anything serializes. The frontend's `LayoutNodeSchema` requires both keys and covers the whole
response, so without normalization a node missing either fails the *entire* workspaces payload
rather than dropping a key; see issue #198. Every frontend call site reads `child.pane` off a layout *child*, so relocating
a root `pane` is what makes such a workspace display at all.

The migration is persisted the next time the layout is saved, so a hand-written config converges on
the `direction` + `children` form rather than being rewritten underneath the operator on read.

**Relocation applies only to a root `pane` with no children.** A node written as `{pane, children}` is
left exactly as the operator wrote it, and the response still carries the root `pane` — prepending it
to children that already sum to 100 would rescale every sibling and surface a pane that has never
rendered. `LayoutNodeSchema` therefore keeps declaring `pane`: a key the schema does not declare is
stripped by `parse()`, stored stripped, and written back on the next split, which would delete it from
`config.yaml`.

**A relocated root pane is validated like any other pane.** Once it becomes a `LayoutChild` it goes
through `validatePane`, so a root pane with no `type`, or one of
`type: ssh` naming a connection that `ssh_connections` does not define, now fails startup with the
same message the equivalent child pane has always produced. This is deliberate: normalization does not
substitute values the operator never wrote (the same reason an invalid `direction` is reported rather
than corrected), and an error naming the pane is more actionable than the previous outcome, which was
an invalid workspace that displays nothing.

### `PUT /api/layout`

Accepts a layout JSON document, validates it, updates in-memory state, and persists it when possible.

- `400`: invalid JSON
- `422`: structurally invalid layout
- `200`: accepted and returned

The request body is normalized before it is validated, stored and echoed, and `~/` expansion runs
after normalization rather than before — the same order `finishLoad` uses. `ExpandLayoutPaths` walks
`children` only, so expanding first would leave a relocated root pane's `cwd` as a literal `~/`, which
is then a relative path for whatever starts that pane's session. A `PUT` therefore persists and
returns the same shape a load would have produced. Without that the echoed body is the only
`LayoutNode` in an API response that never passes through `normalizeLayoutNode` — a node `PUT` without
a direction would come back as `"direction": ""`, which `LayoutNodeSchema`'s enum rejects. The same
applies to `PUT /api/workspaces/{id}/layout`.

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

Returns a list of active sessions with `id`, `type`, `title`, and `state`.

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

The SSH transport-dial step retries transient failures (such as a momentary DNS resolution error) a
bounded number of times with a short backoff before this endpoint returns `500`. Only the initial
TCP/ProxyJump/ProxyCommand connection step is retried, not SSH handshake or authentication failures.
The retry budget is a single wall-clock deadline shared across an entire ProxyJump chain (each hop
reuses the same deadline rather than getting its own fresh budget), and each retried attempt's own
timeout shrinks to whatever of that budget remains, so a hanging/unreachable host cannot make this
endpoint wait dramatically longer than the ceiling a single dial attempt already tolerated before
retries were introduced.

Once the transport is up, the SSH handshake (version exchange, key exchange and authentication) is
bounded too, and by panemux rather than by the transport. Its ceiling is 30 seconds, but it gets
whatever is left of the dial budget when that is less: the handshake shares the budget instead of
opening a window of its own on top of it, because a ProxyJump chain reaches this step once per hop
and unshared windows would multiply — a wedged two-hop chain would wait three times the ceiling the
budget documents. The handshake runs in its own goroutine and the call returns when it finishes or
when that timer fires, whichever comes first. It has to work this way for all three transports:
`golang.org/x/crypto/ssh`'s `NewClientConn` reads no timeout, sets no deadline and takes no context,
and the transports disagree about deadlines anyway — a TCP conn honors them, a ProxyJump hop is an
SSH channel whose `SetDeadline` reports "not supported", and the ProxyCommand transport's is a no-op
because its pipes have none. The timeout prevents a ProxyCommand bastion that hangs while
establishing its own tunnel from blocking a pane's reconnect indefinitely. A timed-out handshake is
reported as a `500` from this endpoint like any other handshake failure.

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

### `GET /api/tasks` and `POST /api/tasks/hosts/{name}/reconnect`

The task dashboard's collection and its per-host reconnect. Their responses and the collection rules
are in [task dashboard behavior](tasks.md#get-apitasks).

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
