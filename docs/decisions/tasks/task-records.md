---
type: Decision
title: "Stage 2: done and labels are recorded on the panemux host (2026-09-26, issue #256)"
description: "Separate records from configuration and observed state while protecting unreadable files and dotfile symlinks."
---

# Stage 2: done and labels are recorded on the panemux host (2026-09-26, issue #256)

Records live separately from config to avoid racing layout saves or polluting dotfiles with
session data. Host/agent/session keys avoid pid reuse. Done stays separate from observed state so
resumed work is visible without GET writing. Retained records do not fabricate uncollected cards.
Unreadable files refuse writes; mtime/size checks preserve hand edits and symlink writes preserve
dotfile placement. Removed-host records remain clearable without coupling cleanup to config saves.
Review caught invisible label text and object-key collisions such as __proto__; bounded visible
tags and Map keys avoid them. See [Task records](../../behavior/task-records.md#done-and-labels) for current record/API rules.
