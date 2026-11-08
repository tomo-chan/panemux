---
type: Security Requirement
title: "Security: SSH execution"
description: "Remote path guards, credential path resolution and SSH proxy command trust boundaries."
---

# Security: SSH execution

See the [General Rules](../security.md#general-rules) and [boundary map](command-execution.md).

## Remote path arguments (SSH working directory)

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
[pane metadata behavior](../behavior/frontend.md#pane-git-and-pr-metadata)). Two values cross a trust boundary there and
both are handled where they cross it:

- **Into** the probe: the session id, which `validClaudeSessionID` (`^[a-zA-Z0-9_-]+$`) has already
  allowlisted before this point and which is `shellQuotePath`-quoted here. The only unquoted part of
  that command is the `*`, which has to expand; the projects root is a compile-time literal.
- **Out of** it: the path the host prints, which is then built into the `stat`/`cat`/`ls` commands
  that read the transcript. It is checked with `validRemotePath` before any of that — a
  regex-allowlist branch ahead of the sink, which is the shape this repository accepts, rather than
  quoting alone — and must additionally name this session's own `<sessionId>.jsonl`, so a host
  answering with some other session's transcript is ignored rather than read.

## SSH private key paths and an unresolvable home directory

SSH credential and configuration reads must not fall back to the process's working directory
when home-directory resolution fails. An error from `os.UserHomeDir` means there is no home,
never that it is `""`: do not join paths against its error result. Resolve home through
`internal/homedir`, as required by [the testability rule](../../DEVELOPMENT.md#test-granularity).

- With neither `key_file` nor `password`, `buildAuthMethods` skips the default-key probe when
  home cannot be resolved, yielding the existing `no auth methods` error.
- `resolveSSHConfig` leaves an `IdentityFile` unchanged when its `~/` or bare-relative value
  cannot be expanded against home. Leaving it unchanged is insufficient protection: both forms
  still resolve relative to the working directory at a file read.
- `requireAbsolutePath` therefore refuses a non-absolute `KeyFile` in `buildAuthMethods` and an
  explicitly configured non-absolute `known_hosts` file in `resolveKnownHostsFile`, before reading.
  Deliberately working-directory-relative `key_file` and `known_hosts_file` values are refused too.
  Successful operator configuration, `expandTilde` and `expandIdentityFile` produce absolute paths.
- `internal/config`'s `expandTilde` also leaves `~/` in place when home cannot be resolved.

A project-local private key could otherwise authenticate an outbound connection, and a project-local
`known_hosts` could decide host-key verification. Missing `HOME` or a uid without a passwd entry
is enough to cause resolution failure; the process must never try those project-local files.

## SSH `ProxyCommand`

`dialViaProxyCommand` runs `exec.Command("/bin/sh", "-c", cmd)` after plain replacement of `%h`
and `%p`. Because the shell reinterprets the whole string, the command and substituted values
must come only from the operator's SSH configuration:

- Only an SSH config `ProxyCommand` directive supplies the command. `ssh_connections` has no
  such field and no API writes one. `POST /api/ssh-config/hosts` writes values as single-line
  directive operands: `sshconfig.CheckHostValues` rejects control characters in name, hostname,
  user and identity file with `422` before writing; `AppendHost` enforces the same check.
  A line break must not introduce another directive, including `ProxyCommand`.
- `%h` is the same `Host` block's `HostName`; `%p` is an integer. An `ssh_connections` entry
  specifying its own `host` inherits neither `ProxyCommand` nor `ProxyJump` from that block,
  so its host cannot be substituted into another host's command.
- `lookupSSHConfigHost` reads only an absolute SSH config path. In particular, it refuses
  `sshconfig.DefaultPath`'s relative `.ssh/config` when home resolution failed.

`ProxyJump` resolution carries the names already on the chain and rejects a repeated name as a
cycle, rather than recursing until a fatal stack overflow.

## Pane identity and remote shell setup

Setting `PANEMUX_PANE_ID` is a sink of its own. A `local` pane receives it as an `exec.Cmd.Env` entry.
An `ssh` pane receives it in its remote shell command (`remotePaneIDSetup` in
`internal/session/paneid.go`): the pane ID is exported only when it matches `^[A-Za-z0-9_.-]{1,128}$`
(`validPaneEnvID`), whose characters are all inert in a shell, and it is quoted with
`shellQuotePath` as well. A pane whose ID does not match gets no variable rather than an escaped one.

That command, like any remote pane command carrying setup, is POSIX script passed whole to
`/bin/sh`: `exec /bin/sh -c ` followed by the script quoted once more with `shellQuotePath`
(`sshShellCommand` in `internal/session/ssh.go`), so the remote login shell — which need not be
POSIX — parses only `exec`, a literal path and one single-quoted word. The script is kept to one line
with no `!`, which csh-family shells expand even inside single quotes: remote paths and pane IDs are
validated to exclude both, and the browser shim's body is written as a `printf` format of octal
escapes (`printfOctalFormat`).
