# Agent Board: cross-host relay

> Part of the current [Agent Board design](../agent-board.md).

## Cross-host relay

agmsg is host-local. Panemux connects those independent installations because it already holds the
SSH connections to configured pane hosts.

For each host/team pair, the relay:

1. reads a bounded newest-message window;
2. finds rows after its opaque persisted cursor in agmsg's returned order;
3. validates the sender;
4. records the row in bounded history;
5. updates status for valid `_system` status rows, or forwards cross-host messages to the
   recipient's host;
6. persists the new cursor.

Same-host delivery remains entirely within agmsg. Unknown senders and recipients are dropped and
logged.

### Sender validation

A normal row is accepted only if `from` names a board-enabled pane on the source host. A row with
`from == "_system"` is accepted only when it consumes a matching entry from panemux's short-lived
own-send ledger. This prevents any process able to call `send.sh --force` from impersonating the
command center merely by choosing the reserved name.

The ledger key includes destination host, team, recipient, and body hash. It is a multiset so two
identical broadcasts remain two independently matchable sends. Failed sends remove their pending
ledger entry.

### Startup and recovery

The board cache is not persisted. Before steady-state polling, the relay performs one larger but
still bounded backfill per host/team to repopulate recent status and history. This shortens the
empty-cache period after restart but cannot recover rows outside the backfill window.

Delivery is at-least-once. A crash after forwarding but before cursor persistence can duplicate a
message. If more rows arrive between polls than the read limit, older overflow rows can be missed.
Both outcomes follow from agmsg's lack of a forward cursor and are accepted constraints, not
exactly-once guarantees.

Panemux is always in the cross-host data path. Hosts do not discover or connect directly to each
other, and no additional listener is created.
