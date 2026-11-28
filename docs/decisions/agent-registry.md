---
type: Decision
title: "Agent Registry decisions"
description: "Design rationale for static agent registration, consumer-owned capability adapters, open-world records, and tagged fixture-agent verification."
---

# Agent Registry decisions

Current contracts live in [Agent Registry](../backend/agent-registry.md) and
[Adding an agent](../backend/agent-registry.md#adding-an-agent).

## One static registry; capabilities are consumer-owned ports (2026-10-12, issue #372)

Agent aliases and process rules had drifted across packages. Explicit static registration in
`internal/agent` centralizes declarations without config-injected patterns or `init()` link-order
ambiguity. Capability ports live in each consumer package in its own types, avoiding speculative
shared interfaces and import cycles. Comparable `(Host, Agent, SessionID)` keys need no escaping
and support multi-session daemons.

## Collection, availability probe, and format anti-corruption layer (2026-10-16, issue #373)

- **Composed collect adapters**: Constant per-agent fragments compose into the collection script
  without interpolating request/config/host values. Missing declared sections fail visibly instead
  of pretending there are no sessions, live sections precede `ps` to drop disappearing processes,
  and startup checks adapters against registry declarations.
- **Separate availability probe**: Because CLI lookups require slow/noisy login shells, a separate
  background probe caches per-host availability without blocking collection, invoking CLI version
  flags, or dropping healthy connections on timeout. Unknown external statuses/events degrade
  visibly instead of guessing live state.

## Separate launch and resume adapters (2026-10-18, issue #374)

`launch` and `resume` are distinct adapters because an agent may support launch without resume.
Adapters supply constant shell fragments under `case $agent:$mode` inside one shared fixed script
(preserving shared tmux, cwd, and prompt-file safety arguments). Pending labels wait whenever a
launch mints no session ID (not by agent name), keyed by `(host, agent, tmux session)` and claimed
only when a single session of that agent appears. `cli_missing` refuses before execution with `409`
and re-probes immediately so newly installed CLIs are picked up; `not_probed` and `probe_failed`
still let the script verify directly.

## Open-world records and session workdir sharing (2026-10-18, issue #375)

- **Open-world records**: Unknown-agent entries in record and summary files are preserved as
  original JSON so downgrades do not block writes for known agents; malformed IDs still fail, and
  aliases are not normalized to avoid key collisions.
- **Shared workdir rules**: Pane headers and the task dashboard share the Task process rule and
  state parser via `internal/session` workdir adapters (keeping Board detection separate because it
  mirrors agmsg, and rejecting shared per-host observations for failure isolation).
- *(Historical)* #375 also introduced per-agent board participation adapters, later retired when
  [PTY onboarding was removed](agent-board.md#global-board-participation-pty-onboarding-retirement-and-live-pane-eligibility-2026-10-292026-11-07-issues-435-495-503-pr-447).

## Capability-driven task snapshot and dashboard (2026-10-23, issue #376)

Registry declarations, host availability, and task actions ride in the existing `GET /api/tasks`
snapshot rather than adding a second agents endpoint for the UI to reconcile. Missing CLIs disable
rather than hide agents so host switches stay legible, and unknown agents retain location-based
pane operations while hiding unsupported agent actions.

## Separate summary source and runner adapters (2026-10-24, issue #347)

Transcript reading (`summarySource`) and execution (`summaryRunner`) are split into two parts,
leaving a future cross-agent seam without offering fallback today. Initial cache version hashes are
preserved across ownership refactors so saved summaries are not re-billed.

## Tagged fixture agent for end-to-end extensibility verification (2026-10-24, issue #377)

A third agent compiled only with `-tags panemux_fixture_agent` verifies capability extensibility
without hidden flags in shipped binaries. Tagged test invocations assert required contract names so
renaming cannot silently shrink coverage; mutation measures the default build and names ignored
tagged files. Browser E2E uses real tagged collection with fake CLIs and `ps`/`lsof` stand-ins that
expose only fixture descendants so local developer sessions are never read.
See [the fixture agent's tagged build](../engineering/quality-gates.md#the-fixture-agents-tagged-build).
