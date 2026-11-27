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
| Which security rules constrain a change? | [Security overview](security/overview.md) |
| How should the interface behave and look? | [UI overview](ui/overview.md), [UI index](ui/index.md) |
| What are the REST and WebSocket wire contracts? | [API index](api/index.md) |
| Which agents are known, and how is one added? | [Agent registry](agent-registry.md), [Adding an agent](agent-registry/adding-an-agent.md) |
| How does Agent Board work? | [Agent Board](agent-board.md) |
| What must the test system protect? | [Quality gates](engineering/quality-gates.md) |
| How is CI, dependency, and release work performed? | [CI and releases](engineering/ci-and-releases.md) |
| Which user scenarios are verified? | [Scenario coverage](scenarios.md) |

Developer workflow is defined in [DEVELOPMENT.md](../DEVELOPMENT.md), outside this directory because
it is also the day-to-day command reference.

## 3. Deep dives

Topic guides link to focused deep dives when the summary is not enough:

- [UI details](ui/) - Shared UI principles, frontend runtime, modals, workspace and pane chrome,
  layout editing, previews, task dashboard, attention, and Agent Board presentation.
- [API contracts](api/) - Workspace, task dashboard, and Agent Board REST and WebSocket protocols.
- [Behavior details](behavior/) - SSH, split preview, URL opening, task events, and the task
  dashboard.
- [Security details](security/) - requirements grouped by command or trust boundary.
- [Agent Board details](agent-board/) - architecture, relay, bootstrap, command center, integration,
  and limitations.
- [Engineering details](engineering/) - quality gates, testing contracts, runtimes, sandboxes,
  screenshots, CI, releases, and agmsg maintenance.
- [Decisions](decisions/index.md) - historical decisions grouped by topic.

## Decision history

[Decisions](decisions/index.md) records why consequential choices were made and what they replaced.
Decisions are grouped into one `type: Decision` topic document per domain under `decisions/<topic>.md`;
[decisions/index.md](decisions/index.md) lists those topics. Current-state contracts stay in guides
and specifications; decisions link to those authorities rather than repeating them. Git history
remains the authority for line-level chronology.

## Bundle contents

* [Documentation](README.md) - GitHub landing page linking to the canonical documentation index.
* [Agent Board](agent-board.md) - Agent Board capabilities, design principles, and routes to focused contracts.
* [Agent Registry](agent-registry.md) - Agent registration, identities, capabilities, and routes to integration requirements.
* [Architecture](architecture.md) - System structure, component ownership, data flow, and trust boundaries.
* [Behavior Specification](behavior.md) - Startup, configuration, operational assumptions, and routes to runtime contracts.
* [Application Overview](overview.md) - Product purpose, capabilities, operating model, and current boundaries.
* [Scenario Coverage](scenarios.md) - User-facing acceptance scenarios and the automated or manual verification for each.
* [agent-board/](agent-board/) - Agent Board architecture, messaging, integration, and command center contracts.
* [agent-registry/](agent-registry/) - Agent integration procedures and capability requirements.
* [api/](api/) - REST API and WebSocket wire contracts grouped by domain.
* [ui/](ui/) - Shared UI principles, frontend runtime, and domain screen contracts.
* [behavior/](behavior/) - Runtime behavior, task events, and task dashboard details.
* [decisions/](decisions/) - Design decisions grouped into topic documents.
* [engineering/](engineering/) - Quality gates, testing contracts, runtimes, sandboxes, screenshots, CI, releases, and agmsg maintenance.
* [images/](images/) - Documentation screenshots captured from the product with placeholder content.
* [models/](models/) - Formal models of terminal replay behavior.
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
  history, not a concept. Decision topic documents use `type: Decision`.
- The directory hierarchy is a tree; Markdown links between concepts are graph edges. Explain the
  relationship in surrounding prose and use relative paths, never `/`-prefixed bundle paths,
  because GitHub resolves the latter from the repository root.
- Split by responsibility without over-fragmenting: one concept is authoritative for each contract
  or rule, and other concepts link to that authority rather than duplicating its contract. Prefer
  cohesive documents (roughly 100–300 lines) over proliferation of tiny files. When creating or
  reorganizing a topic directory, place its entrance inside the directory (`docs/<topic>/index.md`,
  plus `overview.md` when needed) rather than adding a separate top-level `docs/<topic>.md`
  (existing top-level topic guides are being migrated under #553).
- Write all bundle documentation in English; do not maintain parallel translated copies.
- Update actual references when a document moves: Markdown links and filename labels, Go/TS/shell
  comments, and expected documentation paths in tests. Do not leave redirect stub files behind.
  Identically named unrelated documents and independent fixtures retain their own paths.
- Run `make check-docs-okf` and `make test-docs-okf` for metadata/index changes, alongside the
  existing link checks. The OKF gate checks parseable YAML and a non-empty string `type` on
  concepts, and section headings with described link-list entries in indexes. It accepts optional
  metadata and unknown types; title/description completeness and matching index summaries are
  writing/review responsibilities. CI runs the checker and tests explicitly, and pre-push selects
  them for bundle Markdown or checker changes. Symlink directories are not traversed; their paths
  are reported as not inspected. It does not replace link/anchor checks or validate log history.
- Put product purpose and broad boundaries in `overview.md`.
- State current behavior and requirements in the present tense. Avoid rollout tables, "landed" or
  "previously" notes, stale measurements, and implementation status narratives in specification
  documents.
- Document intent, invariants, external contracts, and failure semantics. Do not inventory types,
  functions, tests, or control flow that a code reader can discover directly; name implementation
  locations only when they identify where a rule is enforced.
- Record consequential design rationale, rejected alternatives, or incident lessons as a dated
  section inside the matching topic file under `decisions/<topic>.md` (`type: Decision`). Update
  the current-state authority in the same change, and prune superseded or purely historical notes
  that add no value beyond Git history.
- Keep dated one-off measurements out of timeless specifications; summarize lasting conclusions in
  the relevant `decisions/<topic>.md` entry instead.
- Put user-visible acceptance coverage in `scenarios.md`; do not duplicate it as prose test status.
