# Quality Gateway decisions

The original numbered record lists D7 before D6; that order is retained here.
The individual dates retain the cross-topic milestone log.

## Decisions

* [D1 — Do not raise the coverage threshold above 80%](coverage-scope.md) - Keep coverage at an 80% lower bound and strengthen scope instead of encouraging tautologies with a higher target.
* [D2 — Mutation testing is scoped to the diff and to pull requests](mutation-diff-scope.md) - Scope mutation to changed lines and PRs so existing survivors cannot make every branch start red.
* [D3 — Consolidate gates rather than adding them](consolidated-gates.md) - Use one route authority and an exhaustive integration check instead of accumulating independent route inventories.
* [D4 — Verify "the test came first" after the fact (red-check)](red-check.md) - Require each changed test to pass at HEAD and fail under revert so honest siblings cannot mask tautologies.
* [D5 — The author does not grade its own work](independent-review.md) - Use fresh review context without making adversarial findings a blocking gate that rewards unnecessary complexity.
* [D7 — The ledger is cross-checked, not just required](scenario-cross-check.md) - Resolve scenario paths and test names to detect stale coverage claims rather than merely requiring a ledger row.
* [D6 — The Stop hook does not run everything](fast-stop-hook.md) - Keep turn-local checks fast and leave full suites to explicit runs or CI instead of every Stop hook.
* [D8 — Per-block coverage gates the diff, not a baseline](diff-block-coverage.md) - Gate changed blocks because a repository baseline starts red and concurrent zero-count blocks make ceilings noisy.
* [D9 — Mutation findings require complete, actionable verdicts](mutation-verdicts.md) - Block actionable findings with complete verdict reporting, typed waivers and an explicit measured denominator.
* [D10 — The contract fixtures are rewritten on every run, not diffed against](rewritten-contract-fixtures.md) - Rewrite Go captures before frontend validation so stale schemas see changed keys instead of stopping at golden diffs.
* [D11 — The accessibility ceiling is per rule, counts nodes, and is lowered by hand](accessibility-ceilings.md) - Use per-rule node ceilings on owned page fixtures and lower them manually instead of failing improvements.
* [D12 — A model checker's output is checked in; the model checker itself is not in `make check`](exported-model-conformance.md) - Replay Go against exported transitions so hermetic checks verify implementation conformance without requiring TLC.
* [D13 — Capability probes skip locally and fail in CI](capability-probes.md) - Probe PTY, socket and process capabilities locally while making the same missing capability fail in CI.
* [D14 — Selective pre-push checks and full CI](selective-pre-push.md) - Check pushed changes locally and full suites in CI, falling back to full checks when scope is unknown.
* [golangci-lint caches inside each checkout (2026-10-09, issue #325)](checkout-lint-cache.md) - Keep lint caches inside each checkout to prevent shared findings and unpredictable sandbox fallback.
* [Agent hooks check the checkout that holds the change (2026-10-11, issue #350)](worktree-aware-hooks.md) - Resolve the edited checkout so main-hosted hooks neither vet the wrong module nor execute unrelated dependencies.
