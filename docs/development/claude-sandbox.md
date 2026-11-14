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

The [security rules](../security.md#development-sandbox-permissions) own the permission limits:
macOS still denies PTYs, tmux servers, `ps`, `dscl`, and writes to Git config and user caches.
Tests requiring those capabilities must probe through `internal/testcap` (or the fixture's
matching probe): unavailable capabilities skip locally and fail with CI set.

- Fresh clones need `make install-hooks` once outside the sandbox. Setup writes Git config only
  if the configured hook is not identical and executable; npm `.idea/` EPERM warnings are harmless.
- E2E skips without a PTY; screenshots fail and require execution outside the sandbox.
- Use explicit `git push origin <branch>`: `-u` cannot record upstream in Git config here.
- Lint cache handling follows the [quality-gate commands](../../DEVELOPMENT.md#quality-gate).
- Temporary files follow the [repository rule](../../DEVELOPMENT.md#temporary-file-discipline).

## Protected hook files and worktree switching

Claude Code protects the session checkout's `.claude/hooks/` (and directories such as
`.claude/skills/`) from sandbox writes. Repository `.claude/settings.json` cannot remove this
protection. Switching with `git switch` or `git checkout` to a commit that changes those files
can stop partway through: HEAD can advance while the hook files remain at the old version and
appear as `M` in `git status`. Tests would then exercise the new tests against old hooks.

- Before verifying a switch across hook changes, check HEAD and `git status`; do not treat a
  partial switch as the requested tree.
- For review and verification, extract `git archive <PR HEAD>` into a unique directory allocated
  with an explicit `mktemp -d "${TMPDIR:-/tmp}/panemux-review.XXXXXX"` template, stopping if allocation
  fails, and unpack the archive there. This avoids switching protected files in the session checkout.
- Edit hooks outside the sandbox with the required approval for each edit.
- `git worktree remove` can also fail to remove `.git/worktrees/<name>` and leave a `prunable`
  entry. Request the normal outside-sandbox approval for removal; do not assume cleanup succeeded.

See [#370](https://github.com/tomo-chan/panemux/issues/370) for the observed partial switches and
[D13](../decisions/quality-gateway/capability-probes.md) for capability limits during verification.

After changing sandbox settings, verify `make check`, `make test-e2e`, and `make test-hooks`
from Claude Code with sandboxing active. Runs from another agent or outside the sandbox verify
the suites but do not establish that Claude Code sandbox permissions work. Record any remaining
filesystem or cache restrictions separately from local port binding.
