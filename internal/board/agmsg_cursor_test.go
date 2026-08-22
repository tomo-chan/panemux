package board

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the one property agmsg's own driver-interface spec states
// about a message id: it is OPAQUE. api.sh's `messages` verb documents the
// id as TEXT and orders rows by each source's native counter (`events.seq` /
// `messages.id`), a value it explicitly never compares across sources — so
// the returned order is the only ordering signal a consumer may rely on.
//
// panemux originally compared ids numerically. That was correct only for
// agmsg's legacy integer rowids; the event-log driver emits UUIDv7, which
// parses as no integer at all, so every cursor comparison silently answered
// "nothing parses, everything is new" and the relay re-delivered its whole
// poll window forever. The Tier 2 contract tests catch that against a real
// install; these are its hermetic Tier 1 counterpart.

func rowsWithIDs(ids ...string) []Row {
	rows := make([]Row, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, Row{ID: id, Host: "local"})
	}
	return rows
}

// uuidRelayRow is one pane-a → pane-b row, the shape the relay tests drive
// across hosts.
func uuidRelayRow(id, body string) Row {
	return Row{ID: id, Host: "host-a", Team: "panemux", From: "pane-a", To: "pane-b", Body: body}
}

func idsOf(rows []Row) []string {
	ids := make([]string, 0, len(rows))
	for _, r := range rows {
		ids = append(ids, r.ID)
	}
	return ids
}

// Real UUIDv7 ids captured from agmsg 1.2.0. All four were written in the
// same millisecond, so they share the `01a02760-c340` prefix and differ only
// in random bits: sorting them as strings gives a DIFFERENT order than the
// one api.sh returned. Any comparison-based cursor is therefore wrong, not
// merely fragile.
var uuidIDs = []string{
	"01a02760-c340-7ec7-8f18-071cce739579",
	"01a02760-c340-7ae0-81eb-31430e02886a",
	"01a02760-c340-711a-8382-1825710d6878",
	"01a02760-c340-72ff-839b-0498652568bb",
}

type filterRowsAfterCase struct {
	name    string
	afterID string
	rows    []Row
	want    []string
}

func filterRowsAfterCases() []filterRowsAfterCase {
	uuidRows := rowsWithIDs(uuidIDs...)

	return []filterRowsAfterCase{
		{
			name:    "empty cursor means every row is new",
			rows:    uuidRows,
			afterID: "",
			want:    uuidIDs,
		},
		{
			// The regression: this cursor is lexicographically the LARGEST
			// of the four, so a string comparison would report nothing new,
			// while api.sh returned two rows after it.
			name:    "cursor anchors on position, not on how the id sorts",
			rows:    uuidRows,
			afterID: uuidIDs[1],
			want:    uuidIDs[2:],
		},
		{
			name:    "cursor on the newest row means nothing new",
			rows:    uuidRows,
			afterID: uuidIDs[3],
			want:    nil,
		},
		{
			// The cursor scrolled out of the poll window (more than --limit
			// new rows, or the store was reset). agmsg has no forward "since"
			// primitive to resolve this with, and delivery is documented as
			// at-least-once, so the window is re-delivered rather than
			// silently skipped. It self-corrects on the next poll.
			name:    "cursor absent from the window re-delivers it",
			rows:    uuidRows,
			afterID: unknownCursorID,
			want:    uuidIDs,
		},
		{
			name:    "empty row set is well defined",
			rows:    nil,
			afterID: uuidIDs[3],
			want:    nil,
		},
		{
			// agmsg's legacy sqlite driver exposes integer rowids as decimal
			// strings. Positional anchoring keeps working for them, including
			// the case a string comparison gets wrong ("9" > "10").
			name:    "legacy integer ids still work",
			rows:    rowsWithIDs("1", "2", "3", "4"),
			afterID: "2",
			want:    []string{"3", "4"},
		},
		{
			name:    "legacy integer ids past the single-digit boundary",
			rows:    rowsWithIDs("8", "9", "10", "11"),
			afterID: "9",
			want:    []string{"10", "11"},
		},
		{
			// ids are host-scoped and agmsg promises no global uniqueness.
			// Anchoring on the LAST occurrence never re-delivers rows the
			// previous poll already handled.
			name:    "a repeated id anchors on its newest occurrence",
			rows:    rowsWithIDs("1", "2", "1", "3"),
			afterID: "1",
			want:    []string{"3"},
		},
	}
}

// unknownCursorID is a well-formed id that appears in no response — the
// shape a cursor takes once it has scrolled out of the poll window.
const unknownCursorID = "01a02760-0000-7000-8000-000000000000"

func TestFilterRowsAfter_OpaqueIDs(t *testing.T) {
	for _, tt := range filterRowsAfterCases() {
		t.Run(tt.name, func(t *testing.T) {
			got := filterRowsAfter(tt.rows, tt.afterID)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, idsOf(got))
		})
	}
}

func TestLastRowID_OpaqueIDs(t *testing.T) {
	tests := []struct {
		name string
		prev string
		want string
		rows []Row
	}{
		{
			name: "empty poll keeps the previous cursor",
			rows: nil,
			prev: uuidIDs[3],
			want: uuidIDs[3],
		},
		{
			// The newest row is the LAST one api.sh returned, regardless of
			// how its id compares — here the last id is lexicographically the
			// smallest of the three.
			name: "newest row is the last one returned",
			rows: rowsWithIDs(uuidIDs[0], uuidIDs[1], uuidIDs[2]),
			prev: "",
			want: uuidIDs[2],
		},
		{
			name: "legacy integer ids advance to the last row too",
			rows: rowsWithIDs("8", "9", "10"),
			prev: "7",
			want: "10",
		},
		{
			name: "first poll from an empty cursor advances",
			rows: rowsWithIDs("1"),
			prev: "",
			want: "1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, lastRowID(tt.rows, tt.prev))
		})
	}
}

// TestRelayPollAdvancesCursorAcrossUUIDRows is the end-to-end shape of the
// bug: with UUID ids, a second poll that brings no new rows must relay
// nothing. Before the fix it re-relayed the entire window on every tick,
// forever, because no UUID parsed as the integer the cursor compared.
func TestRelayPollAdvancesCursorAcrossUUIDRows(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		uuidRelayRow(uuidIDs[0], "one"),
		uuidRelayRow(uuidIDs[1], "two"),
	}}
	hostB := &fakeAgmsgClient{hostID: "host-b"}

	cache := NewBoardCache()
	r := newTestRelay(cache,
		map[string]AgmsgClient{"host-a": hostA, "host-b": hostB},
		map[string]string{"pane-a": "host-a", "pane-b": "host-b"})

	require.NoError(t, r.Poll(context.Background()))
	require.Len(t, hostB.sendCalls, 2, "first poll relays both rows to the destination host")
	require.Len(t, cache.MessagesSince(0), 2)

	require.NoError(t, r.Poll(context.Background()))
	assert.Len(t, hostB.sendCalls, 2, "second poll must relay nothing: the cursor advanced past both rows")
	assert.Len(t, cache.MessagesSince(0), 2, "and must not append the same rows to history again")
}

// TestRelayPollCursorOutsideWindowSelfCorrects pins the bounded shape of the
// truncation case: a cursor that scrolled out of the poll window costs one
// re-delivery of that window, not a permanent loop. This is the tradeoff
// docs/agent-board.md documents as at-least-once delivery.
func TestRelayPollCursorOutsideWindowSelfCorrects(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		uuidRelayRow(uuidIDs[2], "one"),
		uuidRelayRow(uuidIDs[3], "two"),
	}}
	cache := NewBoardCache()
	r := newTestRelay(cache,
		map[string]AgmsgClient{"host-a": hostA},
		map[string]string{"pane-a": "host-a", "pane-b": "host-a"})
	// A cursor from before a burst larger than one poll's --limit: agmsg
	// can no longer show panemux where it left off.
	r.LoadCursors([]CursorEntry{{Host: "host-a", Team: "panemux", Cursor: unknownCursorID}})

	require.NoError(t, r.Poll(context.Background()))
	require.Len(t, cache.MessagesSince(0), 2, "an unresolvable cursor re-delivers the window rather than skipping it")

	require.NoError(t, r.Poll(context.Background()))
	assert.Len(t, cache.MessagesSince(0), 2, "and the next poll is quiet: the cursor now anchors inside the window")
}
