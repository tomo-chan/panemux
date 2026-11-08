---
type: Decision
title: "Scope automatic-update success to its host run (2026-10-31, #435 B3 review)"
description: "Scope installer outcomes to their worker run and host identity to prevent stale success claims."
---

# Scope automatic-update success to its host run (2026-10-31, #435 B3 review)

Success and restart guidance survive a queued confirmation within the same worker run, but a new
opt-in run starts without a success claim. Adding a host clears the previous identity's outcomes,
even while automatic updates are off; changing its result generation also rejects late outcomes
from the old identity. The shared result invalidation is used after successful manual updates too.
These are result/notification changes; installation admission and execution are unchanged.

Current contracts: [agent board guide](../../agent-board.md).
