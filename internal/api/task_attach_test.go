package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

// attachCollection holds a claude task in the tmux session "review-api", one
// in a tmux session whose name a pane cannot attach to, and one outside tmux.
func attachCollection() []byte {
	return []byte(strings.Join([]string{
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"in-tmux","cwd":"/workspace/user/project","status":"waiting","statusUpdatedAt":990000}`,
		"::file 9.json",
		`{"pid":9,"sessionId":"odd-tmux","cwd":"/workspace/user/project","status":"busy","statusUpdatedAt":990000}`,
		"::file 11.json",
		`{"pid":11,"sessionId":"outside","cwd":"/workspace/user/project","status":"busy","statusUpdatedAt":990000}`,
		"::section ps",
		"6 1 zsh",
		"7 6 claude",
		"8 1 zsh",
		"9 8 claude",
		"11 1 claude",
		"::section tmux",
		"6 review-api",
		"8 name with spaces",
		"::section cwd",
		"::section transcripts",
		"::end",
	}, "\n") + "\n")
}

// fakeAttachTimers stands in for time.AfterFunc so the grace period is
// driven by the test, never by the wall clock.
type fakeAttachTimers struct {
	timers []*fakeAttachTimer
	mu     sync.Mutex
}

type fakeAttachTimer struct {
	fn      func()
	d       time.Duration
	stopped bool
}

func (f *fakeAttachTimers) afterFunc(d time.Duration, fn func()) boardAttachTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	timer := &fakeAttachTimer{fn: fn, d: d}
	f.timers = append(f.timers, timer)
	return timer
}

func (t *fakeAttachTimer) Stop() bool {
	was := !t.stopped
	t.stopped = true
	return was
}

// live returns the timers neither stopped nor fired.
func (f *fakeAttachTimers) live() []*fakeAttachTimer {
	f.mu.Lock()
	defer f.mu.Unlock()
	var live []*fakeAttachTimer
	for _, timer := range f.timers {
		if !timer.stopped {
			live = append(live, timer)
		}
	}
	return live
}

// fire runs every live timer, as if the grace period had passed.
func (f *fakeAttachTimers) fire() {
	for _, timer := range f.live() {
		timer.stopped = true
		timer.fn()
	}
}

type attachCall struct{ id, title, connection, tmuxSession string }

type attachEnv struct {
	err     error
	h       *Handler
	timers  *fakeAttachTimers
	created map[string]*mockSession
	// gate, when set, holds every attach until it is closed.
	gate  chan struct{}
	calls []attachCall
	mu    sync.Mutex
}

func newAttachEnv(t *testing.T, runLocal func(context.Context, string) ([]byte, error)) *attachEnv {
	t.Helper()
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"dev-server": {Host: "dev.invalid"}}
	e := &attachEnv{timers: &fakeAttachTimers{}, created: map[string]*mockSession{}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.SetTaskService(tasks.New(tasks.Options{
		Hosts:    h.taskHostNames,
		Dial:     func(string) (tasks.Conn, error) { return nil, errors.New("i/o timeout") },
		RunLocal: runLocal,
	}))
	h.boardAttaches.afterFunc = e.timers.afterFunc
	h.createTmuxAttach = func(
		id, title, connection, tmuxSession string, _ map[string]config.SSHConnection,
	) (session.Session, error) {
		if e.gate != nil {
			<-e.gate
		}
		e.mu.Lock()
		defer e.mu.Unlock()
		e.calls = append(e.calls, attachCall{id, title, connection, tmuxSession})
		if e.err != nil {
			return nil, e.err
		}
		sess := newMockSession(id)
		sess.typ = session.TypeTmux
		e.created[id] = sess
		return sess, nil
	}
	t.Cleanup(h.Close)
	e.h = h
	return e
}

func (e *attachEnv) attachCalls() []attachCall {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]attachCall(nil), e.calls...)
}

func (e *attachEnv) post(t *testing.T, id string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(map[string]string{"id": id})
	require.NoError(t, err)
	return postJSON(t, e.h, "/api/tasks/attach", string(body), nil)
}

func (e *attachEnv) delete(t *testing.T, sessionID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/attach/"+sessionID, nil)
	setupRouterWithHandler(e.h).ServeHTTP(rec, req)
	return rec
}

func decodeAttach(t *testing.T, rec *httptest.ResponseRecorder) taskAttachResponse {
	t.Helper()
	var got taskAttachResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), rec.Body.String())
	return got
}

func attachLocal() func(context.Context, string) ([]byte, error) {
	return func(context.Context, string) ([]byte, error) { return attachCollection(), nil }
}

func TestPostTaskAttach_AttachesToTheTasksTmuxSessionWithoutTouchingTheLayout(t *testing.T) {
	e := newAttachEnv(t, attachLocal())
	layoutBefore, err := json.Marshal(e.h.cfg.Layout)
	require.NoError(t, err)

	rec := e.post(t, "local:claude:in-tmux")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decodeAttach(t, rec)
	assert.True(t, strings.HasPrefix(got.SessionID, boardAttachIDPrefix), got.SessionID)
	assert.Equal(t, "review-api", got.TmuxSession)

	require.Len(t, e.attachCalls(), 1)
	assert.Equal(t, attachCall{got.SessionID, "review-api", "", "review-api"}, e.attachCalls()[0],
		"the target comes from the server's own collection, not from the request")
	_, ok := e.h.manager.Get(got.SessionID)
	assert.True(t, ok, "the session is served by /ws/{id} like any other")

	layoutAfter, err := json.Marshal(e.h.cfg.Layout)
	require.NoError(t, err)
	assert.JSONEq(t, string(layoutBefore), string(layoutAfter))

	sessions := httptest.NewRecorder()
	setupRouterWithHandler(e.h).ServeHTTP(sessions, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	assert.NotContains(t, sessions.Body.String(), got.SessionID, "a board attach is not a pane")
}

func TestPostTaskAttach_ReopeningTheSameTaskReturnsTheSameSession(t *testing.T) {
	e := newAttachEnv(t, attachLocal())

	first := e.post(t, "local:claude:in-tmux")
	require.Equal(t, http.StatusCreated, first.Code, first.Body.String())
	second := e.post(t, "local:claude:in-tmux")
	require.Equal(t, http.StatusOK, second.Code, second.Body.String())

	assert.Equal(t, decodeAttach(t, first), decodeAttach(t, second))
	assert.Len(t, e.attachCalls(), 1)
}

func TestPostTaskAttach_ConcurrentRequestsCreateOneSession(t *testing.T) {
	e := newAttachEnv(t, attachLocal())
	e.gate = make(chan struct{})

	const n = 5
	recs := make([]*httptest.ResponseRecorder, n)
	var wg sync.WaitGroup
	for i := range recs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			recs[i] = e.post(t, "local:claude:in-tmux")
		}(i)
	}
	time.Sleep(50 * time.Millisecond) // let every request reach the registry
	close(e.gate)
	wg.Wait()

	ids := map[string]bool{}
	created := 0
	for _, rec := range recs {
		require.Contains(t, []int{http.StatusCreated, http.StatusOK}, rec.Code, rec.Body.String())
		if rec.Code == http.StatusCreated {
			created++
		}
		ids[decodeAttach(t, rec).SessionID] = true
	}
	assert.Equal(t, 1, created)
	assert.Len(t, ids, 1)
	assert.Len(t, e.attachCalls(), 1)
}

func TestPostTaskAttach_AnExitedAttachIsReplaced(t *testing.T) {
	e := newAttachEnv(t, attachLocal())
	first := decodeAttach(t, e.post(t, "local:claude:in-tmux"))
	e.created[first.SessionID].state = session.StateExited

	rec := e.post(t, "local:claude:in-tmux")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	assert.NotEqual(t, first.SessionID, decodeAttach(t, rec).SessionID)
	_, ok := e.h.manager.Get(first.SessionID)
	assert.False(t, ok, "the exited client is removed, not left behind")
}

func TestPostTaskAttach_Refusals(t *testing.T) {
	cases := []struct {
		attach   error
		runLocal func(context.Context, string) ([]byte, error)
		headers  map[string]string
		name     string
		body     string
		want     int
	}{
		{name: "invalid body", body: `{"id":`, want: http.StatusBadRequest},
		{name: "unknown field", body: `{"id":"local:claude:in-tmux","host":"x"}`, want: http.StatusBadRequest},
		{name: "empty id", body: `{"id":""}`, want: http.StatusBadRequest},
		{name: "task gone", body: `{"id":"local:claude:gone"}`, want: http.StatusNotFound},
		{name: "unknown host", body: `{"id":"ssh:nowhere:claude:x"}`, want: http.StatusNotFound},
		{name: "outside tmux", body: `{"id":"local:claude:outside"}`, want: http.StatusConflict},
		{name: "tmux name a pane cannot attach to", body: `{"id":"local:claude:odd-tmux"}`, want: http.StatusConflict},
		{name: "host cannot be collected", body: `{"id":"ssh:dev-server:claude:x"}`, want: http.StatusBadGateway},
		{
			name: "local collection fails", body: `{"id":"local:claude:in-tmux"}`,
			runLocal: func(context.Context, string) ([]byte, error) { return nil, errors.New("boom") },
			want:     http.StatusBadGateway,
		},
		{
			name: "attach fails", body: `{"id":"local:claude:in-tmux"}`,
			attach: errors.New("dial: refused"), want: http.StatusBadGateway,
		},
		{
			name: "cross-site", body: `{"id":"local:claude:in-tmux"}`,
			headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusForbidden,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runLocal := tc.runLocal
			if runLocal == nil {
				runLocal = attachLocal()
			}
			e := newAttachEnv(t, runLocal)
			e.err = tc.attach
			rec := postJSON(t, e.h, "/api/tasks/attach", tc.body, tc.headers)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			assert.Empty(t, e.h.manager.List(), "a refused attach leaves no session")
			assert.Empty(t, e.timers.live())
		})
	}
}

func TestPostTaskAttach_AnSSHTaskAttachesOnItsConnection(t *testing.T) {
	e := newAttachEnv(t, attachLocal())
	e.h.SetTaskService(tasks.New(tasks.Options{
		Hosts: e.h.taskHostNames,
		Dial: func(string) (tasks.Conn, error) {
			return &attachConn{out: attachCollection()}, nil
		},
		RunLocal: func(context.Context, string) ([]byte, error) { return nil, errors.New("must not run") },
	}))

	rec := e.post(t, "ssh:dev-server:claude:in-tmux")
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	require.Len(t, e.attachCalls(), 1)
	assert.Equal(t, "dev-server", e.attachCalls()[0].connection)
	assert.Equal(t, "review-api", e.attachCalls()[0].tmuxSession)
}

type attachConn struct{ out []byte }

func (c *attachConn) Run(context.Context, string, io.Reader) ([]byte, error) { return c.out, nil }
func (c *attachConn) InspectGitContext(context.Context, string) (session.GitContext, error) {
	return session.GitContext{}, nil
}
func (c *attachConn) Ping(context.Context) error { return nil }
func (c *attachConn) Close() error               { return nil }

func TestBoardAttach_GracePeriod(t *testing.T) {
	t.Run("an attach no WebSocket ever reads is destroyed after the grace period", func(t *testing.T) {
		e := newAttachEnv(t, attachLocal())
		got := decodeAttach(t, e.post(t, "local:claude:in-tmux"))
		live := e.timers.live()
		require.Len(t, live, 1)
		assert.Equal(t, boardAttachGrace, live[0].d)

		e.timers.fire()
		_, ok := e.h.manager.Get(got.SessionID)
		assert.False(t, ok)
		assert.True(t, e.created[got.SessionID].closed, "closing the session kills only the board's tmux client")
	})

	t.Run("a connected WebSocket holds it; the last one leaving starts the grace period", func(t *testing.T) {
		e := newAttachEnv(t, attachLocal())
		got := decodeAttach(t, e.post(t, "local:claude:in-tmux"))

		_, _, unsubscribeA, ok := e.h.manager.Subscribe(got.SessionID)
		require.True(t, ok)
		_, _, unsubscribeB, ok := e.h.manager.Subscribe(got.SessionID)
		require.True(t, ok)
		assert.Empty(t, e.timers.live(), "nothing expires while a WebSocket reads the session")

		unsubscribeA()
		assert.Empty(t, e.timers.live(), "one WebSocket is still reading")
		unsubscribeB()
		require.Len(t, e.timers.live(), 1)

		_, _, unsubscribeC, ok := e.h.manager.Subscribe(got.SessionID)
		require.True(t, ok, "a reconnect within the grace period keeps the session")
		assert.Empty(t, e.timers.live())
		unsubscribeC()

		e.timers.fire()
		_, ok = e.h.manager.Get(got.SessionID)
		assert.False(t, ok)
	})

	t.Run("a timer that fires after it was superseded removes nothing", func(t *testing.T) {
		e := newAttachEnv(t, attachLocal())
		got := decodeAttach(t, e.post(t, "local:claude:in-tmux"))
		stale := e.timers.live()[0]

		_, _, unsubscribe, ok := e.h.manager.Subscribe(got.SessionID)
		require.True(t, ok)
		stale.fn() // the timer had already fired when Stop was called
		_, ok = e.h.manager.Get(got.SessionID)
		assert.True(t, ok)
		unsubscribe()
	})

	t.Run("a tmux client that exits while a WebSocket reads it is destroyed after the grace period", func(t *testing.T) {
		e := newAttachEnv(t, attachLocal())
		got := decodeAttach(t, e.post(t, "local:claude:in-tmux"))
		_, stream, unsubscribe, ok := e.h.manager.Subscribe(got.SessionID)
		require.True(t, ok)
		require.Empty(t, e.timers.live())

		require.NoError(t, e.created[got.SessionID].Close()) // tmux exits on its own
		for range stream {
		}
		unsubscribe() // the WebSocket's deferred unsubscribe finds nothing left
		require.Len(t, e.timers.live(), 1, "the exited attach must not be held forever")

		e.timers.fire()
		_, ok = e.h.manager.Get(got.SessionID)
		assert.False(t, ok)
	})

	t.Run("after it expires the task opens a new attach", func(t *testing.T) {
		e := newAttachEnv(t, attachLocal())
		first := decodeAttach(t, e.post(t, "local:claude:in-tmux"))
		e.timers.fire()
		rec := e.post(t, "local:claude:in-tmux")
		require.Equal(t, http.StatusCreated, rec.Code)
		assert.NotEqual(t, first.SessionID, decodeAttach(t, rec).SessionID)
	})
}

func TestDeleteTaskAttach(t *testing.T) {
	e := newAttachEnv(t, attachLocal())
	got := decodeAttach(t, e.post(t, "local:claude:in-tmux"))

	rec := e.delete(t, got.SessionID)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	_, ok := e.h.manager.Get(got.SessionID)
	assert.False(t, ok)
	assert.True(t, e.created[got.SessionID].closed)
	assert.Empty(t, e.timers.live(), "a destroyed attach leaves no timer")

	assert.Equal(t, http.StatusNotFound, e.delete(t, got.SessionID).Code, "already gone")

	e.h.manager.Add(newMockSession("pane-1"))
	assert.Equal(t, http.StatusNotFound, e.delete(t, "pane-1").Code, "a pane is not a board attach")
	_, ok = e.h.manager.Get("pane-1")
	assert.True(t, ok)

	reopened := e.post(t, "local:claude:in-tmux")
	assert.Equal(t, http.StatusCreated, reopened.Code)
}

func TestDeleteTaskAttach_CrossSiteIsRefused(t *testing.T) {
	e := newAttachEnv(t, attachLocal())
	got := decodeAttach(t, e.post(t, "local:claude:in-tmux"))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/api/tasks/attach/"+got.SessionID, nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	setupRouterWithHandler(e.h).ServeHTTP(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
	_, ok := e.h.manager.Get(got.SessionID)
	assert.True(t, ok)
}
