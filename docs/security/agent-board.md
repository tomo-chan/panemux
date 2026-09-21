# Security: agent board writes and agent-reported values

> Part of the [security design](../security.md). That document carries the general rules, the `gosec` policy, and the map of these files.

### Agent board remote writes

`internal/board`'s `RemoteAgmsgClient` (full design in [agent-board.md](../agent-board.md)) writes
cross-pane agent messages into a remote host's message store over the SSH exec channel already used
by `GetCWD`/`InspectGitContext` — `internal/session`'s `BoardExecutor.RunBoardCommand`, implemented
on `SSHSession`/`TmuxSSHSession` — by running an operator-installed
[agmsg](https://github.com/fujibee/agmsg) instance's own scripts. panemux owns no message schema or
storage of its own — it is a client of agmsg only. The `panemux` binary itself is never installed
on a remote host under any circumstances — it is also a server, and a stray copy on an SSH-reached
host could start its own HTTP/WS listener and command center.

Message bodies are arbitrary text written by a Claude (or other agent) process, not a trusted
value, and plain `shellQuotePath`-style quoting of that value alone does not satisfy this
repository's `go/command-injection` CodeQL bar (a quoting transform does not itself break a taint
chain; only a preceding regex-allowlist branch does — see `validateShell`'s explanation in
[command-execution.md](command-execution.md#shell-path-local-ssh-sessions)).
Because a message body is free text and cannot be regex-allowlisted directly the way a path can,
`RemoteAgmsgClient.Send` base64-encodes the body, regex-checks the *encoded* string against
`^[A-Za-z0-9+/]*={0,2}$`, and only then places it in the `RunBoardCommand` argument list — structurally
mirroring `validRemotePath`'s accepted shape (a regex-allowlist branch gating a value before it reaches
the exec sink) for a value that couldn't otherwise take it. **Caveat, stated plainly:** because
`base64.StdEncoding`'s output is by construction always within that alphabet, the `MatchString` branch
can never actually fail for correctly-encoded input — it is a regex-allowlist branch in *shape*, not
one that can reject real input. No CodeQL scan has been run against this code to confirm the taint
chain is actually recognized as broken in practice; treat it as a structurally-motivated best effort,
not a verified one, until a real scan says otherwise — see
[agent-board.md's Security model](../agent-board/security-model.md#security-model) for the same caveat stated in more
detail. `team`/`from`/`to`
identifiers are regex-allowlisted directly (`^[A-Za-z0-9_.-]+$`) for the same reason, since agmsg's
own `--agent` argument validation is not a claim panemux can rely on — it runs inside the remote
shell process that only exists because panemux's own command string has already been parsed, so it
cannot retroactively protect that string's construction. Writes go through `scripts/api.sh`'s
sibling script, `scripts/send.sh <team> <from> <to> <body> [--force]`, which — per agmsg's own
source, verified rather than assumed — takes `body` as a positional argument with **no stdin
option**, so a fixed, non-tainted wrapper script (`sendBase64WrapperScript` in
`internal/board/remote_client.go`) decodes the base64 body back to its original bytes remotely,
via shell positional parameters (`$1`..`$5`), immediately before the one call site that needs the
raw value — never through unquoted string interpolation. `send.sh` does its own SQL escaping
internally, so `RunBoardCommand` remains responsible only for the POSIX shell escaping
(`shellQuotePath`-style, matching the existing `cwd` discipline) that wraps every argument,
including the wrapper script text itself, before the remote command string is built.

`send.sh` and `api.sh` are the two agmsg scripts this design's message read/write path runs
remotely; each runs against agmsg's own local store on that host. The only other remote commands
panemux itself ever executes are two fixed, non-tainted `sh -c` probes, neither of which takes any
caller-supplied data: `internal/board/agmsg_path.go`'s `remoteHomeProbeCmd` (`sh -c 'printf '%s'
"$HOME"'`), run once per remote host to resolve `agent_board.agmsg_path`'s leading `~/` against that
host's own home directory before it is ever placed in a `RunBoardCommand` argument list — see
[agent-board.md](../agent-board.md)'s "`~` in `agmsg_path` is expanded by panemux" section — and
`internal/board/agmsg_presence.go`'s `remoteAgmsgPresenceProbeScript` (`test -f "$1" && printf 'yes'
|| printf 'no'`), run by the bootstrap watcher (and, independently, whenever the relay resolves a
host's client) to check whether `scripts/api.sh` exists at the already-resolved `agmsg_path` before
treating that host as bootstrap-eligible. Because `remoteAgmsgPresenceProbeScript` takes its one
variable input (the path to test) as a positional parameter (`$1`), not string-interpolated into the
script body, it carries no taint from `agent_board.agmsg_path` into the script text itself — the
same discipline `sendBase64WrapperScript` above uses, just with no caller-supplied value needing a
preceding regex-allowlist branch at all here, since the path being tested is
`agent_board.agmsg_path` already resolved to an absolute path by the `~` expansion step, itself
derived from operator config, not runtime request data. panemux only ever detects an existing agmsg
installation — it never installs, updates, or otherwise manages agmsg on the operator's behalf,
locally or remotely. The relay goroutine that drives this on a schedule (`internal/board/relay.go`),
the bootstrap watcher (`bootstrapWatcher` in `bootstrap.go`, `package main`), the `/api/board/*`
REST surface (`GET /status`, `GET /messages`, `POST /broadcast`), and the command center
(`internal/commandcenter`, `internal/boardmcp` — see
[command-center.md](command-center.md#command-center-subprocess-execution)) are all implemented —
see [agent-board.md](../agent-board.md)'s status note.

**The bootstrap watcher's PTY write is not a command-execution sink and is out of scope for the
`exec.Command`-focused rules in [security.md](../security.md#general-rules) and
[command-execution.md](command-execution.md#security-command-execution-sinks).** `bootstrapWatcher`
writes a synthesized onboarding instruction into a pane's PTY via `Session.Write` — the same path
real user keystrokes already go through — not via `exec.Command`, so none of the
shell-argument-escaping or CodeQL taint-chain reasoning those rules carry applies to that write
itself: there is no shell parsing panemux's own Go code performs on that text, and no distinction
between "trusted" and "tainted" content for a PTY write the way there is for a command-string
argument. The one identifier bootstrap itself passes into a `RunBoardCommand` call — the
already-resolved `agmsg_path` used to build the presence probe's `$1` — is quoted with the same
`shellQuotePath`-style discipline `RunBoardCommand` already applies uniformly to every argument,
board-related or not. `agent_board.team`, a pane's own ID, and the agmsg-recognized type string
`session.AgentTypeDetector` returns are written only into the PTY instruction text, never into a
`RunBoardCommand` call bootstrap itself makes; they are operator config or panemux's own fixed
detection-table output either way, not external request data. The same holds for the
`actas-claim.sh`/`watch.sh` invocations the instruction gained for `claude-code` panes (see
[agent-board.md](../agent-board/agmsg-integration.md#two-panes-in-one-project-directory)): the
script names and the `$CLAUDE_CODE_SESSION_ID` reference are compile-time literals, the value behind
that variable is expanded by the agent's own shell and never by panemux, and no part of it reaches
an `exec.Command` argv.

### Agent-reported values in the dashboard UI

Everything in a `board_status` self-report — `state`, `cwd`, `branch`, `repo`, `pr_url`, `last_tool`,
`summary` — is free text written by an agent process, possibly on a remote host, and panemux
validates none of it: `internal/board`'s `ParseStatus` copies each field through verbatim, and the
relay's only gate is `validFrom` (was this row sent by a known board-enabled pane), which says
nothing about a row's *contents*. A pane that has been talked into writing a hostile status report,
or any process on that host able to call `send.sh`, therefore controls these strings end to end.

The dashboard renders them as text children, which React escapes; there is no `dangerouslySetInnerHTML`
anywhere in `frontend/src`. **No agent-reported value reaches a DOM attribute at all** — the dashboard
card renders only `state`, `summary`, `last_tool` and the relative time, each as a text child, and the
component tree now contains no `<a>` element.

That is a change from an earlier design, and the reason it is worth recording here rather than
quietly deleting: the card used to render `pr_url` as an `href`, which is the one shape where an
agent-controlled string carries meaning of its own rather than being escaped as text. It was guarded
by a `safeExternalURL` helper that admitted only `http:`/`https:` and fell back to plain text
otherwise — necessarily so, because React 18, the version this app pins, merely logs *"A future
version of React will block javascript: URLs"* and renders the attribute anyway (React 19 blocks it;
this codebase is not on it), and `target="_blank"`/`rel="noopener noreferrer"` constrain the opened
document rather than whether a script-scheme URL executes. Script running in the dashboard's own
origin would have the board bearer token, so that was a real escalation path.

`pr_url`, `repo` and `branch` were dropped from the card for a product reason — panemux computes
those itself by running git, and the board's self-reported copies could contradict the pane header —
but the security consequence is that the sink is gone rather than guarded. The rule that replaces the
old guard: **any future UI that renders an agent-reported field into an attribute rather than as a
text child reintroduces this sink and needs its own scheme validation**; a `safeExternalURL`-style
allowlist is the pattern to restore, not React's escaping to rely on.
