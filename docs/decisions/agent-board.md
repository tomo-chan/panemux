---
type: Decision
title: "Agent Board decisions"
description: "Design rationale for agmsg transport isolation, installer coordination, pane-independent relay, observed identities, and command-center security."
---

# Agent Board decisions

Current contracts live in [Agent Board](../backend/agent-board/index.md) and [Agent Board and command center security](../security/agent-board.md).

## Agent Board uses agmsg only (2026-08-08, PR #162)

Supersedes an early dual-backend design with a panemux-owned SQLite protocol. agmsg already
provides the messaging contract and multi-agent interoperability, so panemux owns relay/cache
behavior without maintaining a message schema or database. Claude Code's native cross-session
messaging was rejected because it is Claude-only, does not fit arbitrary SSH hosts, and has no
documented process API.

## Usage remains outside the Agent Board status contract (2026-08)

Account-wide usage belongs to the agent provider. Any future per-pane usage reading belongs on
panemux's pane-inspection surface rather than the cooperative agmsg status schema.

## Real agmsg compatibility contract and behavioral assertions (2026-08-23, PR #176)

Fixture tests missed real `watch.sh` output and opaque event-log ID shapes (treating IDs as numbers
broke relay cursor advancement). A second contract tier installs the pinned agmsg release in CI and
runs a daily canary against latest (median release cadence ~2.9 days). Tests assert observable
delivery and lock replacement using `AGMSG_AGENT_PID` rather than process ancestry or diagnostic
wording, borrowing Pact's consumer-contract principle without Pact broker machinery.

## Own-send ledger and model-checking pilot (2026-08–2026-09, PRs #162, #243, issue #168)

Because `send.sh --force` does not authenticate the free-text `from` field, panemux accepts rows
attributed to `_system` only when they match a recent send recorded in the own-send ledger. The
ledger was chosen as the TLA+ model-checking pilot because it is the smallest self-contained state
machine and previously suffered a multiset-versus-set defect.
See [State-machine model checking](../engineering/testing.md#state-machine-model-checking).

## Browser token bootstrap and independent dashboard capability (2026-08-14–2026-08-22, PRs #170, #171)

`GET /api/session-token` sits deliberately outside the authenticated `/api/board/*` subtree so the
bundled frontend can learn the random bearer token under the loopback/trusted deployment model
([loopback token bootstrap](../security/auth.md#loopback-token-bootstrap)). It returns both
`agent_board_enabled` and `command_center_enabled` so the read-only dashboard and command center
can be enabled independently.

## Command center API boundary, argument separation, and context isolation (2026-08, PRs #162, #170, #171)

- **API over scripts**: A narrow stdio MCP server exposes only status, history, and broadcast
  through panemux's authenticated REST API rather than asking the model to compose shell-safe agmsg
  calls.
- **Structural argv separation**: Real-CLI probes exposed dash-prefixed prompts being parsed as
  flags and variadic flag collisions; the runner builds a fixed option segment terminated before
  the prompt operand, and exact argv-shape tests are retained as a security contract
  ([arguments and CLI parsing](../security/agent-board.md#arguments-and-cli-parsing)).
- **Pinned execution context**: Live testing showed plain `claude -p` can report ambient session
  IDs and inherit broadened tool policies. Command center mints its own session ID, disables
  ambient settings, runs in an empty per-query directory, restricts MCP config, and pairs an
  allowlist with an explicit acting-tool denylist
  ([Command Center subprocess execution](../security/agent-board.md#command-center-subprocess-execution)).

## Shared host execution, host-tool port, and transport isolation (2026-10-25–2026-10-26, issues #438–#441)

- **One host identity and execution pool**: Both boards share one per-host SSH command connection
  keyed by configured host name (distinct names stay distinct even on one machine), with fixed stdin
  scripts, membership checks on every lookup, and cancellation isolation.
- **Host tool port**: `internal/hosttools` owns tool status, admission, and background check
  joining behind a `Tool` port (`internal/codexhook` as first adapter), keeping `/api/tasks/codex-hook`
  and `~/.config/panemux/codex-hook.json` unchanged during refactoring.
- **Message transport port**: `internal/board` owns relay/cache orchestration behind a transport
  port while `internal/agmsg` owns CLI paths, parsing, cursors, versions, and mapping validation at
  startup.

## Global board participation, PTY onboarding retirement, and live pane eligibility (2026-10-29–2026-11-07, issues #435, #495, #503, PR #447)

Instance-wide opt-in replaced per-pane switches, and routing now reads immutable snapshots under the
API configuration lock so pane/host edits take effect without restarting panemux.

PTY onboarding (generated participation text written into terminals, including the earlier
same-project `actas` claim flow from PR #171) was retired in #503 because injected text could
interleave with user input. Participation, identity selection, and reception are delegated to
agmsg's official workflow without adding panemux participation hooks.

## Pinned agmsg installation, diagnostics, and global automatic updates (2026-10-30–2026-10-31, issue #435)

- **Verified offline cache and strict version checks**: A SHA-verified bare Git cache supports
  offline reuse without trusting mutable tags. Startup and update checks compare against the exact
  pinned normalized release (superseding PR #176's patch-tolerance and describe-normalization rules):
  `git describe` builds, dirty trees, and prereleases are treated as unknown and never installed
  over.
- **Separate stdout from diagnostics**: Git source acquisition uses a split-output lane so stderr
  warnings cannot corrupt `rev-parse` or `tar` stdout, while installers/probes retain combined
  failure output inside the reserved update window.
- **Global opt-in updates only**: Automatic updates run at startup, off-to-on opt-in, and new host
  persistence, rechecking update-only eligibility inside the reservation so externally removed
  installs never become automatic installs. There is no periodic watcher or retry loop; overlapping
  opt-in events coalesce to one pending confirmation, and success outcomes are scoped to their run
  and host generation.

## Pane-independent relay and qualified forwarded senders (2026-11-07–2026-11-15, issues #495, #505, PR #540)

Relay uses source-host attribution (`local` and `ssh:<key>` BoardIDs) so valid unqualified agmsg
names participate without pane or roster prerequisites. Cross-host addresses use `name@BoardID`.

Every forwarded copy qualifies its sender—including configured pane IDs on legacy bare pane
destination routes (superseding the initial bare-sender exception)—so delayed copies remain
rejected even after pane removal or host moves without expanding the own-send ledger. Eligibility
checks run side-effect-free before acquiring forwarding leases.

Remote message bodies still cross the shell boundary as base64-encoded positional arguments (PR #162
wrapper), and the decoded body is piped to pinned `send.sh --body - --force` via stdin so
option-like bodies are preserved.
See [Relay](../backend/agent-board/relay.md) and [agmsg contract](../backend/agent-board/agmsg.md#agmsg-compatibility-contract).

## Validate SSH connection keys at load and API boundaries (2026-11-15, issues #495, #505, PR #540)

One validator in `internal/host` enforces the shared `ssh_connections` / BoardID alphabet (non-empty
Unicode letters, combining marks, numbers, and `._-+`) across config loading, API writes, and
BoardID parsing. Disallowed handwritten keys now fail configuration loading before startup (even
when Agent Board is disabled) so every configured host can form a safe, unambiguous
`name@ssh:<key>` reply address.
See [SSH connections](../backend/workspace/sessions.md#defining-connections-in-ssh_connections).

## Observed identities extend the pane response contract (2026-11-15, issue #516)

`GET /api/board/status` retains pane-keyed maps and adds required `identities` keyed by name and
BoardID (enforced strictly in frontend schemas and mocks; see
[identity entries](../api/agent-board.md#identity-entries)). Retained task and task-event
observations take priority only under bidirectional uniqueness (one live task per pane and one
configured pane per task on the same host) without adding host polling. Monotonic collection
generations per host prevent stale snapshots from overwriting newer failures or successes, and
connection edits invalidate pooled connections, clear observations, and advance the watermark under
the same configuration lock. History keeps existing bounded rows and source `host`/`from`/`to`
without adding a resolved-recipient cache.
