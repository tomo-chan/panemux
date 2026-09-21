# Agent Board: state-machine model checking

> Part of the [Agent Board design](../agent-board.md). Read that document's status note first — it says which parts of this design are shipped.

## State-machine model checking

Issue [#168](https://github.com/tomo-chan/panemux/issues/168) asks a different question from the
[agmsg compatibility contract](agmsg-contract.md#agmsg-compatibility-contract). That contract keeps
panemux's assumptions about an *external* dependency from drifting. This one asks whether panemux's
*own* small, security-critical state machines are correct at all — because several of the real bugs
PR #167's adversarial review rounds found (the duplicate-broadcast collision in `ownSendLedger`, the
relayed-row double-counting invariant, cross-host `from`-forgery detection) were exactly the shape a
hand-written table-driven test misses however carefully the cases were enumerated. A property test
gives as much confidence as the cases a human thought to write; that is the ceiling this is trying
to get above.

**`ownSendLedger` is the pilot**, chosen over `Relay.processRow` and `dynamicBoardExecutor`'s
candidate resolution. It is the smallest of the three and fully self-contained — `Record` /
`Consume` / `Forget` plus TTL expiry, on one key, with no I/O — so it settles the shape of the two
tiers (where the `.tla` lives, what the exported table looks like, how the CI job runs) at the
lowest cost if that shape turns out wrong. It is also the subsystem where the multiset-vs-set bug
actually lived, which makes it the honest demonstration case rather than a contrived one.
`Relay.processRow` is the larger payoff and should follow now that the scaffolding is settled;
`dynamicBoardExecutor` is a search-and-retry algorithm rather than a state machine and is better
served by Go fuzzing or a property test than by TLA+.

### Two tiers, for the same reason the agmsg contract has two

**Model checking alone proves the model, never the Go code that is supposed to implement it.** A
spec and an implementation can each be internally consistent and still disagree, and nothing about
running TLC notices. So:

- **Tier 1 — fast, hermetic, part of `make check` on every commit.** `internal/board/ledger.go` can
  emit its own transitions through an optional `trace` hook (nil in production — `newOwnSendLedger`
  never sets it). `internal/board/ledger_conformance_test.go` attaches that hook and looks every
  transition the real ledger makes up in the state graph TLC exported to
  `internal/board/testdata/ownsendledger-transitions.json`. A transition the model has no case for,
  or one landing somewhere the model forbids, fails. No JDK, no `tla2tools.jar`, no network — it
  runs like any other Go test.
- **Tier 2 — `make model-check`, a separate pull-request CI job
  (`.github/workflows/model-check.yml`'s `tlc` job).** Runs TLC against
  `spec/agentboard/OwnSendLedger.tla` — invariants, action properties and deadlock over the full
  state space — then regenerates the transition table and diffs it against the committed copy. Out
  of `make check` for the reason the agmsg Tier 2 job is: it depends on an external toolchain
  (a JDK plus `tla2tools.jar`) that `make install-deps` does not install.

### What the model says

The spec abstracts one key's slice of expiry timestamps to two counts — occurrences already past
their expiry that `Consume` has not yet dropped, and occurrences still matchable. That is faithful
only because entries are appended in call order with `now + ttl` and the clock is monotonic, so the
expired ones are always a prefix; the spec's header says so, and the Go-side checker computes both
counts from the real slice, so a run that broke the assumption would show up as a conformance
failure rather than as silent agreement.

Three properties, each verified to be non-vacuous by perturbing the spec until it caught the
specific design bug it exists for:

| Property | The design bug it rejects |
|---|---|
| `Conservation` | Every occurrence ever recorded is still held, consumed, forgotten or garbage-collected — exactly once. A map holding **one** expiry per key, the pre-#167 shape, loses an occurrence on every duplicate `Record` and violates this. |
| `ConsumeNeverForges` | `Consume` returns TRUE only out of a state that held a live occurrence. This is the security bound: a pane on the destination host must not be able to make panemux believe a `From == SystemID` row it never sent. |
| `RecordIsImmediatelyMatchable` | A `Consume` straight after a `Record` always matches. This is the multiset property, and it is the one whose failure is *not* a security hole but a silent data loss: a duplicate broadcast — ordinary input, not an attack — being dropped as "invalid from" and vanishing from history. |

### What Tier 1 actually asserts, and what it does not

Three drivers, because the checks they can make are different:

1. Walking the model's state space with the real implementation, trying every operation from every
   ledger shape it can reach. This ends with **every non-Expire transition in the table exercised**,
   which is asserted. "The implementation never contradicted the model" is much weaker than "the
   implementation matched the model everywhere": only the second rules out a table so permissive it
   would accept anything.
2. Every operation sequence of a fixed length over **two** keys. The spec models a single key and
   assumes keys do not interfere; only a snapshot of the other keys on each step can check that, and
   this is where a `Forget` truncating the wrong key's slice shows up.
3. `Relay.Broadcast` and `Relay.Poll` driving the ledger through their **own** call sites — the
   trace-conformance case proper. It is the relay's use of the ledger that has to conform, not only
   the ledger in isolation.

Stated plainly, because the bound is real: the check is **bounded by `MaxEntries` in the `.cfg`**
(4 occurrences for one key, as committed). A defect that only appears with more occurrences held at
once is outside what Tier 1 says anything about. Raising the bound is a `.cfg` edit plus
`make model-check-write`.
