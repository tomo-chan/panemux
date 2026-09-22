# Agent Board: known limitations

> Part of the current [Agent Board design](../agent-board.md).

## Known limitations

- **A remote host with no reachable session at panemux startup gets no `AgmsgClient` for the rest of
  that process's lifetime, even if a board-enabled pane on it becomes reachable later.**
  `newAgmsgClientForHost` (`board.go`) runs exactly once per host, at `setupBoard` time; a host it
  skips (no live session to resolve `agmsg_path`'s `~/` against, or the probe itself failing) is
  simply absent from `Relay`'s `Clients` map from then on — nothing retries client construction on a
  schedule the way `dynamicBoardExecutor` re-resolves a *session* on every call once a client already
  exists for that host. This is a real, distinct gap from the session-level fix `dynamicBoardExecutor`
  provides: that fix keeps a host's board features working across a pane restart/delete *after* the
  host has a client; it does nothing for a host that had no reachable pane at all when panemux itself
  started. In practice this mostly matters for a host whose only board-enabled pane's SSH connection
  hadn't finished dialing yet when `setupBoard` ran, or one that was genuinely unreachable at boot but
  recovers later — in both cases, that host's board features stay silently unavailable (logged once,
  at startup, as a warning) until the whole panemux process restarts. A future fix would need
  `newAgmsgClientForHost` to be retried on a schedule, similar in shape to how `dynamicBoardExecutor`
  already re-resolves sessions, rather than being a one-shot startup step.
- **Account-wide token/cost totals are explicitly out of scope for Agent Board.** Unlike
  branch/PR/cwd, token usage isn't external world-state an agent can query with an ordinary command
  (`git`, `gh`), and there's no verified way for an agent to introspect its own cumulative usage and
  include it in a self-report. Since panemux-managed panes and the command center all run under the
  same Claude account in the common case, Claude Code's own `/usage` already gives an accurate
  account-wide view; Agent Board does not need to duplicate it.
- **Per-pane usage — "which session is using disproportionately more than the others" — is a
  different, genuinely useful question `/usage` doesn't answer, and is not ruled out by the above.**
  Every assistant turn's `usage` field (input/output/cache tokens) is a foundational, stable part
  of the Messages API response envelope that Claude Code's transcript already records for each
  turn — reading and summing it is a much shallower, lower-risk read than the branch/PR/worktree
  heuristics this redesign moved away from (those depended on deep, undocumented precedence rules
  across `session_meta.cwd`, `turn_context.cwd`, and subagent transcript files, which is what
  actually proved unstable in practice). **This looks like it contradicts [Design
  principles](../agent-board.md#design-principles)'s "ask the agent, don't reverse-engineer its internal state" rule
  — it isn't, and the distinction matters.** That rule targets *inference*: guessing a fact (which
  directory the agent is really in) from a schema panemux does not control and that has already
  drifted once. Reading `usage` is not inference — it is summing a documented, versioned field of
  the Messages API response envelope itself, the same envelope Claude Code's own transcript already
  stores verbatim per turn. There is no guessing step, no precedence chain across several
  loosely-related fields, and no plausible "wrong directory" analogue: a turn's `usage` object either
  is present with the numbers the API returned, or it isn't. This is not Agent Board's own
  responsibility to build, though: it fits naturally as an extension of panemux's existing, pre-board
  per-pane transcript inspection (already used for worktree/PR detection — see
  [architecture.md](../architecture.md) and [behavior.md](../behavior.md)), summing a stable field rather
  than parsing fragile ones, not as something carried through the agmsg self-report this document
  specifies. A future change to that existing mechanism, not to `internal/board`, is the right place
  for it.
- The status/history cache is in-memory only, not persisted to disk. A panemux restart starts it
  empty; the relay's [cold-start backfill](relay.md#cross-host-relay) pass shortens the gap before
  `GET /api/board/status`/`/messages` show current data again, but it is still a bounded `--limit`
  read, not a full replay — a host that produced more rows than the backfill limit since the cursor
  was last persisted still shows a gap. This is the same accepted eventual-consistency tradeoff
  already made for the relay cursor itself.
- No claim/lease semantics: if two workers were both addressed by the same message (not a supported
  case today, since `to` targets one pane), there is no exclusion mechanism. This mirrors agmsg's
  own documented v1 limitation. Distinct from that: agmsg's `actas-claim.sh` lock only prevents two
  sessions from claiming the *same role name* at once (exit code 1 if already held) — it says
  nothing about, and does not provide, message claim/lease semantics for delivery.
- Agents/teams are free-text identifiers with no cryptographic authentication of `from`. The relay's
  own `from` check (see [Cross-host relay](relay.md#cross-host-relay)) rejects a forged `from` that doesn't
  match a known local pane ID or `_system`, but within that set there is still no proof a given
  message actually came from the pane process it claims to — any local process that can reach a
  host's agmsg installation and pick a real, currently-registered pane ID can forge a sender inside
  that host. This is an integrity gap distinct from the transport-confidentiality concerns in
  [Security model](security-model.md#security-model) and
  is accepted for the same reason panemux already accepts same-user process trust elsewhere.
- **A broadcast's delivery and its appearance in dashboard history are decoupled.**
  `POST /api/board/broadcast` calls `AgmsgClient.Send` directly, so the destination pane receives it
  immediately through agmsg's own hook delivery — but `BoardCache.AppendMessage` is only ever called
  from the relay's own poll loop, not from the broadcast handler itself, so the dashboard's history
  view of that same message lags by up to one poll interval, same as any other row. This is a
  deliberate choice to keep `BoardCache` populated from exactly one code path (the relay) rather than
  give the broadcast handler a second, racing write path into the same cache that would need its own
  reconciliation against the row the relay later reads back for the same send. The [own-send
  ledger](architecture.md#package-layout) used for `_system` forgery detection intentionally is not repurposed to
  paper over this lag: it exists for that one security check, not as a second history source.
- Relay delivery is at-least-once, bounded by the poll interval, not real-time or exactly-once; see
  [Cross-host relay](relay.md#cross-host-relay) for why a duplicate message after a panemux restart is an
  accepted outcome rather than something engineered away. Separately, and for a different reason
  (`api.sh` has no forward/since read at all — see [Integration with
  agmsg](agmsg-integration.md#integration-with-agmsg)), delivery is also **not guaranteed-complete**: if more new rows
  land on one host between two poll cycles than the poll's `--limit`, the oldest of that overflow
  are silently missed rather than delivered late. This is accepted as a bound to keep the design
  simple, not engineered around with unbounded polling or pagination.
- **Board features' write path depends on an operator-installed third-party tool (agmsg) whose own
  maintainer makes no compatibility promise for that path.** agmsg has real engineering discipline
  behind it (a bats-core test suite, CI, multiple contributors) — this is not a concern about code
  quality — but its README's stability promise explicitly covers reading through `scripts/api.sh`
  only; `send.sh`'s argument order and behavior, which this design's entire write path depends on,
  carries no such promise. If that script's behavior changes in a future agmsg release, panemux's
  `AgmsgClient` can fail even though nothing in panemux's own config changed. This is a real,
  accepted cost of not vendoring/pinning a copy of agmsg inside panemux itself, and is more exposure
  than a read-only dependency on `api.sh` alone would have carried — worth weighing against the
  cross-agent interoperability agmsg provides that a panemux-owned protocol never could. See [agmsg
  compatibility contract](agmsg-contract.md#agmsg-compatibility-contract) for how this exposure is meant to be
  caught mechanically rather than discovered by a user.
- Bootstrap is not free of side effects outside panemux's own state: `delivery.sh set turn|both` (see
  [Bootstrap flow](bootstrap.md#bootstrap-flow)) writes hook wiring into the pane's project config (agmsg's own
  per-type convention — e.g. `.claude/settings.local.json` for `claude-code`), which persists in that
  Git repository after the pane closes and is never reverted by panemux, including when a pane later
  disables `agent_board.enabled`.
- **Bootstrap tracks "already onboarded" at session-object granularity, not process granularity.**
  panemux has no way to observe a coding-agent *process* restarting inside a pane whose underlying
  `session.Session` never changed — there is no PID exposed anywhere in `internal/session`'s public
  surface for bootstrap to key off of instead (see [`internal/session` capability
  interfaces](architecture.md#internalsession-capability-interfaces)). Concretely: once a pane has
  been bootstrapped, if the agent process inside it exits and a new one starts in the same
  still-open pane, that new process is not re-detected and does not get a fresh onboarding
  instruction — only closing and reopening the pane (which creates a new `session.Session`) resets
  bootstrap eligibility for it. This mirrors, and is accepted for the same reason as, the equivalent
  limitation already documented above for the relay's `AgmsgClient` construction.
- **A remote host with no reachable session when `setupBoard` runs never becomes bootstrap-eligible
  for the rest of that process's lifetime, even if a board-enabled pane on it becomes reachable
  later** — the same one-shot-at-startup limitation already documented above for
  `newAgmsgClientForHost`'s `Clients` map applies identically to `resolveBootstrapPaths`'
  `ResolvedPaths`, and for the same underlying reason: neither is retried on a schedule.
- **Bootstrap's `resolveBootstrapPaths` and the relay's `newAgmsgClientForHost` each independently
  probe every remote host's `agmsg_path` once at startup, rather than sharing one resolution.** This
  is a deliberate choice, not an oversight: coupling bootstrap eligibility to relay client
  construction (or vice versa) would let a failure in one subsystem silently disable the other, which
  is exactly the kind of cross-subsystem coupling `dynamicBoardExecutor` was introduced to avoid at
  the session level. The accepted cost is one extra `sh -c` round-trip per remote host at startup.
- **The 2-tick bootstrap debounce is a partial mitigation, not a race-free design.** It narrows, but
  does not eliminate, the window in which an operator's own keystrokes into a pane could interleave
  with panemux's synthesized onboarding instruction if both happen at almost exactly the moment an
  agent process starts in that pane. See [Bootstrap flow](bootstrap.md#bootstrap-flow)'s consent-model paragraph
  for why this tradeoff is accepted rather than solved with typing-detection or input-pausing
  machinery.
- **The onboarding instruction's line-feed-vs-carriage-return input encoding has not been verified
  against a live instance of all six detectable agent types.** `buildBootstrapInstruction` writes the
  instruction as multiple `\n`-separated lines with a single trailing `\r` (matching this codebase's
  existing convention that only `\r` submits input to a pane, per `frontend/src/hooks/useTerminal.ts`'s
  own `term.onData` handling), on the assumption that each agent's own interactive input handling
  treats an embedded `\n` as a literal newline rather than as its own submit/Enter event. This has not
  been confirmed against a real running instance of any of the six types. If that assumption is wrong
  for a given type, the instruction would arrive fragmented into several separate prompts instead of
  one coherent message, rather than failing loudly — an operator would see a confused agent, not an
  error. Stated here as an explicit, unverified risk rather than fixed with an unverified
  countermeasure (e.g. bracketed-paste wrapping), since neither this document's authors nor its tests
  have a way to confirm such a countermeasure actually helps without live testing against all six
  types.
- **`agmsgDetectableAgentTypes`'s `excludeTokens` matching (`internal/session/local.go`) can produce
  a false negative for a positional prompt argument that happens to contain an exclude token as its
  own whitespace-separated word** — e.g. `codex "fix the exec path"` tokenizes to include a
  standalone `exec` token the same way `codex exec ...` (the real headless-mode invocation this
  exclusion exists to detect) does, so the interactive invocation would incorrectly be treated as
  headless and never bootstrapped. This is a pre-existing shape inherited from the pattern
  `isInteractiveAgentCommand` (the separate, untouched worktree-detection feature; see
  [architecture.md](../architecture.md)) already uses for its own claude/codex exclusions, not something
  bootstrap introduced — but bootstrap makes it newly load-bearing for five additional agent types.
  Fixing this correctly needs argv-boundary-aware tokenization (distinguishing "a word inside one
  quoted argument" from "a separate argument"), which the current process-listing capture does not
  provide; that is a larger, riskier change than this document's other bootstrap tradeoffs and is
  left as a known gap rather than attempted here.
- Self-reported status depends on the agent's cooperation each time — see the honest tradeoff
  called out in [Status self-report](message-flow.md#status-self-report-and-message-flow). A pane that stops
  following its bootstrap instruction (e.g. a very long uninterrupted tool-use turn) simply stops
  updating its status until its next report, with no separate liveness signal from panemux itself.
- Exactly one command center session exists per panemux instance (see
  [Command center](command-center.md#command-center)); it is not per-workspace and does not support multiple
  concurrent orchestrators today.
- The command center spawns `claude -p` as a subprocess per query; response latency includes
  process startup plus generation time, which is higher than a warm, already-running interactive
  session would give — acceptable for a "converse with an orchestrator" UX, not for anything
  latency-sensitive.
