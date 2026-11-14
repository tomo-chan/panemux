---
type: Decision
title: "golangci-lint caches inside each checkout (2026-10-09, issue #325)"
description: "Keep lint caches inside each checkout to prevent shared findings and unpredictable sandbox fallback."
---

# golangci-lint caches inside each checkout (2026-10-09, issue #325)

`make lint-go` sets `GOLANGCI_LINT_CACHE` to the checkout's own ignored `.cache/golangci-lint/`.
It had used golangci-lint's default under the user cache directory, falling back to `$TMPDIR` where
that was not writable (#315). Both locations are shared by every checkout on the machine, so one
worktree could report issues cached for another, and the sandbox fallback depended on a write probe.
The location is derived from the script's own path rather than the working directory, and an
inherited `GOLANGCI_LINT_CACHE` is overridden rather than honoured: honouring it would let a value
exported in one worktree leak into another, which is the sharing this replaces. Creating the
directory failing is an error, not a fallback. The Go build and module caches were left where they
are; old user-directory lint caches are not moved or deleted.

Current contracts: [quality gateway guide](../../quality-gateway.md).
