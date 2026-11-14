---
type: Guide
title: "Security: command execution sinks"
description: "Routes to shared security rules and execution-boundary requirements."
---

# Security: command execution sinks

Part of the [security design](../security.md).

## Shared rules

The shared rules are defined in the security design's [General Rules](../security.md#general-rules)
and [`gosec` policy](../security.md#gosec-policy).

Value passing and validation differ by boundary. The [boundary concepts below](#execution-boundaries-and-former-sections)
and [Codex permission hook](codex-hook.md) are authoritative for their respective contracts.

## Execution boundaries and former sections

### Shell path (`local`, `ssh` sessions)

See [Shell path (local, ssh sessions)](shell-path.md).

### Tmux session name (`tmux`, `ssh_tmux` sessions)

See [Tmux session name (tmux, ssh_tmux sessions)](tmux.md).

### Remote path arguments (SSH working directory)

See [Remote path arguments (SSH working directory)](ssh-execution.md#remote-path-arguments-ssh-working-directory).

### SSH private key paths and an unresolvable home directory

See [SSH private key paths and an unresolvable home directory](ssh-execution.md#ssh-private-key-paths-and-an-unresolvable-home-directory).

### SSH `ProxyCommand`

See [SSH ProxyCommand](ssh-execution.md#ssh-proxycommand).

### Launching the operator's browser (`--open`)

See [Launching the operator's browser (--open)](browser-open.md).

### Host execution with separate standard input

See [Host execution with separate standard input](host-execution.md).

### agmsg source and installer

See [agmsg source and installer](agmsg-install.md).

### Task dashboard collection

See [Task dashboard collection](task-collection.md).

### Task agent probe

See [Task agent probe](task-probe.md).

### Task launch and resume

See [Task launch and resume](task-launch.md).

### Task summaries

See [Task summaries](task-summaries.md).

#### Codex summary runner and rollout reader

See [Codex summary requirements](task-summaries.md#codex-summary-runner-and-rollout-reader).
