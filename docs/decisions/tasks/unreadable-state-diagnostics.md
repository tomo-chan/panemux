---
type: Decision
title: "Unreadable state files are diagnostics, not tasks (2026-10-09, issue #313)"
description: "Report unreadable files as diagnostics and their live processes as tasks instead of unusable sessionless cards."
---

# Unreadable state files are diagnostics, not tasks (2026-10-09, issue #313)

Unreadable-file cards had no usable session ID, so they could not be labeled, completed or resumed.
The live process is now the task and its unreadable file a diagnostic; omitting the process would
list its newest log again as a stopped task. Only invalid pid/sessionId make the file unreadable;
malformed optional fields are absent. Warnings aggregate across hosts and log on change rather
than each poll. Dead-process leftovers remain ignored.
See [Unreadable state files](../../behavior/task-collection.md#unreadable-state-files) for the authoritative diagnostic contract.
