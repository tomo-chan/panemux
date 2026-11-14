---
type: Specification
title: "Agent Board: cross-host relay"
description: "Cross-host message relay, deduplication, and delivery lifecycle."
---

# Agent Board: cross-host relay

> Part of the current [Agent Board design](../agent-board.md).

## Cross-host relay

agmsg is host-local. Panemux reaches the local host and every `ssh_connections` key through
the execution port shared with the task board. Polling needs no pane or live pane session.
The participant identity is `(host, team, name)`. Only `agent_board.team` is polled.
Hosts use `BoardID()`: `local` or `ssh:<ssh_connections key>`. `ssh:local` is distinct
from `local`; an OpenSSH alias alone does not add a Board host.

### Addresses and forwarding

Names and SSH connection keys use `[A-Za-z0-9_.-]+`. A qualified address is exactly
`<name>@local` or `<name>@ssh:<key>`. Empty parts, extra `@` or `:`, shell syntax,
unknown Board hosts and qualified `_system` addresses are rejected before history or forwarding.
Team validation retains its existing identifier allowlist.

- An unqualified ordinary recipient stays on the source host. agmsg delivers it;
  panemux records the original with source host/team and does not search other hosts.
- A qualified recipient is sent to its configured host with `to=<name>` and
  `from=<original name>@<source BoardID>`, even when it names the source host itself.
  Neither a roster entry nor a pane is required on either host.
- Compatibility: an unqualified recipient matching a configured pane on another host
  is forwarded there. `from` remains unqualified only when the sender is itself a
  configured pane on the source host. A pane-free sender is qualified on this route too,
  so the receiver can reply and the forwarded copy cannot enter history twice.
- Unqualified `_system` is the source host's panemux recipient. Ordinary and malformed
  status bodies remain history; only valid `kind: "board_status"` bodies update status.
  Reports are keyed by `name@BoardID` inside the cache, so equal names on different hosts
  coexist. Existing pane status/participation API responses project only reports matching
  that configured pane's host; pane-free identity listing is a separate API/UI change.

### Sender validation

The public `api.sh` read from host H establishes the source host. A valid unqualified
name is accepted as originating at H without roster lookup or prior registration in panemux.
The relay rejects every `from` containing `@`, including forwarded copies, and unqualified
configured pane IDs belonging to another host. Rejected rows affect neither history, status,
nor forwarding. A row for another team is rejected too.

`from == "_system"` requires consuming one matching entry from the existing own-send
ledger. The key remains `(destination host, team, unqualified recipient, body hash)`.
It is a short-lived multiset: identical sends consume independently, failures cancel
one entry, and unmatched/expired copies are rejected. Matched own sends are recorded
without forwarding them again. No forwarding ledger or model transition change is needed.

Core Broadcast accepts qualified IDs and legacy pane IDs, validates every recipient before
sending any, and returns the original requested IDs delivered before its first send failure.
It records the destination-visible, unqualified recipient in the ledger for `_system` sends.

### Plugin send and reply contract

For cross-host traffic use agmsg's public command, with your own unqualified name as sender:

```sh
bash ~/.agents/skills/agmsg/scripts/send.sh panemux worker orch@local 'ready for review' --force
```

Use the received `from` verbatim as the reply recipient, for example `worker@ssh:build`.
`--force` is required because remote identities are absent from the local roster.
Configured pane-to-pane compatibility retains its old unqualified reply addresses.
Send a status JSON body to unqualified `_system` to report to panemux on the source host.
Panemux neither registers proxy roster entries nor distributes a send wrapper.

Source-host attribution is not same-user authentication: another process on H running as
the same user can impersonate a name in H's agmsg installation. Reports are self-reports;
receiving a message does not authorize work or override an agent's approval rules.

During each poll, the relay reads a bounded newest window, selects rows after its opaque
cursor, validates and records originals, forwards eligible messages, then advances the
source cursor even after a forwarding failure. A managed tool-update pause leaves the
source batch retryable before cache/ledger/cursor changes. A pass with cursor changes
atomically persists one snapshot; a pass without changes performs no cursor-file write.

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
remain available to board execution, including hosts without a board pane. Sender validation uses the source-host rules above, independently of panes.

Operator sends wait for a missing SSH connection within the send context, including before the
first poll and after reconnect or transport loss. Polling and bootstrap continue to use the
non-waiting runner; all runners share the same connection pool.
