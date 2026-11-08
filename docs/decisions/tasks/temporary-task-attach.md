---
type: Decision
title: "The board's temporary tmux attach (2026-10-02, issue #283)"
description: "Use exact attach-only sessions with disconnect grace so reopen works without creating shells or leaking clients."
---

# The board's temporary tmux attach (2026-10-02, issue #283)

Issue [#280](https://github.com/tomo-chan/panemux/issues/280) wants a task opened in a popup over the
dashboard rather than as a pane added to a workspace; #283 is its backend. Decided:

- **A dedicated `POST`/`DELETE /api/tasks/attach`, not `POST /api/sessions`.** The request names only
  a task ID and the server collects that task's host again for the tmux session name and host.
  Reusing `POST /api/sessions` would have let the browser name any tmux session and host, made the
  session look like a pane to `GET /api/sessions`, and tied its lifetime to a layout it is not in.
- **Attach only, never `new-session -A`.** A pane's `new-session -A` creates the session when it is
  missing; for an ended task that would start a shell under the task's name. `attach-session -t =<name>`
  fails instead, and the `=` stops tmux's prefix match from attaching to another task's session.
- **`has-session` before the attach** (PR #286 review). `attach-session` starts its client before it
  reports a missing session, so a session that ended after the collection returned `201` and an
  attach that exited at once. Checking first keeps the documented `502`; the alternative of treating
  an exit soon after start as a failure would have needed a timeout that guesses how slow a host is.
  The window between the check and the attach remains and is documented rather than closed.
- **Destroyed 10s after the last WebSocket, and reused on reopen.** Destroying on the WebSocket's close
  would lose the client on a reload; keeping it until `DELETE` would leak a tmux client per closed
  tab. The same 10s applies when no WebSocket ever connects. Attaches are in memory only, as a pane's
  sessions are. The manager reports subscriber counts in the order they changed, dropping one older
  than a count already delivered, and reports 0 when a client's exit drops its WebSockets: a stale 0
  had destroyed an attach still being read, and an exited client had been kept forever (PR #286
  review).
- **No `window-size` change.** Setting a tmux option would change the user's server for every client.
  With tmux's default `latest`, the window follows the client used last and returns to the other
  client's size when the attach ends, which was checked on a private tmux server.

Current contracts: [tasks guide](../../behavior/tasks.md).
