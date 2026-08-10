package board

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type sentMessage struct{ Team, From, To, Body string }

// fakeAgmsgClient is safe for concurrent use: the runLoop tests drive it
// from a background goroutine while the test goroutine polls its recorded
// calls, so every field access goes through mu.
type fakeAgmsgClient struct {
	sinceErr    error
	sendErr     error
	hostID      string
	sinceRows   []Row
	sinceLimits []int
	sendCalls   []sentMessage
	mu          sync.Mutex
}

func (f *fakeAgmsgClient) HostID() string { return f.hostID }

func (f *fakeAgmsgClient) Since(_ context.Context, _, afterID string, limit int) ([]Row, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sinceLimits = append(f.sinceLimits, limit)
	if f.sinceErr != nil {
		return nil, f.sinceErr
	}
	return filterRowsAfter(f.sinceRows, afterID), nil
}

func (f *fakeAgmsgClient) Send(_ context.Context, team, from, to, body string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sendCalls = append(f.sendCalls, sentMessage{Team: team, From: from, To: to, Body: body})
	return f.sendErr
}

func (f *fakeAgmsgClient) sinceCallCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sinceLimits)
}

func newTestRelay(cache *BoardCache, clients map[string]AgmsgClient, paneHosts map[string]string) *Relay {
	return NewRelay(cache, RelayConfig{
		Team: "panemux", Clients: clients, PaneHosts: paneHosts,
		Limit: 100, BackfillLimit: 1000,
	})
}

func TestRelay_Poll_NoKnownHosts_NoOp(t *testing.T) {
	r := newTestRelay(NewBoardCache(), nil, nil)
	assert.NoError(t, r.Poll(context.Background()))
}

func TestRelay_Poll_AlreadyCanceledContext_ReturnsErrorImmediately(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a"}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-a": hostA}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := r.Poll(ctx)
	require.Error(t, err)
	assert.Empty(t, hostA.sinceLimits, "no client call should happen once ctx is already canceled")
}

func TestRelay_Poll_FromKnownLocalPane_ToKnownPaneSameHost_Appended(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "pane-a", To: "pane-b", Body: "hi"},
	}}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a", "pane-b": "host-a"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)

	require.NoError(t, r.Poll(context.Background()))

	rows := cache.MessagesSince(0)
	require.Len(t, rows, 1)
	assert.Equal(t, "hi", rows[0].Row.Body)
	assert.Empty(t, hostA.sendCalls, "same-host delivery needs no relay Send")
}

func TestRelay_Poll_ToKnownPaneDifferentHost_RelaysAndAppendsOnce(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "claude-a", To: "codex-b", Body: "please review"},
	}}
	hostB := &fakeAgmsgClient{hostID: "host-b"}
	cache := NewBoardCache()
	paneHosts := map[string]string{"claude-a": "host-a", "codex-b": "host-b"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA, "host-b": hostB}, paneHosts)

	require.NoError(t, r.Poll(context.Background()))

	rows := cache.MessagesSince(0)
	require.Len(t, rows, 1, "the row must be appended exactly once, from host-a's own poll")
	assert.Equal(t, "please review", rows[0].Row.Body)

	require.Len(t, hostB.sendCalls, 1)
	want := sentMessage{Team: "panemux", From: "claude-a", To: "codex-b", Body: "please review"}
	assert.Equal(t, want, hostB.sendCalls[0])
}

// TestRelay_Poll_RelayedRowNotDoubleCounted is the regression test for the
// central correctness property of cross-host relay: a message relayed to
// host B keeps its original From (not SystemID). When host B is next
// polled and that row is read back, from-validation against host B's own
// known panes must reject it — the original pane isn't local to host B —
// so it is not re-appended to history a second time.
func TestRelay_Poll_RelayedRowNotDoubleCounted(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "claude-a", To: "codex-b", Body: "please review"},
	}}
	hostB := &fakeAgmsgClient{hostID: "host-b"}
	cache := NewBoardCache()
	paneHosts := map[string]string{"claude-a": "host-a", "codex-b": "host-b"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA, "host-b": hostB}, paneHosts)

	require.NoError(t, r.Poll(context.Background()))
	require.Len(t, hostB.sendCalls, 1)

	// Simulate agmsg on host B actually having stored the relayed row, now
	// visible to a later poll of host B with a host-B-scoped agmsg id.
	hostB.sinceRows = append(hostB.sinceRows, Row{
		ID: "1", Team: "panemux", From: "claude-a", To: "codex-b", Body: "please review",
	})

	require.NoError(t, r.Poll(context.Background()))

	rows := cache.MessagesSince(0)
	assert.Len(t, rows, 1, "the relayed copy read back from host B must not be appended again")
	assert.Len(t, hostB.sendCalls, 1, "host B's own row must never itself be re-relayed (no new Send call)")
}

func TestRelay_Poll_FromSystemIDWithLedgerMatch_Accepted(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a"}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)

	_, err := r.Broadcast(context.Background(), SystemID, []string{"pane-a"}, "hello from command center")
	require.NoError(t, err)
	require.Len(t, hostA.sendCalls, 1)

	hostA.sinceRows = []Row{
		{ID: "1", Team: "panemux", From: SystemID, To: "pane-a", Body: "hello from command center"},
	}
	require.NoError(t, r.Poll(context.Background()))

	rows := cache.MessagesSince(0)
	require.Len(t, rows, 1)
	assert.Equal(t, "hello from command center", rows[0].Row.Body)
}

func TestRelay_Poll_FromSystemIDWithNoLedgerMatch_DroppedAsForgery(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: SystemID, To: "pane-a", Body: "forged"},
	}}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)

	require.NoError(t, r.Poll(context.Background()))

	assert.Empty(t, cache.MessagesSince(0), "an unmatched SystemID from must be dropped, never cached")
}

func TestRelay_Poll_FromSystemIDWithExpiredLedgerEntry_DroppedAsForgery(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a"}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)
	r.ledger.ttl = time.Millisecond

	_, err := r.Broadcast(context.Background(), SystemID, []string{"pane-a"}, "hello")
	require.NoError(t, err)

	time.Sleep(5 * time.Millisecond)

	hostA.sinceRows = []Row{
		{ID: "1", Team: "panemux", From: SystemID, To: "pane-a", Body: "hello"},
	}
	require.NoError(t, r.Poll(context.Background()))

	assert.Empty(t, cache.MessagesSince(0), "an expired ledger entry must no longer match")
}

func TestRelay_Poll_FromUnknownPane_DroppedAndNotCached(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "stranger", To: "pane-a", Body: "hi"},
	}}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)

	require.NoError(t, r.Poll(context.Background()))

	assert.Empty(t, cache.MessagesSince(0))
}

func TestRelay_Poll_ToSystemID_StatusBody_RecordsStatusAndAppendsHistory(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "pane-a", To: SystemID, Body: `{"kind":"board_status","state":"working"}`},
	}}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)

	require.NoError(t, r.Poll(context.Background()))

	snap := cache.StatusSnapshot()
	require.Contains(t, snap, "pane-a")
	assert.Equal(t, "working", snap["pane-a"].State)

	rows := cache.MessagesSince(0)
	require.Len(t, rows, 1, "status rows are still appended to history, per the design doc")
	assert.Empty(t, hostA.sendCalls, "status reports are never relayed cross-host")
}

func TestRelay_Poll_ToSystemID_NonStatusBody_AppendedAsOrdinaryMessage(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "pane-a", To: SystemID, Body: "just chatting"},
	}}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)

	require.NoError(t, r.Poll(context.Background()))

	assert.Empty(t, cache.StatusSnapshot())
	rows := cache.MessagesSince(0)
	require.Len(t, rows, 1)
	assert.Equal(t, "just chatting", rows[0].Row.Body)
}

func TestRelay_Poll_ToUnknownPane_DroppedAndNotCached(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "pane-a", To: "no-such-pane", Body: "hi"},
	}}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)

	require.NoError(t, r.Poll(context.Background()))

	assert.Empty(t, cache.MessagesSince(0))
}

func TestRelay_Poll_ClientSinceError_LoggedAndDoesNotStopOtherHosts(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceErr: errors.New("agmsg down")}
	hostB := &fakeAgmsgClient{hostID: "host-b", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "pane-b", To: "pane-b2", Body: "ok"},
	}}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-b": "host-b", "pane-b2": "host-b"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA, "host-b": hostB}, paneHosts)

	err := r.Poll(context.Background())
	require.Error(t, err)

	rows := cache.MessagesSince(0)
	require.Len(t, rows, 1, "host-b must still be processed despite host-a's failure")
	assert.Equal(t, "ok", rows[0].Row.Body)
}

func TestRelay_Poll_ColdStartUsesBackfillLimit_SubsequentUseSteadyStateLimit(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a"}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-a": hostA}, nil)

	require.NoError(t, r.Poll(context.Background()))
	require.NoError(t, r.Poll(context.Background()))

	require.Len(t, hostA.sinceLimits, 2)
	assert.Equal(t, 1000, hostA.sinceLimits[0])
	assert.Equal(t, 100, hostA.sinceLimits[1])
}

func TestRelay_CursorPersistence_AcrossSimulatedRestart(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "pane-a", To: "pane-b", Body: "one"},
	}}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a", "pane-b": "host-a"}
	r1 := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)
	require.NoError(t, r1.Poll(context.Background()))

	persisted := r1.Cursors()
	require.Len(t, persisted, 1)
	assert.Equal(t, "1", persisted[0].Cursor)

	// New relay instance ("restart"), seeded from the persisted cursor.
	hostA2 := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "pane-a", To: "pane-b", Body: "one"},
		{ID: "2", Team: "panemux", From: "pane-a", To: "pane-b", Body: "two"},
	}}
	cache2 := NewBoardCache()
	r2 := newTestRelay(cache2, map[string]AgmsgClient{"host-a": hostA2}, paneHosts)
	r2.LoadCursors(persisted)
	require.NoError(t, r2.Poll(context.Background()))

	rows := cache2.MessagesSince(0)
	require.Len(t, rows, 1, "only the genuinely new row (id 2) should be delivered after resuming from cursor 1")
	assert.Equal(t, "two", rows[0].Row.Body)
}

func TestRelay_CursorPersistence_AtLeastOnceDuplicateAccepted(t *testing.T) {
	// Simulates a crash between relaying and persisting the cursor: the
	// same row is seen again on the next Poll and is delivered again, not
	// silently deduplicated — the documented accepted tradeoff.
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "pane-a", To: "pane-b", Body: "one"},
	}}
	cache := NewBoardCache()
	paneHosts := map[string]string{"pane-a": "host-a", "pane-b": "host-a"}
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)

	require.NoError(t, r.Poll(context.Background()))
	// Cursor deliberately NOT advanced/reloaded — simulate a crash before
	// persistence by constructing a fresh relay with no cursor loaded.
	r2 := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA}, paneHosts)
	require.NoError(t, r2.Poll(context.Background()))

	rows := cache.MessagesSince(0)
	assert.Len(t, rows, 2, "a row seen again after a lost cursor is delivered again, not dropped")
}

func TestRelay_Poll_PersistCursorsCalledWithSnapshot(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "5", Team: "panemux", From: "pane-a", To: "pane-b", Body: "hi"},
	}}
	var captured []CursorEntry
	r := NewRelay(NewBoardCache(), RelayConfig{
		Team: "panemux", Clients: map[string]AgmsgClient{"host-a": hostA},
		PaneHosts: map[string]string{"pane-a": "host-a", "pane-b": "host-a"},
		Limit:     100, BackfillLimit: 1000,
		PersistCursors: func(entries []CursorEntry) { captured = entries },
	})

	require.NoError(t, r.Poll(context.Background()))

	require.Len(t, captured, 1)
	assert.Equal(t, "host-a", captured[0].Host)
	assert.Equal(t, "5", captured[0].Cursor)
}

func TestRelay_Broadcast_KnownPanesSingleHost_Delivered(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a"}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-a": hostA}, map[string]string{
		"pane-a": "host-a", "pane-b": "host-a",
	})

	delivered, err := r.Broadcast(context.Background(), SystemID, []string{"pane-a", "pane-b"}, "hello")
	require.NoError(t, err)
	assert.Equal(t, []string{"pane-a", "pane-b"}, delivered)
	assert.Len(t, hostA.sendCalls, 2)
}

func TestRelay_Broadcast_KnownPanesAcrossTwoHosts_Delivered(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a"}
	hostB := &fakeAgmsgClient{hostID: "host-b"}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-a": hostA, "host-b": hostB}, map[string]string{
		"pane-a": "host-a", "pane-b": "host-b",
	})

	delivered, err := r.Broadcast(context.Background(), SystemID, []string{"pane-a", "pane-b"}, "hello")
	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"pane-a", "pane-b"}, delivered)
	assert.Len(t, hostA.sendCalls, 1)
	assert.Len(t, hostB.sendCalls, 1)
}

func TestRelay_Broadcast_OneUnknownPane_AllOrNothing(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a"}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-a": hostA}, map[string]string{
		"pane-a": "host-a",
	})

	delivered, err := r.Broadcast(context.Background(), SystemID, []string{"pane-a", "no-such-pane"}, "hello")
	require.Error(t, err)
	var unknownErr *UnknownPaneError
	require.ErrorAs(t, err, &unknownErr)
	assert.Equal(t, []string{"no-such-pane"}, unknownErr.IDs)
	assert.Nil(t, delivered)
	assert.Empty(t, hostA.sendCalls, "no Send may happen once any pane is unresolved")
}

func TestRelay_Broadcast_SendFailurePartway_ReportsPartialDelivery(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a"}
	hostB := &fakeAgmsgClient{hostID: "host-b", sendErr: errors.New("ssh: connection lost")}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-a": hostA, "host-b": hostB}, map[string]string{
		"pane-a": "host-a", "pane-b": "host-b",
	})

	delivered, err := r.Broadcast(context.Background(), SystemID, []string{"pane-a", "pane-b"}, "hello")
	require.Error(t, err)
	assert.Equal(t, []string{"pane-a"}, delivered)
}

func TestRelay_Broadcast_SendFailure_OwnSendLedgerEntryForgotten(t *testing.T) {
	// Regression test: Broadcast records a ledger entry before Send so a
	// fast poll racing a *successful* Send can still match it — but if Send
	// then fails, that entry must not stay live for the rest of its TTL.
	// Otherwise any pane on the destination host could forge a
	// From == SystemID row with the same to/body within that window and
	// have it accepted as legitimate, even though nothing was ever
	// actually delivered.
	hostB := &fakeAgmsgClient{hostID: "host-b", sendErr: errors.New("ssh: connection lost")}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-b": hostB}, map[string]string{
		"pane-b": "host-b",
	})

	_, err := r.Broadcast(context.Background(), SystemID, []string{"pane-b"}, "hello")
	require.Error(t, err)

	forged := Row{Host: "host-b", Team: "panemux", From: SystemID, To: "pane-b", Body: "hello"}
	assert.False(t, r.validFrom("host-b", forged),
		"a Send failure must not leave a matchable own-send-ledger entry behind")
}

func TestRelay_Broadcast_SendSucceeds_OwnSendLedgerEntryStillMatchable(t *testing.T) {
	// Contrast case for the regression above: a *successful* Send's ledger
	// entry must remain matchable, since that's the normal path a real
	// relayed-back row is validated against.
	hostB := &fakeAgmsgClient{hostID: "host-b"}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-b": hostB}, map[string]string{
		"pane-b": "host-b",
	})

	delivered, err := r.Broadcast(context.Background(), SystemID, []string{"pane-b"}, "hello")
	require.NoError(t, err)
	assert.Equal(t, []string{"pane-b"}, delivered)

	row := Row{Host: "host-b", Team: "panemux", From: SystemID, To: "pane-b", Body: "hello"}
	assert.True(t, r.validFrom("host-b", row), "a successful Send's ledger entry must remain matchable")
}

func TestRelay_Broadcast_UnreachableHost_Error(t *testing.T) {
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{}, map[string]string{"pane-a": "host-a"})

	delivered, err := r.Broadcast(context.Background(), SystemID, []string{"pane-a"}, "hello")
	require.Error(t, err)
	assert.Empty(t, delivered)
}

func TestRelay_RunLoop_PollsImmediatelyThenOnEachTick(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a"}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-a": hostA}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	tick := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		r.runLoop(ctx, tick)
		close(done)
	}()

	// Immediate poll on entry.
	require.Eventually(t, func() bool { return hostA.sinceCallCount() == 1 }, time.Second, time.Millisecond)

	tick <- time.Now()
	require.Eventually(t, func() bool { return hostA.sinceCallCount() == 2 }, time.Second, time.Millisecond)

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("runLoop did not exit after ctx cancellation")
	}
}

func TestRelay_RunLoop_PollErrorLoggedNotPanicked(t *testing.T) {
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceErr: errors.New("agmsg down")}
	r := newTestRelay(NewBoardCache(), map[string]AgmsgClient{"host-a": hostA}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	tick := make(chan time.Time)
	done := make(chan struct{})

	assert.NotPanics(t, func() {
		go func() {
			r.runLoop(ctx, tick)
			close(done)
		}()
		require.Eventually(t, func() bool { return hostA.sinceCallCount() == 1 }, time.Second, time.Millisecond)
		cancel()
		<-done
	})
}
