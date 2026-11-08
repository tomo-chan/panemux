---
type: Security Requirement
title: "Security: tmux sessions"
description: "Validated exact tmux targets and guarded task and host terminal attachment."
---

# Security: tmux sessions

See the [General Rules](../security.md#general-rules) and [boundary map](command-execution.md).

`validTmuxSessionName` in `internal/session/tmux_ssh.go` uses a strict regex (`^[a-zA-Z0-9_.-]+$`)
validated at construction time. Local tmux arguments are discrete `exec.Command` args. Remote tmux
commands single-quote the validated name; when browser interception is enabled the fixed POSIX
setup and quoted operands are passed as one line to `/bin/sh`, just as for an ordinary SSH pane.
See [new remote tmux interception](url-open.md#new-ssh_tmux-sessions) for its session-local
environment and pane-local passthrough scope.

**The task dashboard's attach** (`POST /api/tasks/attach`, [behavior](../behavior/task-api.md#post-apitasksattach))
passes a tmux session name that a host's collection reported, not one a person configured. It reaches
the sink only through `NewTmuxLocalAttach` / `NewTmuxSSHAttach` in `internal/session`, and:

- The request names only a task ID. The tmux session name and the host come from a fresh collection
  of the host that ID names (`tasks.Service.FindTask`); nothing else in the request reaches tmux.
- `validateTmuxAttachName` refuses an empty name and anything outside `^[a-zA-Z0-9_.-]+$` before
  exec, and before the SSH dial. The allowlist excludes quotes, whitespace, shell metacharacters,
  `:` (a tmux window target) and `=`.
- Locally the argv is the literal `attach-session`, `-t`, `=<name>`, discrete arguments with no
  shell. Over SSH the remote command is `tmux attach-session -t '=<name>'`; the single quotes are
  safe because the allowlist excludes a quote. The `has-session` check run before it takes the same
  name the same way: argv `has-session`, `-t`, `=<name>` locally, `tmux has-session -t '=<name>'` over
  SSH, after the same validation.
- The `=` prefix makes tmux match the session name exactly. Without it tmux resolves a target by
  prefix, so `task-7c21` could attach to `task-7c21e0a4`. `attach-session` never creates a session,
  unlike the panes' `new-session -A`: a name that no longer exists fails rather than starting a shell
  under the task's name.

**A host opened from the dashboard** (`POST /api/hosts/session-name`, `POST /api/hosts/terminal`,
[behavior](../behavior/task-navigation.md#opening-a-host)) starts an `ssh` or `ssh_tmux` session on a
connection a browser names:

- `connection` must be a key of `ssh_connections`; an empty name (the panemux host) and any other
  name are refused before a session is built. The SSH target, user and key come from that entry,
  never from the request.
- The tmux session name is generated in one place on the server (`hostTmuxSessionName` in
  `internal/api/host_terminal.go`): each character outside `[a-zA-Z0-9_.-]` becomes `-`, then
  `-<8 hex digits from crypto/rand>` is appended, and the result must pass
  `session.IsValidTmuxSessionName` (the regex above). A `tmux_session` a request sends back for
  `ssh_tmux` passes the same check; `ssh` refuses one.
- The session is built through `session.CreateFromConfig` like any `ssh` / `ssh_tmux` pane, so the
  `new-session -A` argv and its quoting are the panes' own.
- Both routes refuse a cross-site request and decode the body strictly (unknown fields refused,
  bounded size), as `POST /api/tasks/attach` does.
- At most 4 host terminals are open on one connection at a time (`maxHostTerminalsPerConnection`);
  past that the request is answered `429` before any SSH connection is made. A terminal that failed
  to open, was deleted, expired or whose shell has exited does not count. This bounds the SSH
  connections, and the `ssh_tmux` sessions left on the host, that a looping client can open.
- Host terminals share the board's attach registry, under `host-terminal:<session id>`, but
  `POST /api/tasks/attach` skips their entries, so the task route neither returns nor removes one.
