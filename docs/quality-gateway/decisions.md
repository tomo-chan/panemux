---
type: Decision
title: "Quality gateway: design decisions"
description: "Compatibility routes to the numbered quality gateway decisions."
---

# Quality gateway: design decisions

See the [quality decision index](../decisions/quality-gateway/index.md).

## D1

[D1 — Do not raise the coverage threshold above 80%](../decisions/quality-gateway/coverage-scope.md).

## D2

[D2 — Mutation testing is scoped to the diff and to pull requests](../decisions/quality-gateway/mutation-diff-scope.md).

## D3

[D3 — Consolidate gates rather than adding them](../decisions/quality-gateway/consolidated-gates.md).

## D4

[D4 — Verify "the test came first" after the fact (red-check)](../decisions/quality-gateway/red-check.md).

## D5

[D5 — The author does not grade its own work](../decisions/quality-gateway/independent-review.md).

## D7

[D7 — The ledger is cross-checked, not just required](../decisions/quality-gateway/scenario-cross-check.md).

## D6

[D6 — The Stop hook does not run everything](../decisions/quality-gateway/fast-stop-hook.md).

## D8

[D8 — Per-block coverage gates the diff, not a baseline](../decisions/quality-gateway/diff-block-coverage.md).

## D9

[D9 — Mutation findings require complete, actionable verdicts](../decisions/quality-gateway/mutation-verdicts.md).

## D10

[D10 — The contract fixtures are rewritten on every run, not diffed against](../decisions/quality-gateway/rewritten-contract-fixtures.md).

## D11

[D11 — The accessibility ceiling is per rule, counts nodes, and is lowered by hand](../decisions/quality-gateway/accessibility-ceilings.md).

## D12

[D12 — A model checker's output is checked in; the model checker itself is not in `make check`](../decisions/quality-gateway/exported-model-conformance.md).

## D13

[D13 — Capability probes skip locally and fail in CI](../decisions/quality-gateway/capability-probes.md).

## D14

[D14 — Selective pre-push checks and full CI](../decisions/quality-gateway/selective-pre-push.md).
