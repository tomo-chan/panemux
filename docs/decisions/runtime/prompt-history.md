---
type: Decision
title: "Command-center prompts use a panemux-owned history entry (2026-08-17)"
description: "Rationale for Command-center prompts use a panemux-owned history entry."
---

# Command-center prompts use a panemux-owned history entry (2026-08-17)

The Claude stream contains `stream_event`, `system`, `assistant`, and `result` frames but does not
repeat the prompt that produced them. Deriving history from the captured stream alone therefore
made each turn unreadable. Panemux now writes the prompt first as `panemux_prompt`, a type the CLI
does not emit, then appends the captured subprocess frames. A subprocess failure still records the
prompt; a request that never starts a subprocess does not create a turn.

Current contracts: [runtime guide](../../behavior.md).
