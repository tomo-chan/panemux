---
type: Decision
title: "D6 \u2014 The Stop hook does not run everything"
description: "Rationale for D6 \u2014 The Stop hook does not run everything."
status: deprecated
---

# D6 — The Stop hook does not run everything

All of make check at every Stop sacrifices fast feedback and encourages bypass. Keep turn-local
formatting/tests there. [D14](selective-pre-push.md) replaced the original pre-push responsibility:
the whole suite belongs to CI or an explicit manual run.
See [development](../../../DEVELOPMENT.md) for current completion requirements.

[D14](selective-pre-push.md) replaces the original pre-push part; the fast Stop-hook rationale remains relevant.
