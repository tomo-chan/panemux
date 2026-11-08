---
type: Decision
title: "Pane workdirs per session, on the Task rule (2026-10-18, issue #375)"
description: "Rationale for Pane workdirs per session, on the Task rule."
---

# Pane workdirs per session, on the Task rule (2026-10-18, issue #375)

The header and dashboard used different process rules. Reusing the Task rule and one state parser
aligns wrapped/native executables and Codex option semantics. Prefilters use the same case rule;
Board detection remains separate because it mirrors agmsg.
The workdir port lives in session to avoid an import cycle. A shared per-host observation was
rejected for failure isolation, freshness and migration risk: share rule/parser/model instead.
See [registry](../../agent-registry.md) for the current workdir contract.
