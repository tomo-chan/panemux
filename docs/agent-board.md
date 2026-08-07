# Agent Board: Cross-Pane Claude Messaging and Status Aggregation

> **Status: design, not yet implemented.** This document specifies the target design for the
> `internal/board` package and its supporting API, config, and security surface. Update this
> status note (and cross-link it from [architecture.md](architecture.md), [security.md](security.md),
> and [behavior.md](behavior.md) as appropriate) once a phase below actually ships; until then, no
> other doc should describe `board` endpoints or config fields as current behavior.

## Purpose

panemux currently infers what an interactive Claude process in a pane is doing by parsing
`~/.claude/sessions/<pid>.json` and the matching transcript JSONL (see the Claude worktree
resolution notes in [architecture.md](architecture.md) and [behavior.md](behavior.md)). That
approach is read-only, heuristic, and pane-local: it cannot tell whether a session is idle or
mid-turn, and it gives panemux no way to send an agent a message or to let two agents in different
panes talk to each other.

Agent Board replaces that inference with a small, self-reported, structured channel that:

1. Lets panemux collect accurate per-pane Claude status (idle / working / waiting for approval,
   last tool used, current summary) instead of guessing from transcripts.
2. Lets a human (or one designated "supervisor" pane) broadcast a message to one or more panes'
   Claude sessions without racing raw keystrokes into a live PTY.
3. Aggregates all of the above into one dashboard and, for a supervisor pane, one queryable feed.

## Design principles

- **No new daemon, no new listening port.** The mechanism is a local SQLite file per host, opened
  directly by both panemux and the Claude process running in that host's panes, plus reuse of the
  SSH exec channel panemux already holds open for `GetCWD`/`InspectGitContext`
  (`internal/session/ssh.go`).
- **Claude drives its own participation with its own tools.** panemux does not push data into
  Claude's context out-of-band. It types a one-time bootstrap instruction into the pane (the same
  mechanism already used for terminal input) telling Claude to use the `Monitor` tool to watch its
  own inbox and to run a small CLI to report status and send messages. Everything panemux observes
  is something Claude itself chose to write, using its own `Bash`/`Monitor` tool calls.
- **Hooks are avoidable, not required — because panemux has something agmsg doesn't.**
  [agmsg](https://github.com/fujibee/agmsg) is a plain bash+sqlite3 skill with no host-side
  controller, so it needs a `SessionStart` hook just to auto-launch `Monitor` when a Claude Code
  session starts, and a `Stop` hook for its "turn mode" fallback (`/agmsg mode monitor|turn|both`) —
  hooks are how agmsg gets *anything* to happen automatically, for both modes, not only the
  fallback one. panemux does not have that constraint: it already has write access to every pane's
  PTY (the same path used for all terminal input), so it can type the `Monitor`-launching bootstrap
  instruction itself, once, the moment it detects a `claude` process starting in a board-enabled
  pane — reproducing agmsg's "automatic, no user action" property without ever touching the user's
  `~/.claude/settings.json`. A user who additionally wants a `Stop`-hook "turn mode" fallback for
  extra delivery reliability (mirroring agmsg's `turn`/`both` modes) can still add one manually; the
  bootstrap message prints the hook snippet to add, but panemux never installs it itself.
- **Delivery mode is configurable per pane, mirroring agmsg's `/agmsg mode`.** `join` accepts
  `--mode monitor|turn|both` (default `monitor`). `turn` and `both` require the user-added `Stop`
  hook above; panemux cannot upgrade a pane into those modes on its own.
- **panemux is a trusted relay, not an end-to-end encrypted channel.** See
  [Cross-host relay](#cross-host-relay) and [Security model](#security-model).

## Schema

One table per host, based directly on agmsg's proven schema with two added columns for
cross-host relay dedup:

```sql
CREATE TABLE messages (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  team TEXT NOT NULL DEFAULT 'panemux',
  from_agent TEXT NOT NULL,
  to_agent TEXT NOT NULL,
  kind TEXT NOT NULL DEFAULT 'message',   -- 'message' | 'status'
  body TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
  read_at TEXT,
  origin_host TEXT,     -- NULL if this row originated on this host; source host id if relayed in
  origin_id INTEGER     -- the row's id on origin_host, for relay dedup
);
CREATE UNIQUE INDEX idx_relay_dedup ON messages(origin_host, origin_id) WHERE origin_host IS NOT NULL;
CREATE INDEX idx_unread ON messages(team, to_agent, read_at) WHERE read_at IS NULL;
CREATE INDEX idx_history ON messages(team, created_at DESC);
```

`from_agent`/`to_agent` are pane IDs (already globally unique across workspaces per
[architecture.md](architecture.md)). `kind='status'` rows carry the latest self-reported activity
summary; `kind='message'` rows carry chat/instruction content.

## Package layout

### `internal/board`

```go
type Row struct {
    ID, OriginID int64
    Team, FromAgent, ToAgent, Kind, Body string
    CreatedAt time.Time
    ReadAt    *time.Time
    OriginHost string
}

type Store interface {
    HostID() string
    Insert(ctx context.Context, r Row) (int64, error)
    InsertRelayed(ctx context.Context, r Row) error // requires OriginHost/OriginID; INSERT OR IGNORE
    Since(ctx context.Context, afterID int64) ([]Row, error)
    LatestStatusByAgent(ctx context.Context, team string) (map[string]Row, error)
    MarkRead(ctx context.Context, ids []int64) error
}
```

- `LocalStore` opens `~/.config/panemux/board.db` directly with a pure-Go SQLite driver (no CGO,
  consistent with the single-binary distribution story in [overview.md](overview.md)), in WAL
  mode. One file is shared by every local and local-tmux pane on the host, and by panemux itself.
- `RemoteStore` never opens a remote file directly (SQLite's WAL guarantees do not extend across a
  network filesystem boundary). It calls the fixed remote command `panemux board recv` over the
  existing SSH exec channel and writes the row as JSON to that command's **stdin**. It never
  interpolates message bodies into a shell command string. See
  [Security model](#security-model) and [security.md](security.md).

### `internal/session` capability interfaces

Following the existing optional-capability pattern (`CWDGetter`, `ActiveWorkdirGetter`,
`GitContextGetter`, `SSHConnNamer`):

```go
// BoardHostID is implemented by every session type. It returns the identifier of the host whose
// board this session's pane belongs to: "local" for local/tmux sessions, the SSH connection name
// for ssh/ssh_tmux sessions.
type BoardHostID interface {
    BoardHostID() string
}

// BoardExecutor is implemented by SSH-backed sessions. It runs `panemux board recv` on the remote
// host over the session's existing exec channel, writing stdin and returning stdout, without ever
// building a shell command string from body content.
type BoardExecutor interface {
    RunBoardCommand(ctx context.Context, args []string, stdin []byte) ([]byte, error)
}
```

`LocalSession`/`TmuxLocalSession` implement only `BoardHostID` (`"local"`). `SSHSession`/
`SSHTmuxSession` implement both.

## Cross-host relay

Two Claude processes on two different SSH-reached hosts cannot share a board file directly (no
shared filesystem, and the two hosts may not even be able to reach each other — see the write-up in
this document's revision history / PR discussion for the TURN-server analogy). panemux is the only
node with a connection to every host, so it relays:

1. A single goroutine polls every known host's `Store.Since(cursor)` every 3 seconds.
2. `cursor` is one value per source host, persisted in a `relay_cursors(source_host TEXT PRIMARY
   KEY, last_id INTEGER)` table in the local board so a panemux restart resumes correctly.
3. For each new row, panemux resolves `to_agent` to its owning host via the already-known
   pane→session config. If the destination host differs from the source host, panemux calls
   `InsertRelayed` on the destination `Store` with `OriginHost`/`OriginID` set to the source row, so
   a re-relay after a crash/restart is a harmless no-op against `idx_relay_dedup`.
4. Same-host `to_agent` needs no relay: sender and receiver already share one file.
5. panemux's own dashboard/status reads (`GET /api/board/status`) go directly to every host's
   `Store` and are not relayed; relay only matters for agent-to-agent delivery.

This makes panemux's relay role structurally similar to a TURN server (always in the data path for
the life of the exchange, because a direct path between the two remote hosts is not assumed to
exist) rather than a STUN server (which only helps two peers find each other and then steps aside).
Unlike a general-purpose TURN server, panemux is the only possible relay for a given pair of hosts
(it is the sole node holding SSH credentials to both), so there is no negotiation step — delivery is
always routed through it by construction.

## CLI subcommands

Same binary, `panemux board <subcommand>`:

| Subcommand | Run where | Purpose |
|---|---|---|
| `panemux board join <pane-id> [--role worker\|supervisor] [--mode monitor\|turn\|both]` | inside the pane, by Claude | Prints usage (including the `Stop` hook snippet for `turn`/`both`) and writes an initial `status` row. `--mode` defaults to `monitor` |
| `panemux board inbox <pane-id> --watch` | inside the pane, by Claude via `Monitor` (`monitor`/`both` modes) or a `Stop` hook script (`turn`/`both` modes) | Polls for unread rows addressed to `<pane-id>`, prints one line per message, marks each read after printing. Under `Monitor` this polls every ~1s; under a `Stop` hook it runs once, between turns |
| `panemux board status <pane-id> "<summary>"` | inside the pane, by Claude | Inserts a `kind='status'` row |
| `panemux board send <to-pane-id> "<body>"` | inside a `role: supervisor` pane, by Claude | Inserts a `kind='message'` row addressed to another pane |
| `panemux board recv` | remote host only, invoked by panemux over the exec channel | Reads one JSON row from stdin, inserts it with parameterized SQL. The only board subcommand panemux itself ever executes remotely |

## Bootstrap flow

1. A pane config (or the global default) sets `agent_board.enabled: true` (optionally with a
   non-default `mode`).
2. panemux's existing interactive-agent process detection (already used for the Claude worktree
   override in [architecture.md](architecture.md)) notices a `claude` process start in that pane.
3. panemux writes a one-time instruction into the pane's PTY (the same `Session.Write` path already
   used for all terminal input) telling Claude to run `panemux board join <pane-id> [--mode ...]`
   and to start `panemux board inbox <pane-id> --watch` under the `Monitor` tool. This single PTY
   write is what lets panemux skip the `SessionStart` hook agmsg needs for the same auto-launch
   effect — see [Design principles](#design-principles).
4. panemux installs one skill file, e.g. `~/.claude/commands/panemux-board.md`, once, explicitly
   (not a silent `settings.json` hooks edit) so the bootstrap instruction can also be given as a
   short slash command. Installing this skill is itself gated on `agent_board.enabled` and is
   idempotent.
5. If the pane was joined with `--mode turn` or `--mode both`, `join` prints the exact `Stop` hook
   entry to add to `~/.claude/settings.json`; panemux never writes that file itself, so this step
   requires the user (or Claude, with the user's confirmation, since editing `settings.json` is a
   file write like any other) to apply it manually.

## Roles: worker and supervisor

- `role: worker` (default): only reads its own inbox and reports its own status.
- `role: supervisor` (explicit opt-in only, never default): may read every agent's latest status
  (`panemux board status --all`) and send to any pane (`panemux board send <any-pane-id> ...`).
  A supervisor pane is typically a local pane the user starts specifically for this purpose.

**Trust implication, stated explicitly:** giving a pane `role: supervisor` means its Claude process
can inject instructions into every other board-enabled pane, local or remote, including ones
executing arbitrary shell commands. This is the same class of risk called out for the `SendMessage`
tool in Claude Code itself: a receiving pane must not treat a board message as pre-authorized, only
as an ordinary instruction subject to its own normal confirmation flow. This must be stated in the
bootstrap instruction text itself, not just in this document.

## API additions

All of the following require the global bearer-token auth described in
[Security model](#security-model) — board auth is not scoped narrower than the rest of the API,
because an unauthenticated board endpoint would be a smaller problem than the pre-existing
unauthenticated `/ws/{sessionID}` full-shell endpoint, and once auth exists it should cover both.

| Endpoint | Purpose |
|---|---|
| `GET /api/board/status` | Latest `kind='status'` row per pane, across every host panemux manages |
| `GET /api/board/messages?since=<id>` | History feed for the dashboard UI |
| `POST /api/board/broadcast` | `{ "to": ["pane-a","pane-b"], "body": "..." }`; inserts directly into each target's own host `Store` (never via PTY injection, so it is safe to send to a pane mid-turn) |

## Config additions

```yaml
server:
  host: "127.0.0.1"
  port: 8080
  auth_token: ""   # empty = auto-generate on first run, saved to ~/.config/panemux/token (0600)

panes:
  - id: pane-a
    type: local
    agent_board:
      enabled: true
      role: worker   # or supervisor; supervisor must always be explicit, never a default
      mode: monitor  # monitor (default) | turn | both; turn/both need a manually-added Stop hook
```

A global `agent_board.enabled` default may also be supported so individual panes don't need to
repeat it.

## Security model

Full implementation rules live in [security.md](security.md); this section states the requirements
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
- **Message bodies never reach a remote shell as an interpolated argument.** `RunBoardCommand`
  always sends the body over the exec channel's stdin to the fixed argv `panemux board recv`, never
  via a constructed shell string. This is the same rule already applied to `cwd` in
  `internal/session/ssh.go` (`validRemotePath` / `shellQuotePath`), extended to board content.
- **Local board files are `0600`.** `~/.config/panemux/board.db` and the remote equivalent are
  created with owner-only permissions. This does not create a new trust boundary — any other
  process running as the same OS user is already inside panemux's existing trust boundary per
  [overview.md](overview.md) — but it does prevent a different local *user* on a shared host from
  reading cross-pane agent chatter.
- **panemux sees plaintext at each relay hop.** SSH encrypts each hop (panemux↔host A,
  panemux↔host B), but panemux itself decrypts and re-serializes the row in between, so the
  panemux process/host must be trusted for the relay to be meaningful. There is no end-to-end
  encryption between two remote agents' Claude processes.

## Known limitations

- No claim/lease semantics: if two workers were both addressed by the same message (not a supported
  case today, since `to_agent` targets one pane), there is no exclusion mechanism. This mirrors
  agmsg's own documented v1 limitation.
- Agents/teams are free-text identifiers with no cryptographic authentication of `from_agent`. Any
  local process that can open the board file can forge a sender. This is an integrity gap distinct
  from the transport-confidentiality concerns above and is accepted for the same reason panemux
  already accepts same-user process trust elsewhere.
- Relay latency is bounded by the 3s poll interval, not real-time; cross-host messaging is not
  suitable for anything requiring sub-second delivery.

## Testing plan (see DEVELOPMENT.md for the TDD/coverage rules this must follow)

- `internal/board`: insert/read roundtrip, unread filtering and `MarkRead`, relay dedup (same
  `origin_host`+`origin_id` inserted twice is a no-op), cursor persistence across a simulated
  restart, `LatestStatusByAgent` with multiple status rows (only the newest per agent wins), empty
  board.
- `internal/session`: `RunBoardCommand` never places body content in `exec.Command` args (mirrors
  the existing `validateShell` test pattern).
- `internal/config`: `host != loopback && auth_token == ""` is a validation error; all other
  combinations are valid.
- `internal/api`: missing/incorrect bearer token is rejected (401) on both REST and the WebSocket
  handshake; correct token succeeds.

## Related documents

- Implementation structure: [architecture.md](architecture.md)
- Security requirements for implementation: [security.md](security.md)
- Runtime behavior and API specification: [behavior.md](behavior.md)
- Developer workflow rules: [../DEVELOPMENT.md](../DEVELOPMENT.md)
