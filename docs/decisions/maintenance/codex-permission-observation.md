---
type: Decision
title: "2026-10-15: Codex approval waits from a PermissionRequest hook (#294, #384)"
description: "Approximate approval waits from permission-hook records instead of detecting prompts in terminal output."
---

# 2026-10-15: Codex approval waits from a PermissionRequest hook (#294, #384)

Codex's own files record nothing when it asks to run a command, so approval waits showed as `busy`.
Stage 1 of #294 adds a hand-registered `PermissionRequest` command hook that records the session,
turn and time, read by the existing collection; the wait clears when the person presses
`Input given` or the turn ends.

- **Not proof of a person being asked.** The hook also fires before codex approves a request on its
  own, and nothing tells panemux when the person answers in the pane. The first implementation
  (PR #379, sub-issues #363–#367) tried to establish both, with owner identification, pinned codex
  versions and per-path proofs; it was withdrawn as too heavy. The accepted contract is the
  approximation above.
- **One file per session, in memory for answers.** The latest request replaces the session's
  record, so a later request in the same turn is a new wait; answered requests are kept only in
  panemux's memory, since a request of a finished turn is not shown anyway.
- **Later stages** — SSH hosts, installing from the host menu, a bundled helper, the pane's own
  prompt — are #294's, each its own change.

Current contracts: [maintenance guide](../../maintenance.md).
