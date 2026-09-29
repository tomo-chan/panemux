# Model checking (`make model-check`)

> Part of the [development guide](../../DEVELOPMENT.md). That document carries the summary of this one and the rules that always apply.

### Model checking (`make model-check`, issue #168)

- `internal/board`'s `ownSendLedger` — the record of Send calls panemux itself issued, which the
  relay consults before believing a row that claims `From == SystemID` — has a TLA+ spec at
  `spec/agentboard/OwnSendLedger.tla`. It is the pilot subsystem for [#168](https://github.com/tomo-chan/panemux/issues/168), picked because it is small,
  self-contained, security-critical, and the place PR #167's second review round found a real
  multiset-vs-set bug.
- **Model checking alone proves the model, never the code.** The split that closes that gap is two
  tiers, mirroring [docs/agent-board.md](../agent-board.md)'s agmsg compatibility contract:
  - **Tier 1 — hermetic, inside `make check`.** `internal/board/ledger_conformance_test.go` attaches
    a tracer to the real `ownSendLedger`, drives it (directly, and through `Relay.Broadcast`/`Poll`),
    and looks every transition it makes up in the table TLC exported to
    `internal/board/testdata/ownsendledger-transitions.json`. No JDK, no jar, no network.
  - **Tier 2 — `make model-check`, a pull-request CI job.** Runs TLC over the spec, regenerates the
    table and diffs it against the committed copy. Outside `make check` because it needs a JDK and
    `tla2tools.jar`, which `make install-deps` does not install.
- The Tier 1 reference model is **the exported table and nothing else**. Do not hand-write a second
  Go state machine beside it: it would be a third artifact to keep in sync, and it would drift from
  the spec exactly the way the implementation it is checking might.
- Never hand-edit `internal/board/testdata/*-transitions.json`. Change the `.tla`/`.cfg`, run
  `make model-check-write`, and commit the table diff alongside — that diff is the behavioral change
  a reviewer reads.
- **The bound in the table comes from the `.cfg`, never from the dump.** `make test-model-check`
  (hermetic, inside `make check`) drives `scripts/tla_transitions.py` against committed dot fixtures
  in `scripts/testdata/model-check/` and asserts every rejection arm, the load-bearing one being
  "a TLC run that explored less than `MaxEntries` is refused". An exporter that inferred the bound
  instead would let an under-explored run shrink Tier 1's own drivers to match, which looks green.
  `python3` is optional for it the way `jq` is for `make test-hooks`: absent, those checks report
  themselves as skipped.
- The check is **bounded**: `MaxEntries` in the `.cfg` caps how many occurrences one key may hold, so
  Tier 1 says nothing about a ledger holding more. Raise the bound in the `.cfg` and regenerate if a
  driver needs to go further.
- The Tier 1 drivers assert not only that the implementation never contradicted the model, but that
  it **took every transition the model has**. Without that, a table permissive enough to accept
  anything would pass as quietly as a correct one.

```sh
curl -fsSL -o /tmp/tla2tools.jar \
  https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar
TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check        # check the spec, diff the table
TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check-write  # regenerate the table
```
