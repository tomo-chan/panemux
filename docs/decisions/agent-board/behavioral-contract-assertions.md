---
type: Decision
title: "Contract incidents favor behavior over environment and wording (2026-08-23 onward, PR #176)"
description: "Rationale for Contract incidents favor behavior over environment and wording."
---

# Contract incidents favor behavior over environment and wording (2026-08-23 onward, PR #176)

The live contract first asserted a `watch.sh` diagnostic that is absent on its successful path; it
now observes message delivery. Its first CI run also depended on finding an agent in the test
process ancestry, so locks looked live locally and stale on runners. Tests now set agmsg's documented
`AGMSG_AGENT_PID` override and verify that a dead owner can be replaced.

The scheduled canary later failed when agmsg v1.3.1 changed a diagnostic sentence while preserving
the behavior Agent Board needs. The assertion was narrowed to the stable fact that the dropped pair
is named. The canary remains daily because the measured median between 22 releases from v1.0.2
through v1.2.2 was 2.9 days; a weekly sample could span several releases and obscure the cause.

Current contracts: [agent board guide](../../agent-board.md).
