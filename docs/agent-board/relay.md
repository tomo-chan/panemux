# Agent Board: cross-host relay

> Part of the current [Agent Board design](../agent-board.md).

## Cross-host relay

agmsg is host-local. Panemux reaches the local host and every `ssh_connections` key through
the execution port shared with the task board. Polling needs no pane or live pane session.
Local panes belong to the panemux host; SSH panes belong to the key matching their `connection`
exactly. Names found only in `~/.ssh/config` have no board host, and their rows are unknown senders.

During one poll, the relay processes each host/team pair in order:

1. reads a bounded newest-message window;
2. finds rows after its opaque persisted cursor in agmsg's returned order;
3. validates the sender;
4. for a row addressed to `_system`, records it in bounded history and updates status only when its
   body is a valid status report;
5. for other rows, resolves the recipient, drops an unknown recipient before recording, then records
   the row and forwards it when the recipient is on another host;
6. advances that host's cursor regardless of a forwarding failure.

After all hosts have been processed, a poll that advanced at least one cursor atomically persists
one snapshot containing every host cursor. A poll with no cursor change does not write the file.

Same-host delivery remains entirely within agmsg. Unknown senders and recipients are dropped and
logged.

### Sender validation

A normal row is accepted only if `from` names a configured pane on the source host. A row with
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

Cross-host forwarding is best-effort, not at-least-once. A missing destination client or failed
send is logged, but the source row remains in dashboard history and its cursor advances, so that
failure is not retried. Conversely, a crash after a successful forward but before cursor
persistence can duplicate a message. If more rows arrive between polls than the read limit, older
overflow rows can be missed. These are accepted consequences of the current cursor and forwarding
contracts, not exactly-once delivery.

Panemux is always in the cross-host data path. Hosts do not discover or connect directly to each
other, and no additional listener is created.

SSH connection establishment starts in the background: polling and bootstrap return a connecting state
without waiting for the dial. Later polls retry, so a pane-less unreachable host cannot consume a
relay pass's timeout before healthy hosts are polled. Repeated identical poll errors are logged only
when the failure state changes; a successful pass resets that suppression. All configured keys
remain available to board execution, including hosts without a board pane. Rows still require a
known pane sender on the source host.

Operator sends wait for a missing SSH connection within the send context, including before the
first poll and after reconnect or transport loss. Polling and bootstrap continue to use the
non-waiting runner; all runners share the same connection pool.
