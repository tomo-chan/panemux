---
type: Decision
title: "Host tool management and the Codex hook adapter (2026-10-26, #438 / #440)"
description: "Rationale for Host tool management and the Codex hook adapter."
---

# Host tool management and the Codex hook adapter (2026-10-26, #438 / #440)

`internal/hosttools` owns host tool management and defines the `Tool` port: `Check` returns the
shared six-state status, and `Install` returns the status read back or an installation error.
`internal/codexhook` is its first adapter; it owns the scripts and uses `internal/host.Executor`.
Task collection only emits a successful-collection notification. Management owns the background
check context and joins those checks before the shared execution pool closes.

The four implementation decisions were confirmed in #440: keep Codex-specific automatic-update
conditions, per-host settings and `~/.config/panemux/codex-hook.json` in this step; generalize for
agmsg's global setting in #435. Keep `/api/tasks/codex-hook` and its JSON unchanged and route it to
host management internally. Extract `HostTools` / `HostToolItem` with tool-specific copy supplied
as data, preserving the existing markup, CSS, text and actions. API migration and new visible UI
would expand a refactor without helping its ownership boundary, so neither is part of this step.

Current contracts: [agent board guide](../../agent-board.md).
