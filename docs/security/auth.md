# Security: auth token and transport encryption

> Part of the [security design](../security.md). That document carries the general rules, the `gosec` policy, and the map of these files.

### Auth token and transport encryption

`internal/config`'s `ServerConfig.AuthToken` (`server.auth_token` in `config.yaml`) and the
non-loopback-requires-token validation rule are implemented: panemux does not terminate TLS itself,
`server.host` defaults to `127.0.0.1`, and if it is set to a non-loopback address,
`server.auth_token` must also be set, or startup fails validation (`internal/config/validate.go`,
alongside the existing `server.port` range check). When left empty, `Config.Load`/`Default`
auto-generate a token on first run and persist it to `~/.config/panemux/token` (`0600`), read back
on later runs, and never write it into `config.yaml` itself. An auth token sent over an unencrypted
non-loopback hop can be replayed and the request it authenticates can be tampered with in transit,
so the token only provides real protection once the operator has placed a TLS-terminating reverse
proxy, SSH tunnel, or VPN in front of the non-loopback listener. See
[agent-board/security-model.md](../agent-board/security-model.md#security-model) for the full rationale.

`internal/server`'s constant-time bearer-token middleware (`bearerAuthMiddleware`, `internal/server/auth.go`)
is implemented, unit-tested, and wired in `registerRoutes` — but **only** onto the
`api.BoardRoutePrefix` (`/api/board`) sub-router (`GET /status`, `GET /messages`, `POST /broadcast`,
`GET /command/history`), not onto any pre-existing `/api/*` route or `/ws/{sessionID}`. Widening it to
those routes without a matching frontend change would break every existing, currently-unauthenticated
request, so that remains a separate, larger change.

**Which package owns which half of that sentence matters, because it changed.** The route table
itself — every path, method and the `r.Route` nesting, including which routes sit under
`BoardRoutePrefix` — lives in `internal/api`'s `Handler.Mount` (`internal/api/routes.go`), so that
`internal/server`'s production wiring and `internal/api`'s own handler tests cannot describe
different routes; they previously could, and had already drifted, with `/api/board/*` registered flat
and with no middleware in the test copy. **Choosing the middleware stays with `internal/server`**:
`registerRoutes` passes `bearerAuthMiddleware(authToken)` as `Mount`'s `boardAuth` argument, and the
handler tests pass `nil`. A test that mounts the board routes unauthenticated is therefore no longer
asserting anything about the shipped auth posture, by construction — that assertion lives only in
`internal/server`, against the router `server.New()` really builds.

Two files cover this scoping as a regression. `internal/server/board_routes_test.go` checks that an
incorrect token on `/api/board/*` is rejected with `401`, that the correct token reaches `200`, and
that `/api/session-token` and `/ws/{sessionID}` stay reachable with no `Authorization` header at all.
`internal/server/route_table_test.go` adds the missing-token case and the complementary check that no
route outside `/api/board/` sits behind the middleware at all — it probes each route's own middleware
chain, as `chi.Walk` reports it, rather than dispatching a real request, so the check covers the
`POST`/`PUT`/`DELETE` half of the API the frontend depends on without creating a session or writing
config as a side effect. More importantly, it derives
the list of board routes **from the router itself** with `chi.Walk` rather than naming them: a
`/api/board/*` route added later — or, the failure this actually guards, one registered outside the
authenticated sub-router — is covered the day it is registered, without anyone remembering to extend
a list. It also pins the complete route table, so a route cannot silently appear, move, or be renamed.
Both properties were confirmed by perturbation, not assumed: registering a *reachable*
`/api/board/leaked` outside the sub-router fails the auth test, and renaming a route fails the table
test. One nuance is worth recording, because it bounds what the auth test alone proves: a
`/api/board/*` route registered inside the `/api` sub-router is *shadowed* by the `/api/board/*`
mount, so the probe still gets its `401` from the middleware and the auth test passes — what catches
that case is the table test, which lists a route the router can never actually reach. The two tests
are complementary, not redundant.
See [agent-board.md](../agent-board.md)'s status note.

**`WS /ws/board-command` cannot use the `Authorization` header at all** — browsers do not allow a
WebSocket upgrade request to carry arbitrary headers. `internal/ws/board_command.go`'s
`BoardCommandHandler` instead reads the token from the `Sec-WebSocket-Protocol` request header (the
client dials with `new WebSocket(url, [token])`), validated with the same
`subtle.ConstantTimeCompare` discipline as `bearerAuthMiddleware`, and echoes the same value back in
the response header on success, completing the WebSocket handshake per spec. **A `?token=...` query
parameter was deliberately not used**: unlike a header, a query parameter is written into the
server's own access logs (`middleware.Logger` logs the full request line for every request, including
this one) and into the browser's navigation history if the WS URL were ever opened as a normal page,
and would be replayed in the `Referer` header of any same-origin follow-up navigation — a subprotocol
value has none of those leak paths. This route is registered in `internal/server/server.go` only when
a `*commandcenter.Runner` is non-nil (`command_center.enabled: true` and a token configured); when
disabled the route is absent entirely, not present-but-rejecting, so there is nothing there for an
unauthenticated probe to even find.

**`GET /api/session-token` is the one deliberate, unauthenticated exception**, and exists specifically
to let the browser dashboard learn the token it needs for every route above — there is no other way
for client-side JavaScript to learn a value that may have been randomly generated on first run.

**This endpoint does NOT rely on `corsMiddleware`/CORS for protection, despite an earlier revision of
this document claiming exactly that.** That claim was wrong and was caught by an adversarial review,
not by any test: CORS only controls whether a cross-origin *script* can read a response body — it
never rejects the request from reaching the handler, and a non-browser client (`curl`, any process on
the LAN) ignores CORS entirely. Since this token is the only thing gating every other
`/api/board/*` route (see above) and `internal/config/validate.go`'s own non-loopback-requires-token
rule explicitly permits `server.host` to be a non-loopback address as long as a token is set, a CORS-only
guard would have handed the token to any LAN client that simply asked, in exactly the deployment shape
the token exists to protect.

`GetBoardSessionToken` (`internal/api/board.go`) instead checks two things directly against the
request, neither of which a client fully controls the way it controls the `Origin` header CORS reads:

1. **`r.RemoteAddr`'s IP must be loopback.** This is the check that actually restricts the endpoint to
   the local machine — it rejects any client that genuinely isn't the box panemux runs on, including a
   LAN client reaching a non-loopback `server.host`. Checked with `net.ParseIP(...).IsLoopback()`
   against the net/http-reported socket peer address, not a header.
2. **`r.Host` must also resolve to a loopback authority.** RemoteAddr alone is not enough: DNS
   rebinding (a domain whose DNS answer changes to `127.0.0.1` after a browser's same-origin check
   already passed) makes an attacker page's requests arrive with a genuinely loopback RemoteAddr — the
   TCP connection really is local — while the Host header still carries the attacker's own domain,
   since browsers send the navigation URL's original host, not the resolved IP. Only the Host check
   catches that case; this was verified with a dedicated test
   (`TestGetBoardSessionToken_DNSRebindingHost_Forbidden`) simulating exactly that header combination.

**Accepted limitation, stated explicitly: a dashboard served from a genuinely non-loopback
`server.host` can no longer bootstrap its own token through this endpoint at all**, since no real
remote client can ever satisfy the loopback-RemoteAddr check. This is intentional, not an oversight —
the alternative (allowing the endpoint to answer non-loopback requests) is exactly the exposure this
whole guard exists to close, and the token's own purpose in that deployment shape is to gate
network-reachable access, not to be handed out to it. An operator running panemux non-loopback must
provision the frontend's token some other way (e.g. a reverse proxy that injects it); that mechanism
does not exist yet and is tracked as a follow-up, not solved by this endpoint.

**The RemoteAddr+Host loopback checks alone still have a gap: they trust `r.RemoteAddr` even when the
request actually arrived through a reverse proxy**, and a proxy sitting on the loopback interface
itself produces a genuinely loopback `RemoteAddr` for every request it forwards, no matter its real
origin — the two checks above cannot distinguish "the browser dialed this box directly" from "a local
proxy relayed someone else's request to this box." `GetBoardSessionToken` closes this with a third,
`isProxiedRequest` check: any request carrying an `X-Forwarded-For`, `X-Real-IP`, or `Forwarded` header
is rejected outright, on the theory that a direct loopback browser request never carries proxy
metadata — only a proxy adds those headers. This is a fail-closed heuristic, not a guarantee: a proxy
that strips or never sets any of these three headers still defeats it, and the accepted limitation
above (no token-bootstrap path for a genuinely non-loopback `server.host`) already covers that
residual case by design — an operator fronting panemux with such a proxy must provision the token some
other way, the same as any other non-loopback deployment.

**A `server.host` of `0.0.0.0` is explicitly treated as loopback-equivalent for the `r.Host` check.**
`isLoopbackAuthority` accepts `"0.0.0.0"` alongside `localhost`/`127.0.0.1`/`::1`: a server bound to
`0.0.0.0` listens on every interface including loopback, so a genuinely local browser's request often
carries `Host: 0.0.0.0:<port>` (or the operator configured it that way) even though the connection is
via loopback — rejecting that Host would have made the endpoint unusable for a common, otherwise-safe
deployment shape. This does not widen what the endpoint accepts from the network: the `RemoteAddr`
loopback check and the `isProxiedRequest` check above still gate every request the same way regardless
of which loopback-equivalent Host string it presents.

**This is a narrower fix than the broader DNS-rebinding exposure across this codebase.** Every other
pre-existing unauthenticated `/api/*` route and `/ws/{sessionID}` (see below) still has no Host-header
validation of its own — `checkOrigin` in `internal/ws/handler.go` allows any request with no `Origin`
header at all, and permits `u.Host == r.Host`, which is exactly the tautology DNS rebinding defeats,
since the attacker's page and the rebound request both send the same (attacker-controlled) Host. The
session-token endpoint's guard was added specifically because it is the single highest-value target
(it hands out the credential to everything else), not because the broader gap across other routes has
been closed — it has not. Extending Host-header validation to every route is tracked as a separate,
larger follow-up, out of scope for the command center feature this guard was added alongside.

It is deliberately registered at `/api/session-token`, **not** `/api/board/session-token`: chi routes any
path starting with `/api/board/` into the `bearerAuthMiddleware`-wrapped sub-router regardless of
where else a handler for that literal path is registered — an earlier revision of this endpoint lived
at that path and was silently caught by the very middleware it exists to bypass, discovered only by
an end-to-end `curl` check against the real running server, not by any handler-level unit test (those
construct their own flat test router and never exercise chi's actual mount-precedence behavior).
`internal/server/board_routes_test.go`'s `TestServer_SessionTokenRoute_RemainsUnauthenticated` is a
regression test against the real `server.New()`-constructed router specifically because a
handler-level test would not have caught this class of bug.

**The command center's own MCP config file is the one place this feature writes the bearer token to
disk, deliberately temporarily.** `internal/commandcenter.BuildMCPConfig` writes a JSON file (mode
`0600`, created via `fileops.CreateTemp` then explicitly `fileops.Chmod`'d — the same operations
behind [`internal/fileops`](../architecture.md)' seam, so each arm that could leave a token-bearing file
behind is now reachable from a test) embedding the token as a
`PANEMUX_BOARD_TOKEN` environment variable value for the `claude -p` subprocess's own MCP server
child process (`panemux __board-mcp-server`) to read; the caller's `cleanup` func removes it once that
subprocess has exited. This is a strictly smaller exposure than the existing `~/.config/panemux/token`
file the token already lives in permanently — the temp file exists only for one query's subprocess
lifetime and is never written to `~/.config/panemux/config.yaml` — but it is still a plaintext
token-bearing file on disk, hence the explicit `0600` rather than relying on `os.CreateTemp`'s default
mode. `TestBuildMCPConfigReportsEachFailedStepAndLeavesNoFileBehind` pins that every failure arm
after the file exists calls `cleanup()` on the way out, so a failed build never leaves the token
sitting in the temp directory.
