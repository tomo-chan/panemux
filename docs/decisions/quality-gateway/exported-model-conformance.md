---
type: Decision
title: "D12 \u2014 A model checker's output is checked in; the model checker itself is not in `make check`"
description: "Replay Go against exported transitions so hermetic checks verify implementation conformance without requiring TLC."
---

# D12 — A model checker's output is checked in; the model checker itself is not in `make check`

Recorded 2026-09-21 in the original quality-gateway milestone log.

TLC proves the model, not Go code. Checking in its full transition table permits hermetic Go
conformance replay without a JDK/jar in make check. A handwritten Go reference was rejected as a
third artifact that can drift like the implementation. A separate tier checks spec/export drift.
Drivers assert non-Expire transition coverage. The committed per-key bound is four; larger defects
are outside this evidence. Perturbing properties/implementations prevents vacuous success.
Review found an exporter deriving the expected bound from observed states, accepting truncated
exploration. The bound now comes from configuration, with rejection fixtures testing the exporter.
See [model checking](../../agent-board/model-checking.md#state-machine-model-checking) and
[workflow](../../development/model-checking.md).
