# Contract fixtures (`testdata/api-contract/`)

> Part of the [development guide](../../DEVELOPMENT.md). That document carries the summary of this one and the rules that always apply.

### Contract fixtures (`testdata/api-contract/`)

- The Zod schemas are validated against **JSON that Go actually emitted**, not against hand-written
  TypeScript objects. `internal/server/contract_fixture_test.go` captures each response from the real
  `server.New()` router and rewrites `testdata/api-contract/` on every Go test run;
  `frontend/src/schemas/contract.test.ts` parses each file with the schema that owns it.
- **Run `make test-go` (or `make check`) after changing a Go response struct, before trusting a green
  frontend run.** The direction is one-way — Go → fixture → TypeScript — so the frontend suite only
  sees a renamed field once the Go suite has regenerated the fixture. `make test`, `make check` and
  CI all run them in that order.
- Commit the fixture diff alongside the struct change; it is the contract change, in the diff a
  reviewer reads.
- Never hand-edit a file under `testdata/api-contract/` — the next Go test run overwrites it. Adding
  a response schema means adding a capture there and an entry in `contract.test.ts`; both sides have
  an exhaustiveness check that fails until you do.
- See [testdata/api-contract/README.md](../../testdata/api-contract/README.md) for the values normalized out
  of a capture, and decision D10 in [docs/quality-gateway/decisions.md](../quality-gateway/decisions.md) for why the
  fixtures are rewritten rather than diffed.
