---
okf_version: "0.2"
---

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
| Which agents are known, and how is one added? | [Agent registry](agent-registry.md), [Adding an agent](agent-registry/adding-an-agent.md) |
| How does Agent Board work? | [Agent Board](agent-board.md) |
| What must the test system protect? | [Quality gateway](quality-gateway.md) |
| How is CI, dependency, and release work performed? | [Maintenance guide](maintenance.md) |
| Which user scenarios are verified? | [Scenario coverage](scenarios.md) |

Developer workflow is defined in [DEVELOPMENT.md](../DEVELOPMENT.md), outside this directory because
it is also the day-to-day command reference.

## 3. Deep dives

Topic guides link to focused deep dives when the summary is not enough:

- [Behavior details](behavior/) - REST, WebSocket, frontend, SSH, notifications, URL opening, and the
  task dashboard.
- [Security details](security/) - requirements grouped by command or trust boundary.
- [Agent Board details](agent-board/) - architecture, relay, bootstrap, command center, integration,
  limitations, and tests.
- [Quality-gateway details](quality-gateway/) - numbered decisions, measurements, and mutation
  findings.
- [Development details](development/) - runtime and sandbox setup, screenshots, browser fixtures,
  pre-push selection, and focused testing contracts.

## Decision history

[DECISIONLOG.md](DECISIONLOG.md) records why consequential choices were made, what they replaced,
and the order in which the design changed. Topic guides describe only the current state. Deep dives
focus on current contracts but may retain dated evidence or incident detail that is necessary to
justify a local requirement; cross-cutting chronology and superseded designs belong in the decision
log. Git history remains the authority for line-level change history.

## Bundle contents

* [Decision Log](DECISIONLOG.md) - Rationale, rejected alternatives, and chronology of consequential design decisions.
* [Documentation](README.md) - GitHub landing page linking to the canonical documentation index.
* [Agent Board](agent-board.md) - Agent Board capabilities, design principles, and routes to focused contracts.
* [Agent Registry](agent-registry.md) - Agent registration, identities, capabilities, and routes to integration requirements.
* [Architecture](architecture.md) - System structure, component ownership, data flow, and trust boundaries.
* [Behavior Specification](behavior.md) - Startup, configuration, operational assumptions, and routes to runtime contracts.
* [Maintenance Guide](maintenance.md) - Maintenance procedures for toolchains, dependencies, CI, releases, and Agent Board tools.
* [Application Overview](overview.md) - Product purpose, capabilities, operating model, and current boundaries.
* [Quality Gateway](quality-gateway.md) - Product and test quality characteristics, verification gates, and enforcement responsibilities.
* [Scenario Coverage](scenarios.md) - User-facing acceptance scenarios and the automated or manual verification for each.
* [Security Design](security.md) - Implementation security invariants, development trust boundaries, and routes to sink requirements.
* [UI Design](ui-design.md) - Interface principles and interaction contracts for workspaces, panes, dialogs, and tasks.
* [agent-board/](agent-board/) - Agent Board architecture, messaging, integration, security, and verification contracts.
* [agent-registry/](agent-registry/) - Agent integration procedures and capability requirements.
* [behavior/](behavior/) - Runtime behavior, API contracts, task events, and task dashboard details.
* [development/](development/) - Development and verification contracts, fixtures, and operational procedures.
* [images/](images/) - Documentation screenshots captured from the product with placeholder content.
* [models/](models/) - Formal models of terminal replay behavior.
* [quality-gateway/](quality-gateway/) - Quality gate decisions and dated measurements.
* [security/](security/) - Security requirements grouped by execution sink and trust boundary.

## Writing rules

- Treat `docs/` as one [Open Knowledge Format (OKF) v0.2 bundle](https://github.com/GoogleCloudPlatform/open-knowledge-format/blob/main/SPEC.md).
  The root `README.md`, `AGENTS.md`, `DEVELOPMENT.md`, and `CLAUDE.md` are outside the bundle.
  Every `.md` except the reserved `index.md` and `log.md` is a concept, including `docs/README.md`.
  Begin concepts with YAML frontmatter containing `type`, `title`, and a one-sentence `description`.
  Use `status` (`draft`, `stable`, `deprecated`) and `tags` when useful; do not invent provenance or
  verification metadata merely to populate optional fields.
- Choose descriptive types: `Overview` for product purpose, `Guide` for topic entrances,
  `Specification` for runtime or development contracts, `Security Requirement` for safety
  requirements, `Decision` for rationale, `Reference` for measurements and lists, and `Runbook`
  for operating procedures. These are writing vocabulary, not a closed schema: unknown types and
  additional metadata remain valid under OKF §11.
- Keep `docs/index.md` as the canonical directory entrance and `docs/README.md` as a short GitHub
  landing page linking here. Each subdirectory has an `index.md` with section headings and entries
  such as `* [Application Overview](overview.md) - Product purpose, capabilities, operating model, and current boundaries.` Include a description
  for resource files and subdirectories too. Indexes have no frontmatter except this bundle-root
  index, whose frontmatter contains only `okf_version: "0.2"`. Reserve `log.md` for OKF §9 update
  history, not a concept; `DECISIONLOG.md` remains a concept with `type: Decision`.
- The directory hierarchy is a tree; Markdown links between concepts are graph edges. Explain the
  relationship in surrounding prose and use relative paths, never `/`-prefixed bundle paths,
  because GitHub resolves the latter from the repository root.
- Split by responsibility: one concept owns one responsibility, and one concept is authoritative
  for each contract or rule. Other concepts link to that authority rather than duplicating its
  contract. Keep `docs/<name>.md` as the topic entrance when details live in `docs/<name>/`;
  that directory's `index.md` lists its contents rather than replacing the topic concept.
- Keep translations as ordinary concepts with the same type as the original, localized titles and
  descriptions, and an index entry identifying the translation. For task events, the English
  document remains authoritative and changes update both versions in the same pull request.
- Update actual references when a document moves: Markdown links and filename labels, Go/TS/shell
  comments, and expected documentation paths in tests. Identically named unrelated documents and
  independent fixtures retain their own paths.
- Run `make check-docs-okf` and `make test-docs-okf` for metadata/index changes, alongside the
  existing link checks. The OKF gate checks parseable YAML and a non-empty string `type` on
  concepts, and section headings with described link-list entries in indexes. It accepts optional
  metadata and unknown types; title/description completeness and matching index summaries are
  writing/review responsibilities. CI runs the checker and tests explicitly, and pre-push selects
  them for bundle Markdown or checker changes. Symlink directories are not traversed; their paths
  are reported as not inspected. It does not replace link/anchor checks or validate log history.
- Put product purpose and broad boundaries in `overview.md`.
- Keep each top-level topic guide short enough to scan. Put per-surface or per-component detail in a
  focused concept grouped by topic and link to it through a document map and directory index.
- State current behavior and requirements in the present tense. Avoid rollout tables, "landed" or
  "previously" notes, stale measurements, and implementation status narratives in specification
  documents.
- Document intent, invariants, external contracts, and failure semantics. Do not inventory types,
  functions, tests, or control flow that a code reader can discover directly; name implementation
  locations only when they identify where a rule is enforced.
- Add a dated entry to `DECISIONLOG.md` when the reason, rejected alternative, migration sequence,
  or incident matters to future maintainers. Update the current-state document in the same change.
- Put measurements beside the topic they measure and identify their commit/date. Measurements are
  evidence, not timeless specification.
- Put user-visible acceptance coverage in `scenarios.md`; do not duplicate it as prose test status.
