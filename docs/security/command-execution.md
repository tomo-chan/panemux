# Security: command execution sinks

> Part of the [security design](../security.md). That document carries the general rules, the `gosec` policy, and the map of these files.

### Shell path (`local`, `ssh` sessions)

`validateShell` in `internal/session/local.go` applies three layers:

1. **Absolute-path check**: reject relative paths outright.
2. **Regex character allowlist**: `^(/[a-zA-Z0-9._\\-/]+)$` rejects shell metacharacters such as spaces, semicolons, and quotes.
3. **`/etc/shells` allowlist**: iterate the system shell registry and return the key from the trusted map (`s`), not the caller-supplied value.

The third point is critical for CodeQL's `go/command-injection` analysis. CodeQL tracks data flow from taint sources such as environment variables and HTTP request bodies to exec sinks. A sanitization function only breaks the taint chain if its return value has no data-flow path back to user input.

Returning `m[1]` from `regexp.FindStringSubmatch(shell)` is insufficient because the submatch is still derived from `shell`. Returning the `/etc/shells` map key `s` works because CodeQL does not propagate taint through equality comparisons in a range loop: `s` originates from file I/O, not user input.

For the same reason, `os.Getenv("SHELL")` is not used as a default shell. Environment variables are taint sources in CodeQL's model. The default must remain the hardcoded literal `"/bin/sh"`.

### Tmux session name (`tmux`, `ssh_tmux` sessions)

`validTmuxSessionName` in `internal/session/tmux_ssh.go` uses a strict regex (`^[a-zA-Z0-9_.-]+$`)
validated at construction time. Local tmux arguments are discrete `exec.Command` args. Remote tmux
commands single-quote the validated name; when browser interception is enabled the fixed POSIX
setup and quoted operands are passed as one line to `/bin/sh`, just as for an ordinary SSH pane.
See [new remote tmux interception](url-open.md#new-ssh_tmux-sessions) for its session-local
environment and pane-local passthrough scope.

**The task dashboard's attach** (`POST /api/tasks/attach`, [behavior](../behavior/tasks.md#post-apitasksattach))
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
[behavior](../behavior/tasks.md#opening-a-host)) starts an `ssh` or `ssh_tmux` session on a
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

### Remote path arguments (SSH working directory)

When an SSH or SSH+tmux pane has `cwd` set, the path is passed as part of a remote shell command (`cd <cwd> && exec $SHELL`). User-supplied paths that flow into `sess.Start()` must be validated with `validRemotePath` in `internal/session/ssh.go` before use.

`validRemotePath` is a regex guard:

```text
^(/[^;|&$\`'"<>()\[\]{}!\\\x00-\x1f\x7f]*)+$
```

It accepts only absolute Unix paths and rejects shell metacharacters and control characters. This is the CodeQL-recommended sanitization pattern for shell arguments.

After validation, the path is wrapped with `shellQuotePath`, which single-quotes the value and escapes any interior single quotes. This keeps paths containing spaces or unusual but allowed characters safe when embedded in a shell string.

**A path the remote host itself reports goes through the same guard.** Pane-header metadata resolution
asks a host where a Claude session's transcript directory is when the name panemux derives is wrong
(`remoteClaudeProjectProbeCmd` in `internal/session/ssh.go`; see
[behavior.md](../behavior.md)'s "Pane Git and PR metadata"). Two values cross a trust boundary there and
both are handled where they cross it:

- **Into** the probe: the session id, which `validClaudeSessionID` (`^[a-zA-Z0-9_-]+$`) has already
  allowlisted before this point and which is `shellQuotePath`-quoted here. The only unquoted part of
  that command is the `*`, which has to expand; the projects root is a compile-time literal.
- **Out of** it: the path the host prints, which is then built into the `stat`/`cat`/`ls` commands
  that read the transcript. It is checked with `validRemotePath` before any of that — a
  regex-allowlist branch ahead of the sink, which is the shape this repository accepts, rather than
  quoting alone — and must additionally name this session's own `<sessionId>.jsonl`, so a host
  answering with some other session's transcript is ignored rather than read.

### SSH private key paths and an unresolvable home directory

Three SSH-adjacent paths are resolved against the user's home directory. A home-directory lookup
failure must never fall through to `filepath.Join("", ".ssh", "id_ed25519")`, which produces the
working-directory-relative `.ssh/id_ed25519` and could read a project-local key.

For two of them that reached a private key:

- `buildAuthMethods` in `internal/session/ssh.go` probes OpenSSH's default key names when a pane
  configures neither `key_file` nor `password`. With no home directory it read
  `./.ssh/id_ed25519` — a key belonging to whatever project panemux happened to be launched from,
  or one an unrelated process had placed there — and authenticated the outbound SSH connection with
  it. It now skips the probe entirely when the home directory does not resolve, which surfaces as
  the existing `no auth methods` error.
- `resolveSSHConfig` in `internal/session/factory.go` expands an `IdentityFile` read out of the
  user's `~/.ssh/config`, where both a `~/`-prefixed and a bare relative value are defined by
  OpenSSH as relative to the home directory. Both became working-directory-relative. The path is
  now left exactly as the ssh config wrote it, rather than rebuilt against an empty home.

**Leaving the path alone is not by itself the safety property.** An unexpanded
`~/.ssh/id_ed25519` and an already-relative `.ssh/id_ed25519` are both still relative paths, and no
syscall treats `~` as the home directory, so `os.ReadFile` resolves either against the working
directory just as `.ssh/id_ed25519` was resolved before. The bare-relative form is the more
dangerous of the two, because `.ssh` is an ordinary directory name.

What closes it is `requireAbsolutePath` in `internal/session/ssh.go`, applied at the two reads:
`buildAuthMethods` refuses a `KeyFile` that is not absolute, and `resolveKnownHostsFile` refuses an
explicitly configured `known_hosts` file that is not absolute. Every route that produces one of
these paths yields an absolute path when it works — an operator writing one, `internal/config`'s
`expandTilde`, or `resolveSSHConfig`'s `expandIdentityFile` — so a relative path at the read means an
expansion that could not happen, never a path worth trying. This is the same absolute-path-first
shape `validateShell` and `validRemotePath` already use. The known_hosts half is included because the
consequence is the same class: a `known_hosts` file read out of the working directory decides
host-key verification. `TestBuildAuthMethods_NonAbsoluteKeyFile_IsRefusedRatherThanReadFromTheWorkingDirectory`
plants a readable key at both relative paths first, so it fails if the guard is removed rather than
merely failing to find a file.

Note the accepted behavior change: a `key_file` or `known_hosts_file` deliberately written as a
working-directory-relative path in `config.yaml` is now refused. Every documented form
([behavior.md](../behavior.md)'s table and examples, README's) is `~/`-prefixed or absolute, and a
server process's working directory is not a place credentials are kept on purpose.

The failure needs no attacker to arrange the missing home directory — a systemd unit with no `HOME`,
or a container with no passwd entry for the uid, is enough — but it does need one to place a key
where panemux will be started, so it is recorded here as a hardening fix rather than a disclosed
vulnerability. `internal/config`'s own `~/` expansion had the same shape without the credential
consequence, and was fixed the same way (`expandTilde`, which leaves the `~/` in place).

The general rule this leaves behind: **`os.UserHomeDir` returning an error means there is no home
directory, never that the home directory is `""`.** Do not join against the value it returns
alongside an error. The home directory is reached through `internal/homedir` (see DEVELOPMENT.md's
testability rule), which is also what makes these paths testable at all — the failure arms above had
never been executed by a test before, because nothing could reach them.

### SSH `ProxyCommand`

`dialViaProxyCommand` in `internal/session/ssh.go` runs a `ProxyCommand` through
`exec.Command("/bin/sh", "-c", cmd)` after `substituteProxyCommand` has put the connection's host in
place of `%h` and its port in place of `%p` by plain string replacement. The shell re-interprets the
whole string, so both the command and what is substituted into it must come from the operator's own
SSH configuration and nothing else:

- The command comes only from a `ProxyCommand` directive in the SSH config file. `ssh_connections`
  has no field for one, and no API writes one. The one API that writes the file,
  `POST /api/ssh-config/hosts`, writes each value as the rest of a single line, so
  `sshconfig.CheckHostValues` refuses a name, hostname, user or identity file containing a control
  character — a line break in one used to add a directive of the caller's choosing, `ProxyCommand`
  included. The handler answers `422` before anything is written, and `AppendHost` checks again
  itself. `TestPostSSHConfigHost_LineBreakInAValue_422` and
  `TestAppendHost_RefusesAControlCharacterInAValue` pin both.
- `%h` is the `HostName` of the same `Host` block. `resolveSSHConfig` in
  `internal/session/factory.go` does not let an `ssh_connections` entry that sets its own `host`
  inherit the block's `ProxyCommand` (or its `ProxyJump`), so a `host` from `config.yaml` is never
  substituted into a command written for another host. `%p` is an integer.
- The SSH config file is read only from an absolute path. `sshconfig.DefaultPath` returns the
  relative `.ssh/config` when the home directory cannot be resolved; `lookupSSHConfigHost` refuses to
  read it, since that would take a `Host` block — and its `ProxyCommand` — from whatever directory
  panemux was started in, the same failure as the key paths above.
  `TestResolveSSHConfig_RelativeSSHConfigPath_IsNotRead` plants such a block in the working
  directory and checks it is not used.

`ProxyJump` chains are resolved with the names already on the chain carried along, and one that comes
back to a name on it is an error: a cycle used to recurse until the goroutine's stack overflowed, which
is fatal to the whole process and reachable from any request that resolves the connection.

### Launching the operator's browser (`--open`)

`openChrome` in `main.go` runs the platform's browser opener against panemux's own listen address
when `--open` is given. Its first `exec.Command` argument is a **variable**, which is why it is
recorded here: that is the shape the security design's [General Rules](../security.md#general-rules)
require an argument for, and a reader enumerating `exec.Command` sinks must find it rather than
conclude from [command-center.md](command-center.md#command-center-subprocess-execution) that only
two exist.

The argument for it is short. `browserOpenArgv(goos, url)` is the only source of both the program
name and the arguments, it is pure, and it returns one of exactly three compile-time literals
(`open`, `google-chrome`, `cmd`) or reports that the OS is unsupported — in which case nothing is
launched rather than a guess being executed. `url` is `"http://" + srv.Addr()`, panemux's own
host:port. Every value travels as a discrete argv element; no shell parses any of it. Splitting the
decision out from the exec call is also what makes each per-OS branch testable
(`TestBrowserOpenArgv`), including `TestBrowserOpenArgv_URLStaysADiscreteArgument`, which pins that
the URL is never spliced into another argument.

Before that split the three branches each called `exec.Command` with a literal name, so no variable
first argument existed. The `//nolint:gosec` on the call therefore keeps its reason inline rather
than being a bare suppression: what makes it safe is where `name` comes from, and that is not
visible at the call site.

### Task dashboard collection

The task dashboard ([behavior](../behavior/tasks.md)) runs commands on the panemux host and on every
`ssh_connections` host. There are three sinks, and none of them carries a value from a request, the
config, or a remote host into a command string:

- **The collection script** (`collectScript` in `internal/tasks/collect.go`) is a compile-time
  constant, as is `attentionScript`, which the task event stream observes with: the same constant
  live part (`collectLiveScript`) without the searches for stopped sessions, run exactly the same way.
  The stream runs it only while it has a subscriber, at most once at a time per host, and no more often
  than every 5 seconds per host ([Task events](../behavior/task-events.md#lifecycle)); how many tabs
  subscribe does not change that. Locally it runs as `exec.CommandContext(ctx, "sh", "-s")` — a literal program and a
  literal argument — with the script written to stdin. Remotely the exec request is the literal
  `sh -s` with the same script on stdin, so the remote login shell parses only `sh -s`, whatever
  shell it is. `HOME` is the one environment value the local run sets, from `internal/homedir`, and
  it selects which files are read, not what runs. The local run is its own process group, killed as
  a group when its context ends, so no probe the script started outlives the collection.
- **The hosts** are the `ssh_connections` entries, which `/api/config/ssh-connections` can add and
  edit ([REST API](../behavior/rest-api.md#apiconfigssh-connections)). An entry is connection data
  handed to the pane dialer, never a command: there is no field for `ProxyCommand` or `ProxyJump`,
  and an entry that sets its own `host` takes neither from `~/.ssh/config` (see
  [SSH `ProxyCommand`](#ssh-proxycommand)). The routes refuse cross-site requests, refuse control
  characters in `host`, `user` and the two paths, never return a saved password, and name fields
  rather than values in their errors.
- **Remote git inspection** reuses `remoteGitContext`, the command an `ssh` pane's header runs:
  the working directory reported by the remote host passes `validRemotePath` and is quoted with
  `shellQuotePath` before it reaches the command, exactly as a pane's does.
- **Local git and `gh pr view`** reuse the pane header's lookups: `git` runs with the directory as
  `cmd.Dir` after `sanitizeGitExecDir`, and `gh` receives the branch and repository as discrete
  argv elements. The `--json` field list is a constant (`prTaskFields`), not a value from anywhere.

The script lists only the collecting user's processes (`ps -U "$(id -u)"`). On a shared host,
another user's processes are neither shown as tasks nor accepted as the live process behind a
leftover state file whose pid they reused.

**Codex probes** ([issue #264](https://github.com/tomo-chan/panemux/issues/264)). For each of those
processes whose program is named `codex`, the script lists the files it holds open — `readlink` on
`/proc/<pid>/fd/*`, or `lsof -p <pid> -Fn` where there is no `/proc` — and keeps only names of the
form `rollout-*.jsonl`. It reads those files and the rollouts under `~/.codex/sessions` with `head`,
`tail`, `grep` and `awk` only, and prints fixed fragments of them; nothing read from a rollout is
executed or reaches a command line. One value is put into a command: the session ID, taken from the
rollout's file name, goes into the fixed query
`sqlite3 -readonly ~/.codex/thread_history_1.sqlite "select … where thread_id = '<id>' …"`. The
script first strips the name down to the part after `rollout-YYYY-MM-DDTHH-MM-SS-` and refuses it
(`case $id in *[!0-9a-f-]*|'') id= ;; esac`) unless it is made only of lowercase hex digits and
dashes — the shape of the UUID codex writes there — so it can hold neither a quote to end the SQL
literal nor anything the shell would expand; the query string is double-quoted and `sqlite3` gets it
as one argument, with `/dev/null` on stdin. `-readonly` keeps the database, which codex itself has
open, from being written. The Go side accepts a session ID from a rollout name only when it is a UUID
(`rolloutSessionID`), and reads the fragments with fixed regular expressions, never as commands.

`GET /api/tasks`, the reconnect route and the task event stream `GET /ws/tasks/events` are
unauthenticated like the rest of `/api/*` and `/ws/{sessionID}`, but a GET that dials every host is a
side effect another site could trigger with an `<img>` or a `WebSocket`. These three routes,
`PUT /api/tasks/records`, which writes the operator's record file, and the two routes that start and
resume tasks ([below](#task-launch-and-resume)) therefore refuse a request whose
`Sec-Fetch-Site` is `cross-site` or `same-site`, or whose `Origin` hostname and effective port
do not match the request Host (the shared `internal/requestsecurity` guard), before collecting or dialing anything. The record route runs no command:
it writes `~/.config/panemux/tasks.json` through `fileops.AtomicWrite`, and a label reaches the
browser only as text, never as markup or a URL.
The response itself is never readable cross-site — no CORS header is sent — so this protects the
side effect, not the data. A WebSocket is not subject to CORS, so for `/ws/tasks/events` the refusal
also keeps the task states from another site; the frames carry no conversation text, prompt, command
line or process ID ([Frames](../behavior/task-events.md#frames)).

Everything a host prints back is untrusted input. The parser (`parseCollectOutput`) accepts only its
own line protocol, skips malformed rows, accepts a session ID only if it matches
`^[a-zA-Z0-9_-]+$`, and treats a tmux session name as display text: the dashboard offers to attach a
pane to it only when it matches the pane's own `validTmuxSessionName` rule, which the pane then
enforces again when it starts.

The `PANEMUX_PANE_ID` an agent's environment carries is untrusted twice over: the host prints it, and
any process of the user can set it to anything. The script reads it only for the processes it
already lists, and prints it as data: with `tr`/`grep` from `/proc/<pid>/environ` on Linux, and on
macOS from `ps -E -p <pid> -o command=` with shell parameter expansion and `case` alone, the pid
coming from the script's own `ps` listing. On macOS the arguments `ps -p <pid> -o command=` reports
are removed from the front, so an argument cannot supply the value; more than one
` PANEMUX_PANE_ID=` in what remains — one may sit inside another variable's value — yields nothing;
and the shell prints the value only when it consists of `A-Za-z0-9_.-`, so a newline in it cannot
forge a row. What macOS does **not** prevent: a process with no `PANEMUX_PANE_ID` of its own but
exactly one ` PANEMUX_PANE_ID=<id>` inside another variable's value — preceded by a space in that
value, as in `AAA='x PANEMUX_PANE_ID=<id>'`, including a space that follows a newline — reports
`<id>`, because `ps -E` prints it and the real entry alike. Directly after a newline, with no
space, it is not read. Linux, reading NUL-separated entries, reports nothing for either. This is accepted
([decision log](../DECISIONLOG.md#reading-panemux_pane_id-on-macos-through-ps--e-2026-09-27-issue-263)):
only someone who can already start an agent as the same user outside a pane can plant such a value,
and a process of that user can set `PANEMUX_PANE_ID` itself anyway; the result is at most a
`Go to pane` that focuses another existing pane, and no command runs.
`TestRunLocal_ReadsThePaneIDFromPsEOnMacOS` pins it. The parser keeps it
only if it matches `^[A-Za-z0-9_.-]{1,128}$` (`validPaneID`); and it never reaches a command. The
browser uses it only to look up a `local` or `ssh` pane of the task's host in the workspaces it
holds, and going to that pane only focuses it.

Setting the variable is a sink of its own. A `local` pane receives it as an `exec.Cmd.Env` entry.
An `ssh` pane receives it in its remote shell command (`remotePaneIDSetup` in
`internal/session/paneid.go`): the pane ID is exported only when it matches the same rule
(`validPaneEnvID`), whose characters are all inert in a shell, and it is quoted with
`shellQuotePath` as well. A pane whose ID does not match gets no variable rather than an escaped one.

That command, like any remote pane command carrying setup, is POSIX script passed whole to
`/bin/sh`: `exec /bin/sh -c ` followed by the script quoted once more with `shellQuotePath`
(`sshShellCommand` in `internal/session/ssh.go`), so the remote login shell — which need not be
POSIX — parses only `exec`, a literal path and one single-quoted word. The script is kept to one line
with no `!`, which csh-family shells expand even inside single quotes: remote paths and pane IDs are
validated to exclude both, and the browser shim's body is written as a `printf` format of octal
escapes (`printfOctalFormat`).

The dashboard's links are `href`s the operator clicks, so none may carry a scheme a browser would run.
An issue URL from `gh` is kept only when it is `http` or `https` (`closingIssueLinks`), and
a `task_dashboard.autolinks` `url_template` is refused at load unless it is an `https` URL whose
host and port the browser's URL parser also accepts, with `<num>` after the host
(`validAutolinkURLTemplate`), so an identifier cannot change where a link goes. The browser checks
every link in the response again (`HttpUrlSchema`). The identifier put in place of `<num>` is only
ever digits, or letters, digits and `-` (`isAutolinkIDByte`).

**Unreadable state files** ([behavior](../behavior/tasks.md#unreadable-state-files)) carry two values
from the host to the API, the browser and the server log: the file name and a detail that may quote
what the file holds. Neither reaches a command. Both are made valid UTF-8 with every control and
invisible format character replaced by U+FFFD and cut to 128 and 120 characters
(`boundedText` in `internal/tasks/state_file.go`) before they leave `internal/tasks`; the log
quotes them with `%q` as well, and the dashboard renders them as text only, never as markup or a
link. The details never carry a file's whole content.

### Task launch and resume

`POST /api/tasks` and `POST /api/tasks/resume` ([behavior](../behavior/tasks.md#starting-a-task))
start a claude or codex process on the panemux host or an `ssh_connections` host, from values a
request supplies: a host, an agent, a working directory, a first instruction and a session ID. Unlike the command
center, which hands `exec.CommandContext` an argv, a remote start has to cross the remote login
shell, and tmux runs what it is given as its own child. The design keeps every request value out of
anything a shell parses, rather than escaping it.

**One fixed script, run as `sh -s`.** `launchScriptTemplate` in `internal/tasks/launch.go` runs
exactly as the collection script does: locally `exec.CommandContext(ctx, "sh", "-s")`, remotely an
exec request whose command is the literal `sh -s`, the script on stdin in both cases. The remote
login shell parses only `sh -s`. The template is a compile-time constant; `buildLaunchScript` fills
seven placeholders, each of which is checked first and refused rather than escaped:

| Placeholder | Source | Check |
|---|---|---|
| agent | `claude` or `codex`, a Go constant chosen from the request's value | one of the two (`checkLaunchSessionID`) |
| mode | `new` or `resume`, a Go constant | — |
| session ID | minted by panemux (`newSessionID`, crypto/rand) for a new claude task; none for a new codex task, whose ID codex picks; for a resume, the request's value | A UUID (`validUUID`); empty for a new codex task |
| tmux session name | `task-` and the session ID's first eight characters (claude), its last eight (a codex resume), or eight hex digits from crypto/rand (a new codex task) | `validTmuxSessionName` |
| heredoc tag | 32 hex characters from crypto/rand | `^[0-9a-f]{32}$` |
| working directory | the request, or for a resume the host's conversation log | `session.ValidateRemotePath`, the guard a pane's remote `cwd` passes |
| first instruction | the request | not empty, at most 32 KiB, no NUL, and no line equal to its own heredoc terminator |

The working directory and the instruction are placed in heredocs whose terminator is quoted
(`<<'PANEMUX_CWD_<tag>'`, `<<'PANEMUX_PROMPT_<tag>'`), so the shell performs no expansion inside
them, and read into variables that are only ever used double-quoted. The session ID and tmux name are
single-quoted literals, and both allowlists exclude a quote.

**The program is the literal name `claude` or `codex`.** The script resolves it with
`command -v "$agent"`, and, when that finds nothing, with `"${SHELL:-/bin/sh}" -lc "command -v $agent"`,
because a per-user install is usually on a `PATH` only a login shell sets. `$agent` is the fixed
placeholder above, one of two words. `$SHELL` here is the host user's own login shell, run with a
fixed argument; it chooses where the agent is looked up, not what is run instead of it. What either
lookup prints is used only if it is an absolute path to an executable regular file, and it reaches
tmux as a discrete argument. For codex the directory of that path is added at the **end** of the
`PATH` the agent runs with (`PATH=$PATH:${2%/*}` inside the fixed `sh -c`): an npm install's `codex`
is a `#!/usr/bin/env node` script, and a tmux server an exec channel started does not have node's
directory on its `PATH` — checked on Linux, where `env -i PATH=/usr/bin:/bin <npm prefix>/bin/codex`
failed with `env: 'node': No such file or directory` and the launch with the directory added
started codex. The directory is the one holding the executable already chosen, not a request value.
It goes last so that it only fills a gap: put first (as in the first version, found in review), it
also moved every command codex and its model run ahead to that directory — a `python3` beside
codex won over the one the user's `PATH` names, and a directory others can write would have been
searched before the system's. Last, it is searched only for what nothing earlier provides. This is the script on the host reading its own
environment, not a Go `os.Getenv` value flowing into `exec.Command`, which the
[General Rules](../security.md#general-rules) forbid.

**tmux runs the command without a shell.** `tmux new-session -d -s <name> -- <command...>` with the
command as separate arguments executes it directly (tmux 2.0 and later; verified on tmux 3.4 with an
argument holding `$(id)`, `; echo`, and a quote, all of which arrived unchanged). The `--` keeps an
argument from being read as a tmux option.

**No request value goes to a tmux option that tmux expands.** `-c` looks like a plain directory
argument but is a format: tmux 3.4 turned `/…/proj#Sx` and `/…/a##b` into other paths, found them
missing, and started the command in the home directory — reported as success, with the script's own
`cd` to the directory having succeeded. `validRemotePath` refuses `$ ( ) { } [ ]`, so `#(…)` cannot
run a command and this was never code execution, but claude would have run in a directory nobody
chose. The working directory therefore reaches the session only as a positional parameter of the
fixed `sh -c`, which changes to it (`cd -- "$4"`, and `cd -- "$1"` for a resume) before `exec`.
`TestLaunchScript_RealTmuxStartsClaudeInADirectoryHoldingAHash` runs this against the real tmux where
one is installed, and the stand-in tmux the other tests use refuses `-c` outright. (A pane's own
`tmux new-session -c` in `internal/session` has the same property and is outside this section.)

**A resume can add a window to an existing session of the task's name**
(`tmux new-window -t "=<name>:" -- sh -c …`), with the same arguments as a new session. The script is
told whether it may with a fixed `yes`/`no` that `Service.Resume` sets from the collection it has
just made: only when no running task was found inside that tmux session. Nothing already in the
session is replaced or killed. Because that decision rests on a collection made before the launch,
the resumes of one (host, session) are serialized inside panemux (`Service.lockResume`): a second one
collects only after the first has launched, sees its claude, and is refused rather than adding a
second claude to the same conversation.

**The first instruction reaches claude as a file, then as one argument after `--`.** The script writes
it to a `mktemp` file created under `umask 077`, and the tmux command is a fixed
`sh -c 'p=$(cat -- "$1"); rm -f -- "$1"; cd -- "$4" || exit 1; exec "$2" "--session-id=$3" -- "$p"'`
whose positional parameters are the file, the resolved claude path, the session ID and the working
directory. Command substitution's result is
not parsed again, and `--` ends claude's options: [command-center.md](command-center.md#command-center-subprocess-execution)
records that claude's parser scans all of argv for options, so a prompt beginning with `-` needs it.
Keeping the instruction out of tmux's arguments matters beyond parsing: a tmux server's process
arguments are those of the client that started it, for as long as the server runs, so an instruction
there would be readable in `ps` indefinitely. Measured with a real tmux 3.4, the server's arguments
held the file name and nothing of the instruction. The instruction is still claude's own positional
argument, the form in which the launch gives an interactive claude its first message, so a user who
can read claude's arguments on the host can read it while claude runs.

**Codex gets its first instruction after `--` too, with `--no-daemon` and
`-c check_for_update_on_startup=false`.** The fixed command for a new codex task is
`sh -c 'p=$(cat -- "$1"); rm -f -- "$1"; cd -- "$4" || exit 1; PATH=$PATH:${2%/*}; export PATH; exec "$2" --no-daemon -c check_for_update_on_startup=false -- "$p"'`,
with the same positional parameters (the session ID one is empty). The handling of `--` was verified
with codex-cli 0.142.2 and 0.157.1 on macOS and 0.157.1 on Linux, before `--no-daemon` was added:
without `--`, `codex '-x hello'` is refused as an unknown option, and after `--` a prompt such as
`-h -V --help: …` is sent as the first message. The whole command, `--no-daemon` included, was
verified with 0.157.1 on Linux. codex-cli 0.142.2 has no `--no-daemon` and exits 2 on it, so the
script first runs `"$bin" --help` (with the same `PATH` addition) and refuses with `codex-too-old`
unless the help lists `--no-daemon`; the help's text is only searched, never shown. The `-c`
value is a fixed literal; it stops the update prompt, which would otherwise hold a task nobody is
watching. codex's other start-up screens (trusting a directory, a new-model notice, a usage-limit
offer) are left for a person to answer in the pane: panemux does not write codex's configuration.

**A codex resume passes the ID after `--`**: `codex --no-daemon -c check_for_update_on_startup=false resume -- <id>`.
`--no-daemon`, a fixed flag, keeps the session in the TUI's own process: through codex's shared
daemon nothing would tie the session to the task's tmux session.
Without `--`, `codex resume <id> '-h …'` prints its help and exits 0 without opening the session, and
an ID beginning with `-` would be an option. `codex resume` also accepts a session *name* (names are
set by `/rename` and, with a real model, automatically from the first instruction, and may hold
spaces); only a UUID is accepted, which codex resolves as an ID before any name ("UUIDs take
precedence if it parses"). As for claude, `Service.Resume` resumes only a session the host's own
collection lists as a stopped codex task, in the working directory its rollout's `session_meta`
records, which passes the remote-path guard.

**A resume passes the ID as `--resume=<id>`, and only an ID the host listed.** Verified against claude
2.1.283: `claude --resume --version` printed the version, so `--resume`, whose value is optional,
lets a following argument that begins with `-` be read as an option, and `validSessionID`
(`^[a-zA-Z0-9_-]+$`) admits `--dangerously-skip-permissions`. The `=` form keeps it a value:
`claude -p --resume=--version x` was refused because the value is neither a UUID nor a session
title. `--resume` also matches a session title, so only a
UUID is accepted. Besides, `Service.Resume` collects the host again and resumes only a session listed
there as a stopped claude task, with the working directory from that session's own log — which is
host output and passes the same remote-path guard before use.

**Slash commands are not disabled**, unlike the command center's `claude -p`: this is an interactive
session the operator watches and types into through a pane, and disabling them would remove them from
that pane as well.

**What the host prints back** is searched only for the script's own `::panemux-launch` line; a refusal
is one of eight fixed words, each mapped to a fixed message (`LaunchError`), so no host text reaches the
response. A host that prints no answer is a failed launch.

The labels a start records go through `NormalizeLabels` before anything runs, so a refused label
cannot leave a started task without them. A codex task's labels are held in panemux's memory, keyed
by host and tmux session, and recorded by the first collection that finds a codex session in that
tmux session; the session ID they are recorded under is one the collection read from the host.

`TestLaunchScript_NewTaskHandsThePromptToClaudeAsOneArgumentAfterTheEndOfOptions` runs the real script
under `sh` against stand-ins for tmux and claude that record their arguments, with a prompt holding a
leading option, command substitutions, quotes and a line reading `EOF`, and fails if claude's argv is
not exactly `--session-id=<id>`, `--`, the prompt, if the prompt reaches tmux's arguments, if a
substitution ran, or if the file is left behind. `TestLaunchScript_NewCodexTask` and
`TestLaunchScript_ResumeCodexTask` do the same for codex (argv exactly
`--no-daemon -c check_for_update_on_startup=false -- <prompt>`, and `… resume -- <id>`, with codex's
directory last on `PATH`). `TestBuildLaunchScript_RefusesInputBeforeAnythingRuns` and
`TestBuildLaunchScript_AgentRules` cover every refusal above.

### Task summaries

When `task_dashboard.summary.enabled` is set, the dashboard summarizes each Claude or Codex task
([behavior](../behavior/tasks.md#summaries)). That adds two sinks: a script that reads a conversation
log on a host, and the matching agent CLI process on the panemux host whose input is text from that log —
text written by whoever and whatever took part in the conversation, which panemux does not control.

**The log is read by one fixed script, run as `sh -s`**, like the collection and the launch.
`transcriptScriptTemplate` in `internal/tasks/transcript.go` is a compile-time constant with four
placeholders: three byte counts that are Go constants, and the session ID, which must match
`validSessionID` (`^[a-zA-Z0-9_-]+$`, no quote) and is single-quoted. It is used only inside the
double-quoted path `"$HOME"/.claude/projects/*/"$sid.jsonl"`, so a leading `-` cannot become an
option. A summary is only ever made for a session the host's last collection listed with a log.
The script prints a header with the log's size and then that many bytes; `parseTranscriptOutput`
reads the body by that length and never searches it for a marker, since the log can hold any text.

**claude is given text, never tools or configuration.** `newClaudeSummarizer` in
`internal/tasks/summarize.go` runs the literal `claude` with `exec.CommandContext` — an argv, no
shell — in an empty temporary directory, with the excerpt on stdin and a compile-time instruction as
the only prompt argument, after `--`. The argv follows the command center's, whose every flag was
checked against the real CLI in [command-center.md](command-center.md#command-center-subprocess-execution):

| Argument | Why |
|---|---|
| `--session-id <minted UUID>`, `--no-session-persistence` | A session of panemux's own, not the ambient one, and nothing written under `~/.claude` |
| `--setting-sources ""` | None of the operator's settings, hooks or `CLAUDE.md` |
| `--strict-mcp-config` | No MCP server |
| `--disable-slash-commands` | A `/command` in the conversation stays text |
| `--disallowedTools=<commandcenter.DisallowedTools()>` | Every tool that can execute, write, read files, reach the network or start another agent is refused by name — the denial the command center found survives a permissions override |
| `--output-format=json`, `--json-schema <schema>` | The answer is parsed as a structure, and bounded before it is shown |

The instruction tells claude that the excerpt is data to describe and not instructions. That is not
relied on: a conversation that talks claude into ignoring it can change the summary text and the
list of remaining work — and so, by emptying that list, whether the task is shown as a done
candidate. All of it is rendered as text, and a done candidate is only a label: marking a task done
takes a person's click, so the most such a conversation can do is mislead what the dashboard shows. `--tools ""` (no tools at all) was considered; whether the CLI reads an
empty value as "none" could not be verified here, so the verified denial list is what ships.

**What is sent is bounded and excludes tool output.** Only the text of user and assistant messages is
extracted (`buildExcerpt`); tool calls and results, thinking and attachments — where file contents,
command output and credentials sit, including an attachment whose kind is `credential_org` in the
log this was built against — are never read. The first user message (4 KiB) and the newest messages
(24 KiB, 2 KiB each) are all that reach claude. What a person typed or an assistant quoted is not
masked; the behavior document says so, and that is why summaries are off by default.

**What is kept on disk is the answer, never the conversation.** `SummaryStore` in
`internal/tasks/summary_store.go` saves each summary's text, remaining work and when it was made in
`~/.config/panemux/task-summaries.json` (mode 0600, written with `fileops.AtomicWrite`), with the
SHA-256 of the excerpt it was made from and the log's modification time, size and Codex rollout name.
Neither the log nor the excerpt is written. A summary is the agent's paraphrase of the conversation and
can repeat what was typed in it, so the file is as private as the dashboard: it stays on the panemux
host, nothing outside panemux reads it, and with summaries off it is neither read nor written. Nothing
from it reaches a command: a stored log version is only compared with the collected one, and the
scripts are always built from the collection. A file that does not parse, has another format version,
or holds an entry panemux would not have written (an unknown agent, a session ID failing
`validSessionID`, a malformed hash, a summary longer than the bounds above) is renamed aside unread.

**Nothing claude prints reaches the operator's screen except the parsed answer.** A failure is one of
a few fixed messages (an exit status, a timeout, an answer that was not JSON or had no summary),
because claude's own text can quote the conversation. The answer is trimmed to 1 KiB of summary and
10 items of 300 bytes.

**The process is bounded.** It runs in its own process group, which is killed when its 2-minute limit
passes or panemux shuts down; at most two run at once.

`TestSummaryArgs` pins the argv, `TestBuildExcerpt_KeepsOnlyConversationText` that tool output,
attachments and thinking never reach the excerpt, `TestParseSummaryOutput_Errors` that claude's text
is not passed on, and `TestBuildTranscriptScript` that an unsafe session ID is refused.

#### Codex summary runner and rollout reader

`internal/tasks/codex_summary.go` supplies a separate Codex runner. It runs the literal `codex`
on the panemux host through `exec.CommandContext`, with no shell, in an empty temporary working
directory. Its fixed argv is `exec --ephemeral --skip-git-repo-check --color never --output-schema
<private schema file> --output-last-message <private answer file> -`. The fixed summary instruction
and bounded conversation excerpt are stdin. No model, profile, approval bypass, sandbox override or
user-config suppression is added. The operator's normal authentication, user/global instructions,
MCP, hooks and managed policies apply; those settings may affect model input, cost and actions.
The temporary working directory does not inherit the task's project configuration.

The instruction requests only a summary and explicitly asks for no tools or action on conversation
instructions. Unlike the Claude denial list, this is not a guarantee that all acting tools are
removed: Codex uses the operator's configured execution restrictions and its non-interactive exec
approval behavior. A failed non-interactive run or unsupported CLI becomes a fixed error; the app
does not approve requests or relax permissions. Authentication is neither copied nor rewritten.
`--ephemeral` avoids persisting the summary session, not every CLI cache or runtime file.

CLI stdout/stderr are discarded. The answer is opened without following a final symlink and with
nonblocking open flags; the opened descriptor must be a regular file before any bytes are read.
Path replacement cannot turn a prior pathname check into a blocking FIFO open or special-file read.
The open/stat/read operation stays within the summary context deadline, including after the CLI
exits; a late filesystem operation closes its own handle without retaining a summary slot.
Only the final answer file is read, up to 64 KiB even if it grows during the read, and its required
JSON fields/types are checked before applying the same 1 KiB/10 items/300 bytes display limits.
Setup, execution, timeout and parse errors expose no CLI text or local paths. Cancellation kills
the runner's process group, as for Claude.

`buildCodexTranscriptScript` uses the existing framed head/tail reader with a fixed standard
`~/.codex/sessions/*/*/*/rollout-*-<UUID>.jsonl` glob. A non-UUID is rejected before execution.
`buildCodexExcerpt` keeps only `response_item` payloads of type `message`, user `input_text` and
assistant `output_text`. Calls/results, reasoning, developer instructions, attachments and
`event_msg` mirrors are omitted. Complete leading AGENTS/environment wrappers are excluded from
the source conversation; this does not suppress the summary runner's own inherited instructions.
Repeated IDs are deduplicated, identical messages with distinct/no IDs are retained, and cut lines
are skipped. The existing first/recent text budgets still apply; unknown formats are never sent raw.

These primitives are covered by `TestBuildCodexExcerpt_*`, `TestBuildCodexTranscriptScript`,
`TestRunLocal_CodexTranscriptReadsNewestAndBounds`, `TestCodexSummarizer_*` and
`TestParseCodexSummaryOutput`. The service routes by host, agent and session; Codex never falls back to Claude. The reader pins
the collected basename and version, validates session_meta.id, and rejects a change before or
during the read instead of substituting another rollout.
