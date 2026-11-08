---
type: Decision
title: "Manual pinned agmsg installation (2026-10-30, #435 B2)"
description: "Rationale for Manual pinned agmsg installation."
status: deprecated
---

# Manual pinned agmsg installation (2026-10-30, #435 B2)

A verified SHA-specific bare Git cache permits offline reuse without trusting a mutable tag.
Installer consent, failure diagnostics and shared relay reservations bound the execution window;
success invalidates absence discovery so installation need not require restarting panemux.
The initial pin was 1.2.0 / 3c537a51bb76a23e312e7c27770df4a58ba794ff, not a timeless version requirement.
The inactive auto-update stage was replaced by [global updates](global-agmsg-updates.md), and PTY
onboarding by [retirement](retire-pty-onboarding.md). Current contracts are in
[installer security](../../security/agent-board.md) and [maintenance](../../maintenance.md).
