package board

import (
	"fmt"
	"testing"
	"time"
)

// The relay's polling cost is the second thing docs/quality-gateway.md lists
// as unprotected under performance efficiency, and unlike terminal throughput
// it is paid continuously: the relay polls every host every 5 seconds for as
// long as panemux runs, and the dashboard polls GET /api/board/messages while
// its panel is open. These benchmarks measure that steady cost.
//
// Roadmap item 7 of issue #180 is explicit that this is measurement only. No
// threshold is asserted here; the numbers exist so a threshold can be chosen
// later from real data rather than guessed at now.

func benchRow(i int) Row {
	return Row{
		At:   time.Unix(int64(1_700_000_000+i), 0),
		ID:   fmt.Sprintf("0199a1f0-%04x-7000-8000-000000000000", i%0xffff),
		Host: "local",
		Team: "demo",
		From: fmt.Sprintf("pane-%d", i%8),
		To:   "pane-0",
		Body: "the relay carries free text, so a realistic body is a sentence rather than a token",
	}
}

func filledCache(rows int) *BoardCache {
	c := NewBoardCache()
	for i := 0; i < rows; i++ {
		c.AppendMessage(benchRow(i))
	}
	return c
}

// BenchmarkBoardCacheAppendMessage is what one polled message costs the relay,
// measured at the history limit — the state a long-running panemux is in
// permanently.
//
// There is deliberately no empty-history row. It cannot measure an empty
// history: b.N is millions at the default benchtime, so the first 2000
// iterations fill the cache and every one after that measures exactly what
// this benchmark measures. That is the same b.N-dependent confound
// BenchmarkSessionPublish avoids by starting its replay buffer full, and it
// showed: with -benchtime 100000x the "empty" row came out SLOWER than the
// full one, and at the default benchtime the ordering flipped.
//
// Nor is there anything for it to find. The trim is c.history = c.history[n:],
// a pointer bump; the only real cost is append's amortized regrow, which an
// empty cache and a full one pay alike. An earlier revision of
// docs/quality-gateway.md claimed a ~270 vs ~730 ns/op contrast between them.
// That was noise being read as signal.
func BenchmarkBoardCacheAppendMessage(b *testing.B) {
	c := filledCache(defaultBoardCacheHistoryLimit)
	row := benchRow(1)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.AppendMessage(row)
	}
}

// BenchmarkBoardCacheMessagesSince is the dashboard's own poll. The caller
// almost always holds a recent cursor and gets nothing back, but the scan is
// linear over the whole history either way — which is the property worth
// watching as the history limit is tuned.
func BenchmarkBoardCacheMessagesSince(b *testing.B) {
	c := filledCache(defaultBoardCacheHistoryLimit)
	newest := c.MessagesSince(0)
	latest := newest[len(newest)-1].Seq

	cursors := []struct {
		name  string
		after int64
	}{
		{"caught-up", latest},
		{"behind-by-10", latest - 10},
		{"cold-start", 0},
	}
	for _, tc := range cursors {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = c.MessagesSince(tc.after)
			}
		})
	}
}

// BenchmarkBoardCacheStatusSnapshot is what GET /api/board/status costs per
// poll: the map is copied under a read lock so the handler never renders a
// half-updated view. Pane counts here span a normal session and an unusually
// large one.
func BenchmarkBoardCacheStatusSnapshot(b *testing.B) {
	for _, panes := range []int{4, 16, 64} {
		b.Run(fmt.Sprintf("panes=%d", panes), func(b *testing.B) {
			c := NewBoardCache()
			for i := 0; i < panes; i++ {
				c.RecordStatus(fmt.Sprintf("pane-%d", i), Status{State: "working", Summary: "doing the thing"})
			}

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = c.StatusSnapshot()
			}
		})
	}
}
