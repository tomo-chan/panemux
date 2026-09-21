# Quality gateway: first measurements

> Part of the [quality gateway](../quality-gateway.md). That document defines the gates this one explains.

## First measurements

Roadmap item 7 asked for observation without gating, so the point of it is the numbers.

**Read them as indicative, not as a baseline to diff against.** They come from `make bench
BENCH_ARGS='-count 5'` on a shared container, and the median is quoted with the observed min–max
beside it because that spread is the story: the `publish` rows move by up to **2.9× between runs of
the same binary on the same machine**. Anyone rerunning these and getting a different number has not
found a regression. Choosing a threshold from a single run of these figures would produce a gate
that fires on container noise, which is principle 4's failure mode exactly — a distribution measured
on dedicated hardware is what that step actually needs.

The rows below were taken in two sessions on two different containers — the originals on a 4-core
Intel Xeon @ 2.80GHz, the [#193](https://github.com/tomo-chan/panemux/issues/193) before/after pair
on a 4-core Intel Xeon @ 2.10GHz. **Only compare figures measured together.** The
before-and-after tables under "Terminal output throughput" were run back to back on the same
container for exactly that reason; the relay figures are from the first session and have not been
retaken.

**None of the caution above applies to the accessibility rows**, and it is worth saying so rather
than letting a reader carry it down the page. Those are violation counts, not timings: they have no
distribution to be noisy, they *were* retaken on a second container and came back identical, and
they are the only figures in this section that a check actually enforces. That is what made the
ceiling in #194 possible where a performance threshold still is not.

### Terminal output throughput

`managedSession.publish` is the path every byte a pane produces travels. The first measurement of it
found the replay buffer dominating everything else, and [#193](https://github.com/tomo-chan/panemux/issues/193)
fixed it; both sets of numbers are kept below, because the *shape* of the finding is the useful part
and it changed.

**What was found.** The replay buffer was a plain slice, and retaining a fixed 256KB window meant
reallocating and copying the whole window on every chunk. A pane open for more than a few seconds is
in that state permanently, so it paid a full window copy per 4KB of output, forever — two orders of
magnitude more than the same call on a cold buffer. `publish` is now backed by a ring
(`internal/session/replay_buffer.go`): it writes into storage it already owns and moves two indices,
so the steady-state cost is the length of the chunk and the steady-state allocation is nothing at
all.

**What it costs now.** Per 4KB chunk with no subscribers, five runs before and after, on the same
container back to back:

| Replay buffer | before, ns/op (median) | range | after, ns/op (median) | range |
|---|---|---|---|---|
| Empty (cold) | 1,774 | 1,759 – 1,885 | 56 | 55 – 59 |
| Full (steady state) | 222,527 | 111,808 – 240,915 | 117 | 116 – 124 |

| Replay buffer | before, B/op | after, B/op |
|---|---|---|
| Empty (cold) | 4,096 | 0 |
| Full (steady state) | ~598,000 | 0 |

The cold and full rows are now within ~2× of each other rather than two orders of magnitude apart,
which is #193's completion condition. The residual gap is cache behaviour: the cold benchmark writes
4KB into 4KB of storage, the full one writes it into a 256KB ring at a moving offset.

Two caveats on reading the table across its two halves. The cold rows do not measure quite the same
operation: `BenchmarkSessionPublishColdBuffer` used to drop the buffer's storage each iteration and
now calls `reset()`, which keeps it — that *is* what a ring does when its window empties, but it
means the after-column's 0 B/op is partly the benchmark and not only the change. The full rows are
directly comparable, and they are the ones the finding rests on. Separately, the before-column's
111,808 low is the first of its five runs and roughly half the other four; treat the median, not the
range, as the figure.

The multiple is still not worth quoting to two significant figures — at these spreads the same two
benchmarks support anything from ~120× to ~200× — and an earlier revision of this section said
"~33×", computed from a cold figure that the benchmark's own `b.StopTimer` calls had inflated
threefold. The finding survived that correction comfortably; the precision never existed.

**The second conclusion inverted, and that is the part to carry forward.** With the buffer copy in
place, subscriber fan-out looked cheap — 16 subscribers added roughly 35% to a 4KB chunk, so
many-pane cost appeared to be dominated by the per-pane buffer. Removing the buffer copy leaves
fan-out as the whole of it: `publish` still hands every subscriber its own copy of every chunk under
the session mutex.

| Subscribers (4KB chunk, full buffer) | before, ns/op (median) | after, ns/op (median) |
|---|---|---|
| 0 | 222,527 | 117 |
| 1 | 216,629 | 1,806 |
| 4 | 240,951 | 7,702 |
| 16 | 275,441 | 31,508 |

The after column is close to linear in subscriber count, at ~2µs per subscriber per 4KB chunk. That
is the next thing to look at if terminal throughput becomes a complaint, and it is deliberately not
acted on here — unlike the buffer, nothing yet says the cost is worth the change, and one subscriber
per visible pane is the ordinary case.

`Subscribe` (what a workspace switch pays per remounted pane) still copies the whole buffer, because
the snapshot outlives the call: ~0.8µs empty, ~95µs at the full 256KB, against ~0.8µs and ~90µs
before — unchanged within the noise. Both figures include the matching unsubscribe, for the reason
`BenchmarkSessionSubscribe`'s own comment records.

**What stops the buffer copy coming back is a test, not these numbers.**
`TestManagedSession_Publish_SteadyStateDoesNotCopyTheReplayWindow` asserts that a steady-state
`publish` with no subscribers allocates zero times, and
`TestReplayBuffer_SteadyStateAppendDoesNotAllocate` does the same one layer down. That is a shape,
not a duration: allocation counts have none of the 2.9× spread this section opens by warning about,
so they can be asserted in a hermetic unit test where a timing could not. It is the pattern to
prefer whenever a performance finding turns out to be about work done rather than time taken.

### Relay polling cost

Medians of five runs; these are far steadier than the `publish` rows above (≤1.1× spread).

| Operation | ns/op (median) | range |
|---|---|---|
| `AppendMessage`, at the 2000-row limit | 288 | 276 – 303 |
| `MessagesSince`, caught-up cursor | 9,171 | 9,102 – 9,258 |
| `MessagesSince`, cold start | 602,421 | 567,966 – 625,917 |
| `StatusSnapshot`, 64 panes | 9,873 | 9,575 – 10,654 |

`MessagesSince` scans the whole history even when the caller is caught up and gets nothing back,
which is the shape to watch if the history limit is ever raised.

There is no empty-history `AppendMessage` row, and an earlier revision's claim of a ~270 vs ~730
contrast between empty and full was noise read as signal. A benchmark cannot hold the cache empty:
`b.N` is millions, so the first 2000 iterations fill it and the rest measure the full case. Nor is
there anything to find — the trim is a pointer bump, and append's amortized regrow is paid alike
either way. `BenchmarkBoardCacheAppendMessage`'s comment records both halves.

### Accessibility

Unlike the performance figures above, these are **not** indicative: they are the ceiling
`frontend/e2e/a11y.spec.ts` now enforces (through the comparator and map in
`frontend/e2e/a11y-ceiling.ts`), and they reproduced exactly on a second machine before
being frozen. They are measured against the spec's own fixture and panemux process
(`frontend/e2e/a11y.yml`, port 4178), not the shared E2E server — D11 records why that separation is
load-bearing rather than tidiness. Excluding `.xterm` (xterm.js owns that canvas and its markup):

| Page state | Rule | Impact | Ceiling (nodes) |
|---|---|---|---|
| Dashboard | `aria-required-children` | critical | 1 |
| Dashboard | `color-contrast` | serious | 2 |
| Dashboard | `region` | moderate | 7 |
| Pane settings dialog open | `aria-required-children` | critical | 1 |
| Pane settings dialog open | `select-name` | critical | 1 |
| Pane settings dialog open | `color-contrast` | serious | 10 |
| Pane settings dialog open | `region` | moderate | 7 |

**Every row is a violation this repository still has.** Listing it here is not approval of it; it is
the refusal to ship a gate that starts red, which is what turning axe on over an existing UI would
otherwise mean (principle 4). A count may fall and must not rise, and a rule absent from the table
has a ceiling of zero, so a new *kind* of violation fails the run the day it appears. D11 records
why the ceiling is per rule and counts nodes rather than taking either of the looser forms.

That the gate bites was confirmed by perturbation, not assumed: an `<img>` with no `alt` and a
low-contrast `<span>` added to `TerminalPane` failed both scans, on both branches — `image-alt` as a
new rule at a ceiling of zero, `region` as an existing rule risen from 7 nodes to 9.

That it does **not** bite spuriously was confirmed the harder way, by it doing so: the first version
shared the default E2E server and passed alone while failing inside `make test-e2e`. See D11.

**Lowering a ceiling after fixing something.** The run does the arithmetic; the edit is manual and
takes two files:

1. Run `make test-e2e`. A count that came in under its ceiling prints as
   `a11y[<page state>] ceiling can be lowered` with the exact replacement — `set
   CEILINGS['pane-settings']['color-contrast'] to 9 (was 10)`, or `remove 'select-name' from
   CEILINGS['pane-settings']` when a rule is gone entirely. The same text is in the run's
   `axe-<page state>.txt` attachment.
2. Apply it to `CEILINGS` in `frontend/e2e/a11y-ceiling.ts` and to the table above, in the same commit
   as the fix.

The run passes either way. Nothing forces step 2 — deliberately, per D11 — so a fix that skips it
silently leaves room for the next regression.
