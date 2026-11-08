---
type: Decision
title: "Summaries kept across restarts and reused by their input (2026-10-10, issue #352)"
description: "Rationale for Summaries kept across restarts and reused by their input."
---

# Summaries kept across restarts and reused by their input (2026-10-10, issue #352)

Memory-only, mtime/size-keyed caching paid again after restart or tool-output-only log changes.
Persist answers and hash the actual excerpt with agent/summarizer version to avoid those costs
without storing excerpts. A separate file isolates regenerable cache from human records.
Restore does not immediately ask again; retention lets temporarily absent tasks reuse answers.
Busy logs are not read on every change just to optimize cache reuse.
Unusable files move aside unchanged before saving resumes: refusing unknown versions indefinitely
would leave answers unsaved until someone noticed. Disabling summaries leaves the file untouched.
See [task behavior](../../behavior/tasks.md) for current persistence and retention rules.
