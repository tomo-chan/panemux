package board

import (
	"context"
	"strconv"
	"testing"
	"time"
)

// fakeAgmsgClient is an in-memory AgmsgClient for relay tests. Rows are
// pre-seeded via seed(); Send appends to sent for assertions.
type fakeAgmsgClient struct {
	host string
	rows []Row
	sent []sentCall
}

type sentCall struct {
	Team, From, To, Body string
}

func newFakeAgmsgClient(host string) *fakeAgmsgClient {
	return &fakeAgmsgClient{host: host}
}

func (c *fakeAgmsgClient) HostID() string { return c.host }

func (c *fakeAgmsgClient) Send(_ context.Context, team, from, to, body string) error {
	c.sent = append(c.sent, sentCall{Team: team, From: from, To: to, Body: body})
	// A real send.sh would also make the row observable on this host's
	// team on the next poll; the relay's own from-validation tests exercise
	// that via seed(), not by feeding Send's own output back in, to keep
	// the two concerns independent.
	return nil
}

func (c *fakeAgmsgClient) Since(_ context.Context, team, afterID string, limit int) ([]Row, error) {
	var out []Row
	for _, r := range c.rows {
		if r.Team != team {
			continue
		}
		if !idAfter(r.ID, afterID) {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (c *fakeAgmsgClient) seed(rows ...Row) {
	for i := range rows {
		if rows[i].Host == "" {
			rows[i].Host = c.host
		}
		if rows[i].Team == "" {
			rows[i].Team = "panemux"
		}
	}
	c.rows = append(c.rows, rows...)
}

func newTestRelay(
	t *testing.T, resolver PaneResolver, pairs []HostTeam, clients ...*fakeAgmsgClient,
) (*Relay, *MemCursorStore) {
	t.Helper()
	cache := NewBoardCache()
	cursors := NewMemCursorStore()
	r := NewRelay(cache, resolver, cursors, pairs, WithLogf(func(string, ...any) {}))
	for _, c := range clients {
		r.RegisterClient(c)
	}
	return r, cursors
}

func TestRelay_StatusRowUpdatesCacheNotHistory(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-a", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	statusBody := `{"kind":"board_status","state":"working","branch":"feature/x"}`
	client.seed(Row{ID: "1", From: "pane-a", To: PanemuxSentinel, Body: statusBody})

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	r.PollAll(context.Background())

	snap := r.cache.StatusSnapshot()
	if got := snap["pane-a"]; got.State != "working" || got.Branch != "feature/x" {
		t.Fatalf("expected status recorded for pane-a, got %+v", snap)
	}
	if msgs := r.cache.MessagesSince(0); len(msgs) != 0 {
		t.Fatalf("expected a status row to NOT appear in history, got %+v", msgs)
	}
}

func TestRelay_PlainMessageToPanemuxAppearsInHistory(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-a", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	client.seed(Row{ID: "1", From: "pane-a", To: PanemuxSentinel, Body: "hey are you around?"})

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	r.PollAll(context.Background())

	msgs := r.cache.MessagesSince(0)
	if len(msgs) != 1 || msgs[0].Row.Body != "hey are you around?" {
		t.Fatalf("expected the plain message to appear in history, got %+v", msgs)
	}
	if snap := r.cache.StatusSnapshot(); len(snap) != 0 {
		t.Fatalf("expected no status recorded for a non-status body, got %+v", snap)
	}
}

func TestRelay_SameHostMessageAppendedButNotRelayed(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{
		{ID: "pane-a", HostID: "local"},
		{ID: "pane-b", HostID: "local"},
	})
	client := newFakeAgmsgClient("local")
	client.seed(Row{ID: "1", From: "pane-a", To: "pane-b", Body: "please review"})

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	r.PollAll(context.Background())

	if msgs := r.cache.MessagesSince(0); len(msgs) != 1 {
		t.Fatalf("expected the message to be cached, got %+v", msgs)
	}
	if len(client.sent) != 0 {
		t.Fatalf("same-host message must not be relayed via Send, got %+v", client.sent)
	}
}

func TestRelay_CrossHostMessageAppendedAndRelayed(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{
		{ID: "pane-a", HostID: "hostA"},
		{ID: "pane-b", HostID: "hostB"},
	})
	clientA := newFakeAgmsgClient("hostA")
	clientB := newFakeAgmsgClient("hostB")
	clientA.seed(Row{ID: "1", From: "pane-a", To: "pane-b", Body: "please review"})

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "hostA", Team: "panemux"}}, clientA, clientB)
	r.PollAll(context.Background())

	if msgs := r.cache.MessagesSince(0); len(msgs) != 1 {
		t.Fatalf("expected the message to be cached, got %+v", msgs)
	}
	if len(clientB.sent) != 1 {
		t.Fatalf("expected the message to be relayed to hostB, got %+v", clientB.sent)
	}
	got := clientB.sent[0]
	if got.From != "pane-a" || got.To != "pane-b" || got.Body != "please review" {
		t.Fatalf("unexpected relayed send: %+v", got)
	}
}

func TestRelay_FromValidation_UnknownPaneDropped(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-b", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	client.seed(Row{ID: "1", From: "impersonator", To: "pane-b", Body: "hi"})

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	r.PollAll(context.Background())

	if msgs := r.cache.MessagesSince(0); len(msgs) != 0 {
		t.Fatalf("expected a row with an unknown from to be dropped, never cached, got %+v", msgs)
	}
}

func TestRelay_ToValidation_UnresolvedPaneDropped(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-a", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	client.seed(Row{ID: "1", From: "pane-a", To: "no-such-pane", Body: "hi"})

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	r.PollAll(context.Background())

	if msgs := r.cache.MessagesSince(0); len(msgs) != 0 {
		t.Fatalf("expected a row with an unresolved to be dropped, never cached, got %+v", msgs)
	}
}

func TestRelay_PanemuxFrom_LedgerMatchAccepted(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-b", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	client.seed(Row{ID: "1", From: PanemuxSentinel, To: "pane-b", Body: "broadcast message"})

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	r.RecordOwnSend("local", "panemux", "pane-b", "broadcast message")
	r.PollAll(context.Background())

	if msgs := r.cache.MessagesSince(0); len(msgs) != 1 {
		t.Fatalf("expected a ledger-matched _panemux row to be accepted, got %+v", msgs)
	}
}

// Regression test for the cross-host _panemux impersonation scenario in
// docs/agent-board.md's Security model: unconditionally trusting
// From == "_panemux" would let any agent on any host forge the sentinel.
func TestRelay_PanemuxFrom_NoLedgerMatchDropped(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-b", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	client.seed(Row{ID: "1", From: PanemuxSentinel, To: "pane-b", Body: "forged message"})

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	// Deliberately do NOT call RecordOwnSend — nothing panemux actually sent.
	r.PollAll(context.Background())

	if msgs := r.cache.MessagesSince(0); len(msgs) != 0 {
		t.Fatalf("expected an unmatched _panemux row to be dropped as a suspected forgery, got %+v", msgs)
	}
}

func TestRelay_PanemuxFrom_LedgerMatchIsOneShot(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-b", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	client.seed(
		Row{ID: "1", From: PanemuxSentinel, To: "pane-b", Body: "hi"},
		Row{ID: "2", From: PanemuxSentinel, To: "pane-b", Body: "hi"}, // replay attempt, same body/to
	)

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	r.RecordOwnSend("local", "panemux", "pane-b", "hi")
	r.PollAll(context.Background())

	// Only the first row should have consumed the single ledger entry; the
	// second, otherwise-identical row must be dropped.
	if msgs := r.cache.MessagesSince(0); len(msgs) != 1 {
		t.Fatalf("expected exactly one accepted row, got %+v", msgs)
	}
}

func TestRelay_Truncation_KeepsNewestDropsOldestOverflow(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-a", HostID: "local"}, {ID: "pane-b", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	for i := 1; i <= 5; i++ {
		client.seed(Row{ID: strconv.Itoa(i), From: "pane-a", To: "pane-b", Body: "msg-" + strconv.Itoa(i)})
	}

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	r.pollLimit = 2 // simulate more new rows than one poll's --limit can hold
	r.PollAll(context.Background())

	msgs := r.cache.MessagesSince(0)
	if len(msgs) != 2 {
		t.Fatalf("expected exactly %d rows kept by the bounded poll, got %d: %+v", 2, len(msgs), msgs)
	}
	// The oldest rows within the poll's own ordering (ids 1,2) are what a
	// real --limit N (most-recent-N) call would return; this fake client's
	// Since walks oldest-first and caps at `limit`, matching the same
	// "keep what the bounded call gives you, drop the rest" behavior a real
	// api.sh --limit call has (see docs/agent-board.md's truncation note).
	if msgs[0].Row.Body != "msg-1" || msgs[1].Row.Body != "msg-2" {
		t.Fatalf("unexpected kept rows: %+v", msgs)
	}
}

func TestRelay_CursorPersistsAcrossSimulatedRestart_AtLeastOnceDuplicate(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-a", HostID: "local"}, {ID: "pane-b", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	client.seed(Row{ID: "1", From: "pane-a", To: "pane-b", Body: "hi"})

	cursors := NewMemCursorStore()
	cache1 := NewBoardCache()
	pairs := []HostTeam{{Host: "local", Team: "panemux"}}
	r1 := NewRelay(cache1, resolver, cursors, pairs, WithLogf(func(string, ...any) {}))
	r1.RegisterClient(client)
	r1.PollAll(context.Background())
	if err := r1.LoadCursors(); err != nil {
		t.Fatalf("LoadCursors: %v", err)
	}

	if msgs := cache1.MessagesSince(0); len(msgs) != 1 {
		t.Fatalf("expected 1 message before restart, got %+v", msgs)
	}

	// Simulate a restart: a fresh relay + fresh cache, but the same
	// (persisted) cursor store, WITHOUT having actually persisted the
	// updated cursor yet (a crash between relaying and saving the cursor is
	// exactly the accepted at-least-once duplicate case — see
	// docs/agent-board.md's Cross-host relay section).
	cache2 := NewBoardCache()
	r2 := NewRelay(cache2, resolver, cursors, pairs, WithLogf(func(string, ...any) {}))
	r2.RegisterClient(client)
	// Cursor store was never updated by r1 (cursors is fresh/empty in this
	// branch) to simulate the crash-before-persist window.
	freshCursors := NewMemCursorStore()
	r2.cursors = freshCursors
	if err := r2.LoadCursors(); err != nil {
		t.Fatalf("LoadCursors: %v", err)
	}
	r2.PollAll(context.Background())

	// The message is delivered again — at-least-once, not exactly-once, per
	// docs/agent-board.md. This asserts it IS delivered again, not silently
	// dropped and not an error.
	if msgs := cache2.MessagesSince(0); len(msgs) != 1 {
		t.Fatalf("expected the at-least-once duplicate to be delivered again after restart, got %+v", msgs)
	}
}

func TestRelay_Backfill_ThenSteadyStatePollsFromReachedCursor(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-a", HostID: "local"}, {ID: "pane-b", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	client.seed(
		Row{ID: "1", From: "pane-a", To: "pane-b", Body: "old-1"},
		Row{ID: "2", From: "pane-a", To: "pane-b", Body: "old-2"},
	)

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)
	r.Backfill(context.Background())

	if msgs := r.cache.MessagesSince(0); len(msgs) != 2 {
		t.Fatalf("expected backfill to pick up existing rows, got %+v", msgs)
	}

	client.seed(Row{ID: "3", From: "pane-a", To: "pane-b", Body: "new-3"})
	r.PollAll(context.Background())

	msgs := r.cache.MessagesSince(0)
	if len(msgs) != 3 || msgs[2].Row.Body != "new-3" {
		t.Fatalf("expected steady-state poll to continue from the backfilled cursor, got %+v", msgs)
	}
}

func TestRelay_EmptyTeam_NoRowsIsNotAnError(t *testing.T) {
	resolver := NewStaticPaneResolver(nil)
	client := newFakeAgmsgClient("local")

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "empty-team"}}, client)
	r.PollAll(context.Background())

	if msgs := r.cache.MessagesSince(0); len(msgs) != 0 {
		t.Fatalf("expected no messages for an empty team, got %+v", msgs)
	}
}

func TestRelayOptions_OverridePollAndBackfillLimits(t *testing.T) {
	r := NewRelay(
		NewBoardCache(), NewStaticPaneResolver(nil), NewMemCursorStore(), nil,
		WithPollLimit(7), WithBackfillLimit(42),
	)
	if r.pollLimit != 7 {
		t.Fatalf("pollLimit = %d, want 7", r.pollLimit)
	}
	if r.backfillLimit != 42 {
		t.Fatalf("backfillLimit = %d, want 42", r.backfillLimit)
	}
}

func TestRelay_Broadcast_SendsAndRecordsLedgerEntry(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-b", HostID: "hostB"}})
	clientB := newFakeAgmsgClient("hostB")

	r, _ := newTestRelay(t, resolver, nil, clientB)
	results := r.Broadcast(context.Background(), "panemux", []string{"pane-b"}, "hello")

	if len(results) != 1 || results[0].Error != "" || results[0].Pane != "pane-b" {
		t.Fatalf("unexpected results: %+v", results)
	}
	if len(clientB.sent) != 1 {
		t.Fatalf("expected Send to be called, got %+v", clientB.sent)
	}

	// The relay's own from-validation must now accept a row it later
	// observes claiming From == "_panemux" for this exact (host, team, to,
	// body) — this is the ledger entry Broadcast is responsible for.
	if !r.ledger.Consume("hostB", "panemux", "pane-b", "hello") {
		t.Fatal("expected Broadcast to record a matching own-send ledger entry")
	}
}

func TestRelay_Broadcast_SendsFromPanemuxSentinel(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-b", HostID: "hostB"}})
	clientB := newFakeAgmsgClient("hostB")

	r, _ := newTestRelay(t, resolver, nil, clientB)
	r.Broadcast(context.Background(), "panemux", []string{"pane-b"}, "hello")

	if clientB.sent[0].From != PanemuxSentinel {
		t.Fatalf("From = %q, want %q", clientB.sent[0].From, PanemuxSentinel)
	}
}

func TestRelay_Broadcast_UnknownPane(t *testing.T) {
	resolver := NewStaticPaneResolver(nil)
	r, _ := newTestRelay(t, resolver, nil)
	results := r.Broadcast(context.Background(), "panemux", []string{"no-such-pane"}, "hi")
	if len(results) != 1 || results[0].Error == "" {
		t.Fatalf("expected an error result for an unknown pane, got %+v", results)
	}
}

func TestRelay_Broadcast_MultipleTargetsAcrossHosts(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{
		{ID: "pane-a", HostID: "hostA"},
		{ID: "pane-b", HostID: "hostB"},
	})
	clientA := newFakeAgmsgClient("hostA")
	clientB := newFakeAgmsgClient("hostB")
	r, _ := newTestRelay(t, resolver, nil, clientA, clientB)

	results := r.Broadcast(context.Background(), "panemux", []string{"pane-a", "pane-b"}, "hi all")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %+v", results)
	}
	for _, res := range results {
		if res.Error != "" {
			t.Fatalf("unexpected error for pane %s: %s", res.Pane, res.Error)
		}
	}
	if len(clientA.sent) != 1 || len(clientB.sent) != 1 {
		t.Fatalf("expected each destination host's client to receive exactly one Send")
	}
}

func TestRelay_NoClientForHost_LogsAndDoesNotPanic(t *testing.T) {
	resolver := NewStaticPaneResolver(nil)
	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "unregistered-host", Team: "panemux"}})
	r.PollAll(context.Background()) // must not panic
}

func TestRelay_HasClient(t *testing.T) {
	resolver := NewStaticPaneResolver(nil)
	client := newFakeAgmsgClient("hostA")
	r, _ := newTestRelay(t, resolver, nil, client)

	if !r.HasClient("hostA") {
		t.Fatal("expected HasClient to report true for a registered host")
	}
	if r.HasClient("hostB") {
		t.Fatal("expected HasClient to report false for an unregistered host")
	}
}

func TestRelay_Run_BackfillsThenStopsOnContextCancel(t *testing.T) {
	resolver := NewStaticPaneResolver([]PaneRef{{ID: "pane-a", HostID: "local"}, {ID: "pane-b", HostID: "local"}})
	client := newFakeAgmsgClient("local")
	client.seed(Row{ID: "1", From: "pane-a", To: "pane-b", Body: "hi"})

	r, _ := newTestRelay(t, resolver, []HostTeam{{Host: "local", Team: "panemux"}}, client)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		r.Run(ctx, time.Millisecond)
		close(done)
	}()

	// Give Run's initial Backfill pass time to complete.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(r.cache.MessagesSince(0)) == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if msgs := r.cache.MessagesSince(0); len(msgs) != 1 {
		t.Fatalf("expected Run's initial backfill to populate the cache, got %+v", msgs)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("expected Run to return promptly after context cancellation")
	}
}
