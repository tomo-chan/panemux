---
type: Security Requirement
title: "Authentication and browser request guard"
description: "Bearer authentication, loopback token bootstrap, MCP token files, transport encryption, and the shared browser request guard."
---

# Authentication and browser request guard

> Part of [Security overview and invariants](overview.md).

## Auth token and transport encryption

`server.host` defaults to `127.0.0.1`. Any non-loopback address requires an explicit
`server.auth_token` or startup fails validation alongside the port-range check. When empty on
loopback, `Config.Load`/`Default` generates a token on first run and persists it to
`~/.config/panemux/token` with mode `0600` (never written into `config.yaml`). Panemux does not
terminate TLS: non-loopback deployments require a TLS-terminating reverse proxy, SSH tunnel, or VPN
to prevent token replay and tampering ([Agent Board deployment boundary](agent-board.md#deployment-and-message-trust)).

### Route ownership and bearer authentication

`internal/api`'s `Handler.Mount` (`routes.go`) owns every API path, method, and sub-router;
`internal/server`'s `registerRoutes` passes constant-time `bearerAuthMiddleware(authToken)` as
`boardAuth`. It guards `/api/board` (`GET /status`, `GET /messages`, `POST /broadcast`,
`GET /command/history`), leaving other `/api/*` routes, `/ws/{sessionID}`, and `/ws/tasks/events`
outside bearer auth (handler tests pass `nil` for `boardAuth` and do not establish shipped auth).

Production-router tests (`internal/server/board_routes_test.go`, `route_table_test.go`) derive
routes via `chi.Walk` and enforce both auth scope and the complete route table so shadowed or
renamed routes cannot slip through ([D3](../decisions/quality-gateway.md#d3-consolidate-gates-rather-than-adding-them)).

Because browsers cannot set `Authorization` on WebSocket upgrades, `/ws/board/command` reads the
token from `Sec-WebSocket-Protocol` (`new WebSocket(url, [token])`), verifies it with
`subtle.ConstantTimeCompare`, and echoes it on handshake. Never use `?token=...` (leaks to access
logs, browser history, and `Referer`). When `command_center.enabled` is false or no token is set,
the route is absent rather than present-but-rejecting.

### Loopback token bootstrap

Unauthenticated `GET /api/session-token` lets dashboard JavaScript learn the board token. CORS
controls response reads, never handler execution, so `GetBoardSessionToken` (`internal/api/board.go`)
enforces all three checks:

1. `r.RemoteAddr`'s socket-peer IP must satisfy `net.ParseIP(...).IsLoopback()` (never a header IP).
2. `r.Host` must be a loopback authority (`localhost`, `127.0.0.1`, `::1`, or `0.0.0.0` for wildcard
   local listeners; `TestGetBoardSessionToken_DNSRebindingHost_Forbidden` rejects rebinding domains).
3. Any `X-Forwarded-For`, `X-Real-IP`, or `Forwarded` header rejects the request (fail-closed
   heuristic against loopback proxies; a proxy stripping all three defeats it).

Remote dashboards cannot bootstrap here and require out-of-band token provisioning (not yet
implemented). Keep the route outside `/api/board/` (verified against `server.New()` by
`TestServer_SessionTokenRoute_RemainsUnauthenticated`; see
[bootstrap decision](../decisions/agent-board.md#browser-token-bootstrap-and-independent-dashboard-capability-2026-08-142026-08-22-prs-170-171)).
This endpoint's `Host` check does not close DNS rebinding on other unauthenticated routes.

### MCP token file

`BuildMCPConfig` temporarily writes `PANEMUX_BOARD_TOKEN` in the MCP JSON `env` block for
`panemux __board-mcp-server`. Create it via `fileops.CreateTemp`, `fileops.Chmod` to `0600`, remove
it on subprocess exit, and clean up on every post-creation failure
(`TestBuildMCPConfigReportsEachFailedStepAndLeavesNoFileBehind`). Never write it to `config.yaml`.

## Shared browser request guard

`internal/requestsecurity` guards every `/api/*` route (including `/api/board/*` and
`/api/session-token`) and all three WebSockets (`/ws/{sessionID}`, `/ws/board/command`,
`/ws/tasks/events`) before CORS, `OPTIONS`, routing, authentication, and side effects:

- `Sec-Fetch-Site: same-site` or `cross-site` returns `403`.
- When `Origin` is present, it must be a single `http`/`https` origin without credentials, path,
  query, or fragment, whose case-insensitive hostname and effective port match `Host`. IPv6
  literals require brackets and are normalized; unbracketed IPv6 and zone IDs are refused. Distinct
  loopback hostnames and other loopback ports are refused even without Fetch Metadata.
- Omitted `Origin` is allowed for CLI clients only when neither `Referer` nor Fetch Metadata is
  present. An `Origin`-free browser request requires one valid `http`/`https` `Referer` (no
  credentials/fragment) matching `Host` and effective port, and if `Sec-Fetch-Site` is present it
  must be `same-origin`. Direct navigation (`none`), missing/malformed/duplicate headers, and
  unknown `Sec-Fetch-Site` values return `403`. When both `Origin` and `Referer` are present, both
  must match `Host`.
- CORS reflects only an `Origin` passing this policy; allowed `OPTIONS` returns `204`. Default
  ports are 80 (`http`) and 443 (`https`). Scheme is not compared against backend TLS so upstream
  TLS termination works (cross-scheme access on identical host and explicit port is an accepted
  limitation). Forwarded headers never override this guard. This guard is neither client
  authentication nor DNS rebinding protection.
- Development proxies must preserve `Host` and `Origin` (Vite sets `changeOrigin: false` on `/api`
  and `/ws`). Fetch Metadata alone is insufficient because Chrome WebSocket upgrades may omit it.
