---
type: Decision
title: "Task dashboard decisions"
description: "Design rationale for task collection, Codex observation, launch/resume, records, summaries, notifications, and host management."
---

# Task dashboard decisions

Current contracts live in [Task dashboard](../behavior/tasks.md) and its linked specifications.

## Session dashboard independent of panes (2026-09-25, issue #252)

Reading agent-owned files observes sessions panemux did not start; the private-format dependency is
accepted with visible failures rather than guesses. Process identity requires the collecting user's
actual executable rather than substring matching on argv (which misclassified editors and `tail`),
and liveness prevents reboot PID reuse. A fixed stdin script gives one round trip per host with
identical local and remote parsing; local cancellation kills its process group so child stdout
cannot outlive timeout. Stopped tasks cannot claim current branch/PR state, and browser-owned pane
matching avoids duplicating layout state on the server.

## Unreadable state files are diagnostics, not tasks (2026-10-09, issue #313)

Unreadable-file cards lacked a usable session ID and could not be labeled, completed, or resumed.
The live process now becomes the task and its unreadable file becomes a diagnostic (omitting the
process would re-list its newest log as a stopped task). Only invalid `pid`/`sessionId` make a file
unreadable; malformed optional fields are omitted and warnings log only on change.
See [Unreadable state files](../behavior/task-collection.md#unreadable-state-files).

## Codex sessions: state, starting, and resuming (2026-09-27, issue #264)

Codex has no PID-named state file: open rollout descriptors identify sessions, paired with supported
SQLite turn status or rollout events. A dead process cannot keep a turn busy, process start bounds
stale resumed turns, and newest rollout mtime (not descriptor number) tracks session switches.
Shared app-server sessions carry a daemon location rather than guessing TUI ownership by cwd.
Dashboard launches pass `--no-daemon`, refuse CLIs lacking help support, append (never prepend) the
CLI directory to `PATH`, and filter CLI origin before capping stopped sessions so exec logs cannot
hide TUI sessions. See [Codex sessions](../behavior/task-collection.md#codex-sessions) and
[Codex launch](../behavior/task-launch.md#codex-launch).

## Opening an agent outside tmux through `PANEMUX_PANE_ID` (2026-09-26–2026-09-27, issues #254, #263)

Process ancestry cannot follow detached agents or remote shells, so panemux exports a constrained
`PANEMUX_PANE_ID` environment marker via a fixed `/bin/sh` wrapper on remote shells (direct POSIX
assignments broke `fish`/`tcsh`). The marker only focuses an existing same-host pane and grants no
command execution.

On macOS, real SIP probes showed ordinary Claude/Codex binaries expose environments through `ps -E`
(unlike Apple-signed binaries). Panemux keeps the fixed shell script rather than adding a
`KERN_PROCARGS2` helper, strips independently observed argv, and requires a single marker match. A
single occurrence inside another variable remains an explicitly accepted same-user limitation.
See [Pane environment identity](../behavior/task-navigation.md#the-pane-of-an-agent-outside-tmux)
and [Task collection security](../security/task-collection.md).

## Done and labels are recorded on the panemux host (2026-09-26–2026-10-07, issues #256, #310)

Task records live separately from `config.yaml` so session metadata does not race layout saves or
pollute dotfiles. Host/agent/session keys prevent PID reuse, `done` stays independent of observed
state so `GET` never writes, unreadable files refuse writes, mtime/size checks preserve manual
edits, and symlink writes preserve dotfile placement. Used-label suggestions derive directly from
the record file inside `GET /api/tasks` with code-point sorting shared between Go and TypeScript
and `Map` keys to avoid prototype collisions.
See [Done and labels](../behavior/task-records.md#done-and-labels).

## Issue links and autolinked references (2026-09-26, issue #255)

Closing-issue references reuse the existing PR lookup and retry the older `gh` field set only on
unknown-field refusal. Configured prefix/template pairs replace a brittle regex that misread UTF-8,
SHA-256 hashes, and CVEs. References are links rather than queried APIs so no tracker credentials
are required, and conservative URL validation with a shared Go/TS corpus keeps malformed links from
breaking the task list.
See [Repository and issue metadata](../behavior/task-records.md#repository-branch-pull-request-issues-and-references).

## Starting and resuming tasks through login/interactive shells (2026-09-26–2026-10-29, issues #257, #445)

Launches use fixed stdin scripts, prompt files (avoiding tmux argv retention), UUID operands, and
`--` / `--resume=<id>` so names and prompts cannot become CLI options. An inner fixed shell changes
cwd because `tmux -c` expands format characters. Resumes add a window to an existing task-named
shell rather than destroying its work, and per-host/session serialization prevents duplicate resumes
within one process.

To pick up variables exported in `~/.bashrc` and similar files, bash/zsh/sh launches run through
`-lic` (other shells retain direct start). Because rc files can mutate positional parameters with
`set --` or `shift`, the launcher reads/removes the prompt file first, exports `PANEMUX_LAUNCH_*`
variables, expands them after rc execution, and unsets them before invoking the agent.
See [Launch security](../security/task-launch.md) and [Task launch and resume](../behavior/task-launch.md).

## Working directory suggestions derive from collected tasks (2026-10-09, issue #311)

Directory suggestions derive from tasks already held by the browser rather than storing a separate
list that would drift or probing path existence on every collection; start handles deleted
directories. Suggestions intentionally vanish when their last task leaves the bounded listing.
See [Working-directory discovery](../behavior/task-launch.md#dashboard-discovery).

## Task summaries: bounded input, persistence, cost, and Codex runner (2026-09-26–2026-10-10, issues #258, #312, #352, #353)

- **Bounded conversation text only**: Raw transcript tails contained tool output and
  credential-adjacent metadata; summaries extract only recognized first-message plus bounded recent
  user/assistant text and fail visibly on unknown formats. Summaries are opt-in and unmasked, and
  unknown task states are never auto-summarized.
- **Persistence and input-hash reuse**: Answers are persisted in a separate cache file keyed by the
  excerpt hash plus agent and summarizer version so restarts and tool-output-only updates do not
  re-bill. Unusable cache files are moved aside rather than blocking future saves indefinitely.
- **Cost controls**: [Cost measurements](../behavior/task-summary-cost.md) showed CLI system/tool
  definitions dominated short excerpts. Claude summaries use a fixed system prompt, no offered
  tools, and the `haiku` alias (avoiding retired pinned IDs); if organization policy substitutes a
  costlier model, the paid answer is retained with a bounded model ID and one-time warning.
- **Codex runner**: Codex sessions are summarized by Codex itself in an ephemeral `exec` invocation
  preserving existing authentication and user/managed configuration; it requests no tools and never
  relaxes permissions, without promising a tool-free runtime when hooks or MCP are inherited.

See [Task summaries](../behavior/task-summaries.md).

## Lightweight wait observation and server-published task events (2026-10-02, issues #277, #278)

Server-published state diffs replaced browser polling joined to terminal regexes, which constantly
traded missed notifications for duplicates. Shared live-only collection fragments skip PR/git
lookups, stopped-session scans, records, and summaries while keeping wait-signature derivation
aligned (leaving unreliable waits unsigned rather than inventing unstable IDs).

Independent per-host cycles isolate slow hosts, slow WebSocket subscribers are closed instead of
receiving gaps, and reader/writer goroutines join cleanly. Stopping the last subscriber retains the
model and lets in-flight host probes finish without publishing; shutdown cancels all. Per-tab
session storage tracks notified waits rather than racy cross-tab elections, and attention derives
purely from current waits on healthy (`ok`) hosts. See [Task events](../behavior/task-events.md).

## Temporary tmux attach and Type in pane popup (2026-10-02, issues #280, #283, #284)

Opening a task from the dashboard uses a dedicated `POST`/`DELETE /api/tasks/attach` endpoint
(looking up the session on the server rather than letting the browser name arbitrary sessions via
`POST /api/sessions`), `has-session` followed by exact `attach-session -t =<name>` (never
`new-session -A`), a 10-second grace period after the last WebSocket closes so reloads reuse the
client without leaking tmux clients, and no server-wide `window-size` mutation.

The popup mounts `useTerminal` directly rather than `TerminalPane`, uses `recoverOnDisconnect`
off because `/api/sessions/{id}/restart` is pane-only, leaves `Escape` and `Tab` to the focused
terminal (closing via `Close`, `Cmd/Ctrl+Shift+Esc`, or header `Escape`), stores maximized state in
`localStorage`, and keeps English labels consistent with the dashboard.
See [Task navigation and terminals](../behavior/task-navigation.md#opening-a-task).

## Dashboard host management and host chips (2026-09-29–2026-10-09, issues #272, #314)

Dashboard hosts come from `ssh_connections` (distinct from OpenSSH `Add SSH Host`) so only chosen
hosts are collected while still resolving connection details from `~/.ssh/config` with ProxyJump
cycle tracking and control-character rejection (`ProxyCommand` injection guard). Passwords never
round-trip (`has_password`, keep-on-empty, explicit clear), renaming is rejected to avoid
cross-pane rewrites, deletion checks resolved ProxyJump dependencies, and one configuration lock
covers snapshots through atomic writes. The earlier permissive handwritten-name rule was later
superseded by [shared SSH key validation](agent-board.md#validate-ssh-connection-keys-at-load-and-api-boundaries-2026-11-15-issues-495-505-pr-540).

Clicking a host chip opens a fresh `ssh`/`ssh_tmux` terminal via a destination popover with
server-generated names rather than guessing an existing task session.
See [Opening a host](../behavior/task-navigation.md#opening-a-host) and
[SSH connection management API](../behavior/rest-api.md#apiconfigssh-connections).
