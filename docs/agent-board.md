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
2. Lets a human broadcast a message to one or more panes' Claude sessions without racing raw
   keystrokes into a live PTY, and lets a local **command center** (see
   [Command center](#command-center)) do the same conversationally on the human's behalf.
3. Aggregates all of the above into one dashboard and, for the command center, one continuous,
   reviewable conversation history.
4. Optionally interoperates with [agmsg](https://github.com/fujibee/agmsg), an existing MIT-licensed
   bash+sqlite3 agent-messaging tool that already supports Claude Code, Codex, Gemini CLI, GitHub
   Copilot, Antigravity, OpenCode, and Hermes. Where a pane's host already runs agmsg, a
   panemux-managed pane can join the same team as a Codex pane and reuse agmsg's own Codex support
   (boot-prompt handoff, turn-mode delivery) instead of panemux reimplementing it. See
   [Backends](#backends).

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
- **Hooks are optional for panemux-managed panes because panemux and
  [agmsg](https://github.com/fujibee/agmsg) sit at different layers of the system, not because one
  supersedes the other.** agmsg is deliberately environment-agnostic: a skill that works the same
  way whether or not anything is hosting the terminal it runs in, which is exactly why it relies on
  hooks (`SessionStart` to auto-launch `Monitor`, `Stop` for its "turn mode" fallback via `/agmsg
  mode monitor|turn|both`) — hooks are how *any* Claude Code session gets something to happen
  automatically, agmsg included, when nothing external is watching. panemux, by contrast, already
  sits between the user and the pane as the terminal host, with write access to every pane's PTY
  (the same path used for all terminal input). That host-level position is what lets it type the
  `Monitor`-launching bootstrap instruction itself, once, the moment it detects a `claude` process
  starting in a board-enabled pane, without touching the user's `~/.claude/settings.json` —
  reproducing the same "automatic, no user action" property agmsg achieves through hooks, just by a
  different mechanism available to a host application that a standalone skill does not have access
  to. A user who additionally wants a `Stop`-hook "turn mode" fallback for extra delivery
  reliability (mirroring agmsg's `turn`/`both` modes) can still add one manually; the bootstrap
  message prints the hook snippet to add, but panemux never installs it itself.
- **Delivery mode is configurable per pane, mirroring agmsg's `/agmsg mode`.** `join` accepts
  `--mode monitor|turn|both` (default `monitor`). `turn` and `both` require the user-added `Stop`
  hook above; panemux cannot upgrade a pane into those modes on its own.
- **Backend selection is explicit config, never auto-detected, and panemux never installs agmsg
  itself.** A pane either uses panemux's own built-in ("native") board, or agmsg, per the pane's
  configured `agent_board.backend`. panemux does not probe a host for agmsg and silently switch
  behavior based on what it finds — that would make a pane's messaging behavior depend on
  unrelated host state and would be surprising to debug. If `backend: agmsg` is set for a pane and
  agmsg is not present on that pane's host, board bootstrap for that pane is skipped with a clear,
  visible warning; panemux never runs an installer on the operator's behalf, local or remote. See
  [Backends](#backends).
- **panemux is a trusted relay, not an end-to-end encrypted channel.** See
  [Cross-host relay](#cross-host-relay) and [Security model](#security-model).

## Schema (native backend only)

This schema belongs solely to panemux's own built-in ("native") backend. It is loosely inspired by
agmsg's message-oriented shape (team/from/to/body/read_at), but it is **not** an attempt at
schema-level compatibility with agmsg's actual `messages.db`: agmsg's own README states that
`messages.db` and `teams/*/config.json` are "internal and free to change" and that third parties
should integrate through `scripts/api.sh` instead. panemux follows that guidance — see
[Backends](#backends) for how the agmsg backend actually integrates. One table per host, with two
added columns for cross-host relay dedup that agmsg (a single-host tool) has no equivalent of:

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

## Backends

`agent_board.backend` is `native` (default) or `agmsg`, set explicitly per pane or as a global
default — never inferred by probing the host. Both backends implement the same `Store` interface
(below), so the relay and dashboard code is backend-agnostic; only the bootstrap flow and the CLI
surface a board-enabled pane actually runs differ.

These two backends are not ranked against each other, and this document does not treat either as
the "real" or preferred one. They cover different ground: agmsg is a general, host-agnostic,
multi-agent messaging tool that already works across many CLI agents and many environments, with a
maturity and breadth panemux does not attempt to duplicate. `native` exists for the narrower case
of Claude-only panes on a host where the operator has not installed a separate tool, and it works
only *because* panemux is already the terminal host for those panes, giving it options (PTY
injection, an existing SSH exec channel to relay over) that a portable, environment-agnostic tool
like agmsg deliberately does not assume it has. Choosing one over the other for a given pane is a
question of what that pane needs, not which backend is "better."

### `native`

panemux's own schema and CLI, exactly as described above and in
[CLI subcommands](#cli-subcommands). Self-contained: no third-party tool required on the pane's
host. Claude-to-Claude only — a Codex pane on the same host cannot join a `native` team, because
`native` is a panemux-specific protocol Codex has no knowledge of.

### `agmsg`

Delegates to an agmsg installation already present on the pane's host, using only the interfaces
agmsg documents as stable for third-party use: the CLI/skill entry points (`/agmsg ...` from inside
Claude Code, or the equivalent `$agmsg`/skill invocation for other agents) and `scripts/api.sh` for
JSON-in/JSON-out reads and writes. panemux never reads or writes agmsg's `messages.db` or
`teams/*/config.json` directly, matching agmsg's own README guidance that those are internal.

Choosing `agmsg` for a pane means:

- That pane's bootstrap instruction (see [Bootstrap flow](#bootstrap-flow)) tells Claude to join
  agmsg's team instead of panemux's own, using agmsg's own `join`/`actas` semantics. Any other
  agent already using agmsg on that host (Codex, Gemini CLI, etc.) can then exchange messages with
  it directly through agmsg, with no panemux involvement in that same-host exchange at all.
- agmsg has no native "status" concept, only messages. panemux's status reports map onto ordinary
  agmsg messages addressed to a reserved agent name (e.g. `_panemux_dashboard`) inside the team;
  `LatestStatusByAgent` for this backend reads the history addressed to that reserved name and
  keeps only the newest row per sender. The exact `scripts/api.sh` flags this requires must be
  confirmed against a pinned agmsg version at implementation time — this document does not assert
  a flag surface it has not verified.
- panemux's own dashboard reads and the cross-host relay both go through `scripts/api.sh`, not
  through a shared schema, for the same reason panemux itself avoids the raw file.
- **Detection, not installation.** At bootstrap time panemux checks whether agmsg is available on
  that pane's host (local: presence of `scripts/api.sh` under agmsg's known skill-install location,
  or `command -v agmsg`; remote: the same check run once over the existing SSH exec channel). If
  not found, panemux skips board bootstrap for that pane, logs a clear warning naming the pane, and
  leaves the pane's shell session itself untouched — board is additive, never load-bearing for the
  pane to function. panemux never runs `npx agmsg`, `npm i -g agmsg`, `git clone`, or any other
  installer on the operator's behalf, on any host.
- **Version pinning.** Because agmsg's own compatibility promise only covers `scripts/api.sh`'s
  JSON contract (not internal storage), panemux implementation must pin a specific tested agmsg
  version/tag range and treat a break in that script's behavior as an external dependency
  compatibility bug, tracked the same way any other pinned dependency's breaking change would be.

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

Four concrete implementations, chosen per host by that host's panes' `agent_board.backend`:

- `LocalNativeStore` opens `~/.config/panemux/board.db` directly with a pure-Go SQLite driver (no
  CGO, consistent with the single-binary distribution story in [overview.md](overview.md)), in WAL
  mode. One file is shared by every local and local-tmux pane on the host, and by panemux itself.
- `RemoteNativeStore` never opens a remote file directly (SQLite's WAL guarantees do not extend
  across a network filesystem boundary). It calls the fixed remote command `panemux board recv`
  over the existing SSH exec channel and writes the row as JSON to that command's **stdin**. It
  never interpolates message bodies into a shell command string. See
  [Security model](#security-model) and [security.md](security.md).
- `LocalAgmsgStore` shells out to the local agmsg installation's `scripts/api.sh` (see
  [Backends](#backends)); it never touches `messages.db` directly.
- `RemoteAgmsgStore` runs the remote host's `scripts/api.sh` over the same SSH exec channel as
  `RemoteNativeStore`, with the same stdin-only, no-argv-interpolation discipline for message
  bodies.

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

// BoardExecutor is implemented by SSH-backed sessions. It runs the remote board command for
// whichever backend that pane's host uses (`panemux board recv` for native, agmsg's own
// scripts/api.sh for agmsg) over the session's existing exec channel, writing stdin and returning
// stdout, without ever building a shell command string from body content.
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

1. A single goroutine polls every known `Store.Since(cursor)` every 3 seconds. Note this is one
   cursor per distinct `Store` instance, i.e. per (host, backend) pair — a host running both a
   `native` pane and an `agmsg` pane has two independent stores and two cursors, not one.
2. `cursor` is one value per source store, persisted in a `relay_cursors(source_host TEXT,
   source_backend TEXT, last_id INTEGER, PRIMARY KEY(source_host, source_backend))` table in the
   local native board so a panemux restart resumes correctly.
3. For each new row, panemux resolves `to_agent` to its owning pane, and that pane's `Store` via
   its configured `(host, backend)`, using the already-known pane→session config. If that
   destination `Store` differs from the source `Store`, panemux calls `InsertRelayed` on it with
   `OriginHost`/`OriginID` set to the source row, so a re-relay after a crash/restart is a harmless
   no-op against `idx_relay_dedup`. This is why cross-*backend* messaging works the same way as
   cross-*host* messaging even when both panes happen to be on the same host: a `native` pane and
   an `agmsg` pane on one host are still two different stores, and a message between them still
   goes through this same relay step, not direct sharing.
4. A message needs no relay only when source and destination are the exact same `Store` instance
   (same host, same backend): sender and receiver already share one file.
5. panemux's own dashboard/status reads (`GET /api/board/status`) go directly to every managed
   `Store` and are not relayed; relay only matters for agent-to-agent delivery.

This makes panemux's relay role structurally similar to a TURN server (always in the data path for
the life of the exchange, because a direct path between the two remote hosts is not assumed to
exist) rather than a STUN server (which only helps two peers find each other and then steps aside).
Unlike a general-purpose TURN server, panemux is the only possible relay for a given pair of hosts
(it is the sole node holding SSH credentials to both), so there is no negotiation step — delivery is
always routed through it by construction.

## CLI subcommands

These apply only to panes configured with `agent_board.backend: native`. A pane on `backend: agmsg`
never calls `panemux board *`; it drives agmsg's own `/agmsg`/skill entry points instead — see
[Backends](#backends). Same binary, `panemux board <subcommand>`:

| Subcommand | Run where | Purpose |
|---|---|---|
| `panemux board join <pane-id> [--mode monitor\|turn\|both]` | inside the pane, by Claude | Prints usage (including the `Stop` hook snippet for `turn`/`both`) and writes an initial `status` row. `--mode` defaults to `monitor` |
| `panemux board inbox <pane-id> --watch` | inside the pane, by Claude via `Monitor` (`monitor`/`both` modes) or a `Stop` hook script (`turn`/`both` modes) | Polls for unread rows addressed to `<pane-id>`, prints one line per message, marks each read after printing. Under `Monitor` this polls every ~1s; under a `Stop` hook it runs once, between turns |
| `panemux board status <pane-id> "<summary>"` | inside the pane, by Claude | Inserts a `kind='status'` row for that pane |
| `panemux board status --all` | on panemux's local host, by the command center | Reads `LatestStatusByAgent` across every managed `Store` |
| `panemux board send <to-pane-id> "<body>"` | on panemux's local host, by the command center | Inserts a `kind='message'` row addressed to any pane, local or remote, native or agmsg; the existing relay (not this command) handles cross-`Store` delivery |
| `panemux board recv` | remote host only, invoked by panemux over the exec channel | Reads one JSON row from stdin, inserts it with parameterized SQL. The only board subcommand panemux itself ever executes remotely |

Every board-enabled pane can run `join`/`inbox`/`status` for itself. `status --all` and `send
<any-pane-id>` are not pane-scoped — see [Command center](#command-center) for who actually runs
them and how that is authorized.

## Bootstrap flow

Steps 1–2 are shared; step 3 branches on the pane's configured backend.

1. A pane config (or the global default) sets `agent_board.enabled: true` with a `backend`
   (`native`, the default, or `agmsg`) and optionally a non-default `mode`.
2. panemux's existing interactive-agent process detection (already used for the Claude worktree
   override in [architecture.md](architecture.md)) notices a `claude` process start in that pane.
   For `backend: agmsg`, panemux first runs the detection check described in
   [Backends](#backends); if agmsg is not found on that host, it logs a warning naming the pane and
   stops here — no PTY write happens, and the pane's shell session is otherwise unaffected.
3. panemux writes a one-time instruction into the pane's PTY (the same `Session.Write` path already
   used for all terminal input):
   - **`native`**: tells Claude to run `panemux board join <pane-id> [--mode ...]` and to start
     `panemux board inbox <pane-id> --watch` under the `Monitor` tool.
   - **`agmsg`**: tells Claude to join agmsg's team using agmsg's own onboarding flow (e.g.
     `/agmsg` or the equivalent first-run prompt for that team/agent name) and to rely on agmsg's
     own `Monitor`/hook wiring for delivery, exactly as it would if the user had set this up by
     hand outside of panemux.
   Either way, this single PTY write is panemux's host-level counterpart to the `SessionStart` hook
   agmsg relies on for the same auto-launch effect in environments with no host application present
   — see [Design principles](#design-principles). This step
   only ever establishes *that pane's* participation; it never touches any other pane or any other
   agent already using agmsg on that host (a pre-existing Codex agent, for example, keeps working
   exactly as it did before panemux was involved).
4. `native` only: panemux installs one skill file, e.g. `~/.claude/commands/panemux-board.md`, once,
   explicitly (not a silent `settings.json` hooks edit) so the bootstrap instruction can also be
   given as a short slash command. Installing this skill is itself gated on `agent_board.enabled`
   and is idempotent. `agmsg` panes rely on agmsg's own already-installed skill instead.
5. `native` only: if the pane was joined with `--mode turn` or `--mode both`, `join` prints the
   exact `Stop` hook entry to add to `~/.claude/settings.json`; panemux never writes that file
   itself, so this step requires the user (or Claude, with the user's confirmation, since editing
   `settings.json` is a file write like any other) to apply it manually. `agmsg` panes use agmsg's
   own `/agmsg mode` instead, entirely outside panemux's control.

## Command center

Earlier drafts of this document modeled the orchestrator as a pane with `role: supervisor` running
an ordinary interactive `claude` process. That is no longer the design: the intended experience is
a local, Spotlight-style command palette the user converses with, not a terminal pane sitting in
the layout. This section replaces the old "Roles" section.

### What it is

- A single, persistent **headless** Claude session, not a pane and not a PTY. panemux invokes it as
  a short-lived subprocess per query — `claude -p --resume <command-center-session-id> "<prompt>"`
  — rather than a long-running process, so this does not introduce the "new daemon" this document's
  [Design principles](#design-principles) rule out. `--resume` against one fixed session id is what
  gives the command center conversational continuity across separate queries.
- It reads and writes the board through the exact same `panemux board status --all` / `panemux
  board send <any-pane-id> "<body>"` native CLI already defined in
  [CLI subcommands](#cli-subcommands), invoked as ordinary `Bash` tool calls within its own turn —
  no new board primitive is needed for it.
- `board send` always writes to the *local* native `Store` (the command center is not itself a
  pane bound to a particular host/backend). The existing [cross-host relay](#cross-host-relay)
  — which already routes by the destination pane's `(host, backend)` regardless of where a message
  originated — delivers it onward to native or agmsg panes, local or remote, with no
  command-center-specific routing logic required.
- The reserved agent identity `_panemux` is used both as the `from_agent` when the command center
  sends, and as the `to_agent` workers report status to for dashboard aggregation (unifying the
  identity already introduced for the `agmsg` backend's status mapping in [Backends](#backends)
  rather than adding a second reserved name).

### Authorization

The command center's privilege (it can message *any* board-enabled pane) is not granted by any
board-level pane role — there is no pane role left in this design. It is granted the same way every
other capability in panemux is: `POST`/WS access to the command center's own endpoint requires the
global bearer-token auth described in [Security model](#security-model). This is a cleaner trust
boundary than a pane self-declaring `role: supervisor` ever was, because it is enforced by
panemux's existing authenticated API layer rather than by convention a receiving pane has to trust.

**Trust implication, stated explicitly, still applies:** a message the command center sends is an
ordinary instruction to the receiving pane, not something pre-authorized — the same caveat already
called out for the `SendMessage` tool in Claude Code itself. The receiving pane's own normal
confirmation flow still applies.

### API and streaming

- `POST /ws/board-command` (WebSocket, matching the existing `/ws/{sessionID}` streaming pattern
  rather than a blocking REST call): the frontend sends `{"prompt": "..."}`, panemux runs `claude -p
  --resume <id> --output-format=stream-json "<prompt>"` and streams tokens back as they're
  generated, so the palette can show live output instead of waiting for the full response.
- `GET /api/board/command/history`: returns the command center's own turn-by-turn history, parsed
  from its Claude Code session transcript using the transcript-reading capability panemux already
  has for Claude worktree resolution (see [architecture.md](architecture.md)) — reused as-is, not
  duplicated into the board's `messages` table. Because `board send`/`board status --all` calls
  the command center makes appear as ordinary tool calls in that same transcript, the returned
  history already interleaves "what the user asked," "what the command center did on the board,"
  and "what it told the user" in one chronological feed, with no extra bookkeeping required.

### UI

- A global keyboard shortcut opens a Spotlight-style modal palette (exact binding to be decided at
  implementation time, avoiding conflicts with OS-level shortcuts and existing terminal bindings).
- The palette shows recent history inline on open (via the history endpoint above) and streams the
  live response as it's generated.
- A separate, persistently accessible history panel (following the same UI pattern as the existing
  workspace-summary overlay) exposes the same history outside the quick-palette flow, for scrolling
  back further than what the palette shows inline.

### Scope, kept intentionally narrow for now

Exactly one command center session per panemux instance — not per-workspace, not multiple
concurrent command centers. Nothing in this design forecloses that later, but nothing here should
be built to anticipate it either, per this repository's own guidance against designing for
hypothetical future requirements.

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
| `WS /ws/board-command` | Command center chat: client sends `{"prompt": "..."}`, server streams the headless Claude response — see [Command center](#command-center) |
| `GET /api/board/command/history` | Command center's own transcript-derived conversation history — see [Command center](#command-center) |

## Config additions

```yaml
server:
  host: "127.0.0.1"
  port: 8080
  auth_token: ""   # empty = auto-generate on first run, saved to ~/.config/panemux/token (0600)

command_center:
  enabled: true   # default false; spawns the headless claude -p --resume process on demand

panes:
  - id: pane-a
    type: local
    agent_board:
      enabled: true
      backend: native  # native (default) | agmsg; explicit only, never auto-detected
      mode: monitor    # monitor (default) | turn | both; turn/both need a manually-added Stop hook

  - id: pane-b          # e.g. a Codex pane sharing pane-a's host
    type: ssh
    connection: build-host
    agent_board:
      enabled: true
      backend: agmsg   # required for any non-Claude agent; panemux has no native protocol for Codex
```

A global `agent_board.enabled`/`backend` default may also be supported so individual panes don't
need to repeat it, but `backend: agmsg` still requires agmsg to already be present on that pane's
host — panemux will not install it, per [Backends](#backends).

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
  always sends the body over the exec channel's stdin to a fixed argv (`panemux board recv` for
  `native`, agmsg's own script invocation for `agmsg`), never via a constructed shell string. This
  is the same rule already applied to `cwd` in `internal/session/ssh.go` (`validRemotePath` /
  `shellQuotePath`), extended to board content, and it applies identically to both backends.
- **agmsg is an operator-installed, unpinned-by-panemux external dependency.** panemux only detects
  and calls it; it never bundles, vendors, or auto-installs it (see [Backends](#backends) and
  [Design principles](#design-principles)). MIT license permits depending on it, but panemux's
  implementation still owes itself a pinned tested version/tag and treats a break in
  `scripts/api.sh`'s behavior as an external dependency compatibility bug.
- **Local board files are `0600`.** `~/.config/panemux/board.db` and the remote equivalent are
  created with owner-only permissions. This does not create a new trust boundary — any other
  process running as the same OS user is already inside panemux's existing trust boundary per
  [overview.md](overview.md) — but it does prevent a different local *user* on a shared host from
  reading cross-pane agent chatter.
- **panemux sees plaintext at each relay hop.** SSH encrypts each hop (panemux↔host A,
  panemux↔host B), but panemux itself decrypts and re-serializes the row in between, so the
  panemux process/host must be trusted for the relay to be meaningful. There is no end-to-end
  encryption between two remote agents' Claude processes.
- **The command center's `/ws/board-command` and `/api/board/command/history` are gated by the same
  bearer token as everything else, and that gate is the entire authorization model for
  `board send <any-pane-id>`** — see [Command center](#command-center). There is deliberately no
  separate, weaker permission tier for it; anyone who can authenticate to panemux at all can already
  reach the full-shell terminal WebSocket, so a second, narrower gate here would not reduce real
  risk, only add a second thing to keep in sync.

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
- `backend: agmsg` panes depend on an operator-installed third-party tool panemux does not manage
  the lifecycle of. If that installation is upgraded to a version whose `scripts/api.sh` behavior
  has changed incompatibly, panemux's `LocalAgmsgStore`/`RemoteAgmsgStore` can fail even though
  nothing in panemux's own config changed. This is the accepted cost of not vendoring/pinning a
  copy of agmsg inside panemux itself.
- Exactly one command center session exists per panemux instance (see
  [Command center](#command-center)); it is not per-workspace and does not support multiple
  concurrent orchestrators today.
- The command center spawns `claude -p` as a subprocess per query; response latency includes
  process startup plus generation time, which is higher than a warm, already-running interactive
  session would give — acceptable for a "converse with an orchestrator" UX, not for anything
  latency-sensitive.

## Testing plan (see DEVELOPMENT.md for the TDD/coverage rules this must follow)

- `internal/board`: insert/read roundtrip, unread filtering and `MarkRead`, relay dedup (same
  `origin_host`+`origin_id` inserted twice is a no-op), cursor persistence across a simulated
  restart, `LatestStatusByAgent` with multiple status rows (only the newest per agent wins), empty
  board.
- `internal/session`: `RunBoardCommand` never places body content in `exec.Command` args (mirrors
  the existing `validateShell` test pattern).
- `internal/config`: `host != loopback && auth_token == ""` is a validation error; all other
  combinations are valid. `agent_board.backend` accepts only `native`/`agmsg`; anything else is a
  validation error (no silent fallback to a default).
- `internal/api`: missing/incorrect bearer token is rejected (401) on both REST and the WebSocket
  handshake; correct token succeeds.
- Backend selection and detection: a pane configured with `backend: agmsg` on a host where agmsg is
  absent skips bootstrap and logs a warning without touching the pane's session (asserted against a
  fake/no-op `BoardExecutor`/host check, not a real agmsg install); a pane configured with
  `backend: agmsg` where agmsg is present bootstraps through the agmsg path instead of `panemux
  board *`; mixed-backend same-host relay (a `native` pane and an `agmsg` pane on one host,
  addressed to each other) goes through the relay step rather than being treated as already-shared.
- Command center: `/ws/board-command` rejects an unauthenticated connection the same way the
  terminal WebSocket does; `board send`/`board status --all` issued from the command center's
  subprocess reach a target pane regardless of that pane's `(host, backend)`, using a fake `Store`
  per combination to assert routing without a real agmsg/SSH dependency; `GET
  /api/board/command/history` parses a fixture transcript containing interleaved user turns,
  assistant text, and `board send`/`status --all` tool calls into one ordered feed, and returns an
  empty/well-defined result before the command center has ever been used (no transcript file yet).

## Related documents

- Implementation structure: [architecture.md](architecture.md)
- Security requirements for implementation: [security.md](security.md)
- Runtime behavior and API specification: [behavior.md](behavior.md)
- Developer workflow rules: [../DEVELOPMENT.md](../DEVELOPMENT.md)
