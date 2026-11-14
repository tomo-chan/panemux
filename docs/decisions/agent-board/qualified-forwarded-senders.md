---
type: Decision
title: Qualify every forwarded sender
description: Keep copy exclusion stable across pane changes and carry option-like bodies through the public stdin interface.
---

# Qualify every forwarded sender (2026-11-15, #495 / #505 / PR #540)

The user chose to qualify every forwarded sender, including configured pane IDs on the
bare pane-destination route. This supersedes the bare-sender exception in the
[pane-independent relay decision](pane-independent-relay.md). Pane-ID destination routing
remains; recipients reply verbatim to the received `name@BoardID` sender.

A delayed bare compatibility copy became indistinguishable from an unknown legitimate original
when its sender pane was removed. It could enter history with the wrong host, or bounce back
when its recipient pane moved. Qualified copies are rejected independently of current pane
ownership, including after reconnect and relay restart with persisted cursors. Retaining old
pane ownership would constrain name reuse and require a retention policy; adding a copy ledger
would change the agreed ledger scope. Neither is needed with qualified senders. The own-send
ledger and its TLA+ transitions remain unchanged.

Review also established that rejected rows must pass side-effect-free eligibility checks before
acquiring forwarding leases, so a paused apparent destination cannot block valid source rows.
The public pinned `send.sh` accepts arbitrary option-like bodies through `--body -` with stdin;
passing decoded bodies as positional options loses such messages. The fixed encoded shell
boundary remains, but its decoded body now reaches that public stdin interface with `--force`.
This updates the write-interface assumption in the
[original encoded-wrapper decision](encoded-message-wrapper.md).

Current contracts live in [relay](../../agent-board/relay.md),
[agmsg compatibility](../../agent-board/agmsg-contract.md) and
[Agent Board security](../../security/agent-board.md). Regression verification is indexed in
[scenarios](../../scenarios.md).
