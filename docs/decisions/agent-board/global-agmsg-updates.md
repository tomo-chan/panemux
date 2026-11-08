---
type: Decision
title: "Global agmsg automatic updates (2026-10-31, #435 B3)"
description: "Rationale for Global agmsg automatic updates."
---

# Global agmsg automatic updates (2026-10-31, #435 B3)

B3 activates the previously persisted instance-wide agmsg preference. Host management checks all
configured hosts at process startup and each off-to-on opt-in, and retains outcomes.
Only older installed releases are updated; the eligibility recheck is inside B2's reservation,
and the adapter preserves update-only intent at its final probe, so an externally removed
installation cannot become an automatic Install. Acquisition, prerequisites, archive
transfer and drain/pause use B2's execution path. Failures support manual Update; there
is no retry loop, periodic monitor or automatic recovery. Turning off skips updates that have not
started, while a running installer finishes. The global checkbox is available on every host and
explains unattended installer consent. Its API saves config.yaml and rolls back on save failure;
Codex hook keeps its per-host settings. Pin-bump proposals remain a [separate decision](../maintenance/agmsg-pin-proposals.md).

Current contracts: [agent board guide](../../agent-board.md).
