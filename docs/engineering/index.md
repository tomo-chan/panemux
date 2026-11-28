# Engineering

Quality gates, testing contracts, local runtimes, sandboxes, screenshots, CI operations, and
release and dependency maintenance procedures.

## Quality and verification

* [Quality gates](quality-gates.md) - Quality model, implementation principles (P1–P5), gates (G0–G6), coverage, mutation, red-check, pre-push selection, and accessibility ceilings.
* [Testing and formal verification](testing.md) - Test granularity, injectable filesystem seams, API contract fixtures, browser fixture isolation, state-machine model checking, and Agent Board verification contracts.

## Local environment and capture

* [Runtime and sandbox](runtime-and-sandbox.md) - Exact Go and Node toolchain selection, bootstrap rules, and Claude Code sandbox permissions and limits.
* [Codex verification](codex-verification.md) - Fixed Codex verification commands, approval scope, checkout-local caches, logs, and PR base setup.
* [Documentation screenshots](screenshots.md) - Capturing placeholder-only documentation images, staging recovery, and private tmux isolation.

## CI, releases, and dependency maintenance

* [CI and releases](ci-and-releases.md) - GitHub Actions pinning, runner image constraints, Renovate schedules, model-checking and E2E trace jobs, and release recovery runbooks.
* [agmsg compatibility maintenance](agmsg-maintenance.md) - Running the agmsg compatibility workflow and reviewing automated pin bump proposals.
