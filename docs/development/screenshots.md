---
type: Runbook
title: "Documentation screenshots"
description: "Capturing placeholder-only documentation images, safe staging recovery, and private tmux cleanup."
---

# Documentation screenshots

Run `make screenshots` to regenerate `docs/images/*.png`, then inspect every rewritten image
and commit it. The [development guide](../../DEVELOPMENT.md#documentation-screenshots) owns the
mandatory retake rule. Capture uses Playwright and a real panemux run with placeholder content.
It needs tmux and a pseudo-terminal; run it outside the Claude Code sandbox. It is outside
`make check`. Set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` as for `make test-e2e` when necessary.

## Placeholder environment

The capture clears `XDG_*` and git configuration variables and uses a fake HOME, a throwaway
repository at `/tmp/sample-project`, a private tmux socket, and a stub agmsg store. A fixed task
list replaces real task collection so images cannot expose the developer's agent sessions.
The panes inherit the logical `/tmp/sample-project` PWD, so prompts show the placeholder path.
On macOS `BASH_SILENCE_DEPRECATION_WARNING=1` suppresses the bash-to-zsh notice.

## Staging and recovery

The root is `$TMPDIR/panemux-screenshots`. A run empties it only if it carries the
`.panemux-screenshots` marker or every entry is a known staging name: `home`, `tmux`, `panemux`,
`showcase.yml`, `sample-project`, or `panemux-screenshots-agmsg`.
`/tmp/sample-project` and `/tmp/panemux-screenshots-agmsg` are links into that root.

- A link targeting `<root>/<its own name>` is replaced without deleting its target.
- A legacy directory is removed only if marked or containing nothing but empty directories;
  the latter is removed with `rmdir`, which cannot remove other content.
- Any other file, link, or directory stops the run untouched. Move it away before retrying.
- Links and the staging root remain after capture for reuse. macOS temporary-file cleanup can
  remove unused markers while leaving recently accessed fixture content; the allowlist and
  empty-directory checks handle those remnants without trusting arbitrary content.
- `/tmp/panemux-screenshots.lock` refuses concurrent capture. A lock with a dead process or no
  `pid` file can be taken over.

## Socket and process isolation

Capture startup and teardown clear inherited `TMUX`; it overrides `TMUX_TMPDIR` otherwise.
The private server is stopped by `frontend/screenshots/global-teardown.ts`.
The socket is under `$TMPDIR/panemux-screenshots/tmux`. Its resolved path, including the terminating
NUL, must fit 104 bytes on macOS or 108 on Linux. macOS resolves `/tmp` and `/var/folders` under
`/private`. The fixture checks length before staging and asks for a shorter `TMPDIR`
(such as `TMPDIR=/tmp`) when necessary.

[Browser fixture isolation](browser-fixtures.md) defines the shared isolation checks and E2E
shutdown requirements.
