# Agent Board: cross-host relay

> Part of the current [Agent Board design](../agent-board.md).

## Cross-host relay

Two Claude/Codex processes on two different SSH-reached hosts cannot share one agmsg team directly
(agmsg is a single-host tool with no cross-host awareness, and the two hosts may not even be able
to reach each other). panemux is the only node with a connection to every host, so it relays:

1. A single goroutine polls every known host's `Since(team, cursor, limit)` every few seconds (see
   [Integration with agmsg](agmsg-integration.md#integration-with-agmsg) for why this is a bounded `--limit` poll with
   client-side filtering, not a true incremental read, and the truncation risk that implies).
2. `cursor` is one value per (host, team) — agmsg's own opaque `id` string for that host, *not*
   comparable across hosts and *not comparable at all* (see [Integration with
   agmsg](agmsg-integration.md#integration-with-agmsg): it anchors a position in `api.sh`'s own returned order) —
   persisted in a small local JSON file (e.g.
   `~/.config/panemux/board-relay-cursor.json`) — not a database table, since panemux owns no
   database — so a panemux restart resumes roughly where it left off.
3. For each new row, panemux first checks `from`. If `from` is a board-enabled pane ID panemux
   knows about on that row's source host, the row passes. **If `from == "_system"`, the row passes
   only if it matches an entry in panemux's own short-lived "own-send ledger"** (see [Package
   layout](architecture.md#package-layout)) — a small in-memory record of `(destination host, team, to, body hash)`
   for every `Send` panemux's own broadcast handler and command center have issued recently, kept
   for a few poll intervals and then discarded. This is deliberately stricter than treating
   `_system` as unconditionally trusted: because `send.sh --force` never checks `from` against a
   roster, any agent on any host can locally write a row claiming `from: "_system"`, and nothing at
   the agmsg layer distinguishes that from a row that reached the same host because panemux's own
   broadcast handler really did call `Send` there — the ledger match is what tells them apart. A row
   with `from == "_system"` that matches nothing in the ledger is a suspected forgery: it is dropped
   and logged the same as any other failed check, never relayed and never cached. Any other `from` —
   one that is neither a known local pane ID nor a ledger-matched `_system` — is dropped and logged
   too. See [Security model](security-model.md#security-model) for the forgery scenario this closes and why a
   a universal `_system` allowance is not sufficient. If
   `from` passes: when `to == "_system"`, panemux updates the
   [in-memory status cache](architecture.md#architecture) instead of relaying it — status reports never leave the
   host they were written on. Otherwise, panemux resolves `to` to its owning pane and that pane's
   host via the already-known pane→session config; if that host differs from the source host,
   panemux calls `Send` (always `--force`, per [Integration with
   agmsg](agmsg-integration.md#integration-with-agmsg)) on the destination host's `AgmsgClient`. A `to` that doesn't
   resolve to any known pane is dropped and logged, the same as an invalid `from`.
4. Same-host `to` needs no relay: sender and receiver are already members of the same local agmsg
   team.
5. `GET /api/board/status` never triggers an `AgmsgClient` call at all — it only reads the status
   cache the relay already keeps current, per [Architecture](architecture.md#architecture).

**Cold-start backfill.** `BoardCache` starts empty on every panemux process start (see [Known
limitations](limitations.md#known-limitations)), and the regular poll loop's small `--limit` (tuned for "a few
seconds' worth of new rows," per [Package layout](architecture.md#package-layout)) is the wrong size for
repopulating a cold cache — most panes' latest status could easily be older than that small a
window. Before entering its steady-state poll loop, the relay therefore performs exactly one
larger-`--limit` call per (host, team) — e.g. `--limit 1000` versus the steady-state default — scans
those rows the same way the steady-state loop would (status rows update the cache, per-pane keeping
only the newest; ordinary messages are appended to `history`), and only then starts polling normally
from whatever cursor position that backfill pass reached. This does not change any of the
correctness properties above: it is still a bounded `--limit` read with the same accepted truncation
risk if a host has produced more than the backfill limit's worth of rows since the cursor file was
last written, and it does not retroactively fix a restart that lost the cursor file entirely (that
case still starts from the newest rows only, same as today). It shortens, but does not eliminate,
the window in which the dashboard shows stale or empty status after a panemux restart.

**Delivery is at-least-once, not exactly-once — an accepted simplification, not an oversight.**
Because agmsg's own schema has no field for panemux to mark "this row has already been relayed,"
the cursor file is the only bookkeeping. If panemux crashes or restarts between relaying a message
and persisting the updated cursor, that message can be relayed again, and the destination agent
sees a duplicate. This is judged an acceptable, self-evident nuisance (a repeated message is easy
for an agent to recognize and ignore) rather than something worth a more complex dedup scheme —
consistent with agmsg's own documented v1 limitations elsewhere (see [Known
limitations](limitations.md#known-limitations)).

This makes panemux's relay role structurally similar to a TURN server (always in the data path for
the life of the exchange, because a direct path between the two remote hosts is not assumed to
exist) rather than a STUN server (which only helps two peers find each other and then steps aside).
Unlike a general-purpose TURN server, panemux is the only possible relay for a given pair of hosts
(it is the sole node holding SSH credentials to both), so there is no negotiation step — delivery is
always routed through it by construction.
