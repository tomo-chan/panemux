---
type: Decision
title: "Board participation steps belong to agent adapters"
description: "Why per-agent board participation steps were moved behind adapters."
status: deprecated
---

# Board participation steps belong to agent adapters

Recorded 2026-10-18, issue #375 (stage D of #348).

The bootstrap builder compared agmsg type names to choose Claude identity-claim steps.
Consumer-owned adapters checked against registry declarations replaced that branch, preserving
instruction goldens and keeping agmsg-mirroring detection separate from task rules. Codex's
unverified thread environment marker was deliberately not used.
[PTY onboarding retirement](../agent-board/retire-pty-onboarding.md) later delegated participation,
identity selection and reception to agmsg's official workflow instead of panemux instructions.

Current contracts: [agent registry guide](../../agent-registry.md).
