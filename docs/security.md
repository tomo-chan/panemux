# Security Design

This document defines the implementation-time security requirements for panemux. Treat it as required reading before changing command execution, shell argument handling, SSH path handling, host key handling, or `gosec`-sensitive code.

## Session Command Execution

All session types that execute a local process use `exec.Command` with user-configurable values such as shell paths and tmux session names. These values must be sanitized before reaching the exec sink.

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

### Agent board remote writes (design, not yet implemented)

The planned `internal/board` package (full design in [agent-board.md](agent-board.md)) writes
cross-pane agent messages into a remote host's message store over the SSH exec channel already used
by `GetCWD`/`InspectGitContext`. Two backends are planned, `native` (panemux's own SQLite file) and
`agmsg` (delegating to an operator-installed [agmsg](https://github.com/fujibee/agmsg) instance).
Message bodies are arbitrary text written by a Claude (or other agent) process, not a trusted value,
so both backends must ensure no unescaped body content reaches a remote shell — but they satisfy
that requirement by different means, verified against each tool's actual argument contract rather
than assumed to match:

- `native`: `RunBoardCommand` sends the body over the remote command's **stdin** as a JSON payload
  to the fixed argv `panemux board recv`, the same discipline as `cwd` above. The body never
  appears in the command string at all.
- `agmsg`: reads go through `scripts/api.sh`, which only takes digit- or
  path-traversal-validated arguments. Writes go through `scripts/api.sh`'s sibling script,
  `scripts/send.sh <team> <from> <to> <body> [--force]`, which — per agmsg's own source — takes
  `body` as a positional argument with **no stdin option**. For this script only, `RunBoardCommand`
  must single-quote-escape every argument (`shellQuotePath`-style, matching the existing `cwd`
  discipline) before building the remote command string, since keeping the body out of the command
  string entirely is not possible here. This is a documented exception driven by `send.sh`'s own
  contract, not a relaxation of the "no unescaped user content in a remote shell" requirement.

These are the only board-related commands panemux itself ever executes remotely; each performs a
write against that backend's own local store on that host. panemux only ever detects an existing
agmsg installation — it never installs, updates, or otherwise manages agmsg on the operator's
behalf, locally or remotely.

### Auth token and transport encryption (design, not yet implemented)

panemux does not terminate TLS itself. `server.host` defaults to `127.0.0.1`; if it is set to a
non-loopback address, `server.auth_token` must also be set, or startup must fail validation
(`internal/config/validate.go`, alongside the existing `server.port` range check). An auth token
sent over an unencrypted non-loopback hop can be replayed and the request it authenticates can be
tampered with in transit, so the token only provides real protection once the operator has placed a
TLS-terminating reverse proxy, SSH tunnel, or VPN in front of the non-loopback listener. See
[agent-board.md](agent-board.md#security-model) for the full rationale.

## General Rules

- When adding new session types or new `exec.Command` calls, the command value passed as the first argument must come from a hardcoded literal or from a trusted system source such as a file or registry with no data-flow path to user input.
- Arguments after the command may be user-supplied only when the target binary cannot reinterpret them as commands.
- Do not use `os.Getenv` values in flows that reach `exec.Command` unless the security model for that path is explicitly reworked and documented.

## `gosec` Policy

- Fix `gosec` findings structurally in the implementation rather than suppressing them in shipped code.
- Test-only code may use narrow `//nolint:gosec` annotations when the fixture behavior intentionally violates a production hardening rule.
- Production paths should avoid suppressions and make the safety argument explicit in code structure instead.

## Exception: OpenSSH Hashed `known_hosts` Compatibility

`internal/session/ssh.go` intentionally keeps a narrow `//nolint:gosec` on `crypto/sha1` (`G505`) for matching OpenSSH hashed `known_hosts` entries (`|1|<salt>|<hash>`). This is not application-defined hashing and not a credential-storage decision. It is protocol and file-format compatibility with existing OpenSSH user data.

OpenSSH defines these hashed host patterns in terms of HMAC-SHA1. To decide which host-key algorithms to advertise, panemux scans the user's `known_hosts` file and must recognize both plaintext host entries and hashed `|1|...` entries. Replacing SHA-1 with another hash would make hashed entries stop matching and would reintroduce SSH connection failures for users whose `known_hosts` files were generated by standard OpenSSH tooling such as `ssh-keygen -H`.

This exception is limited to matching OpenSSH `known_hosts` host patterns. It does not weaken host-key verification itself: verification still goes through `knownhosts.New(...)`, and the SHA-1 path is used only to recognize existing hashed entries so the client can narrow `HostKeyAlgorithms` to the key types already recorded for that host.

## Related Documents

- Implementation structure: [architecture.md](architecture.md)
- Runtime behavior and SSH configuration rules: [behavior.md](behavior.md)
- Developer workflow rules: [../DEVELOPMENT.md](../DEVELOPMENT.md)
