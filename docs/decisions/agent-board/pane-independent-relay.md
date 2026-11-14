---
type: Decision
title: Pane-independent relay by source host
description: Attribute agmsg names to their source host while preserving pane compatibility and the own-send ledger.
---

# Pane-independent relay by source host (2026-11-07, #495 / #505)

S3 adopts source-host attribution (design A): public `api.sh` reads establish the host for a
valid unqualified agmsg name in the configured team. Roster lookup adds no authentication
within the same-user boundary, and session/pane mapping would exclude plugin workers.
BoardIDs use `local` and `ssh:<configuration key>` to distinguish even the SSH key `local`.

Cross-host addresses are `name@BoardID`; forwarding strips the destination host and qualifies
the original sender so the recipient can reply verbatim using `send.sh --force`. Qualified
senders are copies or forgeries and are excluded from aggregation and forwarding. Legacy bare
pane destinations retain their route; only configured pane senders keep a bare `from` there.
Pane-free senders are qualified even on that route (orchestrator decision, 2026-11-07), because
otherwise the forwarded copy would be accepted as an unknown original at the destination.
The original pane-sender exception above was superseded by the
[2026-11-15 forwarding decision](qualified-forwarded-senders.md): every copy now has a qualified sender.

The own-send ledger's host/team/bare-recipient/body-hash multiset, TTL, rollback and TLA+
transitions stay unchanged. It remains sufficient for `_system`; forwarded-copy filtering needs
no extra ledger. Status keys are host-qualified internally; the existing pane API gets only the
matching pane/host projection. New API/MCP/UI identities belong to S3b. No proxy roster entries,
send wrapper, panemux participation hook or remote listener is introduced. Received messages
are not work or approval authorization, and same-user name spoofing remains possible.
