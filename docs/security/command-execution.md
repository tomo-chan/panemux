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

`validTmuxSessionName` in `internal/session/tmux_ssh.go` uses a strict regex (`^[a-zA-Z0-9_.-]+$`) validated at construction time. Arguments are passed as discrete `exec.Command` args, not via `sh -c`, so no shell interpolation occurs.

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
  constant. Locally it runs as `exec.CommandContext(ctx, "sh", "-s")` — a literal program and a
  literal argument — with the script written to stdin. Remotely the exec request is the literal
  `sh -s` with the same script on stdin, so the remote login shell parses only `sh -s`, whatever
  shell it is. `HOME` is the one environment value the local run sets, from `internal/homedir`, and
  it selects which files are read, not what runs. The local run is its own process group, killed as
  a group when its context ends, so no probe the script started outlives the collection.
- **Remote git inspection** reuses `remoteGitContext`, the command an `ssh` pane's header runs:
  the working directory reported by the remote host passes `validRemotePath` and is quoted with
  `shellQuotePath` before it reaches the command, exactly as a pane's does.
- **Local git and `gh pr view`** reuse the pane header's lookups: `git` runs with the directory as
  `cmd.Dir` after `sanitizeGitExecDir`, and `gh` receives the branch and repository as discrete
  argv elements.

The script lists only the collecting user's processes (`ps -U "$(id -u)"`). On a shared host,
another user's processes are neither shown as tasks nor accepted as the live process behind a
leftover state file whose pid they reused.

`GET /api/tasks` and the reconnect route are unauthenticated like the rest of `/api/*`, but a GET
that dials every host is a side effect another site could trigger with an `<img>`. Both routes,
`PUT /api/tasks/records`, which writes the operator's record file, and the two routes that start and
resume tasks ([below](#task-launch-and-resume)) therefore refuse a request whose
`Sec-Fetch-Site` is `cross-site` or `same-site`, or whose `Origin` is neither the server's own nor a
loopback origin (`refuseCrossSite` in `internal/api/tasks.go`). The record route runs no command:
it writes `~/.config/panemux/tasks.json` through `fileops.AtomicWrite`, and a label reaches the
browser only as text, never as markup or a URL.
The response itself is never readable cross-site — no CORS header is sent — so this protects the
side effect, not the data.

Everything a host prints back is untrusted input. The parser (`parseCollectOutput`) accepts only its
own line protocol, skips malformed rows, accepts a session ID only if it matches
`^[a-zA-Z0-9_-]+$`, and treats a tmux session name as display text: the dashboard offers to attach a
pane to it only when it matches the pane's own `validTmuxSessionName` rule, which the pane then
enforces again when it starts.

### Task launch and resume

`POST /api/tasks` and `POST /api/tasks/resume` ([behavior](../behavior/tasks.md#starting-a-task))
start a claude process on the panemux host or an `ssh_connections` host, from values a request
supplies: a host, a working directory, a first instruction and a session ID. Unlike the command
center, which hands `exec.CommandContext` an argv, a remote start has to cross the remote login
shell, and tmux runs what it is given as its own child. The design keeps every request value out of
anything a shell parses, rather than escaping it.

**One fixed script, run as `sh -s`.** `launchScriptTemplate` in `internal/tasks/launch.go` runs
exactly as the collection script does: locally `exec.CommandContext(ctx, "sh", "-s")`, remotely an
exec request whose command is the literal `sh -s`, the script on stdin in both cases. The remote
login shell parses only `sh -s`. The template is a compile-time constant; `buildLaunchScript` fills
six placeholders, each of which is checked first and refused rather than escaped:

| Placeholder | Source | Check |
|---|---|---|
| mode | `new` or `resume`, a Go constant | — |
| session ID | minted by panemux (`newSessionID`, crypto/rand) for a start; for a resume, the request's value | A UUID (`validUUID`) |
| tmux session name | `task-` and the session ID's first eight characters | `validTmuxSessionName` |
| heredoc tag | 32 hex characters from crypto/rand | `^[0-9a-f]{32}$` |
| working directory | the request, or for a resume the host's conversation log | `session.ValidateRemotePath`, the guard a pane's remote `cwd` passes |
| first instruction | the request | not empty, at most 32 KiB, no NUL, and no line equal to its own heredoc terminator |

The working directory and the instruction are placed in heredocs whose terminator is quoted
(`<<'PANEMUX_CWD_<tag>'`, `<<'PANEMUX_PROMPT_<tag>'`), so the shell performs no expansion inside
them, and read into variables that are only ever used double-quoted. The session ID and tmux name are
single-quoted literals, and both allowlists exclude a quote.

**The program is the literal name `claude`.** The script resolves it with `command -v claude`, and,
when that finds nothing, with `"${SHELL:-/bin/sh}" -lc 'command -v claude'`, because a per-user
install is usually on a `PATH` only a login shell sets. `$SHELL` here is the host user's own login
shell, run with a fixed argument; it chooses where `claude` is looked up, not what is run instead of
it. What either lookup prints is used only if it is an absolute path to an executable regular file,
and it reaches tmux as a discrete argument. This is the script on the host reading its own
environment, not a Go `os.Getenv` value flowing into `exec.Command`, which the
[General Rules](../security.md#general-rules) forbid.

**tmux runs the command without a shell.** `tmux new-session -d -s <name> -c <dir> -- <command...>`
with the command as separate arguments executes it directly (tmux 2.0 and later; verified on tmux 3.4
with an argument holding `$(id)`, `; echo`, and a quote, all of which arrived unchanged). The `--`
keeps an argument from being read as a tmux option.

**The first instruction reaches claude as a file, then as one argument after `--`.** The script writes
it to a `mktemp` file created under `umask 077`, and the tmux command is a fixed
`sh -c 'p=$(cat -- "$1"); rm -f -- "$1"; exec "$2" "--session-id=$3" -- "$p"'` whose positional
parameters are the file, the resolved claude path and the session ID. Command substitution's result is
not parsed again, and `--` ends claude's options: [command-center.md](command-center.md#command-center-subprocess-execution)
records that claude's parser scans all of argv for options, so a prompt beginning with `-` needs it.
Keeping the instruction out of tmux's arguments matters beyond parsing: a tmux server's process
arguments are those of the client that started it, for as long as the server runs, so an instruction
there would be readable in `ps` indefinitely. Measured with a real tmux 3.4, the server's arguments
held the file name and nothing of the instruction. The instruction is still claude's own positional
argument, the form in which the launch gives an interactive claude its first message, so a user who
can read claude's arguments on the host can read it while claude runs.

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
is one of six fixed words, each mapped to a fixed message (`LaunchError`), so no host text reaches the
response. A host that prints no answer is a failed launch.

The labels a start records go through `NormalizeLabels` before anything runs, so a refused label
cannot leave a started task without them.

`TestLaunchScript_NewTaskHandsThePromptToClaudeAsOneArgumentAfterTheEndOfOptions` runs the real script
under `sh` against stand-ins for tmux and claude that record their arguments, with a prompt holding a
leading option, command substitutions, quotes and a line reading `EOF`, and fails if claude's argv is
not exactly `--session-id=<id>`, `--`, the prompt, if the prompt reaches tmux's arguments, if a
substitution ran, or if the file is left behind. `TestBuildLaunchScript_RefusesInputBeforeAnythingRuns`
covers every refusal above.
