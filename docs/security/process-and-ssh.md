---
type: Security Requirement
title: "Local process, tmux, and SSH execution"
description: "Shell path allowlists, tmux targets, SSH paths and credentials, ProxyCommand, host execution lanes, and operator browser launch."
---

# Local process, tmux, and SSH execution

> Part of [Security overview and invariants](overview.md).

## Shell paths (`local` and `ssh` sessions)

`validateShell` (`internal/session/local.go`) enforces three layers:

1. Reject relative paths outright.
2. Regex character allowlist: `^(/[a-zA-Z0-9._\-/]+)$` (rejects spaces, semicolons, quotes, and
   other shell metacharacters).
3. `/etc/shells` allowlist: iterate the trusted system map and return the map key (`s`), never the
   caller-supplied string or regex submatch, so CodeQL's `go/command-injection` recognizes the
   taint break via file-I/O provenance.

The default shell must stay the literal `"/bin/sh"`, never `os.Getenv("SHELL")`.

## Tmux sessions

`validTmuxSessionName` (`internal/session/tmux_ssh.go`) requires `^[a-zA-Z0-9_.-]+$` at
construction time. Local tmux calls use discrete `exec.Command` args; remote commands single-quote
the validated name (or pass a single-line POSIX script to `/bin/sh` when browser interception is
enabled; see [New `ssh_tmux` sessions](preview-and-urls.md#new-ssh_tmux-sessions)).

### Task dashboard temporary attach (`POST /api/tasks/attach`)

Only `NewTmuxLocalAttach` and `NewTmuxSSHAttach` reach tmux for task attaches
([API](../behavior/task-api.md#post-apitasksattach)):

- The request supplies only a task ID; `tasks.Service.FindTask` resolves host and tmux name from a
  fresh collection.
- `validateTmuxAttachName` rejects empty names and anything outside `^[a-zA-Z0-9_.-]+$` (excluding
  quotes, whitespace, metacharacters, `:`, and `=`) before exec or SSH dial.
- Both `has-session` and `attach-session` target `=<name>` (discrete args locally; single-quoted
  `'=<name>'` over SSH). The `=` prefix forces exact match instead of prefix match, and
  `attach-session` never creates a missing session (unlike panes' `new-session -A`).

### Dashboard host terminals (`POST /api/hosts/session-name`, `POST /api/hosts/terminal`)

- `connection` must be an existing `ssh_connections` key (empty/unknown refused); target, user, and
  key come solely from that entry.
- `hostTmuxSessionName` (`internal/api/host_terminal.go`) replaces characters outside
  `[a-zA-Z0-9_.-]` with `-`, appends `-<8 crypto/rand hex digits>`, and validates with
  `session.IsValidTmuxSessionName`. Any returned `tmux_session` for `ssh_tmux` is revalidated;
  `ssh` rejects one. Sessions build via `session.CreateFromConfig`.
- Both routes enforce the shared browser guard and strict bounded JSON decoding.
- At most 4 active host terminals per connection (`maxHostTerminalsPerConnection`); excess returns
  `429` before dialing SSH. Host terminals use `host-terminal:<session id>` in the shared registry
  and are skipped by `POST /api/tasks/attach`.

## SSH execution

### Remote path arguments (SSH working directory)

User-supplied SSH `cwd` values reaching `cd <cwd> && exec $SHELL` must pass `validRemotePath`
(`internal/session/ssh.go`) and then `shellQuotePath`:

```text
^(/[^;|&$`'"<>()\[\]{}!\\\x00-\x1f\x7f]*)+$
```

Host-reported paths cross the same guard: `remoteClaudeProjectProbeCmd`
([pane metadata](../behavior/pane-metadata.md#pane-git-and-pr-metadata)) allowlists the input
session ID with `validClaudeSessionID` (`^[a-zA-Z0-9_-]+$`) + `shellQuotePath` (only `*` is
unquoted under a compile-time projects root), and checks the host-returned transcript path with
`validRemotePath` plus a `<sessionId>.jsonl` match before passing it to `stat`/`cat`/`ls`.

### SSH private key paths and an unresolvable home directory

Never join paths against a failed home lookup (resolve via `internal/homedir`). When home cannot
be resolved, `buildAuthMethods` skips default keys (`no auth methods`), and `requireAbsolutePath`
refuses any non-absolute `KeyFile` or `known_hosts_file` (including unexpanded `~/` or
cwd-relative paths) before reading so project-local keys or `known_hosts` can never be used.

### SSH `ProxyCommand`

`dialViaProxyCommand` runs `exec.Command("/bin/sh", "-c", cmd)` after `%h`/`%p` substitution, so
inputs must come strictly from the operator's SSH config:

- `ssh_connections` has no `ProxyCommand` field and no API writes one. `POST /api/ssh-config/hosts`
  and `AppendHost` reject control characters in name, hostname, user, and identity file (`422`) via
  `sshconfig.CheckHostValues` so line breaks cannot inject directives.
- An `ssh_connections` entry specifying its own `host` inherits neither `ProxyCommand` nor
  `ProxyJump` from `~/.ssh/config`.
- `lookupSSHConfigHost` reads only an absolute SSH config path (refusing relative `.ssh/config`
  when home lookup failed). `ProxyJump` tracks visited names to reject cycles.

### Pane identity and remote shell setup

`PANEMUX_PANE_ID` is exported only when matching `^[A-Za-z0-9_.-]{1,128}$` (`validPaneEnvID`),
via `exec.Cmd.Env` locally or `remotePaneIDSetup` + `shellQuotePath` remotely. Any remote pane
command carrying setup runs as `exec /bin/sh -c '<quoted one-line script>'` (`sshShellCommand`)
containing no newlines and no `!` (preventing csh/tcsh history expansion and fish syntax conflicts;
browser shim bodies use `printfOctalFormat`).

## Host execution with separate standard input

`host.InputExecutor` (`hostexec.Pool.RunInput`) is an internal port (no HTTP route supplies script
text) passing trusted script text, positional operands, and arbitrary stdin separately:

- Locally: literal `sh -c <script> <fixed $0> <operands...>` with `homedir` `HOME` and
  process-group cancellation.
- Remotely: a fixed one-line bootstrap with single-quoted octal operands (no raw newline, quote, or
  `!`), decoded by POSIX `sh`'s builtin `printf` and executed with operands as positional arguments
  while leaving stdin free for data streams (such as `tar`). Never interpolate variable values into
  script text.
- `RunInput` combines stderr (`exec 2>&1`) and preserves output/errors on failure;
  `hostexec.RunLocalSplit` separates stdout and stderr so warnings never corrupt structured or
  archive stdout.
- Tool management (`internal/hosttools`) drains admitted calls before running prerequisites or
  installation, rejects new calls while paused, and resumes on error/cancel. Consumers must hold
  their admission lease for the entire operation.

## Launching the operator's browser (`--open`)

`openChrome` (`main.go`) opens `"http://" + srv.Addr()` solely through `browserOpenArgv(goos, url)`,
which returns compile-time literals `open`, `google-chrome`, or `cmd` (or fails on unsupported OS)
with discrete argv elements and no shell. Its inline `//nolint:gosec` must cite this literal-only
provenance.
