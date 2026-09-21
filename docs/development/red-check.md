# Red-check (`make efficacy`)

> Part of the [development guide](../../DEVELOPMENT.md). That document carries the summary of this one and the rules that always apply.

### Red-check (`make efficacy`)

- A test you change must **fail** when your implementation diff is reverted. That is the machine-checkable half of the TDD rule in [DEVELOPMENT.md](../../DEVELOPMENT.md): the order lines were written in cannot be recovered after the fact, but the result can.
- Run it yourself with `make efficacy` (compares against `origin/main`; override with `EFFICACY_BASE`). CI runs it on every pull request.
- It is deliberately **not** part of `make check` — it needs the base branch and a second test run, which is a per-pull-request cost, not a per-turn one.
- A branch that changes no implementation, or changes implementation but no test, is skipped rather than failed.
- If a change genuinely should not go red without its implementation — a pure refactor, a test-only rename, a test pinning behavior this branch never touched — mark that test `//efficacy:exempt <reason>` in the comment directly above it. **The reason is required** — a bare marker waives nothing and the run names the tests carrying one — and the marker is the one to reach for first: it is per-test and it sits in the diff a reviewer reads. The `efficacy-exempt` label is the blunter tool and takes the whole branch out of scope.
- Neither is for getting past a test that turned out not to assert anything. A branch where most changed tests survive is worth a second look before it is exempted: on [#190](https://github.com/tomo-chan/panemux/issues/190) 15 of 18 survived and every one was legitimate, because the whole branch existed to pin already-correct behavior — that is the rare shape, not the usual one.
- See decision D4 in [docs/quality-gateway/decisions.md](../quality-gateway/decisions.md).
