---
type: Specification
title: "Browser fixture isolation"
description: "Private E2E processes, stand-in commands, cleanup, and temporary-file safety requirements."
---

# Browser fixture isolation

## Private processes and sessions

The task-dashboard E2E fixture clears inherited `TMUX`, exports a short unique `TMUX_TMPDIR`,
starts tmux with `-f /dev/null`, and stops its private server on exit, including sessions started
or resumed through the dashboard. It never uses the caller's server.

Playwright sends SIGTERM to the task-dashboard, Agent Board, and command-center fixtures; the
latter two have a five-second grace period. Owning shells stop and reap children before deleting
per-run fake HOME directories. SIGKILL cannot run cleanup. Fixture tests retain a negative
control and verify the SIGTERM path; process-group checks skip without optional `python3`.

The task-dashboard runs real collection against stand-in `ps` and `lsof` commands
(`frontend/e2e/fixtures/process-isolation/`). They answer only for descendants of the fixture
startup script or private tmux server and only for panemux's argument shapes; other shapes
produce no output and are refused. A detached `claude` stand-in with a state file verifies that
outside sessions are absent. The `claude` and `codex` stand-ins execute nothing.
`frontend/e2e/stand-ins.sh` puts stand-ins first on PATH immediately before panemux starts and
refuses the run if any resolves elsewhere.

## Isolation checks

`make test-screenshots-check` checks screenshot staging, both fixtures' tmux isolation, CLI
stand-in precedence after runtime selection, and process-filter behavior. Tests clear `TMUX`
and use a separate caller server to verify startup, stop, and teardown leave it untouched.
The process checks require `ps`: they skip in the Claude Code sandbox and fail in CI.
Test sockets use `tmux_short_dir` under TMPDIR when it fits, otherwise `/tmp`; failure to fit
either location reports the path rather than tmux's generic socket error.

## Temporary-file discipline

Every script creates temporary files under TMPDIR using an explicit `mktemp` template and stops
on failure. Bare macOS `mktemp -d` ignores TMPDIR. `make test-tmpdir-guard` checks first and later
allocation failures, including fixture helpers, leave the caller repository untouched; it
rejects missing templates and assignments without same-line failure handling.

For screenshot-specific staging and cleanup, see [Documentation screenshots](screenshots.md).
