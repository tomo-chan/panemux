---
type: Decision
title: "Summary adapters (2026-10-24, issue #347)"
description: "Separate transcript sources from summary runners while preserving cache versions and unknown-agent answers."
---

# Summary adapters (2026-10-24, issue #347)

Source reading and summarizer execution are separate adapter parts, leaving a future cross-agent
seam without offering fallback today. Model warnings stay runner-specific. Preserving initial
version hashes avoids paying for identical saved summaries after an ownership refactor.
Unknown-agent cache entries stay unshown rather than invalidating every other saved answer.
Shared action conditions avoid per-adapter repetition. See [registry](../../agent-registry.md).
