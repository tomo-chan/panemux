---
type: Specification
title: "Agent Registry"
description: "Registered coding agents, identities, session keys, capabilities, adapters, and agent integration steps."
---

# Agent Registry

`internal/agent` is the single place panemux names coding agents. Every recognized agent is a
`Definition` carrying its stable ID, display name, aliases, process detection rules, and declared
capabilities. The package imports nothing else from panemux so `internal/session`, `internal/tasks`,
and later consumers can import it without cycles. Design rationale lives in
[Agent Registry decisions](../decisions/agent-registry.md).

## Registered agents

| ID | Display name | Aliases | Capabilities |
|---|---|---|---|
| `claude` | Claude Code | `claude-code` | detect, collect, launch, resume, workdir, summary, board |
| `codex` | Codex | — | detect, collect, launch, resume, workdir, summary, board |
| `cursor` | Cursor | — | detect, board |
| `gemini` | Gemini CLI | — | detect, board |
| `grok-build` | Grok Build | — | detect, board |
| `opencode` | opencode | — | detect, board |

Registration order is matching order: the first Board rule matching a process names its type. The
four detect-and-board agents belong to Agent Board alone and are not listed on the task dashboard.

## Identity and session keys

- **ID (`agent.ID`)**: carried by records, task IDs, and APIs. Lowercase, starts with a letter,
  words joined by single hyphens; stored IDs never change. `ID.Valid` validates shape only (a stored
  record may name an agent this build does not register).
- **Aliases**: alternate names for the same agent (such as `claude-code` for npm installations).
  `agent.Resolve` maps an ID or alias to its canonical ID. IDs and aliases share the same shape rule
  and are globally unique across both sets.
- External messaging type names belong to the
  [agmsg adapter](agent-board/agmsg.md#agent-type-mapping), which validates its mapping against
  `board` declarations.
- **Session keys (`agent.SessionKey{Host, Agent, SessionID}`)**: identify one agent session (`Host`
  is `""` for the panemux host or an `ssh_connections` key). Session IDs are unique only within an
  `(Host, Agent)` pair. A session whose ID is not yet decided has no key and never matches a keyed
  session. Location is not identity: a pane may show none (outside panemux), a PID is not a key
  (shared daemons such as Codex's app-server host multiple sessions, and PIDs are reused across
  restarts), and a tmux session can outlive the agent inside it.

## Capabilities

A capability declares what panemux supports for an agent, not whether it is currently usable on a
host. Per-host `collect`, `launch`, and `resume` status lives in
[agent availability](tasks/collection.md#agent-availability); [`GET /api/tasks`](../api/tasks.md#get-apitasks)
exposes registered `agents`, per-host `availability`, and per-task `actions`.

| Capability | Meaning | Rule checked by registry |
|---|---|---|
| `detect` | Processes are recognized | At least one detection rule |
| `collect` | Sessions and states appear on the task dashboard | Task rule (a Task rule requires `collect` or `workdir`) |
| `launch` | Panemux can start a new session | — |
| `resume` | Panemux can reopen a stopped session | — |
| `workdir` | Working directories and conversation logs can be resolved | Task rule |
| `summary` | Sessions can be summarized | — |
| `usage` | Usage limits and spend can be read (declared by no agent yet, #345) | — |
| `board` | Can participate in Agent Board via the message port | Board rule |

`internal/tasks`'s `recordable` set derives from `collect` declarations; stored records for any
other agent are preserved untouched ([Done and labels](tasks/lifecycle.md#done-and-labels)). A
running task carries `Task.Key`, PID, working directory, and (for Codex) its rollout so downstream
capabilities do not recollect.

### Formats and versions

The registry carries no per-release versions. Each detection rule records:

- `VerifiedWith`: last verified release/date (`interactive process rules checked 2026-08` for Board
  rules, `codex-cli 0.157.0` for Codex's Task rule, empty for others).
- `UnknownArgs`: every registered rule is `accept` (an unknown argument counts as an interactive
  session rather than silently hiding a new interactive mode).

Observed agent versions (`~/.claude/sessions` `version`, Codex `session_meta.cli_version`) are
recorded without comparison. Unparseable files or unknown status/turn events render as `unknown`,
suppress notifications, and mark `collect` `degraded` / `unknown_format` on that host
([Agent availability](tasks/collection.md#agent-availability)).

## Detection rules

Two rule sets exist: Board rules detect interactive agents for Agent Board participation, while Task
rules drive task collection and pane-header worktree resolution. Within a rule set, agents are tried
in registration order and the first matching program decides: a headless invocation of that agent
yields no agent even if a later rule could match.

| Rule | Consumer | Matching semantics |
|---|---|---|
| Board | Agent Board polling (`AgentTypeDetector`) | Program basename against patterns (trailing `*` matches any suffix) with no exclude token in argv |
| Task | Task collection and pane workdirs (`ActiveWorkdirGetter` via `MatchInteractiveSession` for `workdir` agents) | Claude Code: path component of program or `node`/`bun`/`deno` script is `claude` or `claude-code`, excluding `-p`/`--print`. Codex: basename `codex` whose first positional argument is not a non-interactive subcommand |

Board exclude tokens are `-p`/`--print` (Claude) and `exec` (Codex); none are verified for the other
four agents. Codex's Task rule parses options matching Codex CLI: value-taking flags (`-m`, `-c`,
`-p`/`--profile`, `-C`, …) skip their value (so `codex -m x exec` is headless while `codex -p exec`
is interactive), and `-i`/`--image` consumes arguments until the next option (so `codex -i a.png
review` is interactive). Because Claude matches path components, `/opt/claude/bin/codex` matches
Claude Code. Only registered agents with Board rules are detected.

## Capability adapters

Consumers implement declared capabilities and validate their registration maps at startup and in
contract tests—any declaration without an adapter, or adapter without a declaration, fails startup.

- **Collection (`collectAdapters` in `internal/tasks/collect_adapters.go`)**: declares
  `<agent ID>/<name>` collection script sections, converts rows into tasks, and supplies fixed
  `stateNote` (`state_note`).
- **Launch and resume (`launchAdapters` / `resumeAdapters` in `internal/tasks/launch_adapters.go`)**:
  separate so an agent can launch without resuming. Launch adapters name new session/tmux IDs (or
  none when the agent picks its own, holding labels until exactly one session appears in that tmux
  session; see [Starting a task](tasks/lifecycle.md#starting-a-task)), supply fixed `notes.launch`,
  and emit a fixed shell fragment taking no request values and refusing only declared codes. Resume
  adapters validate session IDs and name target tmux sessions. Undeclared agents or hosts with
  probed `cli_missing` are refused.
- **Working directories (`workdirAdapters` in `internal/session/workdir.go`)**: operates per session
  (`locate` → `WorkdirSession{SessionKey, PID, cwd, log}`, `workdirs` → all session and subagent
  directories) over a host fetch abstraction. Panes match agents via `agent.MatchInteractiveSession`;
  tasks carry `Task.WorkdirSession` (#420). `session.ParseClaudeSessionState` is the single parser
  for `~/.claude/sessions` files (wrapped by `internal/tasks` for unreadable-file diagnostics).
- **Summaries (`summaryAdapters` in `internal/tasks/summary_adapters.go`)**: each adapter provides a
  `summarySource` (session ID rule, fixed log-reading script, excerpt builder) and `summaryRunner`
  (runs the agent's own CLI on the panemux host). Common code owns scheduling, excerpt-hash reuse,
  retries, concurrency, timeouts, persistence, and `tasks[].actions.summary` without branching on
  agent ID. Each part folds its version into summary identity (`""` adds nothing). A runner reports
  an unexpected answer model or nothing.
- **Board participation (`internal/board` ports implemented by `internal/agmsg`)**: session
  detection returns `agent.ID` from Board rules; `internal/agmsg` maps exactly `board`-declared IDs
  to transport type names ([Agent type mapping](agent-board/agmsg.md#agent-type-mapping)) and checks
  host availability without writing to PTYs.

## Adding an agent

An agent is added through registration alone—common pipelines, API schemas, and the dashboard never
branch on agent name:

1. Add the `ID` constant and `Definition` to `registered()` in `internal/agent/registry.go` (no
   `init()` registration; shipped definitions followed by `fixtureAgents()` form the entire
   registry).
2. Declare only supported capabilities with required detection rules.
3. Run `internal/agent` tests (`Validate` rejects malformed/duplicate IDs or aliases, collisions,
   unknown/repeated capabilities, and mismatched rules; invalid registrations stop startup).
4. Update `TestRegisteredAgents` in the same change.
5. Register an adapter in every table below for declared capabilities and add contract test inputs
   (summary adapters require a contract sample). For `board`, add the transport mapping in
   `internal/agmsg/agent_adapters.go` keeping transport names out of `internal/agent` and
   `internal/session`.
6. Match CLI name to agent ID (launch scripts and availability probes currently look up the CLI by
   ID, [#428](https://github.com/tomo-chan/panemux/issues/428)).
7. Add acceptance rows to [docs/scenarios.md](../scenarios.md) and collection details to
   [Task collection](tasks/collection.md).

### Capabilities and where they are checked

| Capability | Consumer interface | Registered in | Contract test | API and dashboard surface |
|---|---|---|---|---|
| `detect` | `Detection` (Task rule, Board `ProcessRule`) in `internal/agent/agent.go` | `registered()` (`internal/agent/registry.go`) | `TestRegisteredDefinitionsAreValid` | `agents[].capabilities`; processes recognized |
| `collect` | `collectAdapter` (`internal/tasks/collect_adapters.go`) | `collectAdapters` | `TestCollectAdapters_MatchTheRegistry`, `TestCollectPrefilter_CoversEveryTaskRule` | Tasks listed; `hosts[].availability` (`collect`); Mark done & labels |
| `launch` | `launchAdapter` (`internal/tasks/launch_adapters.go`) | `launchAdapters` | `TestLaunchAdapters_MatchTheRegistry` | `hosts[].availability` (`launch`); New task dialog; `POST /api/tasks` |
| `resume` | `resumeAdapter` (`internal/tasks/launch_adapters.go`) | `resumeAdapters` | `TestLaunchAdapters_MatchTheRegistry` | `tasks[].actions.resume`; Resume button; `POST /api/tasks/resume` |
| `summary` | `summarySource` & `summaryRunner` (`internal/tasks/summary_adapters.go`) | `summaryAdapters` | `TestSummaryAdapters_MatchTheRegistry`, `TestSummaryAdapters_Contract` | `tasks[].actions.summary`, `tasks[].summary`; Work & Summarize |
| `workdir` | `workdirAdapter` (`internal/session/workdir.go`) | `workdirAdapters` | `TestWorkdirAdaptersMatchDeclarations` | Pane header working directory and log follow agent session |
| `board` | `MessageClient` / `Participation` (`internal/board`), implemented by `internal/agmsg` | `internal/agmsg/agent_adapters.go` | `TestBoardAdapters_MatchTheRegistry` | Detected for Agent Board display (detection never joins) |
| `usage` | — (declared by no agent yet, [#345](https://github.com/tomo-chan/panemux/issues/345)) | — | — | — |

An absent capability is absent everywhere (no availability row, no task action, no button, and
server routes refuse it).

### The fixture agent: a minimal adapter set

Built only under the `panemux_fixture_agent` build tag (never shipped; `*_off.go` returns empty
lists), `panemux-fixture-agent` (`internal/agent/fixture_agent.go`, `internal/tasks/fixture_agent.go`)
declares `detect`, `collect`, `launch`, and `summary` (omitting `resume`, `workdir`, `board`, and
`usage`). Collection reads `~/.panemux-fixture-agent/sessions` and `~/.panemux-fixture-agent/logs`;
launch mints a session ID; summary reads the log and runs `--print` (excluded by its Task rule).
[The fixture agent's tagged build](../engineering/quality-gates.md#the-fixture-agents-tagged-build)
owns its end-to-end verification contract, fixture JSON, and untagged gate scope.

## Command Center boundary

The [Command Center](agent-board/command-center.md) invokes the `claude` CLI directly with its own
arguments, resume handling, stream-JSON parser, and MCP config. It is not a registry consumer:
registering an agent does not make it a Command Center backend.
