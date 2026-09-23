# Documentation

This directory describes panemux at three levels. Start at the level that matches the question you
are trying to answer; a reader should not have to reconstruct the current product from its project
history.

## 1. Overview

Read [Application overview](overview.md) for a two-minute description of the product, its main
capabilities, data flow, and current boundaries.

## 2. Topic guides

Topic guides are concise explanation boards. They state the current model, the rules that always
apply, and where to continue. They do not narrate how the model evolved.

| Question | Guide |
|---|---|
| How is the system arranged? | [Architecture](architecture.md) |
| What does the running product do? | [Behavior specification](behavior.md) |
| Which security rules constrain a change? | [Security design](security.md) |
| How should the interface behave and look? | [UI design](ui-design.md) |
| How does Agent Board work? | [Agent Board](agent-board.md) |
| What must the test system protect? | [Quality gateway](quality-gateway.md) |
| How is CI, dependency, and release work performed? | [Maintenance guide](maintenance.md) |
| Which user scenarios are verified? | [Scenario coverage](scenarios.md) |

Developer workflow is defined in [DEVELOPMENT.md](../DEVELOPMENT.md), outside this directory because
it is also the day-to-day command reference.

## 3. Deep dives

Topic guides link to focused deep dives when the summary is not enough:

- [Behavior details](behavior/) — REST, WebSocket, frontend, SSH, notifications, and URL opening.
- [Security details](security/) — requirements grouped by command or trust boundary.
- [Agent Board details](agent-board/) — architecture, relay, bootstrap, command center, integration,
  limitations, and tests.
- [Quality-gateway details](quality-gateway/) — numbered decisions, measurements, and mutation
  findings.
- [Development details](development/) — test granularity, fixtures, coverage, mutation, red-check,
  and model checking.

## Decision history

[DECISIONLOG.md](DECISIONLOG.md) records why consequential choices were made, what they replaced,
and the order in which the design changed. Topic guides describe only the current state. Deep dives
focus on current contracts but may retain dated evidence or incident detail that is necessary to
justify a local requirement; cross-cutting chronology and superseded designs belong in the decision
log. Git history remains the authority for line-level change history.

## Writing rules

- Put product purpose and broad boundaries in `overview.md`.
- Keep each top-level topic guide short enough to scan. Put per-surface or per-component detail in a
  same-named directory and link to it through a document map.
- State current behavior and requirements in the present tense. Avoid rollout tables, "landed" or
  "previously" notes, stale measurements, and implementation status narratives in specification
  documents.
- Add a dated entry to `DECISIONLOG.md` when the reason, rejected alternative, migration sequence,
  or incident matters to future maintainers. Update the current-state document in the same change.
- Put measurements beside the topic they measure and identify their commit/date. Measurements are
  evidence, not timeless specification.
- Put user-visible acceptance coverage in `scenarios.md`; do not duplicate it as prose test status.
