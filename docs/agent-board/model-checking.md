# Agent Board: state-machine model checking

> Part of the current [Agent Board design](../agent-board.md).

## State-machine model checking

The agmsg contract checks an external dependency. Model checking covers panemux's own small,
security-sensitive state machines, where example-based tests can miss operation sequences.

The pilot model is the own-send ledger: one key, duplicate records, single-consume semantics,
explicit forget, and TTL expiry. It is small enough to explore exhaustively and directly protects
the `_system` sender boundary.

### Properties

| Property | Requirement |
|---|---|
| Conservation | Each recorded occurrence is held, consumed, forgotten, or expired exactly once. |
| Consume never forges | A successful consume requires a live matching occurrence. |
| Record is immediately matchable | Recording one occurrence makes one subsequent consume succeed, including for duplicate keys. |

The ledger must be a multiset. A single value per key loses identical broadcasts; accepting a
consume without a live occurrence would let a pane forge a panemux-originated `_system` row.

### Two tiers

- **Tier 1, in `make check`:** replay the real Go implementation against the committed transition
  graph, including two-key independence and relay call sites. Every modeled non-expiry transition
  must be exercised.
- **Tier 2, `make model-check`:** run TLC against `spec/agentboard/OwnSendLedger.tla`, verify
  invariants and deadlock freedom, regenerate the graph, and require no diff.

Tier 2 needs a JDK and `tla2tools.jar`, so it runs separately in CI. The committed configuration
bounds the model to four simultaneous occurrences for one key; behavior requiring a larger state is
outside the current proof. Raising the bound requires regenerating and reviewing the transition
table.
