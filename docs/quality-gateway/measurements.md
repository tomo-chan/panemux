---
type: Reference
title: "Quality gateway: first measurements"
description: "Dated quality measurements, performance evidence, and routes to accessibility ceilings."
---

# Quality gateway: first measurements

> Part of the [quality gateway](../quality-gateway.md). That document defines the gates this one explains.

## First measurements

`make bench` reports observations without a performance gate.

**These timings are indicative, not a regression baseline.** They are medians and observed min–max
from five shared-container runs of `make bench BENCH_ARGS='-count 5'`. Identical `publish` binaries
vary up to **2.9×** on the same machine. Different rerun figures do not establish regression;
a timing threshold needs a distribution on dedicated hardware to avoid principle 4's noise gate.

Originals used a 4-core Intel Xeon @ 2.80GHz; the [#193](https://github.com/tomo-chan/panemux/issues/193)
pair used another container (4-core Intel Xeon @ 2.10GHz). **Only compare figures measured together.**
The throughput before/after pairs ran back to back on the same container; relay figures remain
from the original session and have not been retaken.

Accessibility counts are enforced separately; see [Accessibility](#accessibility).

### Terminal output throughput

`managedSession.publish` carries every pane-output byte. [#193](https://github.com/tomo-chan/panemux/issues/193)
replaced the slice replay buffer, which copied a 256KB window per 4KB chunk at steady state,
with a ring (`internal/session/replay_buffer.go`). It writes into existing storage and advances
two indices; steady-state work scales with chunk length and allocates nothing.

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

The spread supports only approximate speedup comparisons; do not quote precise multiples.

Before the ring, 16 subscribers added roughly 35% per 4KB chunk, so buffer copies dominated.
Afterwards, fan-out dominates: `publish` copies every chunk for every subscriber under the
session mutex.

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

There is no empty-history `AppendMessage` row: `b.N` fills the 2000-row cache, so the benchmark
measures the full case. Trimming bumps a pointer and append pays amortized regrowth in either
case. See `BenchmarkBoardCacheAppendMessage`'s comment.

### Accessibility

See [Accessibility ceilings](accessibility.md) for the enforced counts and update procedure.
