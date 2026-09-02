# API contract fixtures

Real JSON, captured from the router `server.New()` builds, so the frontend's
Zod schemas can be validated against what Go actually emits instead of against
hand-written TypeScript objects. This closes gate **G3(c)** in
[../../docs/quality-gateway.md](../../docs/quality-gateway.md) — see issue #191.

## Who writes these

`internal/server/contract_fixture_test.go`. It drives each response through the
real router (or, for the two `ws-*` files, over a real WebSocket handshake) and
**rewrites every file here on each run**. Nothing in this directory is edited by
hand.

## Who reads them

`frontend/src/schemas/contract.test.ts`. It parses each file with the schema
that owns it, and fails when a schema the frontend uses on a server response has
no fixture at all.

## Why the direction is one-way

Go → fixture → TypeScript. The Go suite runs before the frontend suite in
`make test`, `make check` and `.github/workflows/ci.yml`, so a Go-side field
rename lands in the fixture first and the **frontend** test is what goes red.
A golden test that failed on drift instead would stop at the Go suite, and the
schema mismatch this gate exists to catch would never reach the side that has
the stale schema.

Practical consequence: after changing a Go response struct, run `make test-go`
(or `make check`) before trusting a green frontend run, and commit the fixture
diff alongside the struct change.

## What is normalized, and what is not

Keys, nesting and JSON types are never touched — those are the contract. Only
these *values* are replaced, each because it would otherwise differ on every run
or on every machine and leave a clean checkout dirty:

| Value | Replaced with | Why |
|---|---|---|
| Any RFC3339 timestamp | `2026-01-01T00:00:00Z` | Wall clock at capture time |
| The capture's temp `HOME` | `/tmp/sample-project` | Machine-specific, and [DEVELOPMENT.md](../../DEVELOPMENT.md)'s path-sanitization rule forbids committing a real path |
| `BoardCache.Epoch()` | `0000000000000000` | Random per process |
| The detected local shell | `/bin/sh` | Whatever shell the capturing machine runs |

`TestAPIContractFixtures_ContainNoMachinePaths` re-checks the path rule against
**the bytes each capture produces**, not against the files already committed —
for that failure the committed file is the thing under suspicion, not the
reference. So a normalization rule that stops matching fails the suite even when
run alone, rather than quietly committing someone's home directory.

## A capture must be deterministic

`TestAPIContractFixtures_AreDeterministic` runs each capture 20 times and
requires identical bytes. Nothing else can see a wobble: the files are rewritten
rather than diffed, so a capture whose order varies succeeds silently and leaves
a modified file behind for the next `git status` to blame on whatever branch
happened to run the suite — and a fixture diff that is sometimes meaningless is
a weaker signal than one that is always real.

The trap is response order that comes from a Go map. `GET /api/sessions` returns
`session.Manager.List()`, which ranges over one, so the `sessions` capture sorts
its rows before writing them; it sorts the raw JSON elements rather than
decoding into a struct, because decoding and re-encoding would drop any key the
struct does not name. Sort in the *capture*, not in the handler, unless a client
actually depends on the order.

Note that the repetition check is a net, not a proof: Go randomizes a bucket's
start offset rather than handing out uniform permutations, so a two-element map
agrees with itself about seven times in eight per run.

## Optional fields a capture cannot reach

Accepting a capture proves nothing about a field the capture does not contain.
`contract.test.ts` walks each schema against its fixture and names every
optional field that no capture populates anywhere, comparing that against a
declared list with a reason per entry — so a newly optional Go field that no
fixture exercises fails the suite rather than passing silently. Two are declared
today: `GitInfoSchema`'s repository fields (they need a pane whose live cwd is a
real git repository, plus a `gh pr view` lookup) and `OpenUrlResponseSchema.port`
(a forward is only opened for an `ssh` pane, which needs a second host).
