---
type: Decision
title: "Browser interception in new remote tmux sessions (2026-10-09, issue #339)"
description: "Rationale for Browser interception in new remote tmux sessions."
---

# Browser interception in new remote tmux sessions (2026-10-09, issue #339)

Claude MCP authentication showed panemux's approval strip in an `ssh` pane but not an `ssh_tmux`
pane. The difference was the missing shim environment and tmux consuming the private OSC, rather
than an earlier Open suppressing a later request. Support is limited to new sessions created by
panemux's `ssh_tmux` pane, as requested: existing shells and agents cannot inherit environment
changes retroactively. Session-local `new-session -e` sets the browser and opt-in marker without
seeding settings into the server's global environment. Real-tmux tests showed that tmux overwrites
the initial pane's PATH from the attaching client, ignoring `-e PATH`. A fixed new-pane bootstrap
therefore prepends the shim after that override, then invokes the operator's configured default
command literally or starts the configured login shell. The configured options remain unchanged.
The shim opts only its emitting pane into passthrough and wraps its OSC; tmux 3.3 is the minimum
for that scoped option. Global passthrough remains unchanged. Browser approval remains mandatory.

Current contracts: [runtime guide](../../behavior.md).
