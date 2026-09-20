# `spec/agentboard/`

TLA+ specifications for `internal/board`'s state machines, and Tier 2 of issue
[#168](https://github.com/tomo-chan/panemux/issues/168)'s model-checking split.

| File | Models |
|---|---|
| `OwnSendLedger.tla` / `.cfg` | `internal/board/ledger.go`'s `ownSendLedger`: `Record` / `Consume` / `Forget` plus TTL expiry, for one key. The record of Send calls panemux itself issued, which the relay consults before believing a polled-back row that claims `From == SystemID`. |

**These are not the check.** TLC proves things about the spec and nothing about
the Go code that is supposed to implement it. What runs on every commit is
Tier 1: `make model-check` exports each spec's full state graph to
`internal/board/testdata/<name>-transitions.json`, and
`internal/board/ledger_conformance_test.go` replays the real implementation's
own transitions against that table inside `make check` — with no JDK and no
`tla2tools.jar`.

So a change here is only half a change. Run `make model-check-write` and commit
the transition-table diff alongside the `.tla` edit; that diff is what a
reviewer reads, and Tier 1 keeps checking the old behavior until it lands.

```sh
curl -fsSL -o /tmp/tla2tools.jar \
  https://github.com/tlaplus/tlaplus/releases/download/v1.7.4/tla2tools.jar
TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check        # check specs, diff tables
TLA_TOOLS_JAR=/tmp/tla2tools.jar make model-check-write  # regenerate tables
```

Full rationale, including why `ownSendLedger` was the pilot and what the bound
in each `.cfg` costs: [docs/agent-board.md](../../docs/agent-board.md#state-machine-model-checking)
and decision D12 in [docs/quality-gateway.md](../../docs/quality-gateway.md).

Alloy models live elsewhere, under `docs/models/`; both are checked by
`.github/workflows/model-check.yml`.
