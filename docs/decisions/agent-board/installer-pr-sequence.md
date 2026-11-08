---
type: Decision
title: "Split installer work into three PRs (2026-10-30, #435 / #438)"
description: "Split execution infrastructure, manual installation and automatic updates into sequential reviewable slices."
---

# Split installer work into three PRs (2026-10-30, #435 / #438)

The user split the installer into three sequential main-targeting PRs so implementation,
verification and review fit manageable context: common execution/drain/prerequisite ownership,
then manual pinned install, then global automatic updates. The resulting runtime choices are in
[pinned installation](pinned-agmsg-install.md) and [global updates](global-agmsg-updates.md).
The sequence is complete; it creates no current merge-order or release requirement.

Current contracts: [agent board guide](../../agent-board.md).
