---
type: Decision
title: "A lightweight collection and a stable wait signature for input-wait notifications (2026-10-02, issue #278)"
description: "Share live-only collection and stable wait signatures so notification observation avoids full-dashboard work."
---

# A lightweight collection and a stable wait signature for input-wait notifications (2026-10-02, issue #278)

Full snapshots do PR/git lookups, stopped-log searches, records and summaries; notifications need
only live state. Shared script fragments avoid that work without drifting state derivation.
A separate attention endpoint avoided two response shapes, then was replaced by
[server task events](server-task-events.md); lightweight collection remains.
Wait signatures use actual host-recorded wait starts rather than converted status_since or rollout
mtime, which move without a new wait. A versioned opaque hash leaves unreliable waits unsigned
instead of inventing repeatable/changing identities. See [task events](../../behavior/task-events.md).

The attention endpoint was replaced by [server task events](server-task-events.md); its collection/signature rationale remains relevant.
