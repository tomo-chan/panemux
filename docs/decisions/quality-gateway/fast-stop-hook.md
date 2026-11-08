---
type: Decision
title: "D6 \u2014 The Stop hook does not run everything"
description: "Keep turn-local checks fast and leave full suites to explicit runs or CI instead of every Stop hook."
---

# D6 — The Stop hook does not run everything

Recorded 2026-08-28 in the original quality-gateway milestone log.

All of make check at every Stop sacrifices fast feedback and encourages bypass. Keep turn-local
formatting/tests there. [D14](selective-pre-push.md) replaced the original pre-push responsibility:
the whole suite belongs to CI or an explicit manual run.
See [development](../../../DEVELOPMENT.md) for current completion requirements.
