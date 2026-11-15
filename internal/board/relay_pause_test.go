package board

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

type pausingClient struct {
	*fakeMessageClient
	paused bool
	leases int
}

func (c *pausingClient) LeaseMessages() (MessageClient, func(), error) {
	if c.paused {
		return nil, nil, errors.New("tool is updating")
	}
	c.leases++
	return c.fakeMessageClient, func() { c.leases-- }, nil
}

func TestRelayPausedDestinationPreservesSourceCursor(t *testing.T) {
	source := &pausingClient{fakeMessageClient: &fakeMessageClient{hostID: "a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "a-pane", To: "b-pane", Body: "hello"},
	}}}
	dest := &pausingClient{fakeMessageClient: &fakeMessageClient{hostID: "b"}, paused: true}
	r := newTestRelay(NewBoardCache(), map[string]MessageClient{"a": source, "b": dest},
		map[string]string{"a-pane": "a", "b-pane": "b"})
	require.ErrorContains(t, r.Poll(context.Background()), "updating")
	require.Empty(t, r.Cursors())
	require.Empty(t, dest.sendCalls)
	require.Zero(t, source.leases)
	dest.paused = false
	require.NoError(t, r.Poll(context.Background()))
	require.Len(t, dest.sendCalls, 1)
	require.Equal(t, "1", r.Cursors()[0].Cursor)
}

func TestBroadcastWhilePausedDoesNotSend(t *testing.T) {
	c := &pausingClient{fakeMessageClient: &fakeMessageClient{hostID: "a"}, paused: true}
	r := newTestRelay(NewBoardCache(), map[string]MessageClient{"a": c}, map[string]string{"pane": "a"})
	_, err := r.Broadcast(context.Background(), SystemID, []string{"pane"}, "hello")
	require.ErrorContains(t, err, "updating")
	require.Empty(t, c.sendCalls)
}

// Invalid rows must not reserve their apparent destination or block valid siblings.
func TestRelayRejectedRowsDoNotLeasePausedDestination(t *testing.T) {
	for _, row := range []Row{
		{Team: "panemux", From: "worker@ssh:build", To: "worker@ssh:build"},
		{Team: "panemux", From: "$(id)", To: "worker@ssh:build"},
		{Team: "panemux", From: "pane-b", To: "worker@ssh:build"},
		{Team: "other", From: "worker", To: "worker@ssh:build"},
		{Team: "panemux", From: "worker", To: "_system@ssh:build"},
	} {
		t.Run(row.Team+"/"+row.From+"/"+row.To, func(t *testing.T) {
			row.ID = "1"
			source := &fakeMessageClient{hostID: "local", sinceRows: []Row{row,
				{ID: "2", Team: "panemux", From: "orch", To: SystemID, Body: `{"kind":"board_status","state":"working"}`},
			}}
			dest := &pausingClient{fakeMessageClient: &fakeMessageClient{hostID: "ssh:build"}, paused: true}
			cache := NewBoardCache()
			r := newTestRelay(cache, map[string]MessageClient{"local": source, "ssh:build": dest},
				map[string]string{"pane-b": "ssh:build"})
			// Polling the paused host itself still fails; local processing must succeed.
			require.ErrorContains(t, r.Poll(context.Background()), "updating")
			require.Equal(t, "2", r.cursors["local"])
			require.Len(t, cache.MessagesSince(0), 1)
			require.Equal(t, "working", cache.StatusSnapshot()["orch@local"].State)
			require.Empty(t, dest.sendCalls)
		})
	}
}
