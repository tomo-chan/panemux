---
type: Specification
title: "Runtime and sandbox"
description: "Exact Go and Node toolchain selection, bootstrap rules, and Claude Code sandbox permissions and limits."
---

# Runtime and sandbox

## Runtime toolchain contract

`go.mod` and `.node-version` are the sole exact Go and Node version sources for local work and CI
(Node 24.21.0 LTS shipping npm 11.19.0; official support through 2028-04-30 per
[Node release index](https://nodejs.org/dist/index.json) and
[release schedule](https://github.com/nodejs/Release/blob/main/schedule.json)). Do not add parallel
version pins or require per-command environment overrides.

- **Automatic selection**: Ordinary `make` commands, hooks, efficacy/mutation scripts, browser
  fixtures, and `npm run` select both exact SDKs through `scripts/runtime-shell.sh`, overriding
  `GOTOOLCHAIN`, using SDK `go`/`gofmt` and bundled `npm`/`npx`, and preserving `NODE_OPTIONS`.
  Newer ambient SDKs do not change selection; bare interactive `go`/`node` follow the caller's shell.
- **Acquisition and locking**: Go requires a toolchain-capable launcher (Go 1.21+) and uses its
  normal module cache. Node reuses a matching SDK on `PATH` or in `nvm`; otherwise commands
  (including `make install-deps`) download verified official HTTPS archives (SHA256-checked) into
  ignored checkout-local `.cache/runtimes/` using an exclusive lock and atomic publication.
  Supported platforms: macOS/Linux x64 and arm64 (Windows via WSL2); bootstrap needs `curl`, `tar`,
  and `sha256sum` or `shasum`. Offline commands reuse cached SDKs.
- **Fail-closed behavior**: Invalid pins, unavailable SDKs, corrupt caches/archives, and
  unsupported platforms stop the command without overwriting a bad published SDK. Exclusive
  non-recursive `mkdir` is required for the Node lock (Ubuntu 26.04 uutils `mkdir` 0.10.0 can admit
  concurrent creators; CI/releases use Ubuntu 24.04 GNU coreutils; portable locking is tracked in
  [#480](https://github.com/tomo-chan/panemux/issues/480)).
- **Ad hoc commands**: Use `scripts/runtime-shell.sh -c 'node --version; go version'` or, from
  `frontend/`, `../scripts/npm.sh install` / `../scripts/npm.sh ci` (avoid bare `npm` when ambient
  Node differs). The Stop hook handles `stop_hook_active` retries before SDK selection.
- **Verification**: Go workflows use `go-version-file: go.mod`; Node workflows use
  `node-version-file: .node-version`. Run `make test-go-toolchain`, `make test-node-toolchain`, and
  `make check` when changing pins (CI runs both runtime suites explicitly).

## Claude Code sandbox

Shared `.claude/settings.json` sets `sandbox.network.allowLocalBinding: true` on **macOS** so
HTTP/WebSocket tests and Playwright can reach local servers without per-developer settings.

- **macOS scope**: Grants access to **any localhost port**, including services outside the sandbox.
  All contributors inherit this setting; domain allowlists and filesystem restrictions for
  downloads, Git config, and caches remain unchanged.
- **Linux and WSL2**: Sandboxed commands already have a private localhost; `allowLocalBinding` has
  no effect and is not needed.
- **Admin-required sandboxes**: Claude Code ignores repository-level `allowLocalBinding` in
  `.claude/settings.json` and `.claude/settings.local.json`; configure via user, CLI, or managed
  settings as policy permits (see
  [Claude Code sandboxing documentation](https://code.claude.com/docs/en/sandboxing#a-command-fails-to-reach-a-server-on-localhost)).
- **Remaining limits and probes**: Per
  [development sandbox permissions](../security/overview.md#development-sandbox-permissions), macOS still
  denies PTYs, tmux servers, `ps`, `dscl`, and writes to Git config and user caches. Tests needing
  these probe via `internal/testcap` (skip locally, fail when `CI` is set;
  [D13](../decisions/quality-gateway.md#d13-capability-probes-skip-locally-and-fail-in-ci)).
  - Fresh clones need `make install-hooks` once outside the sandbox (npm `.idea/` EPERM warnings
    are harmless).
  - E2E skips without a PTY; `make screenshots` fails closed and runs outside the sandbox.
  - Use `git push origin <branch>` (without `-u`, which writes Git config).

### Protected hook files and worktree switching

Claude Code protects the session checkout's `.claude/hooks/` (and `.claude/skills/`) from sandbox
writes; `.claude/settings.json` cannot disable this. Switching branches across hook edits with
`git switch`/`git checkout` can advance `HEAD` while leaving hook files at the old revision (`M` in
`git status`, [#370](https://github.com/tomo-chan/panemux/issues/370)).

- Always verify `HEAD` and `git status` after switching across hook changes.
- For review/verification of hook changes, unpack `git archive <PR HEAD>` into a unique
  `mktemp -d "${TMPDIR:-/tmp}/panemux-review.XXXXXX"` directory (stopping on allocation failure).
- Edit hooks and remove worktrees (`git worktree remove`) outside the sandbox with normal approval.
- After changing sandbox settings, verify `make check`, `make test-e2e`, and `make test-hooks`
  inside Claude Code with sandboxing active (runs outside the sandbox do not verify sandbox rules).
