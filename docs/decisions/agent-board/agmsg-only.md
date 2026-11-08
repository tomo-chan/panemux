---
type: Decision
title: "Agent Board uses agmsg only (2026-08-08, PR #162)"
description: "Use agmsg alone instead of maintaining a second panemux-owned messaging database protocol."
---

# Agent Board uses agmsg only (2026-08-08, PR #162)

Supersedes an early dual-backend design with a panemux-owned SQLite protocol for Claude panes.
Maintaining two protocols added no capability: agmsg already supplied the messaging contract and
interoperability with multiple agent types. Panemux therefore owns relay/cache behavior but no
message schema or database.

Claude Code's native cross-session messaging was also evaluated and rejected: it is Claude-only,
does not fit arbitrary SSH hosts, and exposes no documented process API that panemux can call.

Current contracts: [agent board guide](../../agent-board.md).
