# Agent Board: bootstrap flow

> Part of the current [Agent Board design](../agent-board.md).

## Bootstrap flow

`bootstrap.go` (`package main`) provides `bootstrapWatcher`, constructed in `board.go`'s
`setupBoard` and polled on its own goroutine from `main.go`'s lifecycle (`defaultBootstrapPollInterval`,
5s — the same interval as the relay), gated the same way the relay is: `HasWork()` (at least one
board-enabled pane) must be true before the goroutine even starts.

1. A pane config sets `agent_board.enabled: true`, optionally overriding `mode`. (There is no
   top-level default for `enabled` — `AgentBoardConfig` only carries `team`/`agmsg_path`, shared by
   every board-enabled pane; each pane opts in individually. Per-pane `team` override does not exist
   either — every board-enabled pane on one panemux instance joins the single `agent_board.team`.)
2. Every poll tick, for every board-enabled pane, `bootstrapWatcher.checkPane` calls that pane's
   `session.AgentTypeDetector.DetectInteractiveAgentType()` — provided by all four session types
   (`LocalSession`, `TmuxLocalSession`, `SSHSession`, `TmuxSSHSession`) — which walks the pane's
   process tree for a live descendant matching one of the six agmsg agent types agmsg's own
   `type.conf` driver files mark `detect_proc` (process-name-based auto-detection) for:
   `claude-code`, `codex`, `cursor`, `gemini`, `grok-build`, `opencode`. The other three CLI types
   agmsg supports (`antigravity`, `copilot`, `hermes`) declare `detect=explicit` in their own
   `type.conf` — agmsg's own maintainers judged process detection unreliable for them — and the
   tenth, `agmsg-app`, is a desktop app, not a spawnable CLI at all (`spawnable=no`). Bootstrap
   inherits this exact boundary rather than picking its own: a type agmsg itself doesn't trust
   process detection for is not a type panemux invents its own detection heuristic for either. A
   pane whose live process doesn't match any of the six is simply never bootstrapped — its shell
   session is otherwise completely unaffected.
3. **Debounce.** A pane is only eligible to bootstrap once the *same* `session.Session` object has
   been seen with a known agent type detected on two consecutive poll ticks (`bootstrapWatcher.pending`,
   compared by session identity only — the detected type itself is not compared tick-to-tick, so a
   pane whose agent type genuinely changes between two ticks still debounces on session identity
   alone and simply uses whichever type the second tick reported). This is a partial mitigation, not
   a complete one, against writing into a pane the instant its shell is still settling right after
   the agent process starts — stated as an honest tradeoff, not a claim of full safety; see [Known
   limitations](limitations.md#known-limitations). Pane IDs and the configured team are also checked against
   `board.ValidAgmsgIdentifier` (the same identifier alphabet `RemoteAgmsgClient` already enforces —
   see [Integration with agmsg](agmsg-integration.md#integration-with-agmsg)) before anything else: a pane ID or team
   outside that alphabet is skipped with one warning, since it could never be joined or addressed
   correctly even if the PTY write itself succeeded.
4. Once debounced, `bootstrapWatcher.agmsgPresent` runs the agmsg detection check from [Integration
   with agmsg](agmsg-integration.md#integration-with-agmsg) (`board.LocalAgmsgPresent`/`board.RemoteAgmsgPresent`,
   `internal/board/agmsg_presence.go`) against a startup-time snapshot of each host's resolved
   `agmsg_path` (`resolveBootstrapPaths` in `board.go`, using the same `resolveAgmsgPathForHost`
   helper `newAgmsgClientForHost` uses for the relay's own client map — resolved independently for
   bootstrap, not shared, so that coupling bootstrap eligibility to relay client construction can't
   reintroduce the kind of permanent-failure regression `dynamicBoardExecutor` exists to avoid; see
   [Known limitations](limitations.md#known-limitations) for the accepted cost of that duplication). For a remote
   host, the probe itself goes through a `dynamicBoardExecutor` (the same fail-over-across-candidates
   wrapper the relay's own client construction uses), not a single fixed candidate session — a
   board-enabled pane's session can still be registered (and report a normal `State()`) after its
   underlying SSH connection has actually died, so trusting only one candidate could otherwise treat
   a perfectly reachable host as permanently unreachable for bootstrap purposes. The probe itself is
   bounded by a bootstrap-specific timeout shorter than the relay's own startup-probe timeout, since
   `pollOnce` checks every board-enabled pane sequentially within one poll interval and an
   unreachable host's probe must not be allowed to stall every other pane queued behind it in the
   same tick. If agmsg is not found on that host, or presence can't be determined at all (an
   unreachable host, a transport error), panemux logs one warning naming the pane and retries on
   every subsequent tick — no PTY write happens, and the pane's shell session is otherwise
   unaffected.

**Every bootstrap warning is logged once per failing streak, never once per tick.** The conditions
the watcher reports — a pane ID or team outside agmsg's identifier alphabet, a detection command that
fails, a host with no resolved `agmsg_path`, a presence probe that errors, agmsg simply not installed
— are not transient by nature: a dead tmux server or a dropped SSH connection stays that way, and the
watcher re-checks every pane on every tick, so logging each occurrence would mean one line per tick
for as long as panemux runs. One broken pane would then drown out everything else panemux writes,
which is the real cost — not the volume itself. `bootstrapWatcher.warnOnce` suppresses a warning
while its condition persists and `clearWarning` releases it the moment the condition clears, so a
failure that returns after a recovery is reported again: "once ever" would swallow the second outage,
which is the one an operator least expects, having just been told the pane recovered. The suppression
is keyed per pane **and per kind**, because the conditions are independent — a pane that cannot be
detected this minute and is missing agmsg the next has hit two different problems, and the first must
not silence the second.

It is also keyed against the pane's *session identity*, the same way `bootstrapped` and `givenUp`
are. A restarted pane is a new `Session` object, and its failures are its own: suppression recorded
against the session it replaced would make the one case where the operator hears nothing at all the
strongest recovery signal there is — the pane having been torn down and recreated. A pane that
disappears from the session manager drops its suppression outright, which is also what keeps the map
from growing for panes that no longer exist.
5. panemux writes a one-time instruction into the pane's PTY (the same `Session.Write` path already
   used for all terminal input; `buildBootstrapInstruction` in `bootstrap.go`) telling the agent to:
   1. Join agmsg's team by running `join.sh <team> <pane-id> <agmsg-type> "$(pwd)" --force` directly
      — **using the pane's own ID (the same `<pane-id>` panemux's config and every other part of
      this document address it by) as the agmsg `agent_id`**, required and stated explicitly because
      agmsg's own onboarding can otherwise prompt for an arbitrary name of the agent's choosing,
      which would silently break every cross-pane and relay address in this design (they all assume
      `from`/`to` *are* pane IDs). Invoking `join.sh` directly, rather than through agmsg's
      slash-command onboarding shorthand, is itself deliberate — see [Integration with
      agmsg](agmsg-integration.md#integration-with-agmsg) for why hardcoding either of agmsg's two `cmd_prefix`
      conventions (`/agmsg` vs. `$agmsg`) into one bootstrap instruction would be wrong for roughly
      half of the six detectable agent types.
   2. **Only when the pane's agent type is `claude-code`:** claim that identity for this process by
      running `actas-claim.sh "$(pwd)" <agmsg-type> <pane-id> "$CLAUDE_CODE_SESSION_ID"`, and — when
      a Monitor-based watcher is running for this mode — replace that watcher with one restricted to
      this pane (`watch.sh "$CLAUDE_CODE_SESSION_ID" "$(pwd)" <agmsg-type> <pane-id>`). See [Two
      panes in one project directory](agmsg-integration.md#two-panes-in-one-project-directory) for what this fixes and
      why it is emitted for one agent type only.
   3. From then on, send every board-related message (status reports, cross-pane messages) with the
      raw `send.sh <team> <from> <to> "<body>" --force` invocation rather than `/agmsg send`/`$agmsg
      send`, per [Integration with agmsg](agmsg-integration.md#integration-with-agmsg).
   4. Include the [status self-report](message-flow.md#status-self-report-and-message-flow) fields on every status
      update, sent to `_system` via that same `send.sh` invocation.
   5. Only if `mode` is `turn` or `both` (mirroring agmsg's own `delivery.sh set
      monitor|turn|both|off`, default `monitor`): also run `delivery.sh set <mode> <agmsg-type>
      "$(pwd)"` directly (again bypassing the `cmd_prefix` divergence) and read/follow the
      `AGMSG-DIRECTIVE:` block it prints — this is agmsg's own mechanism for telling an agent how to
      reconfigure its own delivery behavior, not something panemux parses or acts on itself. agmsg's
      own docs mark `turn` as a legacy mode kept for backward compatibility rather than the
      recommended one; this document does not steer operators toward it, and `mode: turn` in
      panemux's config is a pass-through of an operator's explicit choice, not a default panemux
      picks. `off` is agmsg's own way to disable its hook-driven delivery for a pane without leaving
      the team — panemux's config equivalent is `agent_board.enabled: false`, which is the preferred
      way to keep a pane out of board features entirely, since it skips bootstrap for that pane
      rather than bootstrapping it and then telling it to turn itself back off. **This step has a
      real repo-local side effect worth stating plainly: agmsg's own `delivery.sh set` writes hook
      wiring into the pane's own project config (e.g. `.claude/settings.local.json` for
      `claude-code`; the exact file is agmsg's own per-type convention, not something panemux
      controls), which outlives the pane session and is scoped to that Git repository, not to
      panemux or to this one pane.** "Additive, never load-bearing" (see [Design
      principles](../agent-board.md#design-principles)) describes what happens when board features are *unavailable*
      — the pane's shell keeps working normally — not a claim that bootstrap makes zero changes
      outside panemux's own state. Because this write is agmsg's own behavior, not something panemux
      scripts itself, panemux does not attempt to undo it if a pane later disables
      `agent_board.enabled`; an operator who wants that hook wiring removed does so the same way
      they would for any other agmsg-managed repo, outside panemux entirely.

   Joining and non-board `/agmsg`/`$agmsg` use are unaffected by local vs. remote — it is agmsg's own
   already-installed skill doing the work either way, not something panemux provisions per pane. This
   step only ever establishes *that pane's* participation; it never touches any other pane or any
   other agent already using agmsg on that host (a pre-existing Codex agent, for example, keeps
   working exactly as it did before panemux was involved). A write is only treated as successful if
   `Session.Write` returns the full payload length with no error — this is the first place in this
   codebase where panemux writes synthesized input into a pane's PTY rather than relaying real user
   keystrokes, so a short or failed write is never silently accepted as success. The two failure
   shapes are handled differently, deliberately: a **short** write (`n > 0`) has already put part of
   the instruction into the pane, so retrying would type the full instruction again on top of that
   half-written line, compounding the corruption rather than fixing it — panemux gives up on that
   pane immediately, with one warning, rather than retry. A **clean** failure (`n == 0`, nothing
   written yet) is safe to retry, so panemux retries up to `maxBootstrapWriteAttempts` (3) times
   before giving up the same way. Either way, "given up" is tracked the same way `bootstrapped` is —
   keyed by pane ID and compared by `session.Session` identity — so a pane that is later restarted
   (a new `Session` object) is eligible again.
6. On a successful write, the pane is marked bootstrapped (`bootstrapWatcher.bootstrapped`, keyed by
   pane ID and compared by `session.Session` identity, not by process) and the full set of
   bootstrapped pane IDs is persisted to `~/.config/panemux/board-bootstrap-state.json`
   (`internal/board/bootstrap_store.go`, same atomic-write/`0600` discipline as the relay's cursor
   file). On the very first poll tick after a panemux restart, this persisted set is used once to
   seed `bootstrapped` with each persisted pane ID's *currently live* session — but **only** for a
   `TypeTmux`/`TypeSSHTmux` pane, whose underlying tmux session (and whatever process is running
   inside it) is independent of panemux and can genuinely still be running after a panemux restart. A
   `TypeLocal`/`TypeSSH` pane's session is recreated from scratch on every panemux startup
   (`session.CreateFromConfig` always spawns a brand-new shell for these two types, with nothing
   running in it yet) — seeding one of those from persisted state would wrongly treat "we
   bootstrapped a previous, now-gone process" as "the current, fresh shell is already onboarded",
   permanently blocking that pane from ever being bootstrapped again on any future run. Seeding is
   never consulted again after this first tick either way; from then on only session-object identity
   governs re-bootstrap decisions. See [Known limitations](limitations.md#known-limitations) for what this
   session-identity granularity does and does not catch.

**Consent model, stated explicitly.** Setting `agent_board.enabled: true` on a pane *is* the consent
mechanism for this PTY write — it is not a new category of risk beyond what that config flag already
implies enabling. The 2-tick debounce in step 3 narrows, but does not eliminate, the window in which
an operator who happens to be typing their own input into a pane at the exact moment an agent process
starts there could have panemux's instruction interleaved with it; this is a stated, accepted
tradeoff (see [Known limitations](limitations.md#known-limitations)), not a claim of a race-free design.
