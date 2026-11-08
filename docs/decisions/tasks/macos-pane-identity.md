---
type: Decision
title: "Reading `PANEMUX_PANE_ID` on macOS through `ps -E` (2026-09-27, issue #263)"
description: "Read constrained ps output with explicit ambiguity limits instead of adding a platform-specific environment helper."
---

# Reading `PANEMUX_PANE_ID` on macOS through `ps -E` (2026-09-27, issue #263)

Real macOS/SIP probes showed ordinary Claude/Codex binaries expose environments through ps -E;
Apple-signed binaries hide them, so the earlier /bin/sleep probe was misleading.
Retain the fixed shell script instead of a KERN_PROCARGS2 helper/interpreter. Strip independently
observed argv and require exactly one marker: flattened argv/environment cannot disambiguate duplicates.
A single occurrence inside another variable can still claim a pane. This was explicitly accepted:
the same user can set the marker directly and the effect only focuses an existing same-host pane.
Rejecting multiline environments loses legitimate agents without fixing the space form.
See [Pane environment identity](../../behavior/task-navigation.md#the-pane-of-an-agent-outside-tmux) and [command security](../../security/command-execution.md).
