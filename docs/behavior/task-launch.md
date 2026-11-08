---
type: Specification
title: "Task launch and resume"
description: "Starting and resuming agent sessions, shell environments, and deferred labels."
---

# Task launch and resume

Part of the [task dashboard](tasks.md).

## Starting a task

`New task` starts an agent declaring `launch` (Claude or Codex) in its own detached tmux session
on the panemux host or an `ssh_connections` host. It creates no pane or pane configuration.

| Field | Rule |
|---|---|
| Host | Panemux host or an `ssh_connections` key |
| Working directory | Existing absolute path passing the pane's [remote path rule](../security/command-execution.md#remote-path-arguments-ssh-working-directory) |
| Agent | Declares `launch`, in registry order; show `notes.launch`. An `unavailable` host capability disables Start with its reason; `unknown` is left to the server |
| Labels | Optional comma-separated values following [record rules](task-records.md#done-and-labels) |
| First instruction | Required; trim surrounding whitespace, normalize line endings to LF, at most 32 KiB, no NUL |

Claude runs `claude --session-id=<minted v4 UUID> -- <first instruction>` in the requested directory.
The instruction is one argument even with a leading `-`; slash commands remain enabled.
The tmux name is `task-<first eight UUID characters>`. An existing name refuses the start without
attaching or replacing it. Claude exit ends tmux and leaves a stopped task.
Labels are validated before launch and saved under (host, claude, session ID) immediately after it.
A record-write failure leaves the task running and reports `records_error` to the dashboard.

### Host environment

The host needs tmux and the agent CLI. Lookup uses the exec channel's `PATH`, then the user's
login-shell path, accepting only an absolute executable file. An absolute executable `$SHELL`
named bash, zsh or sh runs as a login and interactive shell, supplying rc exports (`sh` reads
`~/.profile`). Unset, relative, non-executable or other shell names (fish, tcsh, dash, ksh, mksh)
run the agent directly without rc. Rc changes to directory or positional parameters do not change
the requested launch; the agent still starts in the requested directory, including one with `#`.
Rc output and waits appear in the pane. Success means tmux started: rc that waits holds the agent,
not the request, and rc that exits or execs another shell can leave success with no collected task.

The first instruction travels through a mode-0600 host temporary file (`$TMPDIR`, else `/tmp`,
`panemux-task.XXXXXXXX`), read and deleted before rc runs, also deleted if tmux fails to start.
It never enters the SSH command or tmux arguments; it is readable in the agent's own process
arguments. [Launch security](../security/command-execution.md#task-launch-and-resume) owns shell,
tmux and option-boundary requirements.

### Codex launch

- Codex chooses its session ID after the first instruction. Tmux is `task-<eight random hex digits>`;
  the initial response names tmux only.
- The command is `codex --no-daemon -c check_for_update_on_startup=false -- <first instruction>`.
  `--no-daemon` keeps the rollout in the TUI process, tying the session to tmux rather than the
  [shared daemon](task-collection.md#codex-sessions). Only the update prompt is suppressed;
  other Codex configuration is untouched.
- `codex --help` must list `--no-daemon` before tmux is created; otherwise launch/resume refuse
  with "codex on the host is too old: it has no --no-daemon option". Help, not version, decides.
- Codex's directory is appended to `PATH` after rc (or the exec channel's path without rc).
  An npm CLI can find adjacent node if no earlier node exists. An earlier incompatible node can
  end Codex/tmux; install with that node or put the correct directory first in rc. Failure to run
  `codex --help` on the exec channel's path is reported as Codex too old.
- Trust-directory, new-model and usage-limit screens need the person in the pane. Panemux never
  answers them or rewrites configuration to avoid them; the task stays pid-only `run` with its
  startup note until answered.
- Validated labels wait in memory under (host, agent, tmux session), and are added to existing
  labels by the first `GET /api/tasks` finding exactly one Codex session there. A pid-only Codex
  beside it keeps labels pending; other agents neither claim nor delay them. Pending labels are
  dropped on restart, host removal, two Codex sessions there (logged as ambiguous), or a collection
  at least a minute after start finding no Codex task there. Write failure keeps them for retry.
  This lifecycle applies to any agent choosing its own session ID.
- Selection follows the tmux task first as a process, then as a session.

### Dashboard discovery

Working-directory suggestions are unique collected `cwd`s on the chosen host, with the newest task's
agent and last-use time. Running directories say `now` and precede stopped ones, ordered newest
`status_since` first. Invalid paths are excluded; existence is checked only on start. Other absolute
paths can be typed. A directory disappears when its last task leaves the list; nothing else stores it.
After launch the dashboard selects the task when collection finds it, possibly at the next 10-second
poll because Claude writes state only once running. See [scenarios J21/J32](../scenarios.md#j-the-task-dashboard)
for validation coverage and remaining platform checks.

## Resuming a task

Resume follows the server's [task action](task-collection.md#agent-availability), offered on stopped
Claude/Codex tasks with UUIDs, disabled with the reason when `unavailable`.

- Recollect the host first: only a session listed there as stopped for that agent can resume.
  Its recorded working directory must exist and pass the remote-path rule; missing/refused cwd
  prevents resume. IDs come from collected logs/rollouts, never session titles.
- Claude runs `claude --resume=<id>` in `task-<first eight UUID characters>`; Codex runs
  `codex --no-daemon -c check_for_update_on_startup=false resume -- <id>` in
  `task-<last eight UUID characters>` (v7 UUID prefixes can coincide). Codex uses its rollout's
  first-line cwd and the same PATH handling as launch. A resume sends no instruction; a Codex
  session killed mid-turn resumes `idle` ([Codex sessions](task-collection.md#codex-sessions)).
- New tmux sessions and windows added to existing sessions both use the launch shell environment.
  If a recreated/reconnected pane already made the task's named tmux session with a shell, resume
  adds a window only when collection found no running task of any agent, including unknown state,
  inside it. Existing windows are left intact and the attached pane shows the agent. A running
  task there refuses resume; new-task launch always refuses an existing name.
- Resumes for the same (host, session) are serialized: the second recollects only after the first
  launched and is then refused on finding it running. This covers one panemux process, not other
  instances or manual resumes. A request ending while waiting gives up.
- Session ID and record stay unchanged, including done; a running done task returns to Done on
  stopping. A host restart also ends tmux, so resume creates it or adds to a pane-recreated session.
  The dashboard selects the task and shows it running after collection finds it.

Argument forms and validation are authoritative in [launch security](../security/command-execution.md#task-launch-and-resume).
