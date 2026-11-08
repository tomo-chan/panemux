---
type: Decision
title: "Stage 1: a dashboard of agent sessions, independent of panes (2026-09-25, issue #252)"
description: "Rationale for Stage 1: a dashboard of agent sessions, independent of panes."
status: deprecated
---

# Stage 1: a dashboard of agent sessions, independent of panes (2026-09-25, issue #252)

Reading agent-owned files observes sessions panemux did not start; the private-format dependency
is accepted with visible failures rather than guesses. Initial pid-only Codex and unreadable-file
cards were replaced by [rollout observation](codex-session-observation.md) and
[diagnostics](unreadable-state-diagnostics.md).
Process identity requires the collecting user's actual executable, not a name anywhere in argv;
review found editors/tail misrecognized. Liveness also prevents reboot pid reuse.
A fixed stdin script gives one round trip per host and identical parsing locally/remotely. Local
cancellation kills its process group so child stdout cannot outlive the timeout. Stopped tasks
cannot claim today's branch/PR. Browser-owned pane matching avoids duplicating layout state.
See [task behavior](../../behavior/tasks.md) for listing bounds and current interaction rules.
