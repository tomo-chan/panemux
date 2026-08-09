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

### Agent board remote writes

Phase 1 implemented (see [agent-board.md](agent-board.md)'s status note for what's not). The
`internal/board` package writes cross-pane
agent messages into a remote host's message store over the SSH exec channel already used by
`GetCWD`/`InspectGitContext` (`RunBoardCommand` on `SSHSession`/`TmuxSSHSession`, in
`internal/session/ssh.go` and `internal/session/tmux_ssh.go`), by running an operator-installed
[agmsg](https://github.com/fujibee/agmsg) instance's own scripts. panemux owns no message schema or
storage of its own — it is a client of agmsg only. The `panemux` binary itself is never installed
on a remote host under any circumstances — it is also a server, and a stray copy on an SSH-reached
host could start its own HTTP/WS listener and (in a later phase) command center.

Message bodies are arbitrary text written by a Claude (or other agent) process, not a trusted
value. Reads go through `scripts/api.sh`; verified against agmsg's own source, only `--limit` and
`--before-id` are digit-validated there — `--agent` is not validated at all, only escaped by
agmsg's own internal SQL escaping. Writes go through `scripts/api.sh`'s sibling script,
`scripts/send.sh <team> <from> <to> <body> [--force]`, which takes `body` as a positional argument
with **no stdin option**, so it cannot be kept out of the remote command string.

Because neither script's arguments are shell-shape-validated by agmsg itself, and a message body
is arbitrary text that cannot be regex-allowlisted the way `cwd` can, `RunBoardCommand(ctx,
scriptPath, args)` does not build a command string containing any argument content at all. Every
element of `args` (verb, team, `--agent` value, message body, ...) is base64-encoded and sent to
the remote shell over the SSH session's **stdin**, one encoded line per argument — never
concatenated into the command string `Session.Output` runs. The generated command string consists
only of hardcoded shell boilerplate (a fixed number of `IFS= read -r aN || exit 1` lines, one per
argument, followed by `exec <script> "$(printf '%s' "$aN" | base64 -d)" ...`), the validated
`scriptPath`, and `$aN` variable references — the argument count (`len(args)`) drives how many
`read`/`$aN` pairs appear, but no argument's *bytes* ever do. On the remote side, `read -r` pulls
one base64 line off stdin into a shell variable with no re-interpretation of its content, and each
`"$(printf '%s' "$aN" | base64 -d)"`, wrapped in double quotes, decodes it back and substitutes the
result as a single argument with no word-splitting or glob expansion. `scriptPath` itself (e.g.
`<agmsg_path>/scripts/send.sh`) goes through the `validRemotePath`-then-`shellQuotePath` path this
document already documents for `cwd`, since it is an operator-configured filesystem path, not
agent-authored text, and is embedded directly in the command string — kept in its own function
parameter, never read from the same slice as `args`, so a trusted validated value and an untrusted
collection are never different indices of one combined slice (see
[DECISIONLOG.md](DECISIONLOG.md#message-body-escaping-three-attempts-before-one-satisfied-codeql-2026-08-08-pr-163)
for why that split matters). `send.sh` does its own SQL escaping internally, so encoding/transport
is the only escaping layer panemux is responsible for. See `buildBoardCommand` and
`RunBoardCommand` in `internal/session/ssh.go` (shared by `SSHSession` and `TmuxSSHSession` via
`runBoardCommandOverSSH`) for the implementation.

`send.sh` and `api.sh` are the only board-related commands panemux itself ever executes remotely;
each runs against agmsg's own local store on that host. panemux only ever detects an existing agmsg
installation (`scripts/api.sh` presence, never `command -v agmsg`) — it never installs, updates, or
otherwise manages agmsg on the operator's behalf, locally or remotely.

### Auth token and transport encryption (config/validation implemented; auto-generation not yet implemented)

panemux does not terminate TLS itself. `server.host` defaults to `127.0.0.1`; if it is set to a
non-loopback address, `server.auth_token` must also be set, or startup must fail validation
(`internal/config/validate.go`'s `isLoopbackHost` check, alongside the existing `server.port` range
check). This validation rule and the `server.auth_token` config field are implemented; the "empty =
auto-generate on first run, saved to `~/.config/panemux/token` (0600)" behavior described in
[agent-board.md's Config additions](agent-board.md#config-additions) is not — an operator who wants
board features reachable over a non-loopback listener must set `server.auth_token` explicitly today.
`GET /api/board/status`, `GET /api/board/messages`, and `POST /api/board/broadcast` are gated by
this token via `internal/api`'s `BoardAuthMiddleware` (constant-time comparison); an empty token
means those endpoints require no `Authorization` header, matching every other endpoint's pre-board,
unauthenticated-by-default behavior. An auth token sent over an unencrypted non-loopback hop can be
replayed and the request it authenticates can be tampered with in transit, so the token only
provides real protection once the operator has placed a TLS-terminating reverse proxy, SSH tunnel,
or VPN in front of the non-loopback listener. See
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
- Decision history and rationale: [DECISIONLOG.md](DECISIONLOG.md)
