---
type: Specification
title: "Agent capability adapters"
description: "Consumer-owned adapters, registry validation, and per-capability integration contracts."
---

# Agent capability adapters

The [registry](../agent-registry.md#capabilities) declares capabilities; their consumers implement
them. Use [Adding an agent](adding-an-agent.md) for registration steps and the consumer/test matrix.

## Collection

`collect` is implemented by a collect adapter per agent, which `internal/tasks` defines in its own types and lists
explicitly by ID in `collectAdapters` (`internal/tasks/collect_adapters.go`): each adapter declares
its collection script sections, named `<agent ID>/<name>`, and turns their rows into tasks. When
panemux starts, the adapters are checked against the registry — an agent that declares `collect`
has exactly one adapter and no other agent has one — and an invalid set stops it. A collect adapter
also gives the fixed note on what a task's state means for its agent (`stateNote`, the task's
`state_note`).

## Launch and resume

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
([Starting a task](../behavior/task-launch.md#starting-a-task)). An agent that declares neither is refused,
and so is one whose CLI the host's probe found missing ([agent availability](../behavior/task-collection.md#agent-availability)).

## Working directories

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

## Summaries

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

## Board participation

`board` uses messaging and participation ports defined by `internal/board`. Session detection returns
an `agent.ID` from the registry's Board rules. The external adapter supplies the transport type and
availability checks; its mapping must cover exactly the agents declaring `board`, checked
at startup and in tests. See [agmsg integration](../agent-board/agmsg-integration.md#agent-type-mapping)
for that mapping. A capability declaration does not mean
the transport is installed on a host; detection checks availability without writing to the pane.
