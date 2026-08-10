package board

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBoardCache_StatusSnapshot_EmptyOnFreshCache(t *testing.T) {
	c := NewBoardCache()
	assert.Empty(t, c.StatusSnapshot())
}

func TestBoardCache_RecordStatus_NewestWinsPerPane(t *testing.T) {
	tick := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := NewBoardCache()
	c.now = func() time.Time { return tick }

	c.RecordStatus("pane-a", Status{State: "working"})
	tick = tick.Add(time.Minute)
	c.RecordStatus("pane-a", Status{State: "idle"})

	snap := c.StatusSnapshot()
	require.Contains(t, snap, "pane-a")
	assert.Equal(t, "idle", snap["pane-a"].State)
	assert.Equal(t, tick, snap["pane-a"].UpdatedAt)
}

func TestBoardCache_RecordStatus_AcrossMultiplePanes(t *testing.T) {
	c := NewBoardCache()
	c.RecordStatus("pane-a", Status{State: "working"})
	c.RecordStatus("pane-b", Status{State: "idle"})

	snap := c.StatusSnapshot()
	require.Len(t, snap, 2)
	assert.Equal(t, "working", snap["pane-a"].State)
	assert.Equal(t, "idle", snap["pane-b"].State)
}

func TestBoardCache_RecordStatus_SetsUpdatedAt(t *testing.T) {
	fixed := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)
	c := NewBoardCache()
	c.now = func() time.Time { return fixed }

	// UpdatedAt in the input Status must be overwritten by the cache's own
	// clock, not trusted from the caller.
	c.RecordStatus("pane-a", Status{State: "working", UpdatedAt: time.Time{}})

	assert.Equal(t, fixed, c.StatusSnapshot()["pane-a"].UpdatedAt)
}

func TestBoardCache_AppendMessage_AssignsIncreasingSeq(t *testing.T) {
	c := NewBoardCache()
	c.AppendMessage(Row{ID: "1", Host: "host-a", Body: "first"})
	c.AppendMessage(Row{ID: "1", Host: "host-b", Body: "second"}) // colliding agmsg ID, different host

	rows := c.MessagesSince(0)
	require.Len(t, rows, 2)
	assert.Equal(t, "first", rows[0].Row.Body)
	assert.Equal(t, "second", rows[1].Row.Body)
	assert.Equal(t, int64(1), rows[0].Seq)
	assert.Equal(t, int64(2), rows[1].Seq)
}

func TestBoardCache_MessagesSince_FiltersByAfterSeq(t *testing.T) {
	c := NewBoardCache()
	c.AppendMessage(Row{Body: "one"})
	c.AppendMessage(Row{Body: "two"})
	c.AppendMessage(Row{Body: "three"})

	rows := c.MessagesSince(1)
	require.Len(t, rows, 2)
	assert.Equal(t, "two", rows[0].Row.Body)
	assert.Equal(t, "three", rows[1].Row.Body)
}

func TestBoardCache_MessagesSince_EmptyHistory_ReturnsNilNotError(t *testing.T) {
	c := NewBoardCache()
	assert.Nil(t, c.MessagesSince(0))
}

func TestBoardCache_MessagesSince_AllConsumed_ReturnsNil(t *testing.T) {
	c := NewBoardCache()
	c.AppendMessage(Row{Body: "one"})
	assert.Nil(t, c.MessagesSince(1))
}

func TestBoardCache_AppendMessage_BoundedHistory_DropsOldest(t *testing.T) {
	c := NewBoardCache()
	c.maxHistory = 3

	for i := 0; i < 5; i++ {
		c.AppendMessage(Row{Body: string(rune('a' + i))})
	}

	rows := c.MessagesSince(0)
	require.Len(t, rows, 3)
	assert.Equal(t, "c", rows[0].Row.Body)
	assert.Equal(t, "d", rows[1].Row.Body)
	assert.Equal(t, "e", rows[2].Row.Body)
}

func TestBoardCache_AppendMessage_BoundedHistory_SeqKeepsIncreasingPastBound(t *testing.T) {
	c := NewBoardCache()
	c.maxHistory = 2

	c.AppendMessage(Row{Body: "a"})
	c.AppendMessage(Row{Body: "b"})
	c.AppendMessage(Row{Body: "c"})

	// Seq 1 ("a") was dropped by the bound; MessagesSince(0) must return
	// only what survived (b, c), not error or resurrect the dropped row.
	rows := c.MessagesSince(0)
	require.Len(t, rows, 2)
	assert.Equal(t, "b", rows[0].Row.Body)
	assert.Equal(t, "c", rows[1].Row.Body)
	assert.Equal(t, int64(2), rows[0].Seq, "Seq must reflect the original assignment, not be renumbered after truncation")
	assert.Equal(t, int64(3), rows[1].Seq)
}

func TestBoardCache_ConcurrentReadWrite_NoRace(t *testing.T) {
	c := NewBoardCache()
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			c.AppendMessage(Row{Body: "x"})
			c.RecordStatus("pane", Status{State: "working"})
		}(i)
		go func() {
			defer wg.Done()
			c.StatusSnapshot()
			c.MessagesSince(0)
		}()
	}
	wg.Wait()
}
