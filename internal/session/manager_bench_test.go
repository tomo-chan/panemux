package session

import (
	"fmt"
	"io"
	"testing"
)

// Performance efficiency is one of the two ISO 25010 characteristics
// docs/quality-gateway.md records as completely unprotected. These benchmarks
// are the first measurement of it, and they are deliberately only that:
// roadmap item 7 of issue #180 says measure now and decide thresholds once
// real data has accumulated. Nothing here fails on a number.
//
// What they measure is the path every byte a pane produces travels:
// managedSession.publish, which appends to the replay buffer, trims it, and
// fans the chunk out to every connected subscriber while holding one mutex.
// The three variables that matter in practice are chunk size (how chatty the
// program in the pane is), subscriber count (how many browser tabs or panes
// are watching), and whether the replay buffer is already full — because the
// trim reallocates.

// benchSession is a stub that satisfies the Session interface so a
// managedSession can be built without a PTY — `make check` stays hermetic
// (design principle 5). Its I/O methods are never called: every benchmark
// below drives publish or subscribe directly rather than through the pump.
//
// Read returns io.EOF on the first call, so this is NOT a stand-in for a
// never-ending PTY stream. A benchmark that went through Manager.Add — the
// obvious next one to write — would take the io.EOF branch immediately, call
// closeSubscribers, and report a near-zero cost rather than failing. Give
// chunk a value and return a nil error before using it that way.
type benchSession struct {
	id    string
	chunk []byte
}

func (b *benchSession) ID() string    { return b.id }
func (b *benchSession) Type() Type    { return TypeLocal }
func (b *benchSession) Title() string { return b.id }
func (b *benchSession) State() State  { return StateConnected }
func (b *benchSession) Read(p []byte) (int, error) {
	return copy(p, b.chunk), io.EOF
}
func (b *benchSession) Write(p []byte) (int, error) { return len(p), nil }
func (b *benchSession) Resize(_, _ uint16) error    { return nil }
func (b *benchSession) Close() error                { return nil }

// newBenchEntry builds a managedSession directly rather than through
// Manager.Add, so the benchmark drives publish without a pump goroutine
// racing it.
func newBenchEntry() *managedSession {
	return &managedSession{
		session:     &benchSession{id: "bench"},
		subscribers: make(map[int]chan []byte),
	}
}

// BenchmarkSessionPublish measures the per-chunk cost of terminal output
// reaching subscribers, across the shapes that actually occur: a quiet shell
// (256B), an ordinary command's output (4KB — the pump's own read size), and
// a program flooding the terminal (64KB).
//
// The replay buffer starts FULL, deliberately. A pane that has been open for
// more than a few seconds is in that state permanently, and it is the only
// state that gives a stable number: with an empty buffer the result depends on
// how far through b.N the buffer happened to fill, so the same code measures
// differently from run to run. BenchmarkSessionPublishColdBuffer below is the
// other half of the comparison.
func BenchmarkSessionPublish(b *testing.B) {
	for _, size := range []int{256, 4096, 65536} {
		for _, subscribers := range []int{0, 1, 4, 16} {
			name := fmt.Sprintf("chunk=%dB/subscribers=%d", size, subscribers)
			b.Run(name, func(b *testing.B) {
				entry := newBenchEntry()
				entry.history = make([]byte, sessionReplayLimitBytes)
				for i := 0; i < subscribers; i++ {
					_, stream, _ := entry.subscribe()
					go func() {
						for range stream { //nolint:revive // draining is the point
						}
					}()
				}

				chunk := make([]byte, size)
				b.SetBytes(int64(size))
				b.ReportAllocs()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					entry.publish(chunk)
				}
				b.StopTimer()
				entry.closeSubscribers()
			})
		}
	}
}

// BenchmarkSessionPublishColdBuffer is the same path before the replay buffer
// has filled, so the trim in publish never runs. The gap between this and the
// 4KB/0-subscriber case above is what retaining the replay window costs per
// chunk — the first thing to look at if terminal throughput ever becomes a
// complaint.
//
// It resets the buffer every iteration so it stays cold, which is why it is a
// separate benchmark rather than another row in the table above.
//
// The reset is inside the timed region on purpose. b.StopTimer/b.StartTimer
// each call runtime.ReadMemStats, which is stop-the-world: doing that per
// iteration cost ~95µs of pause against a ~2µs operation, so the benchmark
// spent 50s of wall clock on 1s of measured work — and, worse, the repeated
// pauses reset the GC and allocator state this benchmark exists to measure,
// reporting ~5,400 ns/op for a path that actually costs ~1,900. `entry.history
// = nil` is a single pointer store, a constant well under 1% of the operation,
// so timing it is far cheaper than excluding it.
func BenchmarkSessionPublishColdBuffer(b *testing.B) {
	entry := newBenchEntry()
	chunk := make([]byte, 4096)

	b.SetBytes(int64(len(chunk)))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		entry.history = nil
		entry.publish(chunk)
	}
}

// BenchmarkSessionSubscribe is what a workspace switch costs. Every pane the
// browser remounts calls Subscribe, which copies the whole replay buffer so
// the terminal can redraw — so this is per-pane, paid all at once, and it is
// the "many-pane rendering" cost measured on the backend side.
//
// The figure covers subscribe AND its unsubscribe, and the doc rows say so.
// Excluding the unsubscribe would need b.StopTimer per iteration, whose
// stop-the-world ReadMemStats costs ~95µs against a ~1.5µs operation (see
// BenchmarkSessionPublishColdBuffer above). The alternative — keeping every
// subscription alive and draining after the loop — is worse: each one holds a
// 64-slot channel and a copy of the replay buffer, so at b.N in the hundreds of
// thousands the buffered=256KB row would need tens of gigabytes. Unsubscribe is
// a map delete and a channel close, far smaller than the buffer copy it is
// measured beside, and a remounting pane really does pay both.
func BenchmarkSessionSubscribe(b *testing.B) {
	for _, filled := range []int{0, sessionReplayLimitBytes / 2, sessionReplayLimitBytes} {
		b.Run(fmt.Sprintf("buffered=%dB", filled), func(b *testing.B) {
			entry := newBenchEntry()
			entry.history = make([]byte, filled)

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _, unsubscribe := entry.subscribe()
				unsubscribe()
			}
		})
	}
}
