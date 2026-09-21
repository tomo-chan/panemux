# Agent Board: Cross-Pane Claude Messaging and Status Aggregation

> **Status: Phase 1 (Board core) and Phase 2 (Command center) are both implemented.** The
> `internal/board` package's core types (`Row`, `Status`, `AgmsgClient`, `BoardCache`,
> `ownSendLedger`, `LocalAgmsgClient`, `RemoteAgmsgClient`), the
> `BoardHostID`/`BoardExecutor`/`AgentTypeDetector` session capability interfaces, the
> `agent_board`/`command_center`/`server.auth_token` config surface, the relay goroutine
> (`internal/board/relay.go`, cursor persistence in `internal/board/cursor_store.go`), and the three
> Phase 1 REST endpoints in [API additions](agent-board/api-and-config.md#api-additions) — `GET /api/board/status`, `GET
> /api/board/messages`, `POST /api/board/broadcast` — are implemented and tested.
> `bearerAuthMiddleware` is wired onto the `/api/board/*` sub-route, not onto any pre-existing
> `/api/*` route or `/ws/{sessionID}` — see [Security model](agent-board/security-model.md#security-model) and
> [security.md](security/auth.md#auth-token-and-transport-encryption) for why those stay unauthenticated
> (tracked as a separate follow-up issue, orthogonal to both phases). `setupBoard` in `board.go`
> (`package main`) builds the `AgmsgClient`s and pane→host map from config at startup and wires both
> the relay goroutine and the bootstrap watcher into `main.go`'s lifecycle. A remote host's
> `RemoteAgmsgClient` is handed a `dynamicBoardExecutor` (`board.go`), which re-resolves a live
> `BoardExecutor` session on that host on every call rather than holding the one pane's session found
> at startup — an ordinary pane restart/delete of whichever pane was picked first must not
> permanently break board traffic for the rest of that host. The bootstrap watcher
> (`bootstrapWatcher` in `bootstrap.go`, `package main`) polls every board-enabled pane for a newly
> started, agmsg-detectable agent process (`session.AgentTypeDetector`, covering the six agmsg agent
> types agmsg's own `type.conf` marks as process-detectable — see [`internal/session` capability
> interfaces](#internal-session-capability-interfaces)) and writes a one-time onboarding instruction
> into that pane's PTY; see [Bootstrap flow](agent-board/bootstrap.md#bootstrap-flow) for the full algorithm.
>
> **Phase 2 (Command center)** — `internal/commandcenter` (the `Runner` subprocess-per-query
> lifecycle, `SessionState`/`HistoryEntry` persistence, `BuildMCPConfig`) and `internal/boardmcp` (the
> narrow JSON-RPC-over-stdio MCP server exposing exactly `board_status`/`board_messages`/
> `board_broadcast`, backed by an `HTTPBoardAPIClient` that calls panemux's own REST API) implement
> the design in [Command center](agent-board/command-center.md#command-center) below as written, with one addition the original
> design didn't anticipate: **`GET /api/session-token`** (deliberately unauthenticated, see
> [API additions](agent-board/api-and-config.md#api-additions) and [docs/behavior.md](behavior/board-api.md#get-apisession-token)) so the
> browser dashboard itself can learn the bearer token needed to authenticate `/api/board/*` and
> `/ws/board-command` — nothing in the original design specified how the frontend, as opposed to the
> command center subprocess, would ever learn a token that may have been randomly generated on first
> run. `setupCommandCenter` in `command_center.go` (`package main`) builds the `Runner` when
> `command_center.enabled` is true and a `server.auth_token` is configured, wiring it into
> `server.New` alongside the board cache/relay; `/ws/board-command` is registered only when a `Runner`
> is present, so a disabled command center leaves that route entirely absent (a plain `404`) rather
> than reachable-but-erroring. `panemux __board-mcp-server` (`board_mcp_server.go`, `package main`) is
> the hidden subcommand the command center's own `claude -p` subprocess re-invokes as its MCP server,
> per [Process lifecycle](agent-board/command-center.md#process-lifecycle) below. The Spotlight palette (`CommandPalette.tsx`) and
> persistent history panel (`CommandHistoryPanel.tsx`) are implemented per
> [ui-design.md's Agent Board UI section](ui-design.md#agent-board-ui), including the
> concrete decisions that section originally deferred to implementation time.
>
> **An adversarial review after the initial implementation found and fixed several real bugs,
> including two verified live against the real `claude` CLI: the subprocess argument construction let
> a `-`-prefixed prompt be parsed as a CLI flag, and separately made every ordinary (non-flag) prompt
> fail outright due to a variadic-flag argv bug — meaning the command center had never actually
> completed a successful query before the fix.** See
> [security.md's Command center subprocess execution](security/command-center.md#command-center-subprocess-execution)
> for the full detail and how each was verified, and
> [security.md's Auth token and transport encryption](security/auth.md#auth-token-and-transport-encryption)
> for a related fix to `GET /api/session-token`'s own guard (it does not rely on CORS, contrary to an
> earlier revision of that section). A live, end-to-end query through the real browser → WS → Runner →
> real `claude` subprocess stack was used to confirm the fix, not just the unit tests.
>
> **Phase 3 (Dashboard UI and command palette test completion)** is implemented. The read-only status
> dashboard (`BoardDashboardPanel.tsx`, its own `useBoardStatus` polling hook, and the
> `utils/boardStatusColors.ts` state→color/staleness helpers) ships as a right-anchored overlay panel
> reachable via an "Agent Board" button or `Cmd/Ctrl+Shift+B` — see
> [ui-design.md's Agent Board UI section](ui-design.md#agent-board-ui) for the full presentation
> detail. It required one addition the Phase 3 design didn't originally anticipate:
> **`agent_board_enabled`** on `GET /api/session-token`'s response (see
> [API additions](agent-board/api-and-config.md#api-additions) and [docs/behavior.md](behavior/board-api.md#get-apisession-token)), because
> `command_center_enabled` alone can't gate the dashboard button — a config can enable `agent_board`
> without the command center, or vice versa, and the frontend has no other cheap way to learn whether
> any pane has `agent_board.enabled: true` without re-fetching and walking the full layout tree.
> Broadcast-sending from the dashboard was deliberately left out of Phase 3's scope — the dashboard is
> read-only, matching the original Phase 3 issue definition. Phase 3 also completed test coverage the
> command palette and history panel had been shipped without: `schemas/index.test.ts` now exercises
> every board-related Zod schema's accept/reject paths, `CommandPalette.test.tsx` covers fixture
> history rendering, incremental streaming updates, and the busy frame, and a shared
> `useRestoreFocusOnClose` hook (used by the palette, history panel, and dashboard alike) returns
> keyboard focus to whatever triggered an overlay once that overlay closes — behavior the original
> Phase 3b issue text called for that had no implementation at all before this phase.
>
> **Tier 2 of the [agmsg compatibility contract](agent-board/agmsg-contract.md#agmsg-compatibility-contract) is now implemented
> too** — `.github/workflows/agmsg-contract.yml` runs `make test-agmsg-contract` against a real
> agmsg install, daily against agmsg's latest release tag (doing real work once per new release) and
> on pull requests against the pinned `board.TestedAgmsgVersion`. Its first real run against a live
> install found two things every hermetic test had missed: two of the contract's own assertions
> described `watch.sh` output that does not exist in the branch they asserted it in, and — the substantive one — the relay compared
> agmsg's message ids **numerically**, which no id from agmsg's event-log storage driver satisfies,
> so the poll cursor never advanced and every row was re-delivered on every tick. Both are fixed;
> see [Integration with agmsg](agent-board/agmsg-integration.md#integration-with-agmsg) for the cursor rule that replaced the
> comparison, and the contract section for what the tiers now cover.

## Document map

This document is the entry point: the status note above, the purpose below, and the design
principles everything else follows. The detail lives in [`docs/agent-board/`](agent-board/), split by
the section names this document used to carry — a source comment citing "`docs/agent-board.md`'s
Cross-host relay section" resolves through this table.

| Sections | Document |
|---|---|
| Alternatives considered | [alternatives.md](agent-board/alternatives.md) |
| Architecture; Package layout; `internal/session` capability interfaces; Local vs remote resource placement | [architecture.md](agent-board/architecture.md) |
| Integration with agmsg; Version pinning; `~` in `agmsg_path`; Two panes in one project directory | [agmsg-integration.md](agent-board/agmsg-integration.md) |
| Status self-report and message flow | [message-flow.md](agent-board/message-flow.md) |
| Cross-host relay | [relay.md](agent-board/relay.md) |
| Bootstrap flow | [bootstrap.md](agent-board/bootstrap.md) |
| Command center; Process lifecycle; Concurrency; Permissions; Authorization; API and streaming; UI; Scope | [command-center.md](agent-board/command-center.md) |
| API additions; Config additions | [api-and-config.md](agent-board/api-and-config.md) |
| Security model | [security-model.md](agent-board/security-model.md) |
| Known limitations | [limitations.md](agent-board/limitations.md) |
| agmsg compatibility contract | [agmsg-contract.md](agent-board/agmsg-contract.md) |
| State-machine model checking | [model-checking.md](agent-board/model-checking.md) |
| Testing plan | [testing-plan.md](agent-board/testing-plan.md) |

## Purpose

panemux already runs real `git`/`gh` commands to show branch and PR info in a pane header — that
part is not inferred (see `docs/behavior.md`'s pane-header Git/PR section). What *is* inferred is
**which directory to run those commands in**: for an interactive Claude/Codex pane, panemux parses
`~/.claude/sessions/<pid>.json` and the matching transcript JSONL to decide whether the agent has
moved into a sibling worktree, using an undocumented, internal precedence across several transcript
fields (`session_meta.cwd`, `turn_context.cwd`, `Bash` `cd` targets, subagent transcript files —
see [architecture.md](architecture.md)) that has already needed at least one bug fix as Claude Code
itself evolved. When that resolution is wrong, the `git`/`gh` calls still run — just in the wrong
directory, or nowhere confidently resolvable at all — and the header shows a stale, wrong, or
missing branch/PR. In practice this happens often enough that a panemux user ends up asking Claude
directly for its own PR URL instead of trusting the header. The instability is real; it just lives
one layer deeper than "the git/PR data is inferred" — it's "the directory the real git/PR data
comes from is inferred," which is no more stable a thing to reverse-engineer.

This mechanism is also read-only and pane-local: it cannot tell whether a session is idle or
mid-turn, and it gives panemux no way to send an agent a message or to let two agents in different
panes talk to each other.

Agent Board replaces that inference with a small, self-reported channel:

1. Panes report their own state — working directory, branch, repository, PR link, and whether
   they're idle, working, or waiting for approval — by running their own commands (`git`, `gh`)
   and telling panemux the result, instead of panemux guessing from an internal file format. This
   is the same information a human looking at the pane would see, obtained the same way a human
   would get it, so it is exactly as stable as the agent's own tool use — not as fragile as
   reverse-engineering a private transcript schema. See
   [Design principles](#design-principles) for why this generalizes beyond just this one field.
2. Lets a human broadcast a message to one or more panes' Claude sessions without racing raw
   keystrokes into a live PTY, and lets a local **command center** (see
   [Command center](agent-board/command-center.md#command-center)) do the same conversationally on the human's behalf.
3. Aggregates all of the above into one dashboard and, for the command center, one continuous,
   reviewable conversation history.
4. Is built entirely on [agmsg](https://github.com/fujibee/agmsg), an existing MIT-licensed
   bash+sqlite3 agent-messaging tool. As of this writing agmsg's own README lists Claude Code,
   Codex, Gemini CLI, GitHub Copilot, Antigravity, OpenCode, and Hermes as supported agent types;
   treat that as a snapshot of agmsg's documentation, not a list panemux enforces or keeps in sync
   itself — implementation should re-check agmsg's current README rather than trust this document's
   copy of it, since agmsg adding or renaming a supported agent type has no effect on panemux's own
   code (panemux never branches on agent type; it only ever calls `api.sh`/`send.sh`/`join.sh`
   generically). panemux does not maintain a second, parallel messaging protocol of its own — see
   [Design principles](#design-principles) for why, and [Integration with agmsg](agent-board/agmsg-integration.md#integration-with-agmsg) for how.

## Design principles

- **Ask the agent; don't reverse-engineer its internal state.** This is the central lesson behind
  this whole redesign, and it applies uniformly to every piece of external state Agent Board
  touches, not only pane status:
  - panemux does not parse Claude/Codex transcript internals to guess branch/PR/cwd — it asks the
    agent to run `git`/`gh` itself and report the result (see
    [Status self-report](agent-board/message-flow.md#status-self-report-and-message-flow)).
  - panemux does not read agmsg's `messages.db` or `teams/*/config.json` directly — those are
    agmsg's own internal storage, explicitly documented in agmsg's README as "internal and free to
    change." panemux only calls agmsg's own stable, documented entry points (`scripts/api.sh`,
    `scripts/send.sh`) — see [Integration with agmsg](agent-board/agmsg-integration.md#integration-with-agmsg).
  - Any future integration this document doesn't yet cover should default to the same rule: prefer
    a tool's own documented command/output contract over parsing whatever file it happens to keep
    its state in today.
- **One messaging mechanism, not two.** An earlier draft of this design also specified a
  panemux-owned SQLite schema and CLI ("`native`") for Claude-only panes, with agmsg reserved for
  panes that needed to talk to non-Claude agents. Building and maintaining a second protocol next
  to an already-working one turned out not to be worth it: agmsg already covers everything
  `native` tried to do, plus interoperability with Codex/Gemini/etc. that `native` could never
  offer, and every pane on `agmsg` from the start means no relay step is needed even for two panes
  that happen to both be Claude. Agent Board is therefore built entirely on agmsg; panemux owns no
  message schema of its own. The same reasoning was later applied to Claude Code's own native
  cross-session messaging feature — see [Alternatives
  considered](agent-board/alternatives.md#claude-codes-native-cross-session-messaging-2026-08-08).
- **No new daemon, no new listening port — scoped to what panemux itself starts.** panemux talks to
  agmsg the same way any of its scripts or a live agent session would: local `exec.Command` calls on
  the host panemux itself runs on, and the existing SSH exec channel (`GetCWD`/`InspectGitContext`
  already use it) for every other host. panemux itself starts no new process that listens for
  anything, locally or remotely. This claim is specifically about processes *panemux* starts —
  agmsg's own `SessionStart` hook already launches its own `Monitor`/`watch.sh` process per joined
  pane independently of panemux (see [Integration with agmsg](agent-board/agmsg-integration.md#integration-with-agmsg)), exactly as
  it would if an operator had set up agmsg by hand with no panemux involved at all. That process is
  agmsg's, lives and dies by agmsg's own hook lifecycle, and is not a daemon this design introduces
  or is responsible for.
- **panemux itself is never installed on a remote host, under any circumstances.** The `panemux`
  binary is also a server: running it can start the HTTP/WS listener, the auth surface, and the
  command center. Placing a copy on every SSH-reached host that wants board features would mean
  each of those hosts could accidentally end up running a second, unmanaged panemux server. Board
  features on a remote host depend only on an agmsg installation the operator put there themselves
  (see the next principle) — never on anything panemux ships.
- **Backend presence is detected, never installed, by panemux.** If agmsg is not found on a pane's
  host, panemux skips board bootstrap for that pane, logs a clear warning, and leaves the pane's
  shell session otherwise untouched — board is additive, never load-bearing for the pane to
  function. panemux never runs `npx agmsg`, `npm i -g agmsg`, `git clone`, or any other installer
  on the operator's behalf, on any host. See [Integration with agmsg](agent-board/agmsg-integration.md#integration-with-agmsg).
- **panemux is a trusted relay, not an end-to-end encrypted channel.** See
  [Cross-host relay](agent-board/relay.md#cross-host-relay) and [Security model](agent-board/security-model.md#security-model).

## Related documents

- Implementation structure: [architecture.md](architecture.md)
- Security requirements for implementation: [security.md](security.md)
- Runtime behavior and API specification: [behavior.md](behavior.md)
- UI intent for the dashboard, palette, and history panel: [ui-design.md](ui-design.md#agent-board-ui)
- Developer workflow rules: [../DEVELOPMENT.md](../DEVELOPMENT.md)
