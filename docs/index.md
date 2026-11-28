---
okf_version: "0.2"
---

# Documentation

This directory is the current specification, design, and operating reference for panemux (end-user
setup lives in [README.md](../README.md); developer workflow lives in
[DEVELOPMENT.md](../DEVELOPMENT.md)). Start with the overview or jump directly by layer or
cross-cutting concern.

## 1. Overview and system map

- [Application overview](overview.md) - Two-minute description of product purpose, capabilities,
  operating model, and boundaries.
- [Architecture](architecture.md) - Component ownership, state lifecycle, data flows, and trust
  boundaries.
- [Scenario coverage](scenarios.md) - User-facing acceptance ledger mapping each scenario to its
  automated or manual verification.

## 2. Layer-specific documentation

Organized by architectural layer, aligned along the same domain axis (`workspace`, `tasks`,
`agent-board`):

| Layer | Entrance | `workspace` | `tasks` | `agent-board` | Shared |
|---|---|---|---|---|---|
| **UI** | [ui/](ui/) | [workspace.md](ui/workspace.md) | [tasks.md](ui/tasks.md) | [agent-board.md](ui/agent-board.md) | [overview.md](ui/overview.md) |
| **API** | [api/](api/) | [workspace.md](api/workspace.md) | [tasks.md](api/tasks.md) | [agent-board.md](api/agent-board.md) | — |
| **Backend** | [backend/](backend/) | [workspace/](backend/workspace/) | [tasks/](backend/tasks/) | [agent-board/](backend/agent-board/) | [agent-registry.md](backend/agent-registry.md) |

## 3. Cross-cutting concerns

| Concern | Entrance | Scope |
|---|---|---|
| **Security** | [security/overview.md](security/overview.md), [security/](security/) | Invariants, auth/browser guard, process/SSH, tasks, Agent Board, and split preview/URL-open boundaries |
| **Engineering** | [engineering/](engineering/) | Quality gates, testing contracts, runtimes, sandboxes, Codex verification, screenshots, CI, releases, and agmsg maintenance |
| **Decisions** | [decisions/](decisions/) | Historical design rationale and rejected alternatives grouped into one topic file per domain |

## Bundle contents

* [Documentation](README.md) - GitHub landing page linking to the canonical documentation index.
* [Application Overview](overview.md) - Product purpose, capabilities, operating model, and current boundaries.
* [Architecture](architecture.md) - System structure, component ownership, data flow, and trust boundaries.
* [Scenario Coverage](scenarios.md) - User-facing acceptance scenarios and the automated or manual verification for each.
* [ui/](ui/) - Shared UI principles, frontend runtime, and domain screen contracts.
* [api/](api/) - REST API and WebSocket wire contracts grouped by domain.
* [backend/](backend/) - Server-side runtime behavior, configuration, sessions, tasks, Agent Board, and agent registry.
* [security/](security/) - Security requirements grouped by execution sink and trust boundary.
* [engineering/](engineering/) - Quality gates, testing contracts, runtimes, sandboxes, screenshots, CI, releases, and agmsg maintenance.
* [decisions/](decisions/) - Design decisions grouped into topic documents.
* [images/](images/) - Documentation screenshots captured from the product with placeholder content.
* [models/](models/) - Formal models of terminal replay behavior.

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
  cohesive documents (roughly 100–300 lines) over proliferation of tiny files. Place every topic
  entrance inside its directory (`docs/<topic>/index.md`, plus `overview.md` when needed) rather
  than adding top-level `docs/<topic>.md` files alongside the directory.
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
