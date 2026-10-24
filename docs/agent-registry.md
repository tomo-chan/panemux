# Agent Registry

`internal/agent` is the one place panemux names a coding agent. Every agent panemux recognizes is
a `Definition` there: its stable ID, display name, aliases, agmsg type name, the process rules that
detect it, and the capabilities it declares. Other packages ask the registry rather than spelling
an agent's name or binary themselves.

The package imports nothing else from panemux, so `internal/session`, `internal/tasks` and every
later consumer can import it without a cycle.

## Registered agents

| ID | Display name | Aliases | agmsg type | Capabilities |
|---|---|---|---|---|
| `claude` | Claude Code | `claude-code` | `claude-code` | detect, collect, launch, resume, workdir, summary, board |
| `codex` | Codex | — | `codex` | detect, collect, launch, resume, workdir, summary, board |
| `cursor` | Cursor | — | `cursor` | detect, board |
| `gemini` | Gemini CLI | — | `gemini` | detect, board |
| `grok-build` | Grok Build | — | `grok-build` | detect, board |
| `opencode` | opencode | — | `opencode` | detect, board |

The order is the matching order: the first Board rule that matches a process names its type.
The four detect-and-board agents are Agent Board's alone; the task board does not list them.

## Identity

- **ID** (`agent.ID`) is what records, task IDs and the API carry. It is lower case, starts with a
  letter, and joins words with single hyphens. A stored ID never changes. `ID.Valid` checks the
  shape only: a stored record may name an agent this build does not register.
- **Aliases** are other names for the same agent — `claude-code` is the directory an npm install of
  Claude Code lives in. `agent.Resolve` maps an ID or an alias to its ID, exactly.
- **agmsg type** is the agent's type name in agmsg's own scripts. `agent.ByAgmsgType` maps it back.
- IDs, aliases and agmsg types share the same shape rule. IDs and aliases are unique across all of
  them together; agmsg types are unique among themselves.

### Session keys

`agent.SessionKey{Host, Agent, SessionID}` identifies one agent session. `Host` is the
`ssh_connections` key, or `""` for the panemux host, as in a task record. A session ID is unique
only within its agent on its host, so the same session ID under another agent or on another host is
another session. A key is valid only when its session ID is known: a session whose ID is still
being decided has no key and is not matched to any keyed session.

Where a session is found is not part of what it is:

- a **pane** may show a session's process, or none — a session started outside panemux has no pane;
- a **PID** is not a key, since one shared daemon process (codex's app-server) can host several
  sessions, and PIDs are reused after a host restarts;
- a **tmux session** is where a session runs, and a tmux session can outlive the agent in it.

## Capabilities

An agent declares any subset of these. A capability is a declaration of what panemux supports for
the agent, not a statement that it works on a given host right now. Whether `collect`, `launch` and
`resume` work on a host is found out per host and kept inside `internal/tasks` as
[agent availability](behavior/tasks.md#agent-availability): a probe for the agent's CLI, and what the
collection could not read. [`GET /api/tasks`](behavior/tasks.md#get-apitasks) carries the registered
agents with their declarations (`agents`), each host's availability, and each task's actions with why
one cannot be taken, so the dashboard offers what the registry and the host allow rather than what an
agent's name suggests.

| Capability | Meaning | Rule the registry checks |
|---|---|---|
| `detect` | Its processes are recognized | at least one detection rule |
| `collect` | Its sessions and their state are listed on the task board | a Task rule (a Task rule needs `collect` or `workdir`) |
| `launch` | panemux can start a new session | — |
| `resume` | panemux can reopen one of its sessions | — |
| `workdir` | Its working directory and conversation log can be resolved | a Task rule |
| `summary` | Its sessions can be summarized | — |
| `usage` | Its usage limits and spend can be read; declared by no agent yet (#345) | — |
| `board` | It can join Agent Board through agmsg | an agmsg type and a Board rule |

The registry holds declarations; the code behind each capability is in its consumer. `collect` is
implemented by a collect adapter per agent, which `internal/tasks` defines in its own types and lists
explicitly by ID in `collectAdapters` (`internal/tasks/collect_adapters.go`): each adapter declares
its collection script sections, named `<agent ID>/<name>`, and turns their rows into tasks. When
panemux starts, the adapters are checked against the registry — an agent that declares `collect`
has exactly one adapter and no other agent has one — and an invalid set stops it. A collect adapter
also gives the fixed note on what a task's state means for its agent (`stateNote`, the task's
`state_note`).

`launch` and `resume` are implemented the same way, by a launch and a resume adapter per agent listed
in `launchAdapters` and `resumeAdapters` (`internal/tasks/launch_adapters.go`), checked against the
registry when panemux starts. They are separate, so an agent can launch without resuming. A launch
adapter names a new session — the session ID it is started with, or none when the agent picks its own
once it runs, and its tmux session — and a resume adapter checks the session ID it is given and names
the tmux session the resume runs in. A launch adapter also gives the fixed note on what to know
before starting one (`note`, the agent's `notes.launch`); notes name nothing about a host or a
request. Each supplies a fixed shell fragment that the one launch script
composes under the case of its agent and mode; a fragment takes no request value, and refuses only
with codes the script or its adapter declares. A launch that names no session ID has its labels held
until a collection finds exactly one session of that agent in its tmux session
([Starting a task](behavior/tasks.md#starting-a-task)). An agent that declares neither is refused,
and so is one whose CLI the host's probe found missing ([agent availability](behavior/tasks.md#agent-availability)).
`workdir` is implemented by a workdir adapter per agent, which `internal/session` defines in its own
types and lists explicitly by ID in `workdirAdapters` (`internal/session/workdir.go`); a test checks
the list against the registry's `workdir` declarations. It works per session, not per pane: `locate`
turns an agent process into a `WorkdirSession` — its `SessionKey`, PID, recorded cwd and log — and
`workdirs` lists every working directory of that session, its subagents' transcripts included. Where
the session runs, the panemux host or an SSH host, is a host abstraction that only fetches; what the
files mean is decided once, in the adapter. A pane finds its agent with `agent.MatchInteractiveSession`
and calls both; a task names its session with `Task.WorkdirSession`, which the task board is to hand
to `workdirs` to show every working directory of a task (#420). Claude Code's `~/.claude/sessions` files are read by one parser,
`session.ParseClaudeSessionState`, which the task board wraps to keep its unreadable-file reasons.
`summary` is implemented by a summary adapter per agent, which `internal/tasks` defines in its own
types and lists explicitly by ID in `summaryAdapters` (`internal/tasks/summary_adapters.go`), checked
against the registry's `summary` declarations when panemux starts. An adapter is two parts: a
`summarySource`, which says which session IDs it accepts, gives the fixed script that prints the log
on the session's host, and turns that output into the excerpt; and a `summaryRunner`, which
summarizes the excerpt with the agent's own CLI on the panemux host. The common code owns the rest —
which tasks are summarized, the cache and its reuse by excerpt hash, retries, concurrency, timeouts,
the saved file and `tasks[].actions.summary` — and never branches on an agent's ID. A task is
summarized by its own agent's runner, never another's. Each part has a version folded into the
summary's identity; raising it makes earlier summaries outdated. A source at its first version
(`""`) adds nothing to it. A runner says in its own words when an answer came from a model other than
the one it asked for, or says nothing.

`board` is implemented by a board adapter per agent, which the root package defines in its own types
and lists explicitly by ID in `boardAdapters` (`board_adapters.go`), checked against the registry when
panemux starts. The onboarding instruction's shared steps — joining, sending, the status report and
the delivery mode — are the same for every agent; an adapter supplies what differs, which today is
only Claude Code's identity claim and its watcher replacement
([agmsg integration](agent-board/agmsg-integration.md)). The capability is a contract with an external
tool: it needs agmsg on the pane's host, which the bootstrap watcher checks before writing anything,
and its agents are found by the Board rules, which mirror agmsg's own detection and stay apart from
the other rules.

Which agents can be given a done or label record (`internal/tasks`'s `recordable`) is read from the
`collect` declarations; a stored record of any other agent is kept and written back unchanged
([Done and labels](behavior/tasks.md#done-and-labels)).

A running task carries what a later capability needs to find its working directories without
collecting again: its session key (`Task.Key`), its pid, its working directory and, for codex, the
rollout it was found from.

### Formats and versions

Agents change faster than panemux follows them. The registry carries no versions: no ID or
capability is per release.

Each detection rule records:

- `VerifiedWith`, the release it was last checked against: `agmsg v1.1.13` for every Board rule,
  `codex-cli 0.157.0` for codex's Task rule, and nothing recorded for the others.
- `UnknownArgs`, what it does with an argument it does not know — a flag or subcommand a newer
  release added. Every registered rule is `accept`: an unknown argument counts as a session, so a
  new headless subcommand is mistaken for one until the rule learns it. `reject` would instead miss
  a new interactive mode until the rule learns it.

What an agent writes is not gated on a version either: a format change is noticed instead.
The Claude Code state file's `version` and codex's `session_meta` `cli_version` are recorded and
never compared. What the collection cannot read — structure it cannot parse, a status or turn event
outside the set it knows — is listed with its state `unknown` rather than guessed, notifies nothing,
and makes that agent's `collect` `degraded` / `unknown_format` on that host
([Agent availability](behavior/tasks.md#agent-availability)). An unknown argument stays `accept`.

## Detection rules

Two rule sets exist: the Board rules mirror agmsg's own detection, an external contract, and the Task
rules are panemux's own, shared by the task board and the pane header. Within one rule set the agents
are tried in registration order, and the first whose program matches decides: when the process is a
headless run of that agent (a Board exclude token among the arguments, or a Task rule's process that
is not interactive) it is no agent's, even if a later agent's rule would also match the program.

| Rule | Used by | Matches |
|---|---|---|
| Board | Agent Board's agent-type polling (`AgentTypeDetector`) | Program base name against patterns (a trailing `*` matches any suffix), no exclude token among the arguments. Mirrors agmsg's own `type.conf` `detect_proc`. |
| Task | The task board's collection, and a pane's active working directory (`ActiveWorkdirGetter`, through `MatchInteractiveSession` among the agents that declare `workdir`) | Claude Code: a path component of the program, or of the script a `node`, `bun` or `deno` interpreter runs, is `claude` or `claude-code`, and it is not `-p`/`--print`. codex: base name `codex`, and its first positional argument is not a non-interactive subcommand. |

The Board exclude tokens are `-p` and `--print` for Claude Code and `exec` for codex. agmsg documents
no headless invocation for the other four agents, so their Board rules have none. The Task rule reads
codex's arguments as codex does: options that take a value (`-m`, `-c`, `-p`/`--profile`, `-C`, …)
are skipped with their value, so `codex -m x exec` is headless and `codex -p exec` is not; `-i/--image`
takes every argument up to the next option, so `codex -i a.png review` is a session. Because a
path component of the program decides for Claude Code, a program such as `/opt/claude/bin/codex` is
Claude Code's.

agmsg itself does no process detection for antigravity, copilot or hermes, and agmsg-app is the
desktop app's own user, so none of them is registered; an operator on one of them joins agmsg by
hand ([bootstrap](agent-board/bootstrap.md)).

## Adding an agent

1. Add its `ID` constant and its `Definition` to `registered()` in `internal/agent/registry.go`.
   Nothing registers itself: there is no `init()` registration, and that list is the whole
   registry.
2. Declare only the capabilities it has, with the rule each one needs.
3. Run the `internal/agent` tests. `Validate` rejects a malformed or duplicate ID, alias or agmsg
   type, an alias that collides with another name, an unknown or repeated capability, and a
   capability whose rule is missing (or a rule whose capability is). The package builds its
   registry when it loads, so an invalid registration also stops panemux at startup.
4. `TestRegisteredAgents` pins the registered set and order; update it in the same change.
5. An agent that declares `collect` needs a collect adapter in `internal/tasks`, added to
   `collectAdapters`: its sections, its process hints (words the collection's `cwd`/`env` prefilter
   must find in every process its Task rule lists), and the code that turns its rows into tasks.
   `TestCollectAdapters_MatchTheRegistry` and `TestCollectPrefilter_CoversEveryTaskRule` check it.
6. An agent that declares `summary` needs a summary adapter in `internal/tasks`, added to
   `summaryAdapters` as `{id, source, runner}`. The source's `logScript` must be a fixed script that
   takes nothing but a session ID its `validSessionID` accepted (and the collected log version),
   returning `ErrInvalidSummary` for any other ID; `excerpt` reports a log it cannot read at all as an
   error and one with nothing it understands as not ok (unreadable). The runner keeps the agent's own
   authentication, settings and tool limits, and returns fixed error messages that quote nothing the
   CLI printed (`errSummaryCLIMissing` when the CLI is not on `PATH`).
   `TestSummaryAdapters_MatchTheRegistry` checks the list against the declarations, and the new
   adapter's own test calls `checkSummaryAdapterContract` with a session ID and a sample of its
   script's output. The test-only `third` agent in `internal/tasks/summary_adapters_test.go` is the
   smallest example of a source, a runner and their registration.

## Command Center boundary

The [Command Center](agent-board/command-center.md) runs the `claude` CLI itself, with its own
arguments, resume handling, stream format and MCP configuration. It is not a registry consumer:
registering an agent makes it detectable and manageable, not a Command Center backend, and a
Command Center for another agent would be its own extension.

## Related documents

- [Architecture](architecture.md) — where `internal/agent` sits.
- [Task dashboard behavior](behavior/tasks.md) — what the task board does with the Task rules.
- [Agent Board bootstrap](agent-board/bootstrap.md) — what Agent Board does with the Board rules.
- [Decision log](DECISIONLOG.md#agent-registry) — why the registry has this shape.
