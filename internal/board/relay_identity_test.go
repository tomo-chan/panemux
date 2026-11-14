package board

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func identityRelay() (*Relay, *BoardCache, map[string]*fakeMessageClient) {
	cache := NewBoardCache()
	fakes := map[string]*fakeMessageClient{}
	clients := map[string]MessageClient{}
	for _, h := range []string{"local", "ssh:build", "ssh:local"} {
		fakes[h] = &fakeMessageClient{hostID: h}
		clients[h] = fakes[h]
	}
	return newTestRelay(cache, clients, map[string]string{"pane-a": "local", "pane-b": "ssh:build"}), cache, fakes
}

func TestRelay_IdentityRoutes(t *testing.T) {
	for _, tc := range []struct{ source, from, to, dest, sendFrom, sendTo string }{
		{"local", "orch", "impl@ssh:build", "ssh:build", "orch@local", "impl"},
		{"ssh:build", "impl", "orch@local", "local", "impl@ssh:build", "orch"},
		{"local", "same", "same", "", "", ""},
		{"ssh:build", "same", "same", "", "", ""},
		{"local", "same", "same@ssh:build", "ssh:build", "same@local", "same"},
		{"ssh:build", "same", "same@local", "local", "same@ssh:build", "same"},
		{"local", "same", "same@local", "local", "same@local", "same"},
		{"local", "same", "same@ssh:local", "ssh:local", "same@local", "same"},
		{"ssh:local", "same", "same@local", "local", "same@ssh:local", "same"},
		{"local", "pane-a", "pane-b", "ssh:build", "pane-a@local", "pane-b"},
		{"ssh:build", "pane-b", "pane-a", "local", "pane-b@ssh:build", "pane-a"},
	} {
		t.Run(tc.source+"/"+tc.to, func(t *testing.T) {
			r, cache, hosts := identityRelay()
			row := Row{ID: "1", Host: "untrusted", Team: "panemux", From: tc.from, To: tc.to, Body: "hello $() '世界'"}
			hosts[tc.source].sinceRows = []Row{row}
			require.NoError(t, r.Poll(context.Background()))
			history := cache.MessagesSince(0)
			require.Len(t, history, 1)
			assert.Equal(t, tc.source, history[0].Row.Host)
			assert.Equal(t, row.Team, history[0].Row.Team)
			assert.Equal(t, row.From, history[0].Row.From)
			assert.Equal(t, row.To, history[0].Row.To)
			assert.Equal(t, row.Body, history[0].Row.Body)
			for h, c := range hosts {
				if h == tc.dest {
					require.Equal(t, []sentMessage{{"panemux", tc.sendFrom, tc.sendTo, row.Body}}, c.sendCalls)
					// Observe the copy on its destination; neither history nor sends grow.
					r.processRow(context.Background(), h, Row{Team: row.Team, From: tc.sendFrom, To: tc.sendTo, Body: row.Body})
					assert.Len(t, c.sendCalls, 1)
				} else {
					assert.Empty(t, c.sendCalls)
				}
			}
			assert.Len(t, cache.MessagesSince(0), 1)
		})
	}
}

func TestRelay_IdentityRejection(t *testing.T) {
	for _, tc := range []struct{ team, from, to string }{
		{"other", "worker", "_system"}, {"panemux", "x@local", "_system"},
		{"panemux", "x@ssh:build", "pane-a"}, {"panemux", "pane-b", "pane-a"},
		{"panemux", "$(id)", "_system"}, {"panemux", "", "worker"},
		{"panemux", "worker", "_system@local"}, {"panemux", "worker", "x@ssh:unknown"},
		{"panemux", "worker", "x@ssh:"}, {"panemux", "worker", "x@local@local"},
		{"panemux", "worker", "$(id)"}, {"panemux", SystemID, "worker"},
	} {
		t.Run(tc.team+"/"+tc.from+"/"+tc.to, func(t *testing.T) {
			r, cache, hosts := identityRelay()
			hosts["local"].sinceRows = []Row{{ID: "1", Team: tc.team, From: tc.from, To: tc.to,
				Body: `{"kind":"board_status","state":"working"}`}}
			require.NoError(t, r.Poll(context.Background()))
			assert.Empty(t, cache.MessagesSince(0))
			assert.Empty(t, cache.StatusSnapshot())
			for _, h := range hosts {
				assert.Empty(t, h.sendCalls)
			}
			assert.Equal(t, "1", r.cursors["local"], "rejected rows still advance the cursor")
		})
	}
}

func TestRelay_PaneFreeSenderToLegacyPaneDoesNotDuplicateHistory(t *testing.T) {
	r, cache, hosts := identityRelay()
	ctx := context.Background()
	r.processRow(ctx, "local", Row{Team: "panemux", From: "worker", To: "pane-b", Body: "hi"})
	require.Equal(t, []sentMessage{{"panemux", "worker@local", "pane-b", "hi"}}, hosts["ssh:build"].sendCalls)
	r.processRow(ctx, "ssh:build", Row{Team: "panemux", From: "worker@local", To: "pane-b", Body: "hi"})
	assert.Len(t, cache.MessagesSince(0), 1, "the forwarded copy must not enter history twice")
	r.processRow(ctx, "ssh:build", Row{Team: "panemux", From: "pane-b", To: "worker@local", Body: "reply"})
	require.Equal(t, []sentMessage{{"panemux", "pane-b@ssh:build", "worker", "reply"}}, hosts["local"].sendCalls)
	assert.Len(t, cache.MessagesSince(0), 2)
}

func TestRelay_IdentityStatusIsolation(t *testing.T) {
	r, cache, _ := identityRelay()
	for _, h := range []string{"local", "ssh:build"} {
		r.processRow(context.Background(), h, Row{Team: "panemux", From: "same", To: SystemID,
			Body: `{"kind":"board_status","state":"` + h + `"}`})
	}
	for _, body := range []string{"normal", `{"state":"idle"}`, `{"kind":"board_status","state":3}`} {
		r.processRow(context.Background(), "local", Row{Team: "panemux", From: "same", To: SystemID, Body: body})
	}
	snap := cache.StatusSnapshot()
	require.Len(t, snap, 2)
	assert.Equal(t, "local", snap["same@local"].State)
	assert.Equal(t, "ssh:build", snap["same@ssh:build"].State)
	assert.Len(t, cache.MessagesSince(0), 5)
}

func TestRelay_QualifiedBroadcast(t *testing.T) {
	ctx := context.Background()
	for _, bad := range []string{"missing", "x@ssh:missing", "x@@local", "_system@local"} {
		t.Run(bad, func(t *testing.T) {
			r, _, hosts := identityRelay()
			_, err := r.Broadcast(ctx, SystemID, []string{"x@local", bad}, "hi")
			require.Error(t, err)
			for _, h := range hosts {
				assert.Empty(t, h.sendCalls)
			}
		})
	}
	for _, fail := range []bool{false, true} {
		r, cache, hosts := identityRelay()
		if fail {
			hosts["ssh:build"].sendErr = errors.New("unreachable")
		}
		targets := []string{"x@local", "x@local", "x@ssh:build", "pane-a"}
		delivered, err := r.Broadcast(ctx, SystemID, targets, "hi")
		if fail {
			require.Error(t, err)
			assert.Equal(t, targets[:2], delivered)
		} else {
			require.NoError(t, err)
			assert.Equal(t, targets, delivered)
		}
		assert.Equal(t, sentMessage{"panemux", SystemID, "x", "hi"}, hosts["local"].sendCalls[0])
		for i := 0; i < 3; i++ {
			r.processRow(ctx, "local", Row{Team: "panemux", From: SystemID, To: "x", Body: "hi"})
		}
		assert.Len(t, cache.MessagesSince(0), 2, "multiset permits exactly two copies")
		r.processRow(ctx, "ssh:build", Row{Team: "panemux", From: SystemID, To: "x", Body: "hi"})
		want := 3
		if fail {
			want = 2
		}
		assert.Len(t, cache.MessagesSince(0), want, "failed send must cancel its ledger entry")
	}
}

func TestRelay_QualifiedBroadcastRecipientDoesNotRerouteOwnSend(t *testing.T) {
	r, cache, hosts := identityRelay()
	ctx := context.Background()
	_, err := r.Broadcast(ctx, SystemID, []string{"pane-b@local"}, "hi")
	require.NoError(t, err)
	hosts["local"].sinceRows = []Row{{ID: "1", Team: "panemux", From: SystemID, To: "pane-b", Body: "hi"}}
	r.clients["ssh:build"] = &pausingClient{fakeMessageClient: hosts["ssh:build"], paused: true}
	require.Error(t, r.Poll(ctx), "only the paused host poll fails; it must not pause the source batch")
	assert.Equal(t, "1", r.cursors["local"])
	assert.Len(t, cache.MessagesSince(0), 1)
	assert.Empty(t, hosts["ssh:build"].sendCalls, "ledger-matched sends were already delivered to their explicit host")
}

func TestRelay_QualifiedDestinationPausePreservesSourceBatch(t *testing.T) {
	source := &fakeMessageClient{hostID: "local", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "worker", To: "worker@ssh:build", Body: "hi"},
	}}
	dest := &pausingClient{fakeMessageClient: &fakeMessageClient{hostID: "ssh:build"}, paused: true}
	cache := NewBoardCache()
	r := newTestRelay(cache, map[string]MessageClient{"local": source, "ssh:build": dest}, nil)
	require.ErrorContains(t, r.Poll(context.Background()), "updating")
	assert.Empty(t, cache.MessagesSince(0))
	assert.Empty(t, r.cursors)
	dest.paused = false
	require.NoError(t, r.Poll(context.Background()))
	assert.Len(t, dest.sendCalls, 1)
	assert.Len(t, cache.MessagesSince(0), 1)
	assert.Equal(t, "1", r.cursors["local"])
}

func TestRelayDelayedPaneCopySurvivesRoutingChanges(t *testing.T) {
	for _, sourceOwner := range []string{"", "ssh:build"} {
		for _, recipientOwner := range []string{"ssh:build", "local"} {
			for _, restart := range []bool{false, true} {
				t.Run(fmt.Sprintf("source=%s/recipient=%s/restart=%v", sourceOwner, recipientOwner, restart), func(t *testing.T) {
					r, cache, hosts := identityRelay()
					panes := map[string]string{"pane-a": "local", "pane-b": "ssh:build"}
					cfg := RelayConfig{Team: "panemux", Clients: r.clients, CurrentRouting: func() Routing {
						return Routing{PaneHosts: panes, Clients: r.clients}
					}, Limit: 100, BackfillLimit: 1000}
					r = NewRelay(cache, cfg)
					hosts["local"].sinceRows = []Row{{ID: "original", Team: "panemux", From: "pane-a", To: "pane-b", Body: "pending"}}
					hosts["ssh:build"].sinceErr = errors.New("disconnected")
					require.ErrorContains(t, r.Poll(context.Background()), "disconnected")
					require.Len(t, hosts["ssh:build"].sendCalls, 1)
					forwarded := hosts["ssh:build"].sendCalls[0]
					// Only now does the previously unread destination copy become readable.
					hosts["ssh:build"].sinceRows = []Row{{
						ID: "copy", Team: forwarded.Team, From: forwarded.From, To: forwarded.To, Body: forwarded.Body,
					}}
					hosts["ssh:build"].sinceErr = nil
					delete(panes, "pane-a")
					if sourceOwner != "" {
						panes["pane-a"] = sourceOwner
					}
					panes["pane-b"] = recipientOwner
					if restart {
						cursors := r.Cursors()
						r = NewRelay(cache, cfg)
						r.LoadCursors(cursors)
					}
					require.NoError(t, r.Poll(context.Background()))
					require.NoError(t, r.Poll(context.Background()))
					history := cache.MessagesSince(0)
					require.Len(t, history, 1, "a delayed copy must not become an original after pane changes")
					assert.Equal(t, "local", history[0].Row.Host)
					assert.Empty(t, hosts["local"].sendCalls, "the copy must not bounce to the moved recipient")
					assert.Len(t, hosts["ssh:build"].sendCalls, 1)
					assert.Empty(t, cache.StatusSnapshot())
					assert.Equal(t, "copy", r.cursors["ssh:build"], "rejected copies still advance the cursor")
					assert.Equal(t, "pane-a@local", forwarded.From)
				})
			}
		}
	}
}

func TestRelayBarePaneRecipientReplyUsesQualifiedSender(t *testing.T) {
	for _, sender := range []string{"pane-a", "worker"} {
		t.Run(sender, func(t *testing.T) {
			r, cache, hosts := identityRelay()
			hosts["local"].sinceRows = []Row{{ID: "1", Team: "panemux", From: sender, To: "pane-b", Body: "request"}}
			require.NoError(t, r.Poll(context.Background()))
			require.Len(t, hosts["ssh:build"].sendCalls, 1)
			forwarded := hosts["ssh:build"].sendCalls[0]
			require.Equal(t, sender+"@local", forwarded.From)
			require.Equal(t, "pane-b", forwarded.To, "bare pane destination routing is preserved")
			hosts["ssh:build"].sinceRows = []Row{
				{ID: "copy", Team: forwarded.Team, From: forwarded.From, To: forwarded.To, Body: forwarded.Body},
				{ID: "reply", Team: "panemux", From: "pane-b", To: forwarded.From, Body: "reply"},
			}
			require.NoError(t, r.Poll(context.Background()))
			require.Equal(t, []sentMessage{{"panemux", "pane-b@ssh:build", sender, "reply"}}, hosts["local"].sendCalls)
			history := cache.MessagesSince(0)
			require.Len(t, history, 2)
			assert.Equal(t, "local", history[0].Row.Host)
			assert.Equal(t, "ssh:build", history[1].Row.Host)
		})
	}
}
