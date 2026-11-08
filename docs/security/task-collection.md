---
type: Security Requirement
title: "Security: task collection"
description: "Fixed task collection scripts and untrusted host output guards."
---

# Security: task collection

See the [General Rules](../security.md#general-rules) and [boundary map](command-execution.md).

The task dashboard ([behavior](../behavior/task-collection.md)) collects on the panemux host and
every `ssh_connections` host. Request, config and remote-host values must not become command text.

## Collection and metadata execution

- `collectScript` and the running-session-only `attentionScript` are composed once at startup
  from Go constants and collect-adapter fragments. Process hints in the CWD/environment prefilter
  are accepted only as `[a-z0-9-]` words. Neither script changes after startup or accepts request,
  config or host text. Attention collection runs only with subscribers, at most once per host
  concurrently and no more often than every 5 seconds per host, regardless of tab count
  ([event lifecycle](../behavior/task-events.md#lifecycle)).
- Locally the command is `exec.CommandContext(ctx, "sh", "-s")`, remotely the literal `sh -s`,
  with the script on stdin. A remote login shell parses only `sh -s`. HOME is the only environment
  value the local run sets, obtained from `internal/homedir`; it selects files, not commands.
  Local collection has its own process group,
  killed as a group on context end so no child probe outlives it.
- Hosts are connection data passed to the pane dialer, never commands. The guarded
  [SSH connection API](../behavior/rest-api.md#apiconfigssh-connections) refuses cross-site
  requests and control characters in host, user and both paths, never returns a saved password,
  and names fields rather than values in errors. Entries have no `ProxyCommand`/`ProxyJump`
  fields; an explicit host inherits neither from SSH config
  ([SSH proxy requirements](ssh-execution.md#ssh-proxycommand)).
- Remote Git inspection uses the pane header's `remoteGitContext`: host-reported CWD must pass
  `validRemotePath` and `shellQuotePath` before entering the command. Local Git uses `cmd.Dir`
  after `sanitizeGitExecDir`; `gh pr view` takes branch and repository as discrete argv elements
  and a constant `prTaskFields` JSON field list.

The [Codex permission hook](codex-hook.md) owns permission-record and installation safety.

## Collected process and transcript data

The script lists only the collecting user's processes (`ps -U "$(id -u)"`). On a shared host,
another user's processes are neither shown as tasks nor accepted as the live process behind a
leftover state file whose pid they reused.

For each collecting-user `codex` process, open-file probes (`readlink /proc/<pid>/fd/*` or
`lsof -p <pid> -Fn`) retain only `rollout-*.jsonl` names. These and `~/.codex/sessions` rollouts
are read with `head`, `tail`, `grep` and `awk`; their content is never executed or put on a command
line. The filename's session ID is the sole value inserted into the fixed SQLite query:
`sqlite3 -readonly ~/.codex/thread_history_1.sqlite "select … where thread_id = '<id>' …"`.
Strip `rollout-YYYY-MM-DDTHH-MM-SS-` and reject empty IDs or characters outside lowercase hex and
`-` before interpolation, so the ID cannot terminate SQL quoting or expand in the shell. The
query is one double-quoted argument, stdin is `/dev/null`, and `-readonly` prevents DB writes.
Go's `rolloutSessionID` additionally requires a UUID; fragments use fixed regexes, never commands.

Unauthenticated `GET /api/tasks`, reconnect and `GET /ws/tasks/events` can cause host dials;
`PUT /api/tasks/records` writes records; [launch/resume](task-launch.md) start processes. All refuse
`Sec-Fetch-Site: cross-site` or `same-site`, or an Origin whose hostname/effective port differs
from Host, before collection or dialing. Origin-free browser requests need matching Referer and
consistent Fetch Metadata under the [shared request guard](auth.md#shared-browser-request-guard).
Records run no command: `fileops.AtomicWrite` writes `~/.config/panemux/tasks.json`, and labels
render only as text, never markup/URLs. No CORS header permits cross-site response reads; the guard
also prevents side effects. WebSockets do not use CORS, so their guard also prevents cross-site
state reads. [Event frames](../behavior/task-events.md#frames) contain no conversation, prompt,
command line or PID.

Everything a host prints back is untrusted input. The parser (`parseCollectOutput`) accepts only its
own line protocol, skips malformed rows, accepts a session ID only if it matches
`^[a-zA-Z0-9_-]+$`, and treats a tmux session name as display text: the dashboard offers to attach a
pane to it only when it matches the pane's own `validTmuxSessionName` rule, which the pane then
enforces again when it starts.

`PANEMUX_PANE_ID` is untrusted host output that any process of the user can set. For listed
processes only, Linux reads NUL-separated `/proc/<pid>/environ` with `tr`/`grep`; macOS uses
`ps -E -p <pid> -o command=`, shell parameter expansion and `case`, with the pid from its own
`ps` listing. macOS strips the `ps -p <pid> -o command=` arguments first, refuses more than one
` PANEMUX_PANE_ID=` occurrence, and prints only `A-Za-z0-9_.-`, preventing forged newline rows.
A sole space-prefixed occurrence inside another environment value can still supply the ID on
macOS, including when the space follows a newline (`AAA='x PANEMUX_PANE_ID=<id>'`). Directly after
a newline without a space it is not read; Linux accepts neither embedded form. This is
[an accepted limitation](../decisions/tasks/macos-pane-identity.md):
the same user could set the actual variable anyway. Go additionally requires
`^[A-Za-z0-9_.-]{1,128}$` (`validPaneID`); the value never reaches a command. `Go to pane` can
only focus an existing `local` or `ssh` pane of the task's host in the browser's workspaces.

Setting the pane identity and transporting POSIX setup through a non-POSIX login shell follow
[the pane setup requirements](ssh-execution.md#pane-identity-and-remote-shell-setup).

The dashboard's links are `href`s the operator clicks, so none may carry a scheme a browser would run.
An issue URL from `gh` is kept only when it is `http` or `https` (`closingIssueLinks`), and
a `task_dashboard.autolinks` `url_template` is refused at load unless it is an `https` URL whose
host and port the browser's URL parser also accepts, with `<num>` after the host
(`validAutolinkURLTemplate`), so an identifier cannot change where a link goes. The browser checks
every link in the response again (`HttpUrlSchema`). The identifier put in place of `<num>` is only
ever digits, or letters, digits and `-` (`isAutolinkIDByte`).

**Unreadable state files** ([behavior](../behavior/task-collection.md#unreadable-state-files)) carry two values
from the host to the API, the browser and the server log: the file name and a detail that may quote
what the file holds. Neither reaches a command. Both are made valid UTF-8 with every control and
invisible format character replaced by U+FFFD and cut to 128 and 120 characters
(`boundedText` in `internal/tasks/state_file.go`) before they leave `internal/tasks`; the log
quotes them with `%q` as well, and the dashboard renders them as text only, never as markup or a
link. The details never carry a file's whole content.
