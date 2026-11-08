---
type: Specification
title: "Claude Code sandbox"
description: "Shared local-network permissions, platform limits, capability probes, and sandbox verification."
---

# Claude Code sandbox

The shared `.claude/settings.json` sets `sandbox.network.allowLocalBinding: true` on **macOS**
for HTTP/WebSocket tests and Playwright. Keep it shared rather than requiring local settings.

On macOS this grants access to **any localhost port**, including services outside the sandbox.
An unauthenticated service can act outside the sandbox; a non-loopback listener can accept
external connections. All contributors receive this access. Domain allowlists and filesystem
permissions remain unchanged for downloads, Git configuration, and caches.

On **Linux and WSL2**, including Linux cloud containers, a sandboxed command already has its own
private localhost: it can bind ports and reach servers it starts itself, but cannot directly
reach host services there. `allowLocalBinding` has no effect and is not needed on those platforms.

When the sandbox is **admin-required**, Claude Code ignores this permission-loosening key in
repository settings, including `.claude/settings.json` and `.claude/settings.local.json`.
Configure it through user, CLI, or managed settings as permitted by the organization's policy;
managed settings may still prevent it from taking effect. See the official
[Claude Code sandboxing documentation](https://code.claude.com/docs/en/sandboxing#a-command-fails-to-reach-a-server-on-localhost).

No other permissions are granted. The [security rules](../security.md#development-sandbox-permissions)
prohibit adding PTY, Unix-socket, or Git-config access. macOS still denies PTYs, tmux servers,
`ps`, `dscl`, and writes to `.git/config` and user caches:

| Command | Inside the macOS sandbox |
|---|---|
| `make install-deps` | Passes once the hooks are installed: `install-hooks` writes `.git/config` only when `core.hooksPath` does not already lead to an identical, executable `pre-push` (git silently skips one without the executable bit). In a fresh clone run `make install-hooks` once outside the sandbox. `npm install` may warn `EPERM` on `.idea/` files inside packages; those warnings are harmless. |
| `make check` | Passes. A Go test that needs a pty, tmux, `ps` or `dscl` calls `internal/testcap`'s `RequirePTY`/`RequireTmux`/`RequirePS`/`RequireDscl`, and the screenshot fixtures' tmux checks probe the same way: each reports itself **skipped** where the probe fails. `golangci-lint` caches in the checkout's own `.cache/golangci-lint/`, never in the user cache directory (`scripts/golangci_lint_cache.sh`). |
| `make test-e2e` | Reports itself skipped: every pane needs a pty (`scripts/require_pty.sh`). |
| `make screenshots` | Fails, saying to run it outside the sandbox. It writes tracked images, so it is never skipped. |
| `git push -u` | Pushes the branch, then cannot record its upstream in `.git/config`. Name the remote and branch on every push instead: `git push origin <branch>`. |

With `CI` set, every one of those probes that fails **fails** instead of skipping, so CI is where the
skipped tests are verified. Write a new test that needs one of these capabilities the same way: call
the `testcap` helper first, never let it fail on the sandbox's error.

Temporary-file safety follows [Browser fixture isolation](browser-fixtures.md#temporary-file-discipline).

After changing sandbox settings, verify `make check`, `make test-e2e`, and `make test-hooks`
from Claude Code with sandboxing active. Runs from another agent or outside the sandbox verify
the suites but do not establish that Claude Code sandbox permissions work. Record any remaining
filesystem or cache restrictions separately from local port binding.
