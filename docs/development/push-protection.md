---
type: Specification
title: "Pre-push check selection"
description: "Diff bases, changed-file check selection, fallback behavior, and hook isolation."
---

# Pre-push check selection

Compare each branch to its remote counterpart, or to the merge base with `origin/main` on
first push. Branch deletion selects nothing. Checks cover changed packages/modules, not importers:

| Change | Checks |
|---|---|
| Go, including package testdata | gofmt -s, vet, pinned golangci-lint with checkout cache, package tests without race detection |
| Package with fixture-agent files | Also tagged vet and `make test-go-fixture-agent` over `FIXTURE_AGENT_PKGS`; no root package or frontend build |
| frontend/src TypeScript | tsc --noEmit and vitest related |
| testdata/api-contract | Frontend contract test |
| Shell | Matching scripts/<name>_test.sh and TMPDIR guard for any .sh |
| .claude/ | Hook tests |
| frontend/screenshots/ or frontend/e2e/*.sh | Screenshot fixture tests |
| docs/ Markdown or scripts/docsokf/ | OKF checker and tests; Go checker edits also select package vet/lint/tests |
| Markdown | Link check; scenarios.md also selects scenario check |

Fall back to `make check` for shared dependencies/configuration (`go.mod`, `go.sum`, Makefile,
.golangci.yml, .node-version, frontend package/build config, runtime selectors, .githooks/) or an
unknown range (missing remote commit, missing origin/main, unreadable hook input).
The hook clears repository-scoped Git environment variables before nested fixture repositories
run. `make test-pre-push` verifies selection in CI.

The [development guide](../../DEVELOPMENT.md#push-protection) owns setup and the no-bypass rule.
