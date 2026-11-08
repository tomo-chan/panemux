---
type: Decision
title: "Screenshots are generated, and the task dashboard's tasks are a fixture (2026-10-03, issue #285)"
description: "Rationale for Screenshots are generated, and the task dashboard's tasks are a fixture."
---

# Screenshots are generated, and the task dashboard's tasks are a fixture (2026-10-03, issue #285)

The README and docs images are captured by `make screenshots` (Playwright, `frontend/screenshots/`)
rather than by hand, so they can be regenerated when the UI changes. The capture has its own
Playwright config instead of being an e2e spec: it writes tracked files under `docs/images/`, and
neither `make test-e2e` nor CI should do that or fail on a pixel difference.

The workspace, pane settings and Agent Board images come from the real server against placeholder
state (a fake `HOME`, a sample git repository, a private tmux socket, a stub agmsg store). The task
dashboard image does not: the real collection lists every claude and codex process of the user
running it (`ps -U <own uid>`), so a developer's own sessions and their working directories would
appear in the image. The capture serves `/api/tasks` from a fixed placeholder list and keeps the
task event stream from connecting instead.

Current contracts: [development guide](../../index.md).
