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
the written files, so a normalization rule that stops matching fails the suite
rather than quietly committing someone's home directory.
