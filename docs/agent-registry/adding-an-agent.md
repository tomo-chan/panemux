# Adding an agent

> Part of the [agent registry](../agent-registry.md). That document carries the capabilities, the
> identity rules and the summary of this one.

An agent is added by registration alone: a definition in the registry, and one adapter per
capability it declares, each listed by ID in its consumer. The common code — the collection, the
launch script, the summary pipeline, `GET /api/tasks` and the dashboard — reads the declarations and
the adapters, and never branches on an agent's name. The fixture agent (issue #377), which only a
build with the `panemux_fixture_agent` tag registers, is a third agent added that way and checked
end to end; it is the example below.

## Steps

1. Add its `ID` constant and its `Definition` to `registered()` in `internal/agent/registry.go`.
   Nothing registers itself: there is no `init()` registration, and that list — the shipped
   definitions, then `fixtureAgents()` — is the whole registry.
2. Declare only the capabilities it has, with the rule each one needs
   ([Capabilities](../agent-registry.md#capabilities)).
3. Run the `internal/agent` tests. `Validate` rejects a malformed or duplicate ID, alias or agmsg
   type, an alias that collides with another name, an unknown or repeated capability, and a
   capability whose rule is missing (or a rule whose capability is). The package builds its
   registry when it loads, so an invalid registration also stops panemux at startup.
4. `TestRegisteredAgents` pins the registered set and order; update it in the same change.
5. Add an adapter for each capability it declares, to the list the [table](#capabilities-and-where-they-are-checked)
   names, and give the capability's contract test what it asks of a new adapter (a summary adapter
   needs a contract sample). Each list is checked against the registry when panemux starts, so a
   declaration without its adapter, or an adapter without its declaration, stops it.
6. Name its CLI like its ID. The launch script and the availability probe find an agent's CLI by its
   ID, so an agent whose CLI is named otherwise cannot be launched or probed yet
   ([#428](https://github.com/tomo-chan/panemux/issues/428)).
7. Add its rows to [docs/scenarios.md](../scenarios.md) and its behavior to
   [docs/behavior/tasks.md](../behavior/tasks.md). Nothing in the API schema or the dashboard
   changes: `agents`, each host's `availability` and each task's `actions` already carry what it
   declares.

## Capabilities and where they are checked

| Capability | Consumer interface | Registered in | Contract test | In the API and the dashboard |
|---|---|---|---|---|
| `detect` | `Detection` (its Task rule, its Board `ProcessRule`) in `internal/agent/agent.go` | `registered()` (`internal/agent/registry.go`) | `TestRegisteredDefinitionsAreValid` | `agents[].capabilities`; its processes are recognized |
| `collect` | `collectAdapter` (`internal/tasks/collect_adapters.go`) | `collectAdapters` | `TestCollectAdapters_MatchTheRegistry`, `TestCollectPrefilter_CoversEveryTaskRule` | Its sessions are tasks; `hosts[].availability` (`collect`); Mark done and labels |
| `launch` | `launchAdapter` (`internal/tasks/launch_adapters.go`) | `launchAdapters` | `TestLaunchAdapters_MatchTheRegistry` | `hosts[].availability` (`launch`); offered in the New task dialog; `POST /api/tasks/launch` |
| `resume` | `resumeAdapter` (`internal/tasks/launch_adapters.go`) | `resumeAdapters` | `TestLaunchAdapters_MatchTheRegistry` | `tasks[].actions.resume`; the Resume button; `POST /api/tasks/resume` |
| `summary` | `summarySource` and `summaryRunner` (`internal/tasks/summary_adapters.go`) | `summaryAdapters` | `TestSummaryAdapters_MatchTheRegistry`, `TestSummaryAdapters_Contract` | `tasks[].actions.summary` and `tasks[].summary`; the Work section and Summarize |
| `workdir` | `workdirAdapter` (`internal/session/workdir.go`) | `workdirAdapters` | `TestWorkdirAdaptersMatchDeclarations` | A pane's working directory and log follow its agent's session |
| `board` | `boardAdapter` (`board_adapters.go`) | `boardAdapters` | `TestBoardAdapters_MatchTheRegistry` | It can join Agent Board |
| `usage` | — (declared by no agent yet, [#345](https://github.com/tomo-chan/panemux/issues/345)) | — | — | — |

An absent capability is absent everywhere: no availability row, no action on its tasks, no button,
and the route refuses it ([a task's actions](../behavior/tasks.md#agent-availability)).

## The fixture agent: a minimal adapter set

The fixture agent declares `detect`, `collect`, `launch` and `summary`, and leaves `resume`,
`workdir`, `board` and `usage` out, so it also stands for an agent supported in part. Its code is in
files that build only with the tag:

- `internal/agent/fixture_agent.go` — its `Definition`: the ID `panemux-fixture-agent`, the
  capabilities, and a Task rule that recognizes a process whose program is named like the ID, and a
  one-shot run (`--print`) as not a session.
- `internal/tasks/fixture_agent.go` — its adapters, joined to the lists through
  `fixtureCollectAdapters()`, `fixtureLaunchAdapters()` and `fixtureSummaryAdapters()`:
  - collect: two sections — the state files under `~/.panemux-fixture-agent/sessions`, and each log's
    modification time and size under `~/.panemux-fixture-agent/logs` — its process hint, and the code
    that turns a state file of a running interactive process into a task;
  - launch: a session ID panemux mints and the fixed fragment that starts the CLI with it. There is
    no resume adapter, as there is no `resume` declaration;
  - summary: a source whose fixed script prints a session's log, and a runner that summarizes with
    the CLI's `--print` mode.
- `*_off.go` beside each — the untagged build's empty `fixtureAgents()` and adapter lists, so a
  shipped binary has none of it.

What checks it:

- `make test-go-fixture-agent` runs its own tests and every contract test in the table above with
  the tag, and the HTTP routes' handling of it (`internal/api`): a launch reaches the host, a resume
  is refused and runs nothing. It writes `testdata/api-contract/tasks-fixture-agent.json`, the
  `GET /api/tasks` response with the fixture agent
  ([contract fixtures](../development/contract-fixtures.md)).
- `frontend/src/schemas/contract.test.ts` reads that capture with the shipped schemas, and
  `frontend/src/components/TaskDashboard.fixtureAgent.test.tsx` renders it: its task shows as one
  of a known agent, with its pane and summary and no Resume, and the New task dialog offers and
  starts it.
- The task-dashboard E2E (`frontend/e2e/task-dashboard.spec.ts`) runs panemux built with the tag
  against a fake CLI, `frontend/e2e/fixtures/fixture-agent/panemux-fixture-agent`, first on `PATH`:
  a running session is listed in its state with its summary, and a task is started from the New task
  dialog in a tmux session. The fixture's stand-ins for the CLIs — the fake one, and a `claude` and a
  `codex` that refuse a summary — are put first on `PATH` right before panemux starts
  (`frontend/e2e/stand-ins.sh`), so no real CLI answers for the machine's own sessions the collection
  lists.

The tagged files sit outside the coverage, red-check and mutation gates, and the Claude Code
`PostToolUse` hook vets them untagged
([the fixture agent's tagged build](../quality-gateway.md#the-fixture-agents-tagged-build)).
