package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

func readIdentities(t *testing.T, h *Handler) map[string]boardIdentityEntry {
	t.Helper()
	rr := httptest.NewRecorder()
	h.GetBoardStatus(rr, httptest.NewRequest(http.MethodGet, "/api/board/status", nil))
	require.Equal(t, 200, rr.Code)
	var got boardStatusResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &got))
	require.NotNil(t, got.Identities)
	return got.Identities
}

func TestBoardIdentitiesSeparateHostsAndSources(t *testing.T) {
	c := board.NewBoardCache()
	h := setupBoardRouter(c, nil)
	defer h.Close()
	h.cfg.AgentBoard.Team = "panemux"
	h.cfg.SSHConnections = map[string]config.SSHConnection{"build": {}}
	at := time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)
	c.RecordStatus("worker@ssh:build", board.Status{State: "something-new", Summary: "reported"})
	c.AppendMessage(board.Row{Host: "local", Team: "panemux", From: "worker", To: "recipient-only", At: at})
	c.AppendMessage(board.Row{Host: "local", Team: "panemux", From: board.SystemID, To: "worker", At: at})
	got := readIdentities(t, h)
	require.Len(t, got, 2)
	local := got["worker@local"]
	assert.Equal(t, "worker", local.Name)
	assert.Equal(t, "local", local.Host)
	assert.Equal(t, "panemux", local.Team)
	assert.Equal(t, "unreported", local.Source)
	assert.Nil(t, local.State)
	assert.Nil(t, local.PaneID)
	assert.Equal(t, at, *local.LastMessageAt)
	remote := got["worker@ssh:build"]
	assert.Equal(t, "ssh:build", remote.Host)
	assert.Equal(t, "self_report", remote.Source)
	require.NotNil(t, remote.State)
	assert.Equal(t, "something-new", *remote.State)
	require.NotNil(t, remote.SelfReport)
	assert.Equal(t, "reported", remote.SelfReport.Summary)
	assert.Nil(t, remote.StatusSince)
}

func TestBoardIdentitiesTaskPriorityAndFallback(t *testing.T) {
	c := board.NewBoardCache()
	h := setupBoardRouter(c, nil)
	defer h.Close()
	h.cfg.AgentBoard.Team = "panemux"
	h.cfg.Workspaces.Items = nil
	h.cfg.Layout = config.LayoutNode{Children: []config.LayoutChild{{Pane: &config.PaneConfig{ID: "pane-a",
		Type: "local"}}}}
	c.RecordStatus("pane-a@local", board.Status{State: "working"})
	since := time.Date(2026, 11, 15, 1, 0, 0, 0, time.UTC)
	observed := since.Add(time.Minute)
	task := tasks.Task{ID: "task",
		Host:        "",
		State:       tasks.StateWait,
		WaitingFor:  "review",
		StatusSince: &since,
		Location: tasks.Location{Kind: tasks.LocationOutside,
			PaneID: "pane-a"}}
	result := tasks.HostResult{Name: "", Status: tasks.HostOK, CollectedAt: &observed}
	h.recordBoardTasks(h.beginBoardTaskCollection(), result, []tasks.Task{task})
	got := readIdentities(t, h)["pane-a@local"]
	assert.Equal(t, "task", got.Source)
	require.NotNil(t, got.State)
	assert.Equal(t, "wait", *got.State)
	assert.Equal(t, "review", *got.WaitingFor)
	assert.Equal(t, since, *got.StatusSince)
	assert.Equal(t, observed, *got.TaskObservedAt)
	assert.Equal(t, "pane-a", *got.PaneID)
	assert.Equal(t, "working", got.SelfReport.State)
	h.recordBoardTasks(h.beginBoardTaskCollection(), result, []tasks.Task{task, task})
	assert.Equal(t, "self_report", readIdentities(t, h)["pane-a@local"].Source)
	h.recordBoardTasks(h.beginBoardTaskCollection(), result, []tasks.Task{task})
	h.recordBoardTasks(h.beginBoardTaskCollection(), tasks.HostResult{Name: "", Status: tasks.HostError}, nil)
	assert.Equal(t, "self_report", readIdentities(t, h)["pane-a@local"].Source)
}

func TestBoardBroadcastRejectsNULBeforeCallingRelay(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(context.Context, []string, string) ([]string, error) {
		t.Fatal("must reject before send")
		return nil, nil
	})
	defer h.Close()
	rr := postBoardBroadcast(t,
		setupRouterWithHandler(h),
		boardBroadcastRequest{To: []string{"pane-a",
			"worker@local"},
			Body: "a\x00b"})
	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
}

func TestBoardIdentitiesAmbiguousTaskWithoutReportIsUnreported(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	defer h.Close()
	task := tasks.Task{State: tasks.StateBusy, Location: tasks.Location{Kind: tasks.LocationOutside, PaneID: "main"}}
	h.recordBoardTasks(h.beginBoardTaskCollection(), tasks.HostResult{Status: tasks.HostOK}, []tasks.Task{task, task})
	got := readIdentities(t, h)
	require.Contains(t, got, "main@local")
	assert.Equal(t, "unreported", got["main@local"].Source)
}

func TestBoardIdentitiesMatchOnlyCurrentPaneOnSameHost(t *testing.T) {
	for _, tc := range []struct {
		name, kind, paneType, taskHost, paneHost, session, paneSession, claim string
		state                                                                 tasks.State
		want                                                                  bool
	}{
		{name: "outside local", kind: "outside", paneType: "local", claim: "main", want: true},
		{name: "outside remote",
			kind:     "outside",
			paneType: "ssh",
			taskHost: "build",
			paneHost: "build",
			claim:    "main",
			want:     true},
		{name: "wrong host", kind: "outside", paneType: "ssh", taskHost: "other", paneHost: "build", claim: "main"},
		{name: "wrong pane", kind: "outside", paneType: "local", claim: "other"},
		{name: "outside cannot claim tmux", kind: "outside", paneType: "tmux", claim: "main"},
		{name: "tmux local", kind: "tmux", paneType: "tmux", session: "work", paneSession: "work", want: true},
		{name: "tmux remote",
			kind:        "tmux",
			paneType:    "ssh_tmux",
			taskHost:    "build",
			paneHost:    "build",
			session:     "work",
			paneSession: "work",
			want:        true},
		{name: "tmux wrong session", kind: "tmux", paneType: "tmux", session: "other", paneSession: "work"},
		{name: "tmux empty session", kind: "tmux", paneType: "tmux"},
		{name: "tmux cannot claim shell", kind: "tmux", paneType: "local", session: "work", paneSession: "work"},
		{name: "daemon", kind: "daemon", paneType: "local", claim: "main"},
		{name: "stopped", kind: "outside", paneType: "local", claim: "main", state: tasks.StateStop},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := setupBoardRouter(board.NewBoardCache(), nil)
			defer h.Close()
			h.cfg.SSHConnections = map[string]config.SSHConnection{"build": {}, "other": {}}
			p := h.cfg.Layout.Children[0].Pane
			p.Type, p.Connection, p.TmuxSession = tc.paneType, tc.paneHost, tc.paneSession
			state := tc.state
			if state == "" {
				state = tasks.StateIdle
			}
			task := tasks.Task{Host: tc.taskHost,
				State: state,
				Location: tasks.Location{Kind: tasks.LocationKind(tc.kind),
					PaneID:      tc.claim,
					TmuxSession: tc.session}}
			h.recordBoardTasks(h.beginBoardTaskCollection(),
				tasks.HostResult{Name: tc.taskHost, Status: tasks.HostOK}, []tasks.Task{task})
			got := readIdentities(t, h)
			if tc.want {
				require.Len(t, got, 1)
				for _, e := range got {
					assert.Equal(t, "task", e.Source)
				}
			} else {
				assert.Empty(t, got)
			}
		})
	}
}

type identityMessageClient struct {
	host string
	rows []board.Row
	sent []string
	fail bool
}

func (c *identityMessageClient) HostID() string { return c.host }
func (c *identityMessageClient) Since(context.Context, string, string, int) ([]board.Row, error) {
	return c.rows, nil
}
func (c *identityMessageClient) Send(_ context.Context, _ string, _ string, to, body string) error {
	if c.fail {
		return errors.New("unreachable")
	}
	c.sent = append(c.sent, to+":"+body)
	return nil
}

func TestBoardIdentitiesUseOnlyRelayAcceptedSendersAndBoundedHistory(t *testing.T) {
	c := board.NewBoardCache()
	h := setupBoardRouter(c, nil)
	defer h.Close()
	h.cfg.AgentBoard.Team = "panemux"
	local := &identityMessageClient{host: "local", rows: []board.Row{
		{ID: "1", Team: "panemux", From: "worker", To: "recipient-only"},
		{ID: "2", Team: "panemux", From: "copy@ssh:build", To: "worker"},
		{ID: "3", Team: "panemux", From: "remote-pane", To: "worker"},
		{ID: "4", Team: "other", From: "wrong-team", To: "worker"},
		{ID: "5", Team: "panemux", From: board.SystemID, To: "worker"},
		{ID: "6", Team: "panemux", From: "bad-target", To: "_system@local"},
	}}
	relay := board.NewRelay(c,
		board.RelayConfig{Team: "panemux",
			PaneHosts: map[string]string{"remote-pane": "ssh:build"},
			Clients: map[string]board.MessageClient{"local": local,
				"ssh:build": &identityMessageClient{host: "ssh:build"}}})
	require.NoError(t, relay.Poll(context.Background()))
	got := readIdentities(t, h)
	require.Len(t, got, 1)
	require.Contains(t, got, "worker@local")
	for range 2000 {
		c.AppendMessage(board.Row{Host: "local", Team: "panemux", From: "newer", To: "worker"})
	}
	got = readIdentities(t, h)
	require.Len(t, got, 1)
	require.Contains(t, got, "newer@local")
}

func TestBoardBroadcastQualifiedRecipientsAndPartialFailure(t *testing.T) {
	for _, tc := range []struct {
		name      string
		to        []string
		delivered []string
		code      int
		calls     int
		fail      bool
	}{
		{name: "qualified without pane",
			to: []string{"worker@local",
				"worker@ssh:build",
				"main"},
			code: 200,
			delivered: []string{"worker@local",
				"worker@ssh:build",
				"main"},
			calls: 3},

		{name: "invalid mixed", to: []string{"worker@local", "bad@@local"}, code: 422},
		{name: "unknown host", to: []string{"worker@local", "worker@ssh:missing"}, code: 422},
		{name: "reserved", to: []string{"worker@local", "_system@local"}, code: 422},
		{name: "bare nonpane", to: []string{"worker@local", "worker"}, code: 422},
		{name: "partial failure",
			to: []string{"worker@local",
				"worker@ssh:build",
				"main"},
			fail:      true,
			code:      502,
			delivered: []string{"worker@local"},
			calls:     1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			local := &identityMessageClient{host: "local"}
			remote := &identityMessageClient{host: "ssh:build", fail: tc.fail}
			c := board.NewBoardCache()
			relay := board.NewRelay(c,
				board.RelayConfig{Team: "panemux",
					PaneHosts: map[string]string{"main": "local"},
					Clients: map[string]board.MessageClient{"local": local,
						"ssh:build": remote}})
			h := NewHandler(defaultTestConfig(), session.NewManager(), c, relay)
			defer h.Close()
			rr := postBoardBroadcast(t, setupRouterWithHandler(h), boardBroadcastRequest{To: tc.to, Body: "hello"})
			require.Equal(t, tc.code, rr.Code)
			assert.Len(t, append(local.sent, remote.sent...), tc.calls)
			if tc.code != 422 {
				var resp boardBroadcastResponse
				require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
				assert.Equal(t, tc.delivered, resp.Delivered)
			} else {
				assert.Contains(t, rr.Body.String(), "recipient")
			}
		})
	}
}

func TestBoardIdentitiesExistingCollectorsPublishAndClearObservations(t *testing.T) {
	for _, live := range []bool{false, true} {
		t.Run(strconv.FormatBool(live), func(t *testing.T) {
			h := setupBoardRouter(board.NewBoardCache(), nil)
			defer h.Close()
			h.cfg.Layout.Children[0].Pane.Type = "tmux"
			h.cfg.Layout.Children[0].Pane.TmuxSession = "task-7c21"
			fail := false
			h.SetTaskService(tasks.New(tasks.Options{RunLocal: func(context.Context, string) ([]byte, error) {
				if fail {
					return nil, errors.New("collection failed")
				}
				return taskCollection("sample", "/workspace/user/project"), nil
			}}))
			collect := func() {
				if live {
					_, _ = h.TaskEventSource().CollectHostLive(context.Background(), "")
				} else {
					h.GetTasks(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
				}
			}
			collect()
			got := readIdentities(t, h)
			require.Contains(t, got, "main@local")
			assert.Equal(t, "task", got["main@local"].Source)
			fail = true
			collect()
			assert.Empty(t, readIdentities(t, h))
		})
	}
}

func TestBoardIdentitiesLegacyReportsDetectionAndTimestamps(t *testing.T) {
	c := board.NewBoardCache()
	h := setupBoardRouter(c, nil)
	defer h.Close()
	h.cfg.AgentBoard.Team = "panemux"
	c.RecordStatus("main", board.Status{State: "idle"})
	c.RecordStatus("orphan", board.Status{State: "ignored"})
	c.RecordStatus("bad@@local", board.Status{})
	c.RecordStatus(board.SystemID, board.Status{})
	c.RecordStatus("silent@local", board.Status{})
	c.RecordParticipation("main", "local", "codex", false)
	c.RecordParticipation("removed", "local", "codex", false)
	c.RecordParticipation("missing-host", "", "codex", false)
	c.RecordParticipation("main", "ssh:other", "codex", false)
	at := time.Date(2026, 11, 15, 2, 0, 0, 0, time.UTC)
	for _, when := range []time.Time{at, at.Add(-time.Minute), at.Add(time.Minute)} {
		c.AppendMessage(board.Row{Host: "local", Team: "panemux", From: "main", At: when})
	}
	for _, row := range []board.Row{
		{Host: "local", Team: "panemux", From: "copy@local"},
		{Host: "local", Team: "other", From: "other"},
		{Host: "invalid", Team: "panemux", From: "wrong-host"},
		{Host: "local", Team: "panemux", From: "bad name"},
	} {
		c.AppendMessage(row)
	}
	got := readIdentities(t, h)
	require.Len(t, got, 2)
	assert.Equal(t, at.Add(time.Minute), *got["main@local"].LastMessageAt)
	assert.Equal(t, "main", *got["main@local"].PaneID)
	silent := got["silent@local"]
	assert.Equal(t, "self_report", silent.Source)
	assert.Nil(t, silent.State)
}

func TestBoardIdentitiesDetectedAndIneligiblePanes(t *testing.T) {
	for _, tc := range []struct {
		name, paneID, kind, connection string
		want                           bool
	}{
		{name: "detected local", paneID: "main", kind: "local", want: true},
		{name: "nonidentity pane id", paneID: "name@local", kind: "local"},
		{name: "ssh config only", paneID: "main", kind: "ssh", connection: "not-configured"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := board.NewBoardCache()
			h := setupBoardRouter(c, nil)
			defer h.Close()
			*h.cfg.Layout.Children[0].Pane = config.PaneConfig{ID: tc.paneID, Type: tc.kind, Connection: tc.connection}
			c.RecordParticipation(tc.paneID, "local", "codex", false)
			got := readIdentities(t, h)
			if tc.want {
				require.Contains(t, got, "main@local")
				assert.Equal(t, "unreported", got["main@local"].Source)
			} else {
				assert.Empty(t, got)
			}
			task := tasks.Task{State: tasks.StateBusy, Location: tasks.Location{Kind: tasks.LocationOutside, PaneID: tc.paneID}}
			h.recordBoardTasks(h.beginBoardTaskCollection(), tasks.HostResult{Status: tasks.HostOK}, []tasks.Task{task})
			got = readIdentities(t, h)
			if tc.want {
				assert.Equal(t, "task", got["main@local"].Source)
			} else {
				assert.Empty(t, got)
			}
		})
	}
}
