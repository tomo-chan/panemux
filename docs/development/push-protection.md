---
type: Specification
title: "Pre-push check selection"
description: "Diff bases, changed-file check selection, fallback behavior, and hook isolation."
---

# Pre-push check selection

- The change is the difference between each pushed branch and the same branch on the remote. A branch the remote does not have yet is measured from its merge base with `origin/main`, so a first push checks what the branch adds and nothing `main` gained since. Deleting a branch checks nothing.
- What the changed files select:
  - `.go` files: `gofmt -s` on each, then `go vet`, `golangci-lint` (the pinned binary and this checkout's cache, as `make lint-go` uses) and `go test` on the packages that hold them — without `-race`, and not on the packages that import them. A file under a package's `testdata/` selects that package.
  - A package holding a file built with or without the `panemux_fixture_agent` tag also gets `go vet` with that tag, and `make test-go-fixture-agent`, which tests the tagged internal packages listed by `FIXTURE_AGENT_PKGS`. The suite does not test the root package or need a frontend build.
  - `frontend/src` TypeScript: `tsc --noEmit` and `vitest related` on the changed modules. A file under `testdata/api-contract/` selects the frontend contract test.
  - Shell scripts: `scripts/<name>.sh` selects `scripts/<name>_test.sh`, and any `.sh` selects the `$TMPDIR` guard. `.claude/` selects the hook tests; `frontend/screenshots/` and `frontend/e2e/*.sh` select the screenshot fixtures' tests.
  - Markdown under `docs/` and changes to `scripts/docsokf/` select both `make check-docs-okf`
    and `make test-docs-okf`; checker Go edits also receive normal package vet/lint/tests.
  - Markdown selects `make check-docs-links`; `docs/scenarios.md` also selects `make check-scenarios`.
- It falls back to `make check` when the change cannot be narrowed — `go.mod`/`go.sum`, the `Makefile`, `.golangci.yml`, `.node-version`, the frontend's package and build configuration, the runtime selection scripts, `.githooks/` — and when the range cannot be worked out: a remote commit this clone does not have, no `origin/main`, or an unreadable pre-push line.
- `make test-pre-push` tests the selection, and CI runs it.
- The hook clears Git's repository-scoped hook environment variables first so nested test Git repositories behave the same way they do outside hook execution.

The [development guide](../../DEVELOPMENT.md#push-protection) defines hook setup and the no-bypass rule.
