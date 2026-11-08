---
type: Decision
title: "Opening an agent outside tmux through `PANEMUX_PANE_ID` (2026-09-26, issue #254)"
description: "Rationale for Opening an agent outside tmux through `PANEMUX_PANE_ID`."
---

# Opening an agent outside tmux through `PANEMUX_PANE_ID` (2026-09-26, issue #254)

An environment marker locates agents outside tmux without ancestry, which cannot follow detached
agents or remote shells. A narrow inert ID alphabet protects exports across remote shell boundaries.
Remote setup uses a fixed /bin/sh wrapper: direct POSIX assignments failed on fish/tcsh, and tcsh
expanded exclamation marks even in quoted script text. Exporting only when the browser shim is on
would leave ordinary SSH agents unreachable.
The browser matches existing local/ssh panes on the same host; the marker grants no command execution.
Linux-only reading was extended by [macOS observation](macos-pane-identity.md).
See [command security](../../security/command-execution.md).
