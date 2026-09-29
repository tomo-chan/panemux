# `scripts/testdata/model-check/`

Fixtures for `scripts/model_check_test.sh`, which tests
`scripts/tla_transitions.py` — the exporter that turns a TLC state graph into
the transition table Tier 1 checks the Go implementation against.

Testing the exporter matters more than testing most scripts here: the table it
writes **is** the reference model every Tier 1 assertion is compared to. If the
exporter is wrong, the hermetic gate is wrong in the direction that looks green.

## How `complete-max2.dot` was produced

It is real TLC output, not hand-written. `Max2.cfg` runs the repository's own
`spec/agentboard/OwnSendLedger.tla` at a deliberately tiny bound
(`MaxEntries = 2`, `MaxRecords = 2`) so the dump stays reviewable at 183 lines
while still covering every shape the exporter has to parse: a state with no
outgoing `Record` because it sits at the bound, multiple `Expire` edges out of
one state, self-loops, and negative node ids.

```sh
cp spec/agentboard/OwnSendLedger.tla scripts/testdata/model-check/Max2.cfg /tmp/gen/
cd /tmp/gen && java -cp /path/to/tla2tools.jar tlc2.TLC \
  -cleanup -dump dot,actionlabels complete-max2.dot -config Max2.cfg OwnSendLedger.tla
```

`truncated-max2.dot` is the same command with `MaxEntries = 1`. The test feeds
it to the exporter **paired with `Max2.cfg`**, which asks for 2 — the
under-exploration case, and the one the exporter originally could not detect
because it derived the bound from the states it was handed rather than from the
`.cfg`.

## The rest

Each is `complete-max2.dot` with one thing broken, so the test can assert the
exporter rejects it rather than writing a quietly wrong table:

| Fixture | Broken how |
|---|---|
| `lattice-gap-max2.dot` | every `{expired:1 live:1}` node and its edges removed — a hole in the reachable lattice, with the `held == MaxEntries` rim still present |
| `action-label-mismatch.dot` | one edge relabelled, so the label disagrees with its destination state's own `action` variable |
| `missing-variable.dot` | one node's `live` assignment dropped |
| `dangling-edge.dot` | one edge redirected at a node id the dump never declares |
| `empty.dot` | empty, as a dump TLC never wrote would be |

`no-constants.cfg` declares no `MaxEntries`, asserting the exporter refuses to
guess a bound.

Regenerating any of these means re-running the command above and re-applying the
one-line break; they are small enough to inspect by eye.
