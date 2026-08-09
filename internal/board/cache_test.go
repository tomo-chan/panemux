package board

import (
	"testing"
	"time"
)

func TestBoardCache_RecordStatus_NewestWinsPerPane(t *testing.T) {
	c := NewBoardCache()
	var now time.Time
	c.nowFn = func() time.Time { return now }

	now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c.RecordStatus("pane-a", Status{State: "working"})

	now = time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)
	c.RecordStatus("pane-a", Status{State: "idle"})

	now = time.Date(2026, 1, 1, 0, 1, 0, 0, time.UTC)
	c.RecordStatus("pane-b", Status{State: "waiting_approval"})

	snap := c.StatusSnapshot()
	if len(snap) != 2 {
		t.Fatalf("expected 2 panes in snapshot, got %d", len(snap))
	}
	if got := snap["pane-a"]; got.State != "idle" || !got.UpdatedAt.Equal(time.Date(2026, 1, 1, 0, 5, 0, 0, time.UTC)) {
		t.Fatalf("pane-a: got %+v, want newest write to win", got)
	}
	if got := snap["pane-b"]; got.State != "waiting_approval" {
		t.Fatalf("pane-b: got %+v", got)
	}
}

func TestBoardCache_AppendMessage_SeqOrdering(t *testing.T) {
	c := NewBoardCache()

	seq1 := c.AppendMessage(Row{Host: "local", From: "pane-a", To: "pane-b", Body: "hi"})
	seq2 := c.AppendMessage(Row{Host: "hostA", ID: "1", From: "pane-c", To: "pane-b", Body: "hi again"})
	// Colliding/incomparable agmsg-native IDs across hosts must still get a
	// stable, panemux-local total order.
	seq3 := c.AppendMessage(Row{Host: "hostB", ID: "1", From: "pane-d", To: "pane-b", Body: "collision"})

	if seq1 != 1 || seq2 != 2 || seq3 != 3 {
		t.Fatalf("expected monotonic seq 1,2,3; got %d,%d,%d", seq1, seq2, seq3)
	}
	if got := c.LatestSeq(); got != 3 {
		t.Fatalf("LatestSeq() = %d, want 3", got)
	}
}

func TestBoardCache_MessagesSince(t *testing.T) {
	c := NewBoardCache()
	c.AppendMessage(Row{From: "a", Body: "one"})
	c.AppendMessage(Row{From: "a", Body: "two"})
	c.AppendMessage(Row{From: "a", Body: "three"})

	msgs := c.MessagesSince(1)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after seq 1, got %d", len(msgs))
	}
	if msgs[0].Row.Body != "two" || msgs[1].Row.Body != "three" {
		t.Fatalf("unexpected order: %+v", msgs)
	}

	if msgs := c.MessagesSince(0); len(msgs) != 3 {
		t.Fatalf("expected 3 messages from 0, got %d", len(msgs))
	}

	if msgs := c.MessagesSince(999); len(msgs) != 0 {
		t.Fatalf("expected 0 messages beyond latest seq, got %d", len(msgs))
	}
}

func TestBoardCache_EmptyReadsAreWellDefined(t *testing.T) {
	c := NewBoardCache()
	if snap := c.StatusSnapshot(); len(snap) != 0 {
		t.Fatalf("expected empty status snapshot, got %+v", snap)
	}
	if msgs := c.MessagesSince(0); len(msgs) != 0 {
		t.Fatalf("expected empty history, got %+v", msgs)
	}
	if seq := c.LatestSeq(); seq != 0 {
		t.Fatalf("expected LatestSeq() == 0 on fresh cache, got %d", seq)
	}
}

func TestBoardCache_HistoryBounded(t *testing.T) {
	c := NewBoardCacheWithLimit(3)
	for i := 0; i < 5; i++ {
		c.AppendMessage(Row{Body: "msg"})
	}
	msgs := c.MessagesSince(0)
	if len(msgs) != 3 {
		t.Fatalf("expected history capped at 3, got %d", len(msgs))
	}
	// The oldest two (seq 1, 2) must have been dropped, keeping 3,4,5.
	if msgs[0].Seq != 3 {
		t.Fatalf("expected oldest surviving seq to be 3, got %d", msgs[0].Seq)
	}
}

// A status row addressed to _agent-board must not itself decide anything about
// history membership — that's the relay's job (see relay_test.go). Here we
// only assert BoardCache's two write paths are independent: recording a
// status does not implicitly append to history, and appending to history
// does not implicitly record a status.
func TestBoardCache_StatusAndHistoryAreIndependentWrites(t *testing.T) {
	c := NewBoardCache()
	c.RecordStatus("pane-a", Status{State: "working"})
	if msgs := c.MessagesSince(0); len(msgs) != 0 {
		t.Fatalf("RecordStatus must not append to history, got %+v", msgs)
	}

	c.AppendMessage(Row{From: "pane-a", To: "pane-b", Body: "hi"})
	if snap := c.StatusSnapshot(); len(snap) != 1 {
		t.Fatalf("AppendMessage must not touch status map beyond existing entries, got %+v", snap)
	}
}
