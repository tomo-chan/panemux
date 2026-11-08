---
type: Decision
title: "Command center uses panemux's API, not agmsg scripts (2026-08, PR #162)"
description: "Rationale for Command center uses panemux's API, not agmsg scripts."
---

# Command center uses panemux's API, not agmsg scripts (2026-08, PR #162)

Direct script execution would have made the LLM responsible for shell-safe composition and would
have limited it to one host's agmsg state. A narrow MCP server exposes only board status, message
history, and broadcast, backed by panemux's authenticated REST API. The board initially owned script calls; the later
[transport adapter](message-transport-port.md) moved those calls behind its port.

Current contracts: [agent board guide](../../agent-board.md).
