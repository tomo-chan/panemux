---
type: Decision
title: "2026-11-07: retire Agent Board PTY onboarding (#503, #495 S1)"
description: "Rationale for 2026-11-07: retire Agent Board PTY onboarding."
---

# 2026-11-07: retire Agent Board PTY onboarding (#503, #495 S1)

Generated participation text can interleave with user input. Remove generation, PTY writes/retries
and persistence rather than invent another terminal-input protocol. Old state files stay untouched;
transitional instructed stays false, deprecated mode is omitted on save, and presence is session-scoped.
The initial plan coupled S1's merge to S6/S7. On 2026-11-07 the user replaced it: merge S1 independently
and delegate participation, identity selection and reception to agmsg's official workflow instead of
adding panemux participation hooks. See [Agent Board](../../agent-board.md) for current contracts;
#495 tracks the remaining changes.
