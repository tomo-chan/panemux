# Agent Board: testing plan

> Part of the current [Agent Board design](../agent-board.md).

## Testing plan (see DEVELOPMENT.md for the TDD/coverage rules this must follow)

- `internal/board`: status JSON parsing (valid full payload with `kind: "board_status"`, missing
  optional fields, a body that isn't valid JSON falls back to being treated as a plain message, and
  — the regression test for shape-sniffing ambiguity — a body that
  *is* valid JSON, contains a `state` field, but is missing or has the wrong `kind` is also treated
  as a plain message rather than mistaken for a status update), `BoardCache.StatusSnapshot` with
  multiple status rows for one pane (only the
  newest wins, `UpdatedAt` reflects when it was recorded) and across multiple panes, `BoardCache`'s
  own `Seq` assignment giving a stable total order across rows from different hosts even when their
  agmsg-native `ID`s collide or aren't comparable, `MessagesSince(afterSeq)` ordering and bounding,
  a `board_status` row addressed to `_system` both updating `status` and remaining in history (the
  API marks it with `is_status`, and the dashboard filters it from the visible message feed), while a
  plain non-status row addressed to `_system` appears as an ordinary message; an empty cache read
  (fresh start / post-restart) returning a well-defined empty
  result rather than an error, relay cursor persistence across a simulated restart (including the
  accepted at-least-once duplicate case — assert it is delivered again, not that it's silently
  dropped or that the relay errors), the accepted truncation case (more new rows on one host than
  one poll's `--limit` — assert the newest ones are kept and the oldest of the overflow are dropped,
  not that everything is delivered), the relay's `from`-validation (a row whose `from` is neither a
  known local pane ID on its source host nor a ledger-matched `_system` is dropped and logged, never
  cached or relayed), the own-send ledger specifically (a `from == "_system"` row that matches a
  recently recorded `Send` is accepted; a `from == "_system"` row with no matching ledger entry —
  including one crafted with a `to`/`body` that doesn't match any real recent send — is dropped and
  logged, never treated as legitimate on the strength of the string alone; an entry past its TTL is
  no longer matchable — this is the regression test for the cross-host `_system` impersonation
  scenario in [Security model](security-model.md#security-model), and why a blanket
  `_system` allowance was insufficient), empty team.
- `internal/session`: for `RemoteAgmsgClient`, a body containing shell metacharacters (`'`, `;`,
  `` ` ``, `$(...)`) round-trips through the built `send.sh` command string as a single escaped
  literal argument, not as executed shell syntax — and the same for a `team`/`--agent` value
  containing metacharacters on the **read** path (`api.sh get ...`), which protects the requirement
  that free-text read arguments are escaped; every
  `AgmsgClient.Send` call is asserted to include `--force` unconditionally, with no code path that
  omits it.
- `internal/config`: `host != loopback && auth_token == ""` is a validation error; all other
  combinations are valid. `agent_board.team` defaults to `"panemux"` when unset. A pane config with
  `id: "_system"` is a validation error, both alone and alongside otherwise-valid other panes.
- `internal/server`: missing/incorrect bearer token on `/api/board/*` is rejected (401); the correct
  token succeeds; pre-existing `/api/*` routes and `/ws/{sessionID}` remain reachable with no
  `Authorization` header at all — a required regression test, since scoping the middleware to
  `/api/board` only (rather than gating the whole API) is exactly the choice that could silently
  widen later. `/ws/board-command` follows the bearer-token rule. The unauthenticated
  `/api/session-token` bootstrap route is covered separately: both its remote address and `Host`
  must be loopback because the response contains the token itself.
- `internal/api`: `GetBoardStatus`/`GetBoardMessages`/`PostBoardBroadcast` handler-level behavior —
  empty cache returns a well-formed empty response (`{"statuses":{}}` / `{"messages":[]}`, never
  `null`), `since` omitted defaults to `0`, a non-numeric `since` is `400`, `to`/`body` validation
  failures and `*board.UnknownPaneError` are `422`, malformed request JSON is `400`, any other
  downstream `AgmsgClient`/relay error is `502`, success is `200`. Auth itself is not this package's
  concern — that is `internal/server`'s bullet above, since `bearerAuthMiddleware` sits in front of
  these handlers, not inside them.
- `internal/ws`: `/ws/board-command` is under the same package as the existing terminal
  WebSocket handler, so it is covered by `coverage-go`'s existing `internal/ws` gate (see
  `DEVELOPMENT.md`) — no separate coverage carve-out is introduced for it. Its handshake rejection
  and message-framing tests follow the same pattern as the terminal socket's existing tests.
- Frontend (schema-first, per `DEVELOPMENT.md`): `frontend/src/schemas/index.ts` gets Zod schemas
  for every board API shape before any component consumes it — `BoardStatus`, `BoardMessage` (the
  `GET /api/board/messages` row shape), and the `board-command` WS frame shapes (prompt, streamed
  assistant text/tool-use chunks, error frame) — with acceptance tests for a valid payload and
  rejection tests for a payload missing a required field or carrying an unexpected type, matching
  the existing coverage pattern for other API schemas. The Spotlight-style command palette and the
  separate history panel (see [Command center](command-center.md#command-center)) each need component tests covering:
  opening the palette renders inline history from a fixture history response; a streamed response
  updates the visible output incrementally as WS frames arrive; an error frame renders a visible
  error state rather than leaving the palette silently stuck; the history panel's empty state before
  the command center has ever been used; and dismiss/close behavior returning focus to the
  previously focused pane. These fall under `coverage-frontend`'s existing `frontend/src/hooks/` and
  `frontend/src/schemas/` gates for the schema and any new hook, plus ordinary component tests for
  the palette/history UI itself, matching the existing project convention rather than introducing a
  new coverage carve-out.
- agmsg detection: a pane on a host where the configured/default agmsg path doesn't contain
  `scripts/api.sh` skips bootstrap and logs a warning without touching the pane's session (asserted
  against a fake/no-op `BoardExecutor`/host check, not a real agmsg install, and not via `command -v
  agmsg` — see [Integration with agmsg](agmsg-integration.md#integration-with-agmsg) for why that check is unreliable);
  a pane on a host where agmsg is present bootstraps normally.
- `agmsg_path` expansion: a config value with a leading `~` resolves to a fully expanded absolute
  path for a local host (via the injectable home-dir override, per `DEVELOPMENT.md`'s testability
  rule) and for a remote host (via a fake exec channel returning a fixed `$HOME` probe response)
  before it reaches any `RunBoardCommand` call, asserted by inspecting the built argument list
  directly rather than the final shell string — this is the regression test for a literal `~`
  reaching the remote shell inside single quotes and failing to expand.
- Command center: `/ws/board-command` rejects a missing or incorrect bearer token and accepts the
  configured token, while the ordinary terminal WebSocket remains outside this authentication
  boundary; a `POST /api/board/broadcast` call issued from the command center's own
  HTTP client reaches a target pane regardless of that pane's host, using a fake `AgmsgClient` per
  host to assert routing without a real agmsg/SSH dependency, and never invokes `AgmsgClient`
  directly from the command center's own code path (it goes through the same handler a browser
  request would); `GET /api/board/command/history` returns a correctly ordered feed built from a
  fixture `stream-json` capture containing interleaved user turns, assistant text, and tool calls,
  and returns an empty/well-defined result before the command center has ever been used; enabling
  `command_center` does not require or check for a local agmsg installation.
- Command center [process lifecycle](command-center.md#process-lifecycle): a first query with no
  persisted session id mints a UUID, passes it with `--session-id` and without `--resume`, ignores
  any session id reported by the subprocess, and persists the UUID panemux supplied after the query
  succeeds; a subsequent query reuses that persisted id with `--resume`; a second query
  arriving while one is still in flight is rejected with the "busy" error and never spawns a second
  subprocess; the invoked command line always includes `--verbose` whenever `--output-format=stream-
  json` is present; the subprocess is always launched with `--allowedTools` scoped to exactly the
  three board MCP tools and never with `--dangerously-skip-permissions`; a non-zero subprocess exit
  and a malformed `stream-json` line each surface as a distinct WS error frame rather than an empty
  or hung response. A failed first query does not persist its newly minted id; a failed resumed query
  clears the stale persisted id so the next query can start a fresh session.
