---
type: Runbook
title: "Documentation screenshots"
description: "Capturing placeholder-only documentation images, safe staging recovery, and private tmux isolation."
---

# Documentation screenshots

The [development guide](../../DEVELOPMENT.md#documentation-screenshots) owns the retake rule.
`make screenshots` uses Playwright against a real panemux run; it requires tmux and a PTY outside
the Claude Code sandbox and is outside `make check`. Set `PLAYWRIGHT_CHROMIUM_EXECUTABLE` as for
E2E if needed.

## CI scope

CI inspects changes under `frontend/src/components/`, `frontend/src/App.tsx`,
`frontend/src/styles/`, `frontend/index.html`, and `frontend/screenshots/` (excluding `*.test.ts`,
`*.test.tsx`, `*_test.sh`; moved files count under their old paths) and requires a direct
`docs/images/*.png` change. Use `screenshots-exempt` only when a change alters nothing shown.

## Placeholder environment

Capture clears `XDG_*` and git configuration variables and runs with a fake `HOME`, a throwaway
repository at `/tmp/sample-project`, a private tmux socket, a stub agmsg store, and a fixed
`/api/tasks` fixture so real developer sessions are never exposed. Panes inherit logical
`/tmp/sample-project` as `PWD` so prompts display the placeholder path.

## Staging and recovery

Staging lives under `$TMPDIR/panemux-screenshots`. A run empties it only when it carries the
`.panemux-screenshots` marker or every entry matches the allowlist (`home`, `tmux`, `panemux`,
`showcase.yml`, `sample-project`, `panemux-screenshots-agmsg`). `/tmp/sample-project` and
`/tmp/panemux-screenshots-agmsg` are symlinks into that root:

- A link targeting `<root>/<its own name>` is replaced without deleting its target.
- A legacy directory is removed only if marked or containing solely empty directories (`rmdir`).
- Any other file, link, or directory stops the run untouched; move it away before retrying.
- Marker loss from macOS temporary cleanup is handled safely by the allowlist and empty-directory
  checks without trusting arbitrary files.
- `/tmp/panemux-screenshots.lock` blocks concurrent captures; stale locks (dead PID or missing
  `pid` file) may be taken over.

## Socket and process isolation

Startup and teardown clear inherited `TMUX` (which otherwise overrides `TMUX_TMPDIR`), and
`frontend/screenshots/global-teardown.ts` stops the private server. Resolved socket paths under
`$TMPDIR/panemux-screenshots/tmux` (including macOS `/private` prefix and trailing NUL) must fit
104 bytes on macOS or 108 on Linux; pre-staging checks length and asks for a shorter `TMPDIR` (such
as `TMPDIR=/tmp`) when needed. Shared isolation checks live in
[Browser fixture isolation](testing.md#browser-fixture-isolation).
