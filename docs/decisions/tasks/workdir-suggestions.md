---
type: Decision
title: "Working directory suggestions come from the board's tasks, stored nowhere (2026-10-09, issue #311)"
description: "Derive directory suggestions from collected host tasks instead of storing a second list or probing every path."
---

# Working directory suggestions come from the board's tasks, stored nowhere (2026-10-09, issue #311)

Suggestions derive from tasks already held by the browser. A separate file or localStorage list
would drift; a remove button would be misleading because the next collection restores the row.
A combobox fits whole paths better than tags. Launch path validation applies, but probing existence
on every collection was rejected; start handles deleted directories. Suggestions intentionally
vanish when their last task leaves the bounded listing.
See [Working-directory discovery](../../behavior/task-launch.md#dashboard-discovery).
