---
type: Runbook
title: "Adding an agent"
description: "Steps and verification requirements for registering and integrating a coding agent."
---

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
3. Run the `internal/agent` tests. `Validate` rejects a malformed or duplicate ID or alias, an alias that collides with another name, an unknown or repeated capability, and a
   capability whose rule is missing (or a rule whose capability is). The package builds its
   registry when it loads, so an invalid registration also stops panemux at startup.
4. `TestRegisteredAgents` pins the registered set and order; update it in the same change.
5. Add an adapter for each capability it declares, to the list the [table](#capabilities-and-where-they-are-checked)
   names, and give the capability's contract test what it asks of a new adapter (a summary adapter
   needs a contract sample). Each list is checked against the registry when panemux starts, so a
   declaration without its adapter, or an adapter without its declaration, stops it.
   For `board`, add the ID-to-type entry and any participation difference in the
   [message adapter](../agent-board/agmsg-integration.md#agent-type-mapping); keep transport type
   names out of the registry and session detection.
6. Name its CLI like its ID. The launch script and the availability probe find an agent's CLI by its
   ID, so an agent whose CLI is named otherwise cannot be launched or probed yet
   ([#428](https://github.com/tomo-chan/panemux/issues/428)).
7. Add its rows to [docs/scenarios.md](../scenarios.md) and its behavior to
   [docs/behavior/task-collection.md](../behavior/task-collection.md). Nothing in the API schema or the dashboard
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
| `board` | `MessageClient` / `Participation` (`internal/board`), implemented by `internal/agmsg` | agent ID → transport type (`internal/agmsg/agent_adapters.go`) | `TestBoardAdapters_MatchTheRegistry` | It can be detected for Agent Board display; detection does not join it |
| `usage` | — (declared by no agent yet, [#345](https://github.com/tomo-chan/panemux/issues/345)) | — | — | — |

An absent capability is absent everywhere: no availability row, no action on its tasks, no button,
and the route refuses it ([a task's actions](../behavior/task-collection.md#agent-availability)).

## The fixture agent: a minimal adapter set

The fixture agent declares `detect`, `collect`, `launch` and `summary`, and leaves `resume`,
`workdir`, `board` and `usage` out, so it also stands for an agent supported in part. The tagged example lives in
`internal/agent/fixture_agent.go` and `internal/tasks/fixture_agent.go`; the adjacent `*_off.go`
files return empty lists in the shipped build.

Its ID and CLI name are `panemux-fixture-agent`. Collection reads state files under
`~/.panemux-fixture-agent/sessions` and log modification times/sizes under
`~/.panemux-fixture-agent/logs`, turning running interactive processes into tasks. Launch mints a
session ID; summary reads the log and runs the CLI in `--print` mode. Its Task rule excludes that
one-shot mode. There is no
resume adapter. The common pipelines and API/dashboard consume these adapters without agent-name
branches.

The [tagged-build contract](../quality-gateway.md#the-fixture-agents-tagged-build) owns verification:
registry-wide adapter checks (including the absent board/workdir adapters), HTTP launch reaching the host/resume refused before execution, Go-produced `tasks-fixture-agent.json`, frontend schema/rendering checks, and isolated E2E
against a fake CLI. It also states the untagged coverage/red-check/mutation scope and hook limit.
