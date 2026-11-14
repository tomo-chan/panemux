---
type: Decision
title: "D13 \u2014 Capability probes skip locally and fail in CI"
description: "Probe PTY, socket and process capabilities locally while making the same missing capability fail in CI."
---

# D13 — Capability probes skip locally and fail in CI

Recorded 2026-10-08, issue #315. The macOS Claude sandbox can deny PTYs, sockets/tmux, ps and dscl.
Broad socket/git-config permissions were rejected: they expose unrelated Docker/agent sockets or
hook execution. Probe capabilities once and skip with actual errors locally; CI fails on the same
missing capability. Inferring from sandbox environment variables was rejected.
Screenshots fail instead of silently skipping because they write tracked artifacts. Temporary-file
allocation must also fail closed: an empty mktemp result once wrote fixtures into the caller.
See [sandbox](../../development/claude-sandbox.md) and
[temporary-file rules](../../../DEVELOPMENT.md#temporary-file-discipline).
