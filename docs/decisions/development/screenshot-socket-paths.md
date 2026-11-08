---
type: Decision
title: "The screenshot-helper tests choose where tmux's sockets go (2026-10-10, issue #343)"
description: "Rationale for The screenshot-helper tests choose where tmux's sockets go."
---

# The screenshot-helper tests choose where tmux's sockets go (2026-10-10, issue #343)

`screenshots-env_test.sh` moved its tmux sockets from `/tmp` to `$TMPDIR` in #315 so the Claude Code
sandbox, which cannot write `/tmp`, could run it. Its deepest socket then overran macOS's 104-byte
limit outside the sandbox, where `$TMPDIR` is `/var/folders/...` and tmux resolves it into
`/private/var/folders/...`; Linux CI and the sandbox both have a short `$TMPDIR`, so neither saw it.
The test now uses `$TMPDIR` when the socket fits there and `/tmp` otherwise, and fails saying so
when neither fits. Flattening the test's directories alone was not enough: the socket's path under
the run root (`panemux-screenshots/tmux/tmux-<uid>/default`) is the fixture's own. The length check
now measures the resolved path, since that, not the path as given, is what tmux binds.

Current contracts: [development guide](../../index.md).
