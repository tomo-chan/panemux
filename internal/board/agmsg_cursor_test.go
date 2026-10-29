package board

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// Real UUIDv7 ids captured from message transport 1.2.0. All four were written in the
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
	hostA := &fakeMessageClient{hostID: "host-a", sinceRows: []Row{
		uuidRelayRow(uuidIDs[0], "one"),
		uuidRelayRow(uuidIDs[1], "two"),
	}}
	hostB := &fakeMessageClient{hostID: "host-b"}

	cache := NewBoardCache()
	r := newTestRelay(cache,
		map[string]MessageClient{"host-a": hostA, "host-b": hostB},
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
// replay of that window, not a permanent loop. This is the cursor tradeoff
// documented in docs/agent-board/relay.md; failed forwards are not retried.
//
//efficacy:exempt documentation-only terminology update; implementation behavior is unchanged
func TestRelayPollCursorOutsideWindowSelfCorrects(t *testing.T) {
	hostA := &fakeMessageClient{hostID: "host-a", sinceRows: []Row{
		uuidRelayRow(uuidIDs[2], "one"),
		uuidRelayRow(uuidIDs[3], "two"),
	}}
	cache := NewBoardCache()
	r := newTestRelay(cache,
		map[string]MessageClient{"host-a": hostA},
		map[string]string{"pane-a": "host-a", "pane-b": "host-a"})
	// A cursor from before a burst larger than one poll's --limit: message transport
	// can no longer show panemux where it left off.
	r.LoadCursors([]CursorEntry{{Host: "host-a", Team: "panemux", Cursor: "missing-cursor"}})

	require.NoError(t, r.Poll(context.Background()))
	require.Len(t, cache.MessagesSince(0), 2, "an unresolvable cursor re-delivers the window rather than skipping it")

	require.NoError(t, r.Poll(context.Background()))
	assert.Len(t, cache.MessagesSince(0), 2, "and the next poll is quiet: the cursor now anchors inside the window")
}
