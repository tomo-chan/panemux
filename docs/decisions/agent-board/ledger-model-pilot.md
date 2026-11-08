---
type: Decision
title: "The own-send ledger is the model-checking pilot (2026-09, PR #243, issue #168)"
description: "Rationale for The own-send ledger is the model-checking pilot."
---

# The own-send ledger is the model-checking pilot (2026-09, PR #243, issue #168)

The ledger was chosen before `Relay.processRow` because it is the smallest self-contained state
machine and had already suffered a multiset-versus-set bug. It established the TLA+, generated
transition graph, and Go conformance tiers at low cost. `Relay.processRow` is the intended next
model; dynamic executor selection is a search-and-retry algorithm better suited to fuzz or property
tests.

Current contracts: [agent board guide](../../agent-board.md).
