---
type: Security Requirement
title: "Security: command center subprocess execution"
description: "Command center subprocess execution, token handling, and working directory constraints."
---

# Security: command center subprocess execution

> Part of the [security design](../security.md). That document carries the general rules, the `gosec` policy, and the map of these files.

### Command center subprocess execution

`internal/commandcenter/runner.go` uses `exec.CommandContext` with discrete argv elements,
never a shell. Under the [General Rules](../security.md#general-rules), the command is the
literal `"claude"` unless an operator overrides `RunnerConfig.ClaudeBin`; request data and
environment variables never select it. Other execution boundaries are documented under
[shell paths](shell-path.md), [tmux](tmux.md), [SSH](ssh-execution.md) and [browser launch](browser-open.md).

#### Arguments and CLI parsing

The option segment contains fixed flags (`-p`, `--output-format=stream-json`, `--verbose`),
a panemux-owned session ID (see below), a `--mcp-config` path created by the runner, and fixed
`AllowedTools()` literals. The free-text prompt is the last operand, immediately after a
literal `--` end-of-options marker. Discrete argv prevents shell injection, but being last
alone does not prevent CLI option injection: a dash-prefixed prompt could otherwise invoke
`--help` or a `--settings` hook. Bind the variadic `--allowedTools` value with `=` in one argv element
(`--allowedTools=a,b,c`), so they cannot swallow the prompt and cause the CLI error
`Input must be provided either through stdin or as a prompt argument when using --print`.

Both parser defects and their fixes were reproduced with CLI v2.1.226 and verified through the
real Runner/WS/browser stack. `TestRunnerBuildArgsShapeIsSafeAgainstArgumentInjection` pins argv;
a fake runner cannot prove how the real CLI parses it. The [argument-separation decision](../decisions/agent-board/command-argument-separation.md)
records the incident.

Pass `--disable-slash-commands`: neither tool list governs the CLI's slash-command registry.
A board-token holder could otherwise invoke `/context`, `/config`, `/model`, `/mcp`, `/doctor`,
`/clear` and the rest. Real-CLI checks returned
"/context isn't available in this environment." while `board_status` still returned data; `TestRunnerDisablesSlashCommands` pins the flag and prompt placement.
The three-tool contract does not prevent the subprocess from describing environment context
injected by the CLI, including a session URL observed on the web; `--append-system-prompt`
only adds instructions, and the flags above do not close that non-tool disclosure.

#### Session, settings and working directory

Pin execution context rather than inherit ambient interactive state:

- **Conversation identity.** `NewSessionID` mints a v4 UUID; first-run argv supplies it with
  `--session-id`. Persist only that panemux-minted ID and use it for `--resume`, never adopt a
  subprocess-reported ID. Plain `claude -p` can report an ambient session with broader tool
  permissions. `TestRunnerFirstRunMintsAndPersistsItsOwnSessionID` checks a differing reported ID.
- **Settings and hooks.** Pass an empty `--setting-sources` to exclude user, project and local
  settings/hooks, and `--strict-mcp-config` to connect only this query's board MCP server.
- **Working directory.** `NewWorkDir` creates an empty per-query temp directory and
  `realCommandFactory` sets `cmd.Dir` to it, rather than panemux's launch directory.

These defaults were verified with CLI v2.1.233, not inferred from help. Empty setting sources
also suppress `CLAUDE.md` discovery. Instructions therefore use `--append-system-prompt`:
`DefaultSystemPrompt` is a compile-time literal, and optional operator text from
`~/.config/panemux/command-center/CLAUDE.md` is appended as text with no execution semantics.
No operator settings file is accepted, including `~/.claude/settings.json` or a command-center
settings file. Host settings assume a watched interactive request; the command center instead
accepts unattended input from any board-token holder. See the [context decision](../decisions/agent-board/command-center-context.md).

#### Acting-tool denial

`--allowedTools` is a permission policy another policy layer can override, so argv also carries
`--disallowedTools` from `DisallowedTools()` in `mcp_config.go`. The real CLI v2.1.233 produced
these results with a single allowed board tool and a prompt asking Bash to write a file:

| argv | `Bash` |
|---|---|
| `--allowedTools` alone | blocked |
| `+ {"permissions":{"defaultMode":"acceptEdits"}}` | **executed** — file written, `permission_denials` empty |
| `+ --disallowedTools=Bash` | blocked |

The settings bypass was reproduced twice. Shipped settings contain no `permissions` key;
the explicit denial also held end to end with a live board, with and without `acceptEdits`,
while `board_status` returned data. Acting tools remain listed and are refused at call time;
they are not absent. An empty `permission_denials` in the bypass case meant the execution
was not even recorded as a permission decision.

Use an explicit list of tools that execute, write, access the network, spawn agents, persist
work or contact anything outside the process. `--disallowedTools="*"` was rejected: it removed
board MCP tools while the model still reported file tools available. The accepted weakness is
list drift as the CLI gains tools; this is the argv-level denial shown to hold.
`TestDisallowedToolsCoversActingTools` and `TestRunnerDeniesActingToolsByName` protect the list
and flag. [Task summaries](task-summaries.md) use the same list, so changes affect both.

#### Sandbox scope and verification limits

`SubprocessSettings` is a fixed literal containing only narrowing keys, currently
`{"sandbox":{"enabled":true}}`. Explicit opt-in is necessary because empty setting sources
cannot inherit a globally enabled sandbox. `TestSubprocessSettingsOnlyNarrows` rejects widening
keys (`permissions`, `hooks`, `apiKeyHelper`, `statusLine`, `env`, `enabledPlugins`,
`additionalDirectories`, `mcpServers`); `TestRunnerSendsOnlyPanemuxOwnSettings` rejects other
`--settings` values. panemux never sets `autoAllowBashIfSandboxed`, which can widen permission
by auto-permitting sandboxed Bash; sandbox settings are not uniformly restrictive.

With Bash denied, the sandbox confines nothing today. Its schema concerns Bash execution
(`autoAllowBashIfSandboxed`, `allowUnsandboxedCommands`, `forbidUnsandboxedCommands`,
`commandPattern`); MCP stdio children were not shown to be wrapped. It remains a fallback
if both argv lists are widened. Leave `network.allowedDomains`/`deniedDomains` unset:
their `*`, `localhost`, `host[:port]` and `*.host[:port]` matches do not guard the board MCP
server's loopback API call, which is outside that scope.

**Sandbox confinement remains unverified.** On the tested implementation host the setting
was ignored harmlessly: queries completed with `is_error: false`, `CLAUDE_CODE_SANDBOXED`
remained unset, and an out-of-workspace write succeeded even with `CLAUDE_CODE_FORCE_SANDBOX=1`.
The CLI describes a per-device capability; the evidence is consistent with that container
lacking it. Verify confinement on a host where `/sandbox` reports availability before claiming it.

#### MCP environment sink

`PANEMUX_BOARD_TOKEN` and `PANEMUX_BOARD_BASE_URL` are in the MCP config's `env` block,
never Runner's argv; see [token-file handling](auth.md#mcp-token-file).
`panemux __board-mcp-server` reads them with `os.Getenv` in `board_mcp_server.go` and only
makes HTTP requests through `HTTPBoardAPIClient`. It calls no `exec.Command`, so these values
do not enter the exec flow prohibited by the [General Rules](../security.md#general-rules).
