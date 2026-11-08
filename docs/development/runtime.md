---
type: Specification
title: "Development runtime contract"
description: "Exact Go and Node selection, bootstrap prerequisites, supported platforms, and failure behavior."
---

# Development runtime contract

`go.mod` and `.node-version` pin exact Go and Node versions for local work and CI (Node 24 LTS).
Ordinary `make` commands select both SDKs before initialization and child processes, overriding
`GOTOOLCHAIN`, using SDK `go`/`gofmt` and bundled `npm`/`npx`, and preserving `NODE_OPTIONS`.
Newer SDKs do not change selection; interactive bare `go` and `node` follow the caller's shell.

Install a Go launcher with toolchain selection (Go 1.21+); Go uses its normal module cache.
Node reuses a matching SDK on PATH or in nvm, otherwise normal commands (including
`make install-deps`) fetch it into ignored checkout-local `.cache/runtimes/`.
Bootstrap requires `curl`, `tar`, and `sha256sum` or `shasum`; it supports macOS/Linux x64
and arm64, with WSL2 for Windows. Acquisition uses official HTTPS archives and SHA256 checksums,
a bounded install lock, and atomic publication after startup/version checks.
Offline commands reuse installed SDKs. Invalid pins, unavailable SDKs, corrupt caches/archives,
and unsupported platforms stop the command. Inspect stale locks or bad caches before removal;
selection never overwrites a bad published SDK.

The Node lock needs exclusive non-recursive `mkdir`. Ubuntu 26.04 uutils 0.10.0 can admit
concurrent creators: use GNU coreutils or corrected uutils before concurrent first acquisition.
CI/release uses Ubuntu 24.04 GNU coreutils; portable locking is tracked in [#480](https://github.com/tomo-chan/panemux/issues/480).

Hooks, efficacy/mutation scripts, browser fixtures, and frontend npm scripts select the same
checkout contract. Ad hoc commands use `scripts/runtime-shell.sh -c 'node --version; go version'`.
From `frontend`, use `../scripts/npm.sh install` or `../scripts/npm.sh ci`. Bare npm rejects a
mismatched Node through the root lifecycle guard but may prepare dependencies before that guard;
use the wrapper or `make install-deps` instead. `npm run` selects runtimes for its actual tools.
Browser fixtures validate SDKs before changing state. The Stop hook handles `stop_hook_active`
retries before selection; its initial invocation validates both SDKs.

Change the single-source pins to update versions; a Go `toolchain` directive alone cannot force
a newer launcher to downgrade. `make test-go-toolchain` and `make test-node-toolchain` cover
selection/bootstrap failures. Diff-gate bases are separate: see [red-check](red-check.md),
[mutation](mutation.md), [coverage](coverage.md), and [pre-push selection](push-protection.md).
Node/npm metadata and support dates: [release index](https://nodejs.org/dist/index.json),
[release schedule](https://github.com/nodejs/Release/blob/main/schedule.json).
