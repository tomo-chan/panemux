# Development decisions

## Decisions

* [Current-state guides and historical decisions are separate (2026-09-23)](current-state-and-history.md) - Separate current contracts, focused detail and history so readers need not infer behavior from rollout narration.
* [Retaking the screenshots is checked by diff, not by pixels (2026-10-03, issue #285)](screenshot-diff-check.md) - Require screenshot diffs instead of brittle pixel comparisons across timestamps and font rendering environments.
* [Screenshots are generated, and the task dashboard's tasks are a fixture (2026-10-03, issue #285)](screenshot-fixtures.md) - Capture placeholder state and stub dashboard collection instead of exposing the developer's sessions.
* [The screenshots' fixed paths are links into the run's own root (2026-10-10, issue #328)](screenshot-owned-roots.md) - Keep screenshot markers under the run root and use fixed symlinks that tolerate temporary-file cleanup safely.
* [The screenshot-helper tests choose where tmux's sockets go (2026-10-10, issue #343)](screenshot-socket-paths.md) - Choose screenshot-test socket roots by resolved path length to respect macOS limits across sandbox environments.
