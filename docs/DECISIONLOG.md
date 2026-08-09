# Decision Log

This file records *why* a design or implementation choice was made, and what earlier approach it
replaced — the history that `docs/*.md` files deliberately do not carry inline. See
[DEVELOPMENT.md](../DEVELOPMENT.md)'s "Documentation updates" rule: the product docs describe only
the current state; this file is where the reasoning and the road not taken live.

Newest entries first within each topic. Entries are dated and reference the PR they landed in where
known.

## Agent Board

### Reserved sentinel identity renamed from `_panemux` to `_agent-board` (2026-08-09, PR #163)

The sentinel agmsg identity used for status reports and panemux-originated messages was originally
`_panemux`, coupling a wire-level protocol constant to the application's current product name. If
panemux is ever renamed, that identity would have had to change too, for a reason the protocol
itself has no stake in. Renamed to `_agent-board` throughout `internal/board`
(`Sentinel`), `internal/config` (`ReservedSentinelPaneID`), and all docs/tests/fixtures.

### Message-body escaping: three attempts before one satisfied CodeQL (2026-08-08, PR #163)

`RunBoardCommand` needs to get an arbitrary, agent-authored message body onto a remote host without
letting it be interpreted as shell syntax. `cwd`'s accepted pattern (`validRemotePath` regex
allowlist, then `shellQuotePath`) doesn't transfer directly, because a message body can't be
regex-allowlisted the way a path can.

1. **Base64-encode each argument, embed the encoded literal inline in the command string**
   (regex-checked against the base64 alphabet, then single-quoted — structurally mirroring
   `validRemotePath`-then-`shellQuotePath`). CodeQL's `go/command-injection` query still reported 2
   critical alerts, one per `RunBoardCommand` call site.
2. **Make the allowlist check return an error on mismatch** instead of silently substituting a
   default value, matching `validateRemotePath`'s early-return shape exactly. Same 2 alerts,
   unchanged. Both attempts missed the actual problem: on the success path — the only one that
   matters, since the encoding can't realistically fail — the checked, encoded value was still
   concatenated into the command string either way. A regex check does not remove a value from a
   taint-tracking dataflow graph just because it passed, if the value still reaches the sink
   afterward.
3. **Move every argument off the command string entirely**, delivered over the SSH session's
   stdin (one base64 line per argument, decoded via `IFS= read -r` into a shell variable on the
   remote side) — but `RunBoardCommand` still took one `args []string` parameter with the script
   path at `args[0]`. This cleared the alert on the method that no longer built a command string at
   all, but the shared helper still building one from `scriptPath` (`args[0]`, already validated)
   stayed flagged. This repository's CodeQL setup does not appear to track slice-index provenance:
   once any read from a slice is tainted because some caller puts untrusted content into it, every
   read from that slice — including one already validated — is treated as tainted too.

**Resolution:** split the script path into its own function parameter
(`RunBoardCommand(ctx, scriptPath string, args []string)`), never read from the same slice as the
untrusted arguments. This is what the current implementation does — see
[security.md](security.md#agent-board-remote-writes) and `buildBoardCommand`/
`runBoardCommandOverSSH` in `internal/session/ssh.go`.

**General lesson for this repository's CodeQL bar:** a regex-allowlist-then-quote pattern is only a
sanitizer when the checked value itself never reaches the sink through string concatenation —
moving a value to a different channel (like stdin) is a stronger guarantee than checking-then-
quoting it in place — *and*, separately, a function that takes both a trusted validated value and an
untrusted collection needs them as genuinely distinct parameters, never different indices of one
combined slice, even after the untrusted collection's content has already been moved off the
dangerous path.

### `api.sh`/`send.sh` behavior corrections from source verification (2026-08, PR #162 review)

An early draft of this design made several claims about agmsg's scripts that turned out to be wrong
once the actual source (not assumption) was read:

- **`api.sh`'s arguments are not all digit-validated.** Only `--limit` and `--before-id` are; the
  free-text `--agent` name is protected only by agmsg's own internal SQL escaping, not any
  argument-shape check. This meant `RemoteAgmsgClient` could not skip shell-escaping on the read
  path on the theory that agmsg had already validated it — reads needed the same escaping discipline
  as writes.
- **`--before-id` is a backwards pagination cursor (`id < X`), not a forward/since read.** An early
  draft had the relay's poll loop using `--before-id` to fetch new rows, which cannot work — that
  flag selects *older* rows. The relay instead polls `--limit N` with no `--before-id` and filters
  client-side to rows newer than its persisted cursor, accepting a bounded truncation risk if more
  than `N` rows land between polls.
- **`send.sh` roster-checks both `to` and `from`, not just `from`.** An early draft assumed only
  `from` was checked, which would have made every status self-report and cross-host relay send fail
  at the source, since neither the reserved sentinel nor a pane on a different host is ever in the
  sending pane's local roster. Fixed by having every board-related `send.sh` call — including a live
  agent's own — always pass `--force`.

### `--agent`/read-path escaping correction (2026-08, PR #162 review)

A revision of this design claimed `api.sh`'s read arguments were fully digit-validated and therefore
safe to leave unescaped by `RunBoardCommand`. Wrong on two counts: factually (`--agent` isn't
validated at all), and structurally (agmsg's own validation runs *inside* the remote shell process
that only exists because panemux's command string has already been parsed — it cannot retroactively
protect the string-construction step). Every argument on every `RemoteAgmsgClient` call, reads
included, is escaped the same way.

### Own-send ledger added to close a `_agent-board` impersonation gap (2026-08, PR #162 review)

An earlier draft trusted any relay-observed row with `from == "_agent-board"` (then `_panemux`)
unconditionally. Because `send.sh --force` never checks `from` against a roster, and every board
send uses `--force` by design, that string carries no more authority than any other free-text
`from` value — any agent on any host could forge it. The own-send ledger (`internal/board`'s
`ownSendLedger`) closes this: a `_agent-board`-attributed row is accepted only if it matches a `Send`
panemux's own broadcast handler or command center actually issued recently, never on the string
alone.

### `history` should not include status rows (2026-08, PR #163 implementation)

A revision of `docs/agent-board.md`'s Package layout section said "every row, status or not, is also
appended to `history`," directly contradicting the same document's own Testing plan requirement that
a status row update `status` and *not* appear in message history. The Testing plan's statement was
correct: a status update is cache-only, never appended to `history`, never cross-host relayed.

### Command center reads/writes through panemux's own REST API, not agmsg directly (2026-08, PR #162 review)

An earlier draft had the command center shell out to `send.sh`/`api.sh` directly. Wrong on two
counts: it made the LLM itself responsible for composing safely-escaped shell invocations — a
second, unaudited path to the same exec sink alongside `AgmsgClient`'s own — and it meant the
command center could only ever see the *local* agmsg installation's status, never a remote pane's,
since the cross-host relay intercepts sentinel-addressed status reports before they leave the host
they were written on. The command center now reads/writes exclusively through panemux's own
authenticated REST API (`GET /api/board/status`, `GET /api/board/messages`,
`POST /api/board/broadcast` — the same endpoints the browser dashboard uses) via a narrow,
purpose-built MCP server, so `AgmsgClient` stays the only code that ever calls agmsg's scripts.

### agmsg-only messaging, no panemux-owned protocol (2026-08, PR #162 design)

An early draft specified a panemux-owned SQLite schema and CLI ("`native`") for Claude-only panes,
with agmsg reserved for panes needing to talk to non-Claude agents. Maintaining a second protocol
next to an already-working one wasn't worth it: agmsg already covers everything `native` attempted,
plus interoperability with Codex/Gemini/etc. that `native` could never offer. Agent Board is built
entirely on agmsg; panemux owns no message schema of its own.

### Claude Code's native cross-session messaging evaluated and rejected (2026-08-08)

Claude Code added native cross-session messaging (`ListAgents`/`SendMessage`, v2.1.224+). Evaluated
as a possible replacement or complement to the agmsg-based design and rejected, for the same reasons
the `native` SQLite draft above was rejected, plus specifics of how the feature is exposed:

- **Claude-only** — no notion of Codex, Gemini CLI, or any other agent type.
- **Doesn't fit panemux's arbitrary-SSH-host model** — same-machine delivery needs shared on-disk
  registration files; cross-machine delivery exists only through a Remote Control connection and
  only as a reply, never an originating send.
- **No documented interface for panemux itself to use** — the only sanctioned path is Claude calling
  the tools itself; there's no REST/CLI entry point a Go process can call, and writing to the
  session's inbox socket directly would be exactly the undocumented-internal-format
  reverse-engineering this design's "ask the agent, don't reverse-engineer its internal state"
  principle already rejects elsewhere.

Doesn't rule out a Claude session choosing to use `SendMessage` for its own, non-board purposes, or a
narrower future integration if a real need shows up — but nothing in Phase 1 is built toward that.
