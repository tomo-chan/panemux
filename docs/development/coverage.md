# Coverage gates

> Part of the [development guide](../../DEVELOPMENT.md). That document carries the summary of this one and the rules that always apply.

### Coverage

- `make coverage-go` enforces at least 80% combined coverage across `internal/config`, `internal/api`, `internal/ws`, `internal/server`, `internal/board`, `internal/portforward`, `internal/commandcenter`, `internal/boardmcp`, `internal/fileops`, `internal/homedir`, `internal/cachedir`, and the root package. The `Makefile`'s `COVERAGE_PKGS` is the authority; this list follows it.
- `make coverage-frontend` enforces at least 80% coverage across `frontend/src/hooks/`, `frontend/src/schemas/`, and `frontend/src/utils/`.
- **The threshold stays at 80%; what gets strengthened is the scope.** Raising it works, but the cheapest way to satisfy a higher number is to generate tautological tests, which lowers both protection against regressions and resistance to refactoring. See decision D1 in [docs/quality-gateway/decisions.md](../quality-gateway/decisions.md).
- The gated package set is checked against `go list ./...` by `TestCoverageScopeCoversEveryPackage`, so a package added to the repository fails the suite until it is either gated or explicitly excluded with a reason. Do not widen the exclusion list to make that failure go away.
- `make coverage-go` builds the frontend first: the root package is gated and `main.go` embeds `frontend/dist`.
- What is excluded, and why, is written in the `Makefile` next to `COVERAGE_PKGS` — `internal/session`'s real PTY / SSH / tmux transports, and the process-lifetime entry points (`main`, `runServer`, `bootstrapWatcher.Run`). Do not add an exclusion without a reason recorded there.

### Per-block coverage (`make coverage-blocks`)

- The 80% threshold above is a *statement* percentage: it cannot see an entire `if err != nil { ... }` body that no test enters, because the happy path around it carries the function past 80%. Issue [#164](https://github.com/tomo-chan/panemux/issues/164) found 28 such branches by hand.
- `make coverage-blocks` re-reads `make coverage-go`'s profile and lists every block the suite never entered; `make coverage-go` prints the count as a one-line summary.
- As a gate it is **scoped to the diff** and pull-request-only — `COVERAGE_BLOCKS_BASE=origin/main make coverage-blocks` fails only on a block covering a line your branch changed. Deliberately not in `make check`: it needs the base branch. Why diff-scoped rather than repository-wide: decision D8 in [docs/quality-gateway/decisions.md](../quality-gateway/decisions.md).
- A block that cannot be reached from a test is marked `//coverage:exempt <reason>` on its opening line or the line above. **The reason is required.** The `coverage-blocks-exempt` label exempts the whole branch and is the blunter tool; prefer the marker, which sits in the diff a reviewer reads.
- A changed file in a package `COVERAGE_PKGS` excludes is reported as **not measured**, not as covered.
