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
