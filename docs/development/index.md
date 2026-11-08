# Development

Development and verification contracts, fixtures, and operational procedures.

* [Codex verification](codex-verification.md) - Fixed Codex verification commands, approval scope, caches, logs, and PR base setup.
* [Contract fixtures (`testdata/api-contract/`)](contract-fixtures.md) - Go-produced API fixtures and their validation against frontend runtime schemas.
* [Coverage gates](coverage.md) - Coverage scope, thresholds, exclusions, and per-block verification requirements.
* [Model checking (`make model-check`)](model-checking.md) - Running model checking and regenerating transition fixtures while preserving conformance.
* [Mutation testing (`make mutation`)](mutation.md) - Mutation gate outcomes, diff scope, and justified exemption requirements.
* [Red-check (`make efficacy`)](red-check.md) - Red-check requirements proving that changed tests detect the implementation change.
* [Test granularity and testability seams](test-granularity.md) - Behavioral factor coverage, test granularity, and injectable side-effect boundaries.
* [Development runtime contract](runtime.md) - Exact Go and Node selection, bootstrap prerequisites, supported platforms, and failure behavior.
* [Claude Code sandbox](claude-sandbox.md) - Shared local-network permissions, platform limits, capability probes, and sandbox verification.
* [Documentation screenshots](screenshots.md) - Capturing placeholder-only documentation images, safe staging recovery, and private tmux cleanup.
* [Browser fixture isolation](browser-fixtures.md) - Private E2E processes, stand-in commands, cleanup, and temporary-file safety requirements.
* [Pre-push check selection](push-protection.md) - Diff bases, changed-file check selection, fallback behavior, and hook isolation.
