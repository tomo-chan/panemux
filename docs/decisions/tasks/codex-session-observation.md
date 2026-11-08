---
type: Decision
title: "Codex sessions: state, starting and resuming (2026-09-27, issue #264)"
description: "Identify Codex sessions through open rollouts and turn records instead of guessing TUI ownership from directories."
---

# Codex sessions: state, starting and resuming (2026-09-27, issue #264)

Codex has no pid-named state file: open rollout descriptors identify sessions, with supported
SQLite turn status or rollout events. A dead process cannot keep a turn busy. Process start bounds
stale resumed turns; newest rollout mtime, not descriptor number, follows A → B → A switches.
Shared app-server sessions have a daemon location: guessing a TUI association by cwd fails when
sessions survive TUI exit. Dashboard starts use --no-daemon and refuse CLIs lacking help support.
Labels wait in memory for a unique session; CLI origin filtering precedes the stopped-session cap
so exec logs cannot hide TUI sessions. UUID-only operands and -- prevent names/options confusion.
The CLI directory is appended to PATH: prepending it could replace arbitrary commands. An earlier
node remains accepted rather than depending on npm/native package layout.
Original no-summary/approval-busy limits were replaced by [Codex summaries](codex-summary-runner.md)
and [permission observation](../maintenance/codex-permission-observation.md).
See [task behavior](../../behavior/tasks.md) for the current collection/launch contract.
