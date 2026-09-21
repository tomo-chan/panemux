# Security: command center subprocess execution

> Part of the [security design](../security.md). That document carries the general rules, the `gosec` policy, and the map of these files.

### Command center subprocess execution

`internal/commandcenter/runner.go`'s `Runner` is the other place in this repository, besides
`internal/session` and `openChrome` (see
[command-execution.md](command-execution.md#security-command-execution-sinks)), that calls
`exec.Command`/`exec.CommandContext` on a value not fully known at compile time. Per the security
design's [General Rules](../security.md#general-rules): the command name (`r.claudeBin`) is a
hardcoded literal (`"claude"`) unless an operator explicitly overrides it via
`RunnerConfig.ClaudeBin` — there is no code path that derives it from request data, environment
variables, or anything else CodeQL would treat as tainted. The arguments after it are a mix of fixed
literal flags (`-p`, `--output-format=stream-json`, `--verbose`), a `--resume <session-id>` pulled
from `SessionState` (itself only ever written by `Runner` from a value `claude` itself reported in
an earlier run — never client-supplied), a `--mcp-config <path>` pointing at a temp file `Runner`
itself created, an `--allowedTools=<list>` value `commandcenter.AllowedTools()` computes from fixed
string literals, and finally the user's free-text prompt as the last argument. None of this goes
through a shell — `exec.CommandContext` passes each argument as a discrete argv element — so there
is no shell-injection risk from the prompt regardless of its content.

**Being the final positional argument does not, by itself, make the prompt safe — this document's
own first draft of this section claimed exactly that, and the claim was wrong.** `buildArgs` in
`internal/commandcenter/runner.go` was verified live against a real installed `claude` CLI (v2.1.226)
to have two distinct, independently-reproduced bugs before the fix now in place:

1. **Argument injection into the `claude` CLI's own parser.** A prompt beginning with `-` (e.g. a
   user typing `--help`, or something far more consequential like a `--settings=<json>` payload
   defining a malicious hook) was not treated as opaque prompt text — the CLI's own option parser
   scans for flags anywhere in argv, not only before the first positional, so a `-`-prefixed prompt
   was parsed as a flag. This was reproduced directly: passing `"--help"` as the trailing argument
   printed the CLI's own help output instead of being sent as a prompt, and a `--settings` payload
   defining a `SessionStart` hook reached and ran that hook. **The fix**: `buildArgs` now inserts a
   literal `"--"` end-of-options marker immediately before the prompt, which the CLI's parser was
   confirmed (live) to honor — everything after it is treated as a positional argument, never a flag,
   regardless of its content.
2. **`--allowedTools` is declared variadic** (`<tools...>`) by the CLI's own `--help` output, so
   passing it and its value as two separate argv elements (`"--allowedTools", "a,b,c"`) let it
   swallow the very next argv element too — which was the prompt, silently breaking every ordinary
   query with `Input must be provided either through stdin or as a prompt argument when using
   --print`, confirmed live. **The fix**: `buildArgs` now uses the `=` form
   (`"--allowedTools=a,b,c"`) as a single argv element, which cannot be extended by a following one.

Both fixes were verified together, live, end-to-end through the real `Runner`/WS/browser stack (not
just the CLI in isolation) before being considered closed. `internal/commandcenter/runner_test.go`'s
`TestRunnerBuildArgsShapeIsSafeAgainstArgumentInjection` pins the exact argv shape as a regression,
since a fake `cmdRunner` cannot reproduce the real CLI's own argument-parsing behavior — it can only
assert what argv panemux itself constructs, which is why this was caught by an adversarial review
verifying claims against the real binary, not by the original test suite.

**Slash commands are disabled, because they are outside both tool lists.** `--allowedTools` and
`--disallowedTools` govern tools; neither governs the CLI's own slash-command registry. Verified
against the real CLI: a prompt of `/context`, sent through the otherwise-shipped argv, returned that
command's own output (a context-usage table) rather than being treated as prompt text. The palette is
reachable by anyone holding the board bearer token, so that put the whole registry — `/config`,
`/model`, `/mcp`, `/doctor`, `/clear` and the rest, 46 commands in the environment this was measured
in — one message away. `buildArgs` passes `--disable-slash-commands`; the same prompt then returns
"/context isn't available in this environment." while `board_status` still returns real data, both
confirmed live. `TestRunnerDisablesSlashCommands` pins the flag and that the prompt still travels as
prompt text after the `--` marker.

One related observation, recorded because it bounds what the three-tool contract actually covers: the
subprocess can still describe its own execution environment, since the CLI injects environment context
into its system prompt that `--append-system-prompt` only adds to. In a Claude Code on the web
environment that context included the session URL. This is not reachable through a tool and is not
closed by any flag above.

**The subprocess's execution context is pinned by panemux, not inherited from the environment.** Three
of `claude`'s defaults resolve from ambient state, and all three were wrong for a subprocess panemux
spawns on an operator's behalf. Each finding below was reproduced against the real CLI (v2.1.233),
not inferred from `--help`:

- **Conversation identity.** A plain `claude -p` with no `--resume` does **not** mint a fresh
  conversation — it reports the *ambient* session id of whatever Claude Code session the environment
  already belongs to. `Runner` previously captured that reported id, persisted it, and `--resume`d it
  on every later query, which attached the command center to a conversation it does not own. This was
  observed live: a palette query returned a reply carrying the operator's own session context,
  referencing a scratch file name that appeared in no prompt panemux ever sent. **The escalation is
  the point:** the command center is deliberately launched with `--allowedTools` scoped to exactly
  three board tools, while the session it joined held that session's full tool permissions, so palette
  text became input to a far more capable agent than the palette's own contract admits.
  `internal/commandcenter/context.go`'s `NewSessionID` now mints a v4 UUID, `buildArgs` pins it with
  `--session-id` on a first run, and the persisted value is always the id panemux minted — the
  subprocess's own reported id is never adopted.
  `TestRunnerFirstRunMintsAndPersistsItsOwnSessionID` pins this by feeding the fake subprocess a
  *different* reported id and asserting it never reaches the session file.
- **Settings and hooks.** With no `--setting-sources`, the subprocess loads the operator's user,
  project and local settings — including their hooks, which would then execute inside a process
  panemux started. `buildArgs` passes `--setting-sources` with an empty value, and
  `--strict-mcp-config` so only the board MCP server this query configured is connected.
- **Working directory.** `claude -p` reads `CLAUDE.md` from its working directory, and `cmd.Dir` was
  unset, so it read whatever project the operator happened to launch panemux from. `NewWorkDir`
  creates an empty per-query temp directory and `realCommandFactory` sets `cmd.Dir` to it.

Note the interaction between the last two: `--setting-sources ''` suppresses `CLAUDE.md` discovery
**as well as** settings files, so "ship our own `CLAUDE.md`" and "inherit none of the operator's
configuration" cannot both be satisfied through files. panemux's own instructions therefore travel via
`--append-system-prompt` (a compile-time literal in `DefaultSystemPrompt`, never operator input, so it
carries no taint into argv). An operator may refine those instructions by placing a `CLAUDE.md` in
`~/.config/panemux/command-center/`; it is optional, and it is *text appended to a system prompt*,
which has no execution semantics.

**No settings file is accepted from anywhere — not the operator's `~/.claude/settings.json`, and not a
command-center-specific one.** This is not caution for its own sake. A settings value can nullify
`--allowedTools`. Reproduced twice against the real
CLI (v2.1.233): with `--allowedTools` scoped to a single board tool, adding
`{"permissions":{"defaultMode":"acceptEdits"}}` let the subprocess run `Bash` — the file it was told to
write appeared on disk, and `permission_denials` was **empty**, so the call was not merely permitted
but not even recorded as a decision. `defaultMode: "acceptEdits"` is an entirely ordinary thing for an
operator to set for their own interactive sessions, so inheriting it would silently unscope the command
center. The trust boundary is the point: host settings are written on the assumption that a human typed
the request and is watching, while the command center accepts input from anyone holding the board
bearer token, unattended.

**`--allowedTools` alone is therefore not a boundary — it is a permission policy another policy layer
can override, and an earlier revision of this document called it "the actual security boundary", which
was wrong.** The argv the subprocess is launched with carries a second, stronger list:
`--disallowedTools`, built by `DisallowedTools()` in `internal/commandcenter/mcp_config.go`. The
difference is measurable, all three rows run against the real CLI with `--allowedTools` scoped to a
single board tool and a prompt instructing the model to write a file with `Bash`:

| argv | `Bash` |
|---|---|
| `--allowedTools` alone | blocked |
| `+ {"permissions":{"defaultMode":"acceptEdits"}}` | **executed** — file written, `permission_denials` empty |
| `+ --disallowedTools=Bash` | blocked |

panemux sends no `permissions` key today, so the middle row is not the shipped configuration — but it
is one settings key away, and the third row is what keeps that from being a full escape. The shipped
argv was then verified end to end against a live board: `Bash` stays blocked *and* `board_status` still
returns real data, both with and without the `acceptEdits` override applied on top.

A wildcard was tried first and rejected on evidence: `--disallowedTools="*"` removes the board MCP
tools as well, leaving the command center with nothing to call, while the model still reported file
tools as available. So the list is an explicit enumeration of the tools that can *act* — execute,
write, reach the network, spawn further agents, persist work, or contact anything outside the process.
It will drift as the CLI gains tools. That weakness is accepted because it is the only argv-level
denial that holds; `TestDisallowedToolsCoversActingTools` and `TestRunnerDeniesActingToolsByName` fail
if the list or the flag disappears.

Relatedly, `AllowedTools`'s own doc comment used to say the subprocess had "no `Bash`, no filesystem
tools". That was imprecise in a way that mattered: those tools are *present* in the subprocess's tool
list and refused at call time, not absent. "Refused" is exactly the property the middle row above
defeats.

What panemux does send is `SubprocessSettings` (`internal/commandcenter/context.go`), a fixed literal
containing only keys that *narrow* what the subprocess may do — currently
`{"sandbox":{"enabled":true}}`. Sandboxing has to be passed explicitly precisely because
`--setting-sources ''` means nothing else can ever enable it, so without this the command center could
never be sandboxed even on a host where the operator had enabled it globally.

**With `Bash` denied by name, the sandbox confines nothing today** — its subject is Bash command
execution (`autoAllowBashIfSandboxed`, `allowUnsandboxedCommands`, `forbidUnsandboxedCommands` and
`commandPattern` all sit in that area of the settings schema, and nothing suggests it wraps MCP stdio
server child processes). It is kept as the layer that would still apply if both argv lists were ever
widened. Its `network` sub-key (`allowedDomains`/`deniedDomains`, matched as `*`, `localhost`,
`host[:port]` or `*.host[:port]`) is deliberately left unset for the same reason: the board MCP
server's loopback call to panemux's own API is outside the sandbox's scope, so there is nothing to
allow-list, and guessing at a network policy could only break the one path the feature depends on.
Note also that `autoAllowBashIfSandboxed` runs in the *widening* direction — being sandboxed can be a
reason to auto-permit `Bash` — so "sandbox" must not be read here as a uniformly restrictive concept.
panemux never sets it, and `TestSubprocessSettingsOnlyNarrows` would fail if a widening key appeared.
`TestSubprocessSettingsOnlyNarrows` fails if a widening key (`permissions`, `hooks`, `apiKeyHelper`,
`statusLine`, `env`, `enabledPlugins`, `additionalDirectories`, `mcpServers`) is ever added, and
`TestRunnerSendsOnlyPanemuxOwnSettings` fails if any other `--settings` value reaches argv.

**Stated plainly, because this repository distinguishes verified claims from unverified ones: the
sandbox was *not* confirmed to confine anything.** What was verified is that passing it is harmless
where the OS cannot provide it — the setting is ignored, the query completes, `is_error` stays false —
and that `CLAUDE_CODE_SANDBOXED` remained unset in the implementation sandbox, with a write outside the
workspace still succeeding, including under `CLAUDE_CODE_FORCE_SANDBOX=1`. The CLI's own error strings
describe sandboxing as a per-device capability, so the reasonable reading is that the container this
was tested in cannot provide it. Treat the confinement itself as unverified until someone runs it on a
host where `/sandbox` reports the sandbox as available.

The `PANEMUX_BOARD_TOKEN`/`PANEMUX_BOARD_BASE_URL` values the `claude -p` subprocess's own
MCP-server child process reads never reach `Runner`'s own `exec.Command` argv at all — they are set
in the MCP config file's `env` block (see [auth.md](auth.md#auth-token-and-transport-encryption) for
that file's own handling), read by `panemux __board-mcp-server` via `os.Getenv` in
`board_mcp_server.go`. This is not a violation of the [General Rules](../security.md#general-rules)'
"do not use `os.Getenv` values in flows that reach `exec.Command`" rule: `runBoardMCPServer` never
calls `exec.Command` itself, it only makes outbound HTTP requests
(`internal/boardmcp.HTTPBoardAPIClient`) — an entirely different sink with no
shell/argv-reinterpretation risk to defend against.
