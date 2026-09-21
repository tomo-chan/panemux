# Agent Board: security model

> Part of the [Agent Board design](../agent-board.md). Read that document's status note first — it says which parts of this design are shipped.

## Security model

Full implementation rules live in [security.md](../security.md); this section states the requirements
that shaped the design.

- **panemux does not terminate TLS.** `server.host` defaults to `127.0.0.1`. Exposing panemux
  beyond loopback is the operator's responsibility, using infrastructure built for it (an
  ALB/nginx/Caddy TLS-terminating reverse proxy, an SSH tunnel, Tailscale/WireGuard, etc.). The
  panemux↔proxy hop is then treated as trusted network, the same way panemux already treats its
  own host as trusted.
- **Auth token without transport encryption is close to meaningless** — a token sniffed on an
  unencrypted hop can be replayed, and worse, a request can be tampered with in transit (this
  matters more than usual here because `POST /api/board/broadcast` and the terminal WebSocket both
  ultimately drive shell-executing agents). Config validation must therefore fail closed: if
  `server.host` resolves to a non-loopback address and `server.auth_token` is empty, startup must
  be rejected (`internal/config/validate.go`, alongside the existing `server.port` range check).
- **No argument, on any `RemoteAgmsgClient` call — reads included — ever reaches the remote shell
  unescaped.** Neither `api.sh` nor `send.sh` has a stdin-based way to receive the values panemux
  passes them, so the remote command string is the only place they can go, and it must be built
  with every argument single-quote-escaped, the same discipline already applied to `cwd` in
  `internal/session/ssh.go` (`validRemotePath` / `shellQuotePath`). Reads are **not** exempt: an
  earlier revision of this document claimed `api.sh`'s arguments were digit-validated and therefore
  safe to leave unescaped, which was both factually wrong (`--agent` isn't validated at all — see
  [Integration with agmsg](agmsg-integration.md#integration-with-agmsg)) and structurally wrong even where validation
  does exist, because agmsg's own argument checks run *inside the remote shell process that only
  exists because panemux's command string has already been parsed* — they cannot protect the
  construction of that string. `send.sh` does its own SQL escaping internally, so shell-escaping is
  the only layer panemux is responsible for on the write path — there is no panemux-owned SQL text
  to also escape, unlike an earlier draft of this design that had panemux building its own SQL.
- **Implementation status: attempted resolution, not a verified one.** `shellQuotePath`-style
  escaping alone does not satisfy this repository's own CodeQL bar for a message body — `docs/security.md`
  is explicit that a quoting or regex-submatch transform does not, by itself, break CodeQL's
  taint-tracking; the accepted pattern (`cwd`) is a **regex allowlist** (`validRemotePath`) applied
  *before* `shellQuotePath`. A message body is arbitrary agent-authored text; it cannot be
  regex-allowlisted the way a path can. `internal/board/remote_client.go`'s `RemoteAgmsgClient.Send`
  base64-encodes the body, regex-checks the *encoded* string against `^[A-Za-z0-9+/]*={0,2}$`, and
  only then places it in the `RunBoardCommand` argument list; a fixed wrapper script
  (`sendBase64WrapperScript`) decodes it back to the original bytes on the remote host via shell
  positional parameters, immediately before `send.sh` sees it — proven correct end to end against a
  real `/bin/sh` (`internal/board/wrapper_script_integration_test.go`).
  **What this construction does not establish**, and what a reviewer should not assume from the code
  alone: Go's `base64.StdEncoding` output is, by construction, always a subset of the checked
  alphabet, so the `MatchString` branch that gates it can never actually fail for correctly-encoded
  input — it is a regex-allowlist branch in *shape* (mirroring `validRemotePath`'s structure), but no
  CodeQL scan has actually been run against this code in the environment that implemented it to
  confirm it is recognized as one in *practice*. Treat the taint chain as *plausibly* broken by
  structural analogy to `validRemotePath`, not as confirmed broken by an actual scan, until a real
  CodeQL run against this code says otherwise.
- **panemux itself is never deployed to a remote host.** Beyond the injection-surface argument
  above, the `panemux` binary is also the server: a copy running on an SSH-reached host could start
  its own HTTP/WS listener, auth surface, and command center — a second, unmanaged instance of
  everything this section already works to contain for the primary one, multiplied by every remote
  host in the config. Board features depend only on an agmsg installation the operator placed
  there, never on anything panemux ships.
- **agmsg is an operator-installed, unpinned-by-panemux external dependency.** panemux only detects
  and calls it; it never bundles, vendors, or auto-installs it (see [Integration with
  agmsg](agmsg-integration.md#integration-with-agmsg) and [Design principles](../agent-board.md#design-principles)). MIT license permits
  depending on it, but panemux's implementation still owes itself a pinned tested version/tag and
  treats a break in `scripts/api.sh`'s or `scripts/send.sh`'s behavior as an external dependency
  compatibility bug.
- **panemux sees plaintext at each relay hop.** SSH encrypts each hop (panemux↔host A,
  panemux↔host B), but panemux itself decrypts and re-serializes the row in between, so the
  panemux process/host must be trusted for the relay to be meaningful. There is no end-to-end
  encryption between two remote agents' Claude/Codex processes.
- **Universal `--force` makes `from` forgeable across the whole team, including impersonating
  `_system` itself, unless the relay actively checks it.** `send.sh --force` accepts any `from`
  without checking it against a roster, and by design *every* board send uses `--force` (see
  [Integration with agmsg](agmsg-integration.md#integration-with-agmsg)) — so nothing at the agmsg layer stops an agent
  on Host A from sending a message with `from: "_system"` to a pane on Host B, which
  [Cross-host relay](relay.md#cross-host-relay) would otherwise happily forward as if the command center had
  sent it. Unconditionally trusting any row with `from == "_system"` would not close this — that
  string carries no more authority than any other free-text `from` value once `--force` is
  universal, so treating it as automatically legitimate is exactly the gap being described, not a
  fix for it. The actual check the relay performs — matching the row against panemux's own
  short-lived **own-send ledger** of `Send` calls it issued itself (see [Cross-host
  relay](relay.md#cross-host-relay) and [Package layout](architecture.md#package-layout)) — is what closes this: a
  `_system`-attributed row is only accepted if it corresponds to a send panemux's own broadcast
  handler or command center actually made, never on the strength of the string alone. The ledger
  entry is recorded immediately before the `Send` call (needed so a fast poll racing a *successful*
  `Send` still matches it) and is explicitly forgotten again if that `Send` then fails — otherwise a
  failed send would leave a live, matchable entry for the rest of its TTL with nothing ever actually
  delivered behind it, which any pane on the destination host could exploit within that window.
- **The command center's `/ws/board-command` and `/api/board/command/history` are gated by the same
  bearer token as everything else, and that gate is the entire authorization model for messaging
  any pane from the command center** — see [Command center](command-center.md#command-center). There is deliberately
  no separate, weaker permission tier for it; anyone who can authenticate to panemux at all can
  already reach the full-shell terminal WebSocket, so a second, narrower gate here would not reduce
  real risk, only add a second thing to keep in sync.
