---
type: Decision
title: "Labels used before come from the record file, as tags on `GET /api/tasks` (2026-10-07, issue #310)"
description: "Rationale for Labels used before come from the record file, as tags on `GET /api/tasks`."
---

# Labels used before come from the record file, as tags on `GET /api/tasks` (2026-10-07, issue #310)

Records are the authority for used labels, avoiding a second list to synchronize. The snapshot
already reads them, so a separate labels endpoint adds a request/failure state without new data.
Tags fit comma-separated input better than a whole-field dropdown. Review caught clicks discarding
completed labels and UTF-16 sorting disagreeing with Go on non-BMP characters; preserve complete
values and use the same code-point ordering on both sides.
See [task behavior](../../behavior/tasks.md) for current UI limits and behavior.
