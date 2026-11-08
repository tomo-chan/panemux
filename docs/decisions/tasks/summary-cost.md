---
type: Decision
title: "Summaries cost less: a fixed system prompt, no tools and Haiku (2026-10-10, issue #353)"
description: "Use fixed prompts, no offered tools and Haiku while exposing the cost of unexpected model fallback."
---

# Summaries cost less: a fixed system prompt, no tools and Haiku (2026-10-10, issue #353)

[Measurements](../../behavior/task-summary-cost.md) showed CLI system/tool definitions dominated
short excerpts and Haiku cost much less, overturning the earlier stage-3 observation.
Fixed system prompts and no offered tools cut overhead; the haiku alias avoids retired pinned IDs.
Existing denial/settings restrictions remain. Prompts restrict answers to the excerpt so the CLI's
empty directory is not invented as remaining work. Organization policy may substitute a costlier
model; retain the already-paid answer, show its bounded model ID and log a warning once per model.
Incremental summaries were rejected for drift and extra saved state; debounce saved nothing in the
measured sequence, and smaller budgets lost context for fractions of a cent.
See [Task summaries](../../behavior/task-summaries.md).
