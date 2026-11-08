---
type: Decision
title: "Codex task summaries (#312)"
description: "Rationale for Codex task summaries."
---

# Codex task summaries (#312)

Codex conversations use Codex rather than the Claude account. The bounded rollout excerpt retains
canonical user/assistant text, and the summary cache includes the agent. The initial isolation
proposal excluded global instructions and required an empty tool allowlist; the operator chose
instead to respect existing Codex configuration and permissions. The runner therefore preserves
authentication, default model and global/managed settings in a dedicated ephemeral exec invocation.
It requests no tools and never approves or relaxes permissions, but does not promise a tool-free
runtime. Inherited instructions, MCP and hooks may affect input, actions and cost.

Current contracts: [tasks guide](../../behavior/tasks.md).
