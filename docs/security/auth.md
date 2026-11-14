---
type: Security Requirement
title: "Security: auth token and transport encryption"
description: "Browser request guards, bearer tokens, loopback bootstrap, and transport encryption requirements."
---

# Security: auth token and transport encryption

> Part of the [security design](../security.md). That document carries the general rules, the `gosec` policy, and the map of these files.

### Auth token and transport encryption

`server.host` defaults to `127.0.0.1`. A non-loopback address requires
`server.auth_token` or startup fails validation, alongside the existing port-range check.
When the token is empty, `Config.Load`/`Default` generate it on first run, persist it to
`~/.config/panemux/token` with mode `0600`, and read it on later runs; it is never written
into `config.yaml`. panemux does not terminate TLS. An unencrypted non-loopback hop permits
token replay and request tampering, so the token protects it only behind a TLS-terminating
reverse proxy, SSH tunnel or VPN. See the [security model](../agent-board/security-model.md#security-model).

#### Route ownership and bearer authentication

`internal/api`'s `Handler.Mount` (`routes.go`) owns every API path, method and sub-router;
`internal/server` chooses authentication. Its `registerRoutes` passes the constant-time
`bearerAuthMiddleware(authToken)` as `Mount`'s `boardAuth` argument. It guards only
`/api/board` (`GET /status`, `GET /messages`, `POST /broadcast`, `GET /command/history`),
not the other `/api/*` routes, `/ws/{sessionID}` or `/ws/tasks/events`. Widening bearer auth
requires a matching frontend change; existing requests are unauthenticated.
Handler tests pass `nil` for `boardAuth` and do not establish the shipped auth posture.

Production-router tests in `internal/server/board_routes_test.go` and `route_table_test.go`
protect both auth scope and the complete route table. Wrong/missing board tokens yield `401`,
a correct token reaches `200`, and non-board routes remain outside bearer auth, including
header-free `/api/session-token` and `/ws/{sessionID}`. The middleware probe derives routes
from `chi.Walk` and covers mutating methods without invoking handlers. The table check catches
renames and unreachable routes too: a reachable board route outside the authenticated mount
fails the auth probe, but a board path shadowed by that mount can still return `401`; only
the table check catches that registration. See the [route-gate decision](../decisions/quality-gateway/consolidated-gates.md).

Browsers cannot add `Authorization` to a WebSocket upgrade. `/ws/board/command` instead takes
the token in `Sec-WebSocket-Protocol` (`new WebSocket(url, [token])`), compares it with
`subtle.ConstantTimeCompare`, and echoes it on success to complete the handshake. Do not use
`?token=...`: the full request line reaches access logs, a URL opened as a page reaches browser
history, and same-origin navigation can replay it in `Referer`; the subprotocol avoids those
leak paths. The route exists only with a non-nil command runner (`command_center.enabled: true`
and a configured token); when disabled it is absent rather than present-but-rejecting.

#### Loopback token bootstrap

Unauthenticated `GET /api/session-token` lets dashboard JavaScript learn the possibly generated
board token. CORS is not its protection: it controls script response reads, never handler
execution, and non-browser clients ignore it. A CORS-only guard would expose the token to LAN
clients of a permitted non-loopback listener. `GetBoardSessionToken` (`internal/api/board.go`)
requires all three checks:

1. `r.RemoteAddr`'s socket-peer IP must pass `net.ParseIP(...).IsLoopback()`, never a header's IP.
   This rejects genuinely remote clients even on a non-loopback listener.
2. `r.Host` must be a loopback authority. DNS rebinding can produce a genuinely loopback peer
   while retaining the attacker's domain in Host; the Host check rejects that combination.
3. Any `X-Forwarded-For`, `X-Real-IP` or `Forwarded` header rejects the request. A loopback proxy
   otherwise makes relayed requests indistinguishable from direct local connections. This is a
   fail-closed heuristic: a proxy that strips or omits all three headers defeats it.

For Host only, `isLoopbackAuthority` accepts `0.0.0.0` alongside `localhost`, `127.0.0.1`
and `::1`: a wildcard listener includes loopback and a local browser may name it that way.
The RemoteAddr and proxy-header checks still apply, so this exception does not admit LAN peers.

A genuinely remote dashboard cannot bootstrap through this endpoint. Its operator must provision
the frontend token another way, such as proxy injection; that mechanism is not implemented and
remains a follow-up. The same limitation applies behind a proxy that omits identifying headers.

This endpoint's Host check does not close DNS rebinding across the server. Other pre-existing
unauthenticated API and terminal-WS routes lack their own Host validation; `checkOrigin` permits
no Origin and `u.Host == r.Host`, which a rebound request can satisfy with the attacker's Host.
Broader Host validation remains a separate follow-up. The shared guard below is also explicitly
not DNS rebinding protection.

Keep the endpoint outside `/api/board/`: chi routes that subtree through bearer auth regardless
of a separately registered literal handler. A flat handler test misses mount precedence;
`TestServer_SessionTokenRoute_RemainsUnauthenticated` exercises the real `server.New()` router.
See the [bootstrap decision](../decisions/agent-board/board-token-bootstrap.md) for history.

#### MCP token file

The command center temporarily writes the bearer token in `BuildMCPConfig`'s JSON `env` block
as `PANEMUX_BOARD_TOKEN`, for the `claude -p` subprocess's `panemux __board-mcp-server` child.
Create it through `fileops.CreateTemp`, explicitly `fileops.Chmod` it to `0600`, and remove it
after the subprocess exits. Every failure after creation must clean it up, as pinned by
`TestBuildMCPConfigReportsEachFailedStepAndLeavesNoFileBehind`; the fileops seam makes each
failure testable. It remains a plaintext token file, though its one-query lifetime is shorter
than `~/.config/panemux/token`'s. It is never written to `~/.config/panemux/config.yaml`.

### Shared browser request guard

`internal/requestsecurity` guards every `/api` route, including `/api/board/*` and
`/api/session-token`, and all three WebSockets (`/ws/{sessionID}`, `/ws/board/command`,
`/ws/tasks/events`). The server checks these path subtrees before CORS, OPTIONS responses
and routing; API mounts also apply the same guard before authentication and handlers. `Sec-Fetch-Site: same-site` and `cross-site` return `403`. When Origin is present,
it must be one http/https origin without credentials, path, query or fragment, and its
hostname and effective port must match the request Host. Hostnames are case-insensitive;
IPv6 representations are normalized; brackets require a valid IPv6 literal, and
unbracketed IPv6 and zone identifiers are refused independently of the Go URL parser. Different loopback hostnames are not interchangeable,
and another loopback port is refused even without Fetch Metadata. The browser guard runs
before terminal subscription, command execution or task observation.

An omitted Origin is permitted for CLI clients only when no Referer or Fetch Metadata is
present. An Origin-free browser request requires one valid http/https Referer with matching
hostname and effective port; if Sec-Fetch-Site is present it must be `same-origin`. Direct
navigation (`none`), missing Referer, malformed/duplicate Referer or Sec-Fetch-Site, and
unknown Sec-Fetch-Site values are refused. Referer may include a path and query but not
credentials or a fragment. When both Origin and Referer are present, both must match Host.
A rejected request returns `403` before disk writes, PTY/SSH creation, task collection or
listener binding. CORS reflects only an Origin that passes this same policy; arbitrary
loopback ports are never allowed. Allowed OPTIONS requests return `204`. Origin's default port is 80 for http and 443 for https; a Host without a port uses that
same default. Scheme is not compared to the backend connection, preserving TLS termination
at a proxy. Consequently, another scheme at the same hostname and explicit port is not
reliably refused. Forwarded headers do not override this policy. This guard is neither
client authentication nor DNS rebinding protection.

Development proxies must preserve the browser Host and Origin. Vite configures both `/api`
and `/ws` with `changeOrigin: false`; it never rewrites Origin. Fetch Metadata alone is
insufficient: Chrome WebSocket upgrades may omit it, so Origin is always checked when present.
