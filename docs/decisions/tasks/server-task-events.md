---
type: Decision
title: "Input-wait notifications come from server-published task events (2026-10-02, issue #277)"
description: "Publish per-host changes, close slow subscribers and derive attention from healthy-host state instead of regex timing."
---

# Input-wait notifications come from server-published task events (2026-10-02, issue #277)

Server-published state changes replaced browser polling joined to terminal regexes: every timing
window traded missed notifications for duplicates. Backend regexes still needed a join rule;
browser-reported detections would trust client input and require an open tab. Terminal detection
was removed; pane matching stays with the browser that owns the workspaces.
Lightweight observation and retained unsigned wait IDs prevent reload-only new waits. Independent
host cycles keep slow hosts from delaying other notifications. Slow subscribers close rather than
receive a stream with holes, and reader/writer lifetimes join so goroutines cannot leak.
Stopping the last subscriber retains the model and lets in-flight observation finish without
publishing: remote cancellation was not established to stop the script. Shutdown cancels all.
Per-tab session storage was chosen over cross-tab elections or racy claims; two tabs may notify.
Review replaced imperative attention toggles with current-state derivation: only waits on ok hosts
flash/notify. Rejudging retained waits when a host becomes ok was rejected because that event may
precede differences showing the wait ended. Some waits present at load or before permission remain
unnotified, though attention can appear. Stale click closures were replaced by latest-store lookup.
See [task events](../../behavior/task-events.md) for authoritative publisher, receiver, reconnect
and timing contracts; PR #300 replaced earlier unknown/failing-host attention rules.
