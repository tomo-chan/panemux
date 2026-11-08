---
type: Decision
title: "Isolate the board message transport (2026-10-26, #438 / #441)"
description: "Rationale for Isolate the board message transport."
---

# Isolate the board message transport (2026-10-26, #438 / #441)

Board-owned relay/cache orchestration uses a transport port; agmsg owns CLI paths, parsing,
cursors, version handling and participation prose. Separating them prevents provider schema from
leaking into orchestration and keeps captured/live contract tests beside the adapter.
Startup checks explicit registry/adapter declarations instead of inferring them.
Participation later changed with [PTY retirement](retire-pty-onboarding.md).
See [architecture](../../architecture.md) and [agmsg integration](../../agent-board/agmsg-integration.md).
