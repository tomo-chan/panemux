---
type: Security Requirement
title: "Security: command execution sinks"
description: "Common command execution requirements and routes to each execution boundary."
---

# Security: command execution sinks

The [security design](../security.md) owns the general executable-source rules and `gosec` policy.
The requirements below apply across execution boundaries; each linked concept owns its sink's
validation, trust assumptions and failure contract.

## Common requirements

- Keep executable selection rooted in hardcoded literals or trusted system sources without a
  data-flow path from user input, as the general rules require. Sanitizing a caller-derived string
  does not by itself make it a trusted executable.
- Keep variable operands separate from shell program text. Use discrete argv where possible.
  Follow each boundary's data transport: quoted heredocs for launch prompts and hook content,
  fixed encoding for arbitrary host-execution operands, and validation before quoting where
  required for paths, tmux names and IDs. Quoting alone is not the accepted remote-path guard.
- A value reported by a host is untrusted too. Validate it before reuse at an execution sink;
  render display-only values as text, with the boundary's bounds and character checks.
- Preserve each boundary's refusal, cancellation, output and privacy contracts. A lookup failure
  must not become a working-directory-relative credential read or a guessed executable.

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
