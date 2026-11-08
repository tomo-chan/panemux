---
type: Decision
title: "The dashboard on the agent capabilities (2026-10-23, issue #376)"
description: "Drive dashboard actions from server capabilities while preserving pane operations for unknown agents."
---

# The dashboard on the agent capabilities (2026-10-23, issue #376)

Server capabilities/actions replace browser agent allowlists and duplicated UUID checks.
Missing CLIs disable rather than hide agents, so changing hosts keeps the available choices
understandable. Unknown action reasons retain server detail instead of disappearing.
Unknown agents retain pane operations that act on location, while agent operations remain hidden;
hiding pane actions too would strand newer agents' sessions. See [registry](../../agent-registry.md).
