---
type: Decision
title: "Stage 3: summaries of a task's work and what is left (2026-09-26, issue #258)"
description: "Send bounded recognized conversation text instead of raw tool output or unknown-format fallbacks."
---

# Stage 3: summaries of a task's work and what is left (2026-09-26, issue #258)

A real transcript was mostly attachments/tool output and included credential-related metadata.
Raw tails and raw-text fallbacks were rejected: format changes must not silently send that material.
Only recognized user/assistant text is interpreted; first-message plus bounded recent text keeps
the goal without sending the whole log. Summaries are opt-in and unmasked, with no reliable masking claim.
Unknown states are not summarized automatically because a new status might trigger paid calls every
poll. Failures use fixed messages and do not retry unchanged input. Selection must match collection:
mtime-only selection once chose the wrong copy when two logs changed in the same second.
Memory-only caching and the original tool policy were replaced by [persistence](summary-persistence.md)
and [cost reduction](summary-cost.md). See [task behavior](../../behavior/tasks.md) for current limits.
