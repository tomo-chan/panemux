package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
)

// hostNameSuffix is the random part hostTmuxSessionName appends.
var hostNameSuffix = `-[0-9a-f]{8}$`

func TestHostTmuxSessionName(t *testing.T) {
	cases := []struct {
		connection string
		wantPrefix string
	}{
		{connection: "gpu-box", wantPrefix: "gpu-box"},
		{connection: "dev_server.v2", wantPrefix: "dev_server.v2"},
		{connection: "user@host:22", wantPrefix: "user-host-22"},
		{connection: "a b/c", wantPrefix: "a-b-c"},
		// Every character outside the set becomes one '-', a multi-byte one
		// included.
		{connection: "開発機", wantPrefix: "---"},
		{connection: "x;rm -rf ~", wantPrefix: "x-rm--rf--"},
		{connection: "'quote'", wantPrefix: "-quote-"},
	}
	for _, tc := range cases {
		t.Run(tc.connection, func(t *testing.T) {
			got, err := hostTmuxSessionName(tc.connection)
			require.NoError(t, err)
			assert.Regexp(t, "^"+regexp.QuoteMeta(tc.wantPrefix)+hostNameSuffix, got)
			assert.True(t, session.IsValidTmuxSessionName(got), got)
		})
	}
}

func TestHostTmuxSessionName_EachCallIsNew(t *testing.T) {
	first, err := hostTmuxSessionName("gpu-box")
	require.NoError(t, err)
	second, err := hostTmuxSessionName("gpu-box")
	require.NoError(t, err)
	assert.NotEqual(t, first, second)
}

type hostTerminalEnv struct {
	err    error
	h      *Handler
	timers *fakeAttachTimers
	panes  []config.PaneConfig
	byID   map[string]*mockSession
	mu     sync.Mutex
}

func newHostTerminalEnv(t *testing.T) *hostTerminalEnv {
	t.Helper()
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{
		"gpu-box":      {Host: "gpu.invalid"},
		"user@host:22": {Host: "odd.invalid"},
	}
	e := &hostTerminalEnv{timers: &fakeAttachTimers{}, byID: map[string]*mockSession{}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.boardAttaches.afterFunc = e.timers.afterFunc
	h.createSession = func(pane *config.PaneConfig, conns map[string]config.SSHConnection) (session.Session, error) {
		e.mu.Lock()
		defer e.mu.Unlock()
		if _, ok := conns[pane.Connection]; !ok {
			return nil, errors.New("the connection was not handed to the session")
		}
		e.panes = append(e.panes, *pane)
		if e.err != nil {
			return nil, e.err
		}
		sess := newMockSession(pane.ID)
		sess.typ = session.Type(pane.Type)
		e.byID[pane.ID] = sess
		return sess, nil
	}
	t.Cleanup(h.Close)
	e.h = h
	return e
}

func (e *hostTerminalEnv) created() []config.PaneConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]config.PaneConfig(nil), e.panes...)
}

func (e *hostTerminalEnv) post(t *testing.T, body string) *httptest.ResponseRecorder {
	t.Helper()
	return postJSON(t, e.h, "/api/hosts/terminal", body, nil)
}

func (e *hostTerminalEnv) delete(t *testing.T, path string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, path, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	setupRouterWithHandler(e.h).ServeHTTP(rec, req)
	return rec
}

func decodeHostTerminal(t *testing.T, rec *httptest.ResponseRecorder) hostTerminalResponse {
	t.Helper()
	var got hostTerminalResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got), rec.Body.String())
	return got
}

func TestPostHostSessionName(t *testing.T) {
	e := newHostTerminalEnv(t)

	rec := postJSON(t, e.h, "/api/hosts/session-name", `{"connection":"user@host:22"}`, nil)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var got hostSessionNameResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	assert.Regexp(t, "^user-host-22"+hostNameSuffix, got.TmuxSession)
	assert.Empty(t, e.created(), "naming a session starts nothing")
	assert.Empty(t, e.h.manager.List())
}

func TestPostHostSessionName_Refusals(t *testing.T) {
	cases := []struct {
		headers map[string]string
		name    string
		body    string
		want    int
	}{
		{name: "invalid body", body: `{"connection":`, want: http.StatusBadRequest},
		{name: "unknown field", body: `{"connection":"gpu-box","type":"ssh"}`, want: http.StatusBadRequest},
		{name: "the panemux host", body: `{"connection":""}`, want: http.StatusBadRequest},
		{name: "not in ssh_connections", body: `{"connection":"elsewhere"}`, want: http.StatusNotFound},
		{
			name: "cross-site", body: `{"connection":"gpu-box"}`,
			headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusForbidden,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newHostTerminalEnv(t)
			rec := postJSON(t, e.h, "/api/hosts/session-name", tc.body, tc.headers)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
		})
	}
}

func TestPostHostTerminal_SSHOpensATerminalOutsideTheLayout(t *testing.T) {
	e := newHostTerminalEnv(t)
	layoutBefore, err := json.Marshal(e.h.cfg.Layout)
	require.NoError(t, err)

	rec := e.post(t, `{"connection":"gpu-box","type":"ssh"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decodeHostTerminal(t, rec)
	assert.True(t, strings.HasPrefix(got.SessionID, boardAttachIDPrefix), got.SessionID)
	assert.Empty(t, got.TmuxSession)

	require.Len(t, e.created(), 1)
	assert.Equal(t, config.PaneConfig{
		ID: got.SessionID, Type: string(session.TypeSSH), Connection: "gpu-box", Title: "gpu-box",
	}, e.created()[0])
	_, ok := e.h.manager.Get(got.SessionID)
	assert.True(t, ok, "the session is served by /ws/{id} like any other")

	layoutAfter, err := json.Marshal(e.h.cfg.Layout)
	require.NoError(t, err)
	assert.JSONEq(t, string(layoutBefore), string(layoutAfter))

	sessions := httptest.NewRecorder()
	setupRouterWithHandler(e.h).ServeHTTP(sessions, httptest.NewRequest(http.MethodGet, "/api/sessions", nil))
	assert.NotContains(t, sessions.Body.String(), got.SessionID, "a host terminal is not a pane")
}

func TestPostHostTerminal_SSHTmuxUsesTheNameItWasGiven(t *testing.T) {
	e := newHostTerminalEnv(t)

	rec := e.post(t, `{"connection":"gpu-box","type":"ssh_tmux","tmux_session":"gpu-box-0123abcd"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decodeHostTerminal(t, rec)
	assert.Equal(t, "gpu-box-0123abcd", got.TmuxSession)
	require.Len(t, e.created(), 1)
	assert.Equal(t, config.PaneConfig{
		ID: got.SessionID, Type: string(session.TypeSSHTmux), Connection: "gpu-box",
		Title: "gpu-box", TmuxSession: "gpu-box-0123abcd",
	}, e.created()[0])
}

func TestPostHostTerminal_SSHTmuxWithoutANameGetsAGeneratedOne(t *testing.T) {
	e := newHostTerminalEnv(t)

	rec := e.post(t, `{"connection":"user@host:22","type":"ssh_tmux"}`)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	got := decodeHostTerminal(t, rec)
	assert.Regexp(t, "^user-host-22"+hostNameSuffix, got.TmuxSession)
	require.Len(t, e.created(), 1)
	assert.Equal(t, got.TmuxSession, e.created()[0].TmuxSession)
}

func TestPostHostTerminal_EveryRequestOpensANewTerminal(t *testing.T) {
	e := newHostTerminalEnv(t)

	first := decodeHostTerminal(t, e.post(t, `{"connection":"gpu-box","type":"ssh"}`))
	second := decodeHostTerminal(t, e.post(t, `{"connection":"gpu-box","type":"ssh"}`))
	assert.NotEqual(t, first.SessionID, second.SessionID)
	for _, id := range []string{first.SessionID, second.SessionID} {
		_, ok := e.h.manager.Get(id)
		assert.True(t, ok, id)
	}
	assert.Len(t, e.timers.live(), 2)
}

func TestPostHostTerminal_Refusals(t *testing.T) {
	cases := []struct {
		create  error
		headers map[string]string
		name    string
		body    string
		want    int
	}{
		{name: "invalid body", body: `{"connection":`, want: http.StatusBadRequest},
		{name: "unknown field", body: `{"connection":"gpu-box","type":"ssh","cwd":"/"}`, want: http.StatusBadRequest},
		{name: "the panemux host", body: `{"connection":"","type":"ssh"}`, want: http.StatusBadRequest},
		{name: "not in ssh_connections", body: `{"connection":"elsewhere","type":"ssh"}`, want: http.StatusNotFound},
		{name: "missing type", body: `{"connection":"gpu-box"}`, want: http.StatusBadRequest},
		{name: "local type", body: `{"connection":"gpu-box","type":"local"}`, want: http.StatusBadRequest},
		{name: "tmux type", body: `{"connection":"gpu-box","type":"tmux"}`, want: http.StatusBadRequest},
		{
			name: "ssh with a tmux session", body: `{"connection":"gpu-box","type":"ssh","tmux_session":"a"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "tmux session name with a quote", body: `{"connection":"gpu-box","type":"ssh_tmux","tmux_session":"a'b"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "tmux session name with a space", body: `{"connection":"gpu-box","type":"ssh_tmux","tmux_session":"a b"}`,
			want: http.StatusBadRequest,
		},
		{
			name: "session cannot be created", body: `{"connection":"gpu-box","type":"ssh"}`,
			create: errors.New("dial: refused"), want: http.StatusBadGateway,
		},
		{
			name: "cross-site", body: `{"connection":"gpu-box","type":"ssh"}`,
			headers: map[string]string{"Sec-Fetch-Site": "cross-site"}, want: http.StatusForbidden,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newHostTerminalEnv(t)
			e.err = tc.create
			rec := postJSON(t, e.h, "/api/hosts/terminal", tc.body, tc.headers)
			assert.Equal(t, tc.want, rec.Code, rec.Body.String())
			assert.Empty(t, e.h.manager.List(), "a refused terminal leaves no session")
			assert.Empty(t, e.timers.live())
		})
	}
}

func TestHostTerminal_GracePeriod(t *testing.T) {
	e := newHostTerminalEnv(t)
	got := decodeHostTerminal(t, e.post(t, `{"connection":"gpu-box","type":"ssh"}`))
	live := e.timers.live()
	require.Len(t, live, 1)
	assert.Equal(t, boardAttachGrace, live[0].d)

	_, _, unsubscribe, ok := e.h.manager.Subscribe(got.SessionID)
	require.True(t, ok)
	assert.Empty(t, e.timers.live(), "nothing expires while a WebSocket reads the terminal")
	unsubscribe()
	require.Len(t, e.timers.live(), 1)

	e.timers.fire()
	_, ok = e.h.manager.Get(got.SessionID)
	assert.False(t, ok)
	assert.True(t, e.byID[got.SessionID].closed)
}

func TestDeleteHostTerminal(t *testing.T) {
	e := newHostTerminalEnv(t)
	got := decodeHostTerminal(t, e.post(t, `{"connection":"gpu-box","type":"ssh_tmux"}`))

	rec := e.delete(t, "/api/hosts/terminal/"+got.SessionID, nil)
	assert.Equal(t, http.StatusNoContent, rec.Code, rec.Body.String())
	_, ok := e.h.manager.Get(got.SessionID)
	assert.False(t, ok)
	assert.True(t, e.byID[got.SessionID].closed, "closing ends the tmux client; the remote session is left alone")
	assert.Empty(t, e.timers.live())

	assert.Equal(t, http.StatusNotFound, e.delete(t, "/api/hosts/terminal/"+got.SessionID, nil).Code, "already gone")

	e.h.manager.Add(newMockSession("pane-1"))
	assert.Equal(t, http.StatusNotFound, e.delete(t, "/api/hosts/terminal/pane-1", nil).Code,
		"a pane is not a host terminal")
	_, ok = e.h.manager.Get("pane-1")
	assert.True(t, ok)
}

func TestDeleteHostTerminal_CrossSiteIsRefused(t *testing.T) {
	e := newHostTerminalEnv(t)
	got := decodeHostTerminal(t, e.post(t, `{"connection":"gpu-box","type":"ssh"}`))

	rec := e.delete(t, "/api/hosts/terminal/"+got.SessionID, map[string]string{"Sec-Fetch-Site": "cross-site"})
	assert.Equal(t, http.StatusForbidden, rec.Code)
	_, ok := e.h.manager.Get(got.SessionID)
	assert.True(t, ok)
}

// A task's attach and a host terminal share the registry, but each DELETE
// route removes only its own kind.
func TestHostTerminalAndTaskAttach_EachRouteDeletesOnlyItsOwnKind(t *testing.T) {
	e := newAttachEnv(t, attachLocal())
	e.h.cfg.SSHConnections = map[string]config.SSHConnection{"gpu-box": {Host: "gpu.invalid"}}
	e.h.createSession = func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		return newMockSession(pane.ID), nil
	}
	attach := decodeAttach(t, e.post(t, "local:claude:in-tmux"))
	terminal := decodeHostTerminal(t, postJSON(t, e.h, "/api/hosts/terminal", `{"connection":"gpu-box","type":"ssh"}`, nil))

	assert.Equal(t, http.StatusNotFound, e.delete(t, terminal.SessionID).Code,
		"the task route leaves a host terminal alone")
	rec := httptest.NewRecorder()
	setupRouterWithHandler(e.h).ServeHTTP(rec,
		httptest.NewRequest(http.MethodDelete, "/api/hosts/terminal/"+attach.SessionID, nil))
	assert.Equal(t, http.StatusNotFound, rec.Code, "the host route leaves a task's attach alone")

	for _, id := range []string{attach.SessionID, terminal.SessionID} {
		_, ok := e.h.manager.Get(id)
		assert.True(t, ok, id)
	}
}
