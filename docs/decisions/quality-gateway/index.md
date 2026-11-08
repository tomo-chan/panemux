# Quality Gateway decisions

## Decisions

* [D1 — Do not raise the coverage threshold above 80%](coverage-scope.md) - Rationale for D1 — Do not raise the coverage threshold above 80%.
* [D2 — Mutation testing is scoped to the diff and to pull requests](mutation-diff-scope.md) - Rationale for D2 — Mutation testing is scoped to the diff and to pull requests.
* [D3 — Consolidate gates rather than adding them](consolidated-gates.md) - Rationale for D3 — Consolidate gates rather than adding them.
* [D4 — Verify "the test came first" after the fact (red-check)](red-check.md) - Rationale for D4 — Verify "the test came first" after the fact.
* [D5 — The author does not grade its own work](independent-review.md) - Rationale for D5 — The author does not grade its own work.
* [D7 — The ledger is cross-checked, not just required](scenario-cross-check.md) - Rationale for D7 — The ledger is cross-checked, not just required.
* [D6 — The Stop hook does not run everything](fast-stop-hook.md) - Rationale for D6 — The Stop hook does not run everything.
* [D8 — Per-block coverage gates the diff, not a baseline](diff-block-coverage.md) - Rationale for D8 — Per-block coverage gates the diff, not a baseline.
* [D9 — Mutation findings require complete, actionable verdicts](mutation-verdicts.md) - Rationale for D9 — Mutation findings require complete, actionable verdicts.
* [D10 — The contract fixtures are rewritten on every run, not diffed against](rewritten-contract-fixtures.md) - Rationale for D10 — The contract fixtures are rewritten on every run, not diffed against.
* [D11 — The accessibility ceiling is per rule, counts nodes, and is lowered by hand](accessibility-ceilings.md) - Rationale for D11 — The accessibility ceiling is per rule, counts nodes, and is lowered by hand.
* [D12 — A model checker's output is checked in; the model checker itself is not in `make check`](exported-model-conformance.md) - Rationale for D12 — A model checker's output is checked in; the model checker itself is not in `make check`.
* [D13 — Capability probes skip locally and fail in CI](capability-probes.md) - Rationale for D13 — Capability probes skip locally and fail in CI.
* [D14 — Selective pre-push checks and full CI](selective-pre-push.md) - Rationale for D14 — Selective pre-push checks and full CI.
* [golangci-lint caches inside each checkout (2026-10-09, issue #325)](checkout-lint-cache.md) - Rationale for golangci-lint caches inside each checkout.
* [Agent hooks check the checkout that holds the change (2026-10-11, issue #350)](worktree-aware-hooks.md) - Rationale for Agent hooks check the checkout that holds the change.
