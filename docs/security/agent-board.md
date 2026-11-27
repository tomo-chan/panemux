---
type: Security Requirement
title: "Agent Board and command center security"
description: "Deployment and message trust, remote agmsg writes, dashboard values, pinned agmsg installation, and command center subprocess isolation."
---

# Agent Board and command center security

> Part of [Security overview and invariants](overview.md). Authoritative security model for
> [Agent Board](../agent-board.md).

## Deployment and message trust

- Panemux assumes loopback or a trusted network and does not terminate TLS. Non-loopback bindings
  require an explicit `server.auth_token` and an external TLS boundary (reverse proxy, SSH tunnel,
  or VPN); the bearer token guards board APIs and command center without making the core terminal
  server a multi-user Internet service ([Authentication](auth.md)).
- SSH protects each hop, but panemux sees plaintext while relaying (not end-to-end encrypted).
- `agent_board.enabled` is instance-wide consent for detection, display, and relay across local and
  configured SSH hosts in one team (`agent_board.mode` is ignored; Agent Board never writes to PTYs
  or alters existing agmsg registrations/hooks on disablement).
- Because `send.sh --force` allows arbitrary `from` names, relay accepts only:
  - valid unqualified names read through public `api.sh` from their source host (no pane or roster
    prerequisite; unqualified pane IDs belonging to another host are rejected);
  - `_system` only when matched against a recent send in panemux's own-send ledger (recorded before
    send, removed on failure, short TTL, single consume).
- Every forwarded copy qualifies its sender with the read host's BoardID (`name@local` or
  `name@ssh:<key>`, including bare pane-destination routes), and every `@`-containing `from` is
  rejected from history, state, and forwarding—preventing duplicates, wrong-host attribution, and
  loops even after pane removal or movement. `local` and `ssh:local` remain distinct.
- Configuration loading, API writes, and BoardID parsing enforce the same SSH connection key
  alphabet (Unicode letters, marks, numbers, and `._-+`, exact spelling); invalid keys fail
  startup even when Agent Board is disabled (no exclusion fallback).
- Same-host, same-user name spoofing remains possible; received messages and dashboard visibility
  never authorize work or bypass an agent's approval rules.

## Agent Board remote writes and dashboard UI

Only `internal/agmsg` invokes operator-installed agmsg scripts through the shared host execution
port (per-operation exec channels so cancelling one does not close the shared SSH connection).
`internal/agmsg` never installs panemux or panemux helper scripts on a remote host.

Remote agmsg calls must satisfy all of:

- Pass every value as an argument and quote every argument at the single SSH construction boundary;
  never interpolate caller data into wrapper or probe script text.
- `team` matches `[A-Za-z0-9_.-]+`; `from`/`to` match that name optionally followed by `@local` or
  `@ssh:<key>` (rejecting qualified `_system`, whitespace, control chars, extra separators, and
  shell syntax).
- Reject NUL-containing message bodies before host discovery or send execution (preserving
  own-send ledger hashes).
- Base64-encode bodies, validate the encoded alphabet, decode via a fixed wrapper using positional
  parameters, and pipe to `send.sh --body -` on stdin (never as an option-bearing argument).
  *(Unverified CodeQL caveat: allowlist-before-exec is structural mitigation; no real CodeQL run
  has confirmed the taint break.)*
- Export compile-time literal `AGMSG_SELF_NAME=off` (never from `os.Getenv`), unset `BASH_ENV` and
  `ENV`, and clear pinned built-in driver pane markers and Codex's thread identity
  ([agmsg contract](../agent-board/agmsg-contract.md)) before running agmsg scripts or installers,
  preserving `HOME`/`PATH`/storage and never modifying agent-owned marks.
- Leave SQL escaping solely to agmsg; expand `~/` in `~/.agents/skills/agmsg` before shell quoting.

**Dashboard UI**: Every agent-reported status field is untrusted text and must render as React text
children—never via `dangerouslySetInnerHTML` or DOM attributes. Self-reported URLs are omitted from
the dashboard and must not become links without an explicit scheme allowlist (script execution in
the dashboard origin would expose the board bearer token).

## agmsg acquisition and installation

- **Source (`internal/agmsg.Source`)**: Executes literal `git` via `hostexec.RunLocalSplit`
  (stdout-only for SHA verification and `tar` bytes; trimmed stderr on failure only) with fixed
  repo URL, SHA-scoped cache path, fixed full commit, and fixed subcommands—no browser input
  reaches argv and no checkout hooks run. Verifies `rev-parse <SHA>^{commit}` before `git archive`.
- **Installer (`internal/agmsg.Tool`)**: Streams `git archive` tar bytes on stdin to a fixed script
  via `host.InputExecutor`. Only empty or `--update` operands are allowed; the script rechecks
  alternate `.agmsg` markers, extracts to a `mktemp` dir (cleaned by trap), and runs literal
  `bash ./install.sh` with stdin closed. Prerequisite checks use the combined input lane.
- **Consent and auto-updates**: Manual Install/Update authorizes execution of the pinned upstream
  code as the target user (which writes outside `~/.agents/skills`, including agent commands,
  skills, shims, and Codex sandbox settings; panemux cannot sandbox those writes or stop
  agent-owned scripts—restart agents afterward). Global automatic updates use the same pinned path
  for **older installed copies only** at startup, opt-in, and host addition (never automatic
  Install or downgrade; eligibility rechecked inside the update reservation).

## Command center subprocess execution

`internal/commandcenter/runner.go` runs literal `"claude"` (or operator-configured
`RunnerConfig.ClaudeBin`) via `exec.CommandContext` with discrete argv and no shell.

### Arguments and CLI parsing

Fixed option flags (`-p`, `--output-format=stream-json`, `--verbose`, `--session-id`, `--mcp-config`,
`--allowedTools=a,b,c` bound with `=`, `--disable-slash-commands`) precede a literal `--`
end-of-options marker, followed by the prompt operand (`TestRunnerBuildArgsShapeIsSafeAgainstArgumentInjection`,
`TestRunnerDisablesSlashCommands`). Slash commands are disabled so board-token holders cannot
invoke `/context`, `/config`, `/model`, `/mcp`, etc. (non-tool environment context injected by the
CLI itself is not closed by these flags).

### Session, settings, and working directory

- Mint a v4 UUID (`--session-id`) and persist only that panemux-owned ID for `--resume` (never
  adopt a subprocess-reported ambient ID; `TestRunnerFirstRunMintsAndPersistsItsOwnSessionID`).
- Pass `--setting-sources ""` and `--strict-mcp-config`; run in an empty per-query temp `cmd.Dir`.
- Pass instructions only via `--append-system-prompt` (compile-time `DefaultSystemPrompt` plus
  optional text from `~/.config/panemux/command-center/CLAUDE.md`). Never load user or custom
  settings files; pass only the compile-time `SubprocessSettings` literal to `--settings`.

### Acting-tool denial

Because settings such as `permissions.defaultMode: acceptEdits` can nullify `--allowedTools`
(reproduced on CLI v2.1.233 where `Bash` executed with empty `permission_denials`), argv also
passes `--disallowedTools` from `DisallowedTools()` (`mcp_config.go`), explicitly naming every
executing, writing, network, agent-spawning, or external tool (`--disallowedTools="*"` was rejected
as it removed MCP board tools). `TestDisallowedToolsCoversActingTools` and
`TestRunnerDeniesActingToolsByName` protect the list (shared with [Task summaries](tasks.md#log-reading-and-claude-runner));
list drift as the CLI adds tools is an accepted weakness.

### Sandbox scope and verification limits

`SubprocessSettings` contains only narrowing keys (`{"sandbox":{"enabled":true}}`), enforced by
`TestSubprocessSettingsOnlyNarrows` and `TestRunnerSendsOnlyPanemuxOwnSettings`. Never set
`autoAllowBashIfSandboxed`, and leave `network.allowedDomains`/`deniedDomains` unset (they do not
guard the MCP child's loopback call). **Sandbox confinement remains unverified**: on the tested
host the setting was ignored harmlessly (`CLAUDE_CODE_SANDBOXED` unset); verify on a host where
`/sandbox` reports availability before claiming confinement.

### MCP environment sink

`PANEMUX_BOARD_TOKEN` and `PANEMUX_BOARD_BASE_URL` live in the MCP config `env` block (never
Runner's argv; see [MCP token file](auth.md#mcp-token-file)). `panemux __board-mcp-server`
(`board_mcp_server.go`) reads them via `os.Getenv` and makes only HTTP calls through
`HTTPBoardAPIClient`—never `exec.Command`.
