---
type: Specification
title: "Test granularity and testability seams"
description: "Behavioral factor coverage, test granularity, and injectable side-effect boundaries."
---

# Test granularity and testability seams

> Part of the [development guide](../../DEVELOPMENT.md). That document carries the summary of this one and the rules that always apply.

### Test granularity

Test at the smallest unit that exercises the logic, not at the outermost entry point.

Before implementing or declaring a feature covered, enumerate the behavioral factors and cover their meaningful combinations in tests. Do not stop at one happy path plus one error path.

For every new feature or behavior change, explicitly consider:

- Input shape variants: legacy and new schemas, omitted optional fields, defaulted fields, invalid enum values, empty lists, duplicate IDs, unknown references, malformed request bodies.
- State variants: active vs inactive items, empty vs populated state, existing vs missing resources, persisted config vs memory-only config, and visible vs dismissed transient UI errors when user actions can fail after optimistic updates.
- Operation variants: read, create, update, delete, switch, save, reload, restart, and no-op cases where applicable.
- Boundary variants: minimum/maximum values, zero/negative values, size sums, single item vs multiple items, nested structures, and paths with `~/` when paths are involved.
- Compatibility and migration: old config/API shape, new config/API shape, precedence when both exist, migration-on-save behavior, and post-reload behavior after migration.
- Persistence and side effects: in-memory updates, disk writes, unchanged unrelated siblings/items, session manager changes, restart behavior, and API response status/body.
- Frontend runtime validation: Zod acceptance/rejection, API fallback behavior, active UI state, invalid selection no-ops, and all visible mode/position variants.

Use table-driven tests when factors form a matrix. For large matrices, test all high-risk cross-products and pairwise combinations for the rest, but document the factors through test names and fixtures so omissions are intentional and reviewable.

Known anti-patterns:

| Anti-pattern | Problem | Correct approach |
|---|---|---|
| Testing a function that makes real network calls to verify config-resolution logic | Network errors hide config errors; any error is accepted as expected | Extract pure config-resolution into a separate function such as `resolveSSHConfig` and test it directly, asserting exact return values |
| `if err != nil { assert.NotContains(t, err.Error(), "not found") }` | Accepts any error except `not found` and masks setup errors | Assert the specific happy path with `require.NoError(t, err)` and verify returned values. For expected-failure paths, assert the exact error string |
| Test data that never exercises the real-world variant | Bugs in real user inputs stay invisible | Add test cases that cover exact user formats such as `~/`-prefixed paths and omitted optional fields |
| Testing only the error case of a new validation rule, not the passing case | The positive path is never confirmed to work | For every new validation rule, write both a failure test and a success test |

**Testability rule:** if production code calls `os.UserHomeDir()`, `DefaultPath()`, or another global singleton directly, add an injectable override so tests can substitute a controlled value without touching the real filesystem or home directory.

Example: `Config.sshConfigPath` uses `sshconfig.DefaultPath()` only when the override is empty.

**The home directory has one seam, and it is `internal/homedir`.** Call `homedir.Dir()`; substitute it
in a test with `homedir.SetForTest(t, dir)` or `homedir.SetFailingForTest(t, err)`. Do **not** call
`os.UserHomeDir()` — `.golangci.yml`'s `forbidigo` rule fails the build on it outside that package —
and do not reach for `t.Setenv("HOME", ...)`, which mutates the process environment every goroutine
and subprocess in the binary shares, and does nothing on Windows (`USERPROFILE` is used).
Substitute the unsynchronized `homedir.dirFn` only from non-parallel tests; unlike `t.Setenv`,
it races silently under `t.Parallel`. Cross-package callers need the shared seam rather than
package-private overrides; see [filesystem seams](../decisions/architecture/filesystem-seams.md)
and [#212](https://github.com/tomo-chan/panemux/issues/212).

**The cache directory has its own seam, and it is `internal/cachedir`.** Same shape — `cachedir.Dir()`,
`cachedir.SetForTest(t, dir)`, `cachedir.SetFailingForTest(t, err)`, and a `forbidigo` rule on
`os.UserCacheDir()` outside that package — and a separate package with different platform behavior: `os.UserCacheDir` never calls
`os.UserHomeDir`. Non-darwin Unix uses `$XDG_CACHE_HOME`, falling back to `$HOME/.cache`; darwin
uses `$HOME/Library/Caches` and ignores `XDG_CACHE_HOME`. Substituting home therefore cannot
influence cache lookup, and neither environment variable alone covers both platforms. Separate
packages keep each `forbidigo` exclusion to one function and catch a stray `os.UserHomeDir` in
the cache seam. See [#226](https://github.com/tomo-chan/panemux/issues/226).

**Neither seam is safe under `t.Parallel`.** Substitute them only from non-parallel tests.
A mutex would remove the data race while leaving cleanup last-writer-wins. If parallel tests
are needed, use per-test injection without globals for the affected packages rather than a lock.
See [filesystem seams](../decisions/architecture/filesystem-seams.md) and
[#227](https://github.com/tomo-chan/panemux/issues/227).

**Persisted files are written through one seam, and it is `internal/fileops`.** `fileops.AtomicWrite`
is the temp-file-plus-rename write every file panemux persists goes through — `config.yaml`, the
auth token file, the relay cursor, bootstrap state, and the command-center session id;
`fileops.CreateTemp` / `OpenFile` / `Chmod` are the individual operations for the two callers that
need the steps rather than the whole write (the MCP config file, which is never renamed, and the
history file, which is appended to). Substitute them in a test with
`fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: err}).Ops())` — a `Spy` performs every real
operation and fails only the steps it is given, so the filesystem still ends up in the state it
would have been in (an injected write failure is a *short* write, as a real ENOSPC is), and
`spy.Files()` names the files it handed out so a test can assert they were cleaned up.

Moving a write onto it changes one thing worth checking: `AtomicWrite` finishes with a rename, and
rename replaces what is at the path rather than following it, so a symlinked target is swapped for a
regular file where `os.WriteFile` would have written through it. If the path is one an operator may
have hand-linked, resolve it first — `internal/config`'s `resolveWriteTarget` is the example, and its
doc comment says why that resolution is at the caller rather than in the seam.

Unlike the home-directory seam, nothing enforces this one — there is no `forbidigo` rule that can
tell a legitimate `os.WriteFile` from one that should have been an `AtomicWrite`. It is true as
written today because the writes were moved onto it, not because the build would fail otherwise, so
a new persisted file is the reader's job to route correctly.

Reach for it when a failure arm sits *after* the file was created by the function under test: a path
whose shape can be broken (a regular file where a directory goes, a directory where a file goes,
`/dev/full`) is still the better fixture and needs no seam, but that only works while the path came
from the caller. Once `os.CreateTemp` has succeeded, the file exists, is writable and is owned by the
process, and CI runs as root, so the `Write`/`Close`/`Chmod` arms — the disk filling partway through
a write, which is the failure the rename discipline exists to survive — have no fixture at all. Same
`t.Parallel` caveat as `homedir`: `fileops.ops` is one unsynchronized package variable. See issue
[#222](https://github.com/tomo-chan/panemux/issues/222).
