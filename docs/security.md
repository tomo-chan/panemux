# Security Design

This document defines the implementation-time security requirements for panemux. Treat it as required reading before changing command execution, shell argument handling, SSH path handling, host key handling, or `gosec`-sensitive code.

## Session Command Execution

All session types that execute a local process use `exec.Command` with user-configurable values such as shell paths and tmux session names. These values must be sanitized before reaching the exec sink.

### Shell path (`local`, `ssh` sessions)

`validateShell` in `internal/session/local.go` applies three layers:

1. **Absolute-path check**: reject relative paths outright.
2. **Regex character allowlist**: `^(/[a-zA-Z0-9._\\-/]+)$` rejects shell metacharacters such as spaces, semicolons, and quotes.
3. **`/etc/shells` allowlist**: iterate the system shell registry and return the key from the trusted map (`s`), not the caller-supplied value.

The third point is critical for CodeQL's `go/command-injection` analysis. CodeQL tracks data flow from taint sources such as environment variables and HTTP request bodies to exec sinks. A sanitization function only breaks the taint chain if its return value has no data-flow path back to user input.

Returning `m[1]` from `regexp.FindStringSubmatch(shell)` is insufficient because the submatch is still derived from `shell`. Returning the `/etc/shells` map key `s` works because CodeQL does not propagate taint through equality comparisons in a range loop: `s` originates from file I/O, not user input.

For the same reason, `os.Getenv("SHELL")` is not used as a default shell. Environment variables are taint sources in CodeQL's model. The default must remain the hardcoded literal `"/bin/sh"`.

### Tmux session name (`tmux`, `ssh_tmux` sessions)

`validTmuxSessionName` in `internal/session/tmux_ssh.go` uses a strict regex (`^[a-zA-Z0-9_.-]+$`) validated at construction time. Arguments are passed as discrete `exec.Command` args, not via `sh -c`, so no shell interpolation occurs.

### Remote path arguments (SSH working directory)

When an SSH or SSH+tmux pane has `cwd` set, the path is passed as part of a remote shell command (`cd <cwd> && exec $SHELL`). User-supplied paths that flow into `sess.Start()` must be validated with `validRemotePath` in `internal/session/ssh.go` before use.

`validRemotePath` is a regex guard:

```text
^(/[^;|&$\`'"<>()\[\]{}!\\\x00-\x1f\x7f]*)+$
```

It accepts only absolute Unix paths and rejects shell metacharacters and control characters. This is the CodeQL-recommended sanitization pattern for shell arguments.

After validation, the path is wrapped with `shellQuotePath`, which single-quotes the value and escapes any interior single quotes. This keeps paths containing spaces or unusual but allowed characters safe when embedded in a shell string.

### Agent board remote writes

`internal/board`'s `RemoteAgmsgClient` (full design in [agent-board.md](agent-board.md)) writes
cross-pane agent messages into a remote host's message store over the SSH exec channel already used
by `GetCWD`/`InspectGitContext` — `internal/session`'s `BoardExecutor.RunBoardCommand`, implemented
on `SSHSession`/`TmuxSSHSession` — by running an operator-installed
[agmsg](https://github.com/fujibee/agmsg) instance's own scripts. panemux owns no message schema or
storage of its own — it is a client of agmsg only. The `panemux` binary itself is never installed
on a remote host under any circumstances — it is also a server, and a stray copy on an SSH-reached
host could start its own HTTP/WS listener and command center.

Message bodies are arbitrary text written by a Claude (or other agent) process, not a trusted
value, and plain `shellQuotePath`-style quoting of that value alone does not satisfy this
repository's `go/command-injection` CodeQL bar (a quoting transform does not itself break a taint
chain; only a preceding regex-allowlist branch does — see `validateShell`'s explanation above).
Because a message body is free text and cannot be regex-allowlisted directly the way a path can,
`RemoteAgmsgClient.Send` base64-encodes the body, regex-checks the *encoded* string against
`^[A-Za-z0-9+/]*={0,2}$`, and only then places it in the `RunBoardCommand` argument list — structurally
mirroring `validRemotePath`'s accepted shape (a regex-allowlist branch gating a value before it reaches
the exec sink) for a value that couldn't otherwise take it. **Caveat, stated plainly:** because
`base64.StdEncoding`'s output is by construction always within that alphabet, the `MatchString` branch
can never actually fail for correctly-encoded input — it is a regex-allowlist branch in *shape*, not
one that can reject real input. No CodeQL scan has been run against this code to confirm the taint
chain is actually recognized as broken in practice; treat it as a structurally-motivated best effort,
not a verified one, until a real scan says otherwise — see
[agent-board.md's Security model](agent-board.md#security-model) for the same caveat stated in more
detail. `team`/`from`/`to`
identifiers are regex-allowlisted directly (`^[A-Za-z0-9_.-]+$`) for the same reason, since agmsg's
own `--agent` argument validation is not a claim panemux can rely on — it runs inside the remote
shell process that only exists because panemux's own command string has already been parsed, so it
cannot retroactively protect that string's construction. Writes go through `scripts/api.sh`'s
sibling script, `scripts/send.sh <team> <from> <to> <body> [--force]`, which — per agmsg's own
source, verified rather than assumed — takes `body` as a positional argument with **no stdin
option**, so a fixed, non-tainted wrapper script (`sendBase64WrapperScript` in
`internal/board/remote_client.go`) decodes the base64 body back to its original bytes remotely,
via shell positional parameters (`$1`..`$5`), immediately before the one call site that needs the
raw value — never through unquoted string interpolation. `send.sh` does its own SQL escaping
internally, so `RunBoardCommand` remains responsible only for the POSIX shell escaping
(`shellQuotePath`-style, matching the existing `cwd` discipline) that wraps every argument,
including the wrapper script text itself, before the remote command string is built.

`send.sh` and `api.sh` are the two agmsg scripts this design's message read/write path runs
remotely; each runs against agmsg's own local store on that host. The only other remote commands
panemux itself ever executes are two fixed, non-tainted `sh -c` probes, neither of which takes any
caller-supplied data: `internal/board/agmsg_path.go`'s `remoteHomeProbeCmd`
(`sh -c 'printf '%s' "$HOME"'`), run once per remote host to resolve `agent_board.agmsg_path`'s
leading `~/` against that host's own home directory before it is ever placed in a `RunBoardCommand`
argument list — see [agent-board.md](agent-board.md)'s "`~` in `agmsg_path` is expanded by panemux"
section — and `internal/board/agmsg_presence.go`'s `remoteAgmsgPresenceProbeScript`
(`test -f "$1" && printf 'yes' || printf 'no'`), run by the bootstrap watcher (and, independently,
whenever the relay resolves a host's client) to check whether `scripts/api.sh` exists at the
already-resolved `agmsg_path` before treating that host as bootstrap-eligible. Because
`remoteAgmsgPresenceProbeScript` takes its one variable input (the path to test) as a positional
parameter (`$1`), not string-interpolated into the script body, it carries no taint from
`agent_board.agmsg_path` into the script text itself — the same discipline
`sendBase64WrapperScript` above uses, just with no caller-supplied value needing a preceding
regex-allowlist branch at all here, since the path being tested is `agent_board.agmsg_path` already
resolved to an absolute path by the `~` expansion step, itself derived from operator config, not
runtime request data. panemux only ever detects an existing agmsg installation — it never installs,
updates, or otherwise manages agmsg on the operator's behalf, locally or remotely. The relay
goroutine that drives this on a schedule (`internal/board/relay.go`), the bootstrap watcher
(`bootstrapWatcher` in `bootstrap.go`, `package main`), the `/api/board/*` REST surface
(`GET /status`, `GET /messages`, `POST /broadcast`), and the command center (`internal/commandcenter`,
`internal/boardmcp` — see "Command center subprocess execution" below) are all implemented — see
[agent-board.md](agent-board.md)'s status note.

**The bootstrap watcher's PTY write is not a command-execution sink and is out of scope for this
document's `exec.Command`-focused rules above.** `bootstrapWatcher` writes a synthesized onboarding
instruction into a pane's PTY via `Session.Write` — the same path real user keystrokes already go
through — not via `exec.Command`, so none of the shell-argument-escaping or CodeQL taint-chain
reasoning above applies to that write itself: there is no shell parsing panemux's own Go code
performs on that text, and no distinction between "trusted" and "tainted" content for a PTY write
the way there is for a command-string argument. The one identifier bootstrap itself passes into a `RunBoardCommand` call — the already-resolved
`agmsg_path` used to build the presence probe's `$1` — is quoted with the same `shellQuotePath`-style
discipline `RunBoardCommand` already applies uniformly to every argument, board-related or not.
`agent_board.team`, a pane's own ID, and the agmsg-recognized type string
`session.AgentTypeDetector` returns are written only into the PTY instruction text, never into a
`RunBoardCommand` call bootstrap itself makes; they are operator config or panemux's own fixed
detection-table output either way, not external request data.

### Agent-reported values in the dashboard UI

Everything in a `board_status` self-report — `state`, `cwd`, `branch`, `repo`, `pr_url`, `last_tool`,
`summary` — is free text written by an agent process, possibly on a remote host, and panemux
validates none of it: `internal/board`'s `ParseStatus` copies each field through verbatim, and the
relay's only gate is `validFrom` (was this row sent by a known board-enabled pane), which says
nothing about a row's *contents*. A pane that has been talked into writing a hostile status report,
or any process on that host able to call `send.sh`, therefore controls these strings end to end.

The dashboard renders them as text children, which React escapes; there is no `dangerouslySetInnerHTML`
anywhere in `frontend/src`. **`pr_url` is the one field that would otherwise reach a
DOM sink with meaning of its own**, as the `href` of the only `<a>` element in the component tree. It
is passed through `safeExternalURL` in `frontend/src/components/BoardDashboardPanel.tsx` first, which
renders an anchor only when the value parses as an `http:` or `https:` URL and falls back to plain
text for everything else. This is not defense in depth over a framework guarantee — it is the only
guard: React 18, the version this app pins, merely logs *"A future version of React will block
javascript: URLs"* and renders the attribute anyway (React 19 blocks it; this codebase is not on it).
`target="_blank"` and `rel="noopener noreferrer"` do not help either, as they constrain the opened
document rather than whether a script-scheme URL executes at all. Script running in the dashboard's
own origin would have access to the board bearer token, so this is a real escalation path, not a
cosmetic one.

`frontend/src/components/BoardDashboardPanel.test.tsx` pins the guard against `javascript:`, `data:`,
and `vbscript:` values, alongside the positive `http:`/`https:` cases. Any future UI that renders a
new agent-reported field into an attribute rather than as a text child needs the same treatment.

### Command center subprocess execution

`internal/commandcenter/runner.go`'s `Runner` is the one other place in this repository, besides
`internal/session`, that calls `exec.Command`/`exec.CommandContext` on a value not fully known at
compile time. Per this document's own [General Rules](#general-rules): the command name
(`r.claudeBin`) is a hardcoded literal (`"claude"`) unless an operator explicitly overrides it via
`RunnerConfig.ClaudeBin` — there is no code path that derives it from request data, environment
variables, or anything else CodeQL would treat as tainted. The arguments after it are a mix of fixed
literal flags (`-p`, `--output-format=stream-json`, `--verbose`), a `--resume <session-id>` pulled
from `SessionState` (itself only ever written by `Runner` from a value `claude` itself reported in an
earlier run — never client-supplied), a `--mcp-config <path>` pointing at a temp file `Runner` itself
created, an `--allowedTools=<list>` value `commandcenter.AllowedTools()` computes from fixed string
literals, and finally the user's free-text prompt as the last argument. None of this goes through a
shell — `exec.CommandContext` passes each argument as a discrete argv element — so there is no
shell-injection risk from the prompt regardless of its content.

**Being the final positional argument does not, by itself, make the prompt safe — this document's
own first draft of this section claimed exactly that, and the claim was wrong.** `buildArgs` in
`internal/commandcenter/runner.go` was verified live against a real installed `claude` CLI (v2.1.226)
to have two distinct, independently-reproduced bugs before the fix now in place:

1. **Argument injection into the `claude` CLI's own parser.** A prompt beginning with `-` (e.g. a
   user typing `--help`, or something far more consequential like a `--settings=<json>` payload
   defining a malicious hook) was not treated as opaque prompt text — the CLI's own option parser
   scans for flags anywhere in argv, not only before the first positional, so a `-`-prefixed prompt
   was parsed as a flag. This was reproduced directly: passing `"--help"` as the trailing argument
   printed the CLI's own help output instead of being sent as a prompt, and a `--settings` payload
   defining a `SessionStart` hook reached and ran that hook. **The fix**: `buildArgs` now inserts a
   literal `"--"` end-of-options marker immediately before the prompt, which the CLI's parser was
   confirmed (live) to honor — everything after it is treated as a positional argument, never a flag,
   regardless of its content.
2. **`--allowedTools` is declared variadic** (`<tools...>`) by the CLI's own `--help` output, so
   passing it and its value as two separate argv elements (`"--allowedTools", "a,b,c"`) let it
   swallow the very next argv element too — which was the prompt, silently breaking every ordinary
   query with `Input must be provided either through stdin or as a prompt argument when using
   --print`, confirmed live. **The fix**: `buildArgs` now uses the `=` form
   (`"--allowedTools=a,b,c"`) as a single argv element, which cannot be extended by a following one.

Both fixes were verified together, live, end-to-end through the real `Runner`/WS/browser stack (not
just the CLI in isolation) before being considered closed. `internal/commandcenter/runner_test.go`'s
`TestRunnerBuildArgsShapeIsSafeAgainstArgumentInjection` pins the exact argv shape as a regression,
since a fake `cmdRunner` cannot reproduce the real CLI's own argument-parsing behavior — it can only
assert what argv panemux itself constructs, which is why this was caught by an adversarial review
verifying claims against the real binary, not by the original test suite.

**The subprocess's execution context is pinned by panemux, not inherited from the environment.** Three
of `claude`'s defaults resolve from ambient state, and all three were wrong for a subprocess panemux
spawns on an operator's behalf. Each finding below was reproduced against the real CLI (v2.1.233),
not inferred from `--help`:

- **Conversation identity.** A plain `claude -p` with no `--resume` does **not** mint a fresh
  conversation — it reports the *ambient* session id of whatever Claude Code session the environment
  already belongs to. `Runner` previously captured that reported id, persisted it, and `--resume`d it
  on every later query, which attached the command center to a conversation it does not own. This was
  observed live: a palette query returned a reply carrying the operator's own session context,
  referencing a scratch file name that appeared in no prompt panemux ever sent. **The escalation is
  the point:** the command center is deliberately launched with `--allowedTools` scoped to exactly
  three board tools, while the session it joined held that session's full tool permissions, so palette
  text became input to a far more capable agent than the palette's own contract admits.
  `internal/commandcenter/context.go`'s `NewSessionID` now mints a v4 UUID, `buildArgs` pins it with
  `--session-id` on a first run, and the persisted value is always the id panemux minted — the
  subprocess's own reported id is never adopted.
  `TestRunnerFirstRunMintsAndPersistsItsOwnSessionID` pins this by feeding the fake subprocess a
  *different* reported id and asserting it never reaches the session file.
- **Settings and hooks.** With no `--setting-sources`, the subprocess loads the operator's user,
  project and local settings — including their hooks, which would then execute inside a process
  panemux started. `buildArgs` passes `--setting-sources` with an empty value, and
  `--strict-mcp-config` so only the board MCP server this query configured is connected.
- **Working directory.** `claude -p` reads `CLAUDE.md` from its working directory, and `cmd.Dir` was
  unset, so it read whatever project the operator happened to launch panemux from. `NewWorkDir`
  creates an empty per-query temp directory and `realCommandFactory` sets `cmd.Dir` to it.

Note the interaction between the last two: `--setting-sources ''` suppresses `CLAUDE.md` discovery
**as well as** settings files, so "ship our own `CLAUDE.md`" and "inherit none of the operator's
configuration" cannot both be satisfied through files. panemux's own instructions therefore travel via
`--append-system-prompt` (a compile-time literal in `DefaultSystemPrompt`, never operator input, so it
carries no taint into argv). An operator may still refine the command center by placing `CLAUDE.md`
and/or `settings.json` in `~/.config/panemux/command-center/`; the first is appended to the system
prompt, the second passed as `--settings`. Both are optional, both are operator-owned config at the
same trust level as `config.yaml`, and neither is required for the feature to work — panemux is
installed as a standalone binary, so it can never rely on a repository being present on disk.

The `PANEMUX_BOARD_TOKEN`/`PANEMUX_BOARD_BASE_URL` values the `claude -p` subprocess's own MCP-server
child process reads never reach `Runner`'s own `exec.Command` argv at all — they are set in the MCP
config file's `env` block (see "Auth token and transport encryption" below for that file's own
handling), read by `panemux __board-mcp-server` via `os.Getenv` in `board_mcp_server.go`. This is not
a violation of this document's "do not use `os.Getenv` values in flows that reach `exec.Command`"
rule: `runBoardMCPServer` never calls `exec.Command` itself, it only makes outbound HTTP requests
(`internal/boardmcp.HTTPBoardAPIClient`) — an entirely different sink with no shell/argv-reinterpretation
risk to defend against.

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
[agent-board.md](agent-board.md#security-model) for the full rationale.

`internal/server`'s constant-time bearer-token middleware (`bearerAuthMiddleware`, `internal/server/auth.go`)
is implemented, unit-tested, and wired into `registerRoutes` — but **only** onto the
`r.Route("/api/board", ...)` sub-route (`GET /status`, `GET /messages`, `POST /broadcast`, `GET
/command/history`), not onto any pre-existing `/api/*` route or `/ws/{sessionID}`. Widening it to
those routes without a matching frontend change would break every existing, currently-unauthenticated
request, so that remains a separate, larger change. `internal/server/board_routes_test.go` covers this
scoping as a regression: missing/incorrect token on `/api/board/*` is rejected with `401`, the correct
token reaches `200`, and pre-existing `/api/*` routes plus `/ws/{sessionID}` stay reachable with no
`Authorization` header at all. See [agent-board.md](agent-board.md)'s status note.

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
`0600`, created via `os.CreateTemp` then explicitly `os.Chmod`'d) embedding the token as a
`PANEMUX_BOARD_TOKEN` environment variable value for the `claude -p` subprocess's own MCP server
child process (`panemux __board-mcp-server`) to read; the caller's `cleanup` func removes it once that
subprocess has exited. This is a strictly smaller exposure than the existing `~/.config/panemux/token`
file the token already lives in permanently — the temp file exists only for one query's subprocess
lifetime and is never written to `~/.config/panemux/config.yaml` — but it is still a plaintext
token-bearing file on disk, hence the explicit `0600` rather than relying on `os.CreateTemp`'s default
mode.

## General Rules

- When adding new session types or new `exec.Command` calls, the command value passed as the first argument must come from a hardcoded literal or from a trusted system source such as a file or registry with no data-flow path to user input.
- Arguments after the command may be user-supplied only when the target binary cannot reinterpret them as commands.
- Do not use `os.Getenv` values in flows that reach `exec.Command` unless the security model for that path is explicitly reworked and documented.

## `gosec` Policy

- Fix `gosec` findings structurally in the implementation rather than suppressing them in shipped code.
- Test-only code may use narrow `//nolint:gosec` annotations when the fixture behavior intentionally violates a production hardening rule.
- Production paths should avoid suppressions and make the safety argument explicit in code structure instead.

## Exception: OpenSSH Hashed `known_hosts` Compatibility

`internal/session/ssh.go` intentionally keeps a narrow `//nolint:gosec` on `crypto/sha1` (`G505`) for matching OpenSSH hashed `known_hosts` entries (`|1|<salt>|<hash>`). This is not application-defined hashing and not a credential-storage decision. It is protocol and file-format compatibility with existing OpenSSH user data.

OpenSSH defines these hashed host patterns in terms of HMAC-SHA1. To decide which host-key algorithms to advertise, panemux scans the user's `known_hosts` file and must recognize both plaintext host entries and hashed `|1|...` entries. Replacing SHA-1 with another hash would make hashed entries stop matching and would reintroduce SSH connection failures for users whose `known_hosts` files were generated by standard OpenSSH tooling such as `ssh-keygen -H`.

This exception is limited to matching OpenSSH `known_hosts` host patterns. It does not weaken host-key verification itself: verification still goes through `knownhosts.New(...)`, and the SHA-1 path is used only to recognize existing hashed entries so the client can narrow `HostKeyAlgorithms` to the key types already recorded for that host.

## Related Documents

- Implementation structure: [architecture.md](architecture.md)
- Runtime behavior and SSH configuration rules: [behavior.md](behavior.md)
- Developer workflow rules: [../DEVELOPMENT.md](../DEVELOPMENT.md)
