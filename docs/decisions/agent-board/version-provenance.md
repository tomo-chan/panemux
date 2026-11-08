---
type: Decision
title: "Version coverage normalizes install provenance (2026-08, PR #176)"
description: "Rationale for Version coverage normalizes install provenance."
status: deprecated
---

# Version coverage normalizes install provenance (2026-08, PR #176)

An installed `VERSION` may be a bare release, a `v`-prefixed tag, or `git describe` provenance.
Literal comparison to the tested pin falsely warned on supported installs. Panemux now normalizes
those forms and treats later patches on the tested major/minor line as covered. This is a warning-noise
policy, not a semver promise; the real-install contract remains authoritative. Patch tolerance also
avoids forcing pin churn at agmsg's observed multi-day release cadence.

The patch-tolerance warning policy was replaced by [pin-normalized version checks](pinned-agmsg-install.md).

Current contracts: [agent board guide](../../agent-board.md).
