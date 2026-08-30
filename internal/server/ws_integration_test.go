package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/commandcenter"
	"panemux/internal/session"
)

// This file is the /ws half of gate G3(a) in docs/quality-gateway.md, and
// issue #191's item (a). api_integration_test.go closed the /api half: every
// HTTP route is driven through the router server.New() actually builds. The
// two WebSocket routes had nothing equivalent — route_table_test.go reported
// that /ws/{sessionID} and /ws/board-command were *registered*, but no test
// ever completed a handshake or exchanged a frame through the production
// wiring.
//
// internal/ws has handler tests, and they are good ones, but they mount the
// handler on a router they build themselves. That is exactly the shape issue
// #178 found: 161 tests passing against a router that does not exist in
// production. A handler test cannot see the middleware stack, the mount
// precedence between /ws/board-command and /ws/{sessionID} (chi resolves the
// literal segment first — asserted below, because nothing else does), or the
// `if commandRunner != nil` block that decides whether the second route
// exists at all.
//
// Everything here goes through httptest.NewServer over a real TCP listener
// rather than httptest.ResponseRecorder: a WebSocket upgrade needs
// http.Hijacker, which a recorder does not implement, so an in-memory
// request cannot reach the handshake this file is about.

// wsReadTimeout bounds every frame read below. Long enough that a loaded CI
// runner does not fail on scheduling alone, short enough that a genuinely
// wedged handler fails the test rather than the package timeout.
const wsReadTimeout = 5 * time.Second

// wsEnv is one hermetic server, published on a real listener.
type wsEnv struct {
	srv  *Server
	http *httptest.Server
	mgr  *session.Manager
	home string
}

// newWSEnv builds a server from the same New() the binary calls. runner may
// be nil, which is what command_center.enabled: false produces — see
// setupCommandCenter in command_center.go.
//
// HOME and XDG_CACHE_HOME point into a temp directory for the same reason
// newAPIEnv does it: nothing here may read or write the developer's own
// files.
func newWSEnv(t *testing.T, runner *commandcenter.Runner) *wsEnv {
	t.Helper()
	return newWSEnvIn(t, t.TempDir(), runner)
}

// newWSEnvIn is newWSEnv with the HOME chosen by the caller. The contract
// fixture for GET /api/board/command/history needs it: that route resolves
// its file path from HOME at request time, so a second env pointing HOME
// somewhere else would have the Runner writing one history file and the
// route reading another — and would have captured an empty list while
// looking like it had captured a real conversation.
func newWSEnvIn(t *testing.T, home string, runner *commandcenter.Runner) *wsEnv {
	t.Helper()

	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))

	mgr := session.NewManager()
	srv := New(testConfigWithToken(integrationToken), mgr, board.NewBoardCache(), nil, runner, emptyFS)
	ts := httptest.NewServer(srv.httpSrv.Handler)

	t.Cleanup(func() {
		ts.Close()
		mgr.CloseAll()
		_ = srv.Shutdown(context.Background())
	})

	return &wsEnv{srv: srv, http: ts, mgr: mgr, home: home}
}

func (e *wsEnv) wsURL(path string) string {
	return "ws" + strings.TrimPrefix(e.http.URL, "http") + path
}

// dial completes a real handshake against the real router. It returns the
// handshake response alongside the connection so a rejected upgrade can be
// asserted on its status code; conn is nil in that case.
func (e *wsEnv) dial(t *testing.T, path string, subprotocols ...string) (*websocket.Conn, *http.Response) {
	t.Helper()

	dialer := websocket.Dialer{
		HandshakeTimeout: wsReadTimeout,
		Subprotocols:     subprotocols,
	}
	conn, resp, err := dialer.Dial(e.wsURL(path), nil)
	if err != nil {
		require.NotNil(t, resp, "dial failed before a response was received: %v", err)
		return nil, resp
	}
	t.Cleanup(func() { _ = conn.Close() })
	return conn, resp
}

// readFrame reads one frame with a deadline, so a handler that never writes
// fails this test rather than the whole package.
func readFrame(t *testing.T, conn *websocket.Conn) (int, []byte) {
	t.Helper()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(wsReadTimeout)))
	msgType, data, err := conn.ReadMessage()
	require.NoError(t, err, "reading a frame from the real router")
	return msgType, data
}

// readControl reads one frame and requires it to be a JSON control message.
func readControl(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()

	msgType, data := readFrame(t, conn)
	require.Equal(t, websocket.TextMessage, msgType, "control frames are text: %s", data)

	var msg map[string]any
	require.NoError(t, json.Unmarshal(data, &msg), "control frame must be JSON: %s", data)
	return msg
}

// ── A fake session, added straight to the real manager ────────────────────
//
// session.Manager.Add takes the session.Session interface, so the pane below
// travels the same Subscribe/replay/pump path a real PTY pane does without a
// real PTY. That matters for what this file is testing: a real local pane's
// output arrives whenever the shell decides to emit it, which would make the
// replay and exit assertions timing-dependent. The pane's realism is
// api_integration_test.go's job (POST /api/sessions creates a genuine one);
// this file's job is the route, the handshake, and the frames.

// wsFakeSession is a session.Session whose output, state and resize calls the
// test drives directly.
//
//nolint:govet // fieldalignment: test-only fields grouped for readability
type wsFakeSession struct {
	id      string
	state   session.State
	out     chan []byte
	in      chan []byte
	resizes chan [2]uint16
	closed  bool
}

func newWSFakeSession(id string) *wsFakeSession {
	return &wsFakeSession{
		id:      id,
		state:   session.StateConnected,
		out:     make(chan []byte, 16),
		in:      make(chan []byte, 16),
		resizes: make(chan [2]uint16, 4),
	}
}

func (s *wsFakeSession) ID() string           { return s.id }
func (s *wsFakeSession) Type() session.Type   { return session.TypeLocal }
func (s *wsFakeSession) Title() string        { return s.id }
func (s *wsFakeSession) State() session.State { return s.state }

func (s *wsFakeSession) Read(p []byte) (int, error) {
	data, ok := <-s.out
	if !ok {
		return 0, io.EOF
	}
	return copy(p, data), nil
}

func (s *wsFakeSession) Write(p []byte) (int, error) {
	s.in <- append([]byte(nil), p...)
	return len(p), nil
}

func (s *wsFakeSession) Resize(cols, rows uint16) error {
	s.resizes <- [2]uint16{cols, rows}
	return nil
}

func (s *wsFakeSession) Close() error {
	if !s.closed {
		s.closed = true
		close(s.out)
	}
	return nil
}

// addFakePane registers a fake pane with the real manager and guarantees it
// is removed, which is what unblocks the handler's own goroutines.
func (e *wsEnv) addFakePane(t *testing.T, id string) *wsFakeSession {
	t.Helper()

	sess := newWSFakeSession(id)
	e.mgr.Add(sess)
	t.Cleanup(func() { _ = e.mgr.Remove(id) })
	return sess
}

// waitForBufferedOutput blocks until the manager's replay buffer for id is
// non-empty. The pump goroutine copies session output into that buffer
// asynchronously, so a test that connects immediately after writing would
// race it — and would then silently assert the *absence* of the replay
// frames it is trying to cover.
func (e *wsEnv) waitForBufferedOutput(t *testing.T, id string) {
	t.Helper()

	require.Eventually(t, func() bool {
		snapshot, _, unsubscribe, ok := e.mgr.Subscribe(id)
		if !ok {
			return false
		}
		unsubscribe()
		return len(snapshot) > 0
	}, wsReadTimeout, 10*time.Millisecond, "the manager never buffered the pane's output")
}

// ── /ws/{sessionID} ───────────────────────────────────────────────────────

func TestWSIntegration_TerminalRoute_UnknownPane_404(t *testing.T) {
	e := newWSEnv(t, nil)

	resp, err := http.Get(e.http.URL + "/ws/missing") //nolint:noctx // httptest server, test-local
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	// chi's own 404 body is "404 page not found"; this one comes from the
	// ws handler, which is what proves the request reached it.
	assert.Contains(t, string(body), "session not found")
}

func TestWSIntegration_TerminalRoute_StreamsBothDirections(t *testing.T) {
	e := newWSEnv(t, nil)
	sess := e.addFakePane(t, "pane-stream")

	conn, resp := e.dial(t, "/ws/pane-stream")
	require.NotNil(t, conn)
	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)

	assert.Equal(t, map[string]any{"type": "status", "state": "connected"}, readControl(t, conn))

	// Server -> browser: terminal output arrives as a binary frame.
	sess.out <- []byte("hello from the pty")
	msgType, data := readFrame(t, conn)
	assert.Equal(t, websocket.BinaryMessage, msgType)
	assert.Equal(t, "hello from the pty", string(data))

	// Browser -> server: a binary frame is keystrokes for the pane.
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("ls -la\r")))
	select {
	case got := <-sess.in:
		assert.Equal(t, "ls -la\r", string(got))
	case <-time.After(wsReadTimeout):
		t.Fatal("the pane never received the keystrokes")
	}

	// Browser -> server: a text frame is a control message. resize is the
	// only one the handler acts on today.
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"resize","cols":120,"rows":40}`)))
	select {
	case got := <-sess.resizes:
		assert.Equal(t, [2]uint16{120, 40}, got)
	case <-time.After(wsReadTimeout):
		t.Fatal("the pane was never resized")
	}
}

func TestWSIntegration_TerminalRoute_ReplaysBufferedOutputOnReconnect(t *testing.T) {
	e := newWSEnv(t, nil)
	sess := e.addFakePane(t, "pane-replay")

	// Output produced while no browser is attached is what the replay
	// buffer exists for: a workspace switch must not show a blank xterm.
	sess.out <- []byte("prompt$ ")
	e.waitForBufferedOutput(t, "pane-replay")

	conn, _ := e.dial(t, "/ws/pane-replay")
	require.NotNil(t, conn)

	assert.Equal(t, map[string]any{"type": "status", "state": "connected"}, readControl(t, conn))
	assert.Equal(t, map[string]any{"type": "replay", "state": "start"}, readControl(t, conn))

	msgType, data := readFrame(t, conn)
	assert.Equal(t, websocket.BinaryMessage, msgType)
	assert.Equal(t, "prompt$ ", string(data))

	assert.Equal(t, map[string]any{"type": "replay", "state": "end"}, readControl(t, conn))
}

func TestWSIntegration_TerminalRoute_ReportsFinalStateWhenThePaneExits(t *testing.T) {
	e := newWSEnv(t, nil)
	sess := e.addFakePane(t, "pane-exit")

	conn, _ := e.dial(t, "/ws/pane-exit")
	require.NotNil(t, conn)
	assert.Equal(t, map[string]any{"type": "status", "state": "connected"}, readControl(t, conn))

	// The shell exiting is the pane's stream ending, which is what the
	// dashboard learns about only through this final status frame.
	sess.state = session.StateExited
	require.NoError(t, sess.Close())

	assert.Equal(t, map[string]any{"type": "status", "state": "exited"}, readControl(t, conn))
}

// The terminal route takes its pane id from the URL, so it would happily
// match "board-command" as a pane id. chi resolves the literal segment
// first, and nothing else in the suite says so — a reordering that broke it
// would turn every command palette connection into a 404 for a pane that
// does not exist.
func TestWSIntegration_BoardCommandRoute_TakesPrecedenceOverThePaneRoute(t *testing.T) {
	e := newWSEnv(t, newFixtureRunner(t, fixtureClaudeScript(t, "")))
	e.addFakePane(t, "board-command")

	_, resp := e.dial(t, "/ws/board-command")

	// No subprotocol was offered, so the board-command handler rejects it.
	// Reaching the pane route instead would have upgraded successfully,
	// because a pane with that id exists.
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

// ── /ws/board-command ─────────────────────────────────────────────────────

// The command center's route is absent, not present-and-rejecting, when no
// runner is configured — see docs/security.md's "Auth token and transport
// encryption". A 404 here and a 401 in the tests below is the difference
// between "there is nothing to probe" and "there is something here".
func TestWSIntegration_BoardCommandRoute_AbsentWhenCommandCenterDisabled(t *testing.T) {
	e := newWSEnv(t, nil)

	resp, err := http.Get(e.http.URL + "/ws/board-command") //nolint:noctx // httptest server, test-local
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusNotFound, resp.StatusCode)

	// Not the handler's own 401 body, and not an upgrade: the route simply
	// is not there. registerFrontend's SPA catch-all only claims GET /*,
	// which /ws/board-command does not match.
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.NotContains(t, string(body), "unauthorized")
}

func TestWSIntegration_BoardCommandRoute_RejectsWrongOrMissingToken(t *testing.T) {
	e := newWSEnv(t, newFixtureRunner(t, fixtureClaudeScript(t, "")))

	for name, subprotocols := range map[string][]string{
		"no subprotocol at all": nil,
		"wrong token":           {"not-the-token"},
		"empty token":           {""},
	} {
		t.Run(name, func(t *testing.T) {
			conn, resp := e.dial(t, "/ws/board-command", subprotocols...)
			assert.Nil(t, conn, "the handshake must not complete")
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestWSIntegration_BoardCommandRoute_AcceptsTheTokenAsASubprotocol(t *testing.T) {
	e := newWSEnv(t, newFixtureRunner(t, fixtureClaudeScript(t, "")))

	conn, resp := e.dial(t, "/ws/board-command", integrationToken)
	require.NotNil(t, conn)

	assert.Equal(t, http.StatusSwitchingProtocols, resp.StatusCode)
	// Echoing the offered subprotocol is required by the WebSocket spec: a
	// browser closes a connection whose handshake response does not name one
	// of the protocols it offered, so dropping this would break the palette
	// even though the token was accepted.
	assert.Equal(t, integrationToken, resp.Header.Get("Sec-WebSocket-Protocol"))
	assert.Equal(t, integrationToken, conn.Subprotocol())
}

func TestWSIntegration_BoardCommandRoute_MalformedPromptGetsAnErrorFrame(t *testing.T) {
	e := newWSEnv(t, newFixtureRunner(t, fixtureClaudeScript(t, "")))

	conn, _ := e.dial(t, "/ws/board-command", integrationToken)
	require.NotNil(t, conn)

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("{not json")))

	frame := readControl(t, conn)
	assert.Equal(t, "error", frame["type"])
	assert.Contains(t, frame["message"], "invalid request")
}

func TestWSIntegration_BoardCommandRoute_StreamsAQueryToCompletion(t *testing.T) {
	e := newWSEnv(t, newFixtureRunner(t, fixtureClaudeScript(t, "")))

	conn, _ := e.dial(t, "/ws/board-command", integrationToken)
	require.NotNil(t, conn)

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(`{"prompt":"which panes are working?"}`)))

	line := readControl(t, conn)
	assert.Equal(t, "line", line["type"])
	assert.NotNil(t, line["raw"], "a line frame carries the subprocess's own stream-json line")

	assert.Equal(t, map[string]any{"type": "done"}, readControl(t, conn))
}

// Two palette connections against one Runner: the second query is refused
// rather than queued, because two concurrent `claude -p --resume <same-id>`
// invocations have no ordering guarantee. See commandcenter.ErrBusy.
func TestWSIntegration_BoardCommandRoute_SecondConcurrentQueryIsBusy(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	release := filepath.Join(dir, "release")

	// The gate makes this deterministic rather than timing-based: the first
	// query blocks inside the subprocess until the test releases it, so the
	// second prompt provably arrives while the first is still in flight.
	gate := "touch " + shellQuote(started) + "\n" +
		"while [ ! -f " + shellQuote(release) + " ]; do sleep 0.02; done\n"
	e := newWSEnv(t, newFixtureRunner(t, fixtureClaudeScript(t, gate)))

	first, _ := e.dial(t, "/ws/board-command", integrationToken)
	require.NotNil(t, first)
	second, _ := e.dial(t, "/ws/board-command", integrationToken)
	require.NotNil(t, second)

	require.NoError(t, first.WriteMessage(websocket.TextMessage, []byte(`{"prompt":"the slow one"}`)))
	require.Eventually(t, func() bool {
		_, err := os.Stat(started)
		return err == nil
	}, wsReadTimeout, 10*time.Millisecond, "the first query's subprocess never started")

	require.NoError(t, second.WriteMessage(websocket.TextMessage, []byte(`{"prompt":"the impatient one"}`)))
	assert.Equal(t, map[string]any{"type": "busy"}, readControl(t, second))

	require.NoError(t, os.WriteFile(release, nil, 0o600))
	assert.Equal(t, "line", readControl(t, first)["type"])
	assert.Equal(t, map[string]any{"type": "done"}, readControl(t, first))
}

// ── A `claude` stand-in ───────────────────────────────────────────────────

// fixtureStreamJSONLine is the one stream-json line the stand-in below
// emits. Its shape is the CLI's, not panemux's: the Runner only requires a
// JSON object per line, and the WS handler forwards it verbatim as a line
// frame's `raw`.
const fixtureStreamJSONLine = `{"type":"assistant","subtype":"text",` +
	`"session_id":"00000000-0000-4000-8000-000000000000",` +
	`"message":{"role":"assistant","content":[{"type":"text","text":"pane-1 is working."}]}}`

// fixtureClaudeScript writes a POSIX-shell stand-in for the `claude` binary
// and returns its path. prelude, when non-empty, is shell run before the
// line is emitted, which is how the busy test above blocks a query.
//
// A stand-in rather than a fake commandFactory because RunnerConfig.NewCommand
// is typed with unexported types (commandFactory, cmdRunner), so only
// internal/commandcenter's own tests can substitute one. That is the right
// boundary for that package — and it means an integration test in this one
// reaches the subprocess path the same way production does, through
// RunnerConfig.ClaudeBin, which docs/security.md already documents as the
// operator's own override.
func fixtureClaudeScript(t *testing.T, prelude string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "claude-stand-in")
	script := "#!/bin/sh\n" + prelude + "printf '%s\\n' " + shellQuote(fixtureStreamJSONLine) + "\n"
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700)) //nolint:gosec // G306: it has to be executable
	return path
}

// shellQuote single-quotes a value for the stand-in script, mirroring
// internal/session's shellQuotePath. Nothing user-supplied reaches it — the
// inputs are this file's own literals and temp paths — but the script is
// still shell, and quoting it is cheaper than reasoning about whether a
// temp path can contain a space.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// newFixtureRunner builds a real commandcenter.Runner — the same type
// server.New() takes in production — pointed at the stand-in binary, with
// every path it persists to inside a temp directory.
func newFixtureRunner(t *testing.T, claudeBin string) *commandcenter.Runner {
	t.Helper()

	dir := t.TempDir()
	mcpPath := filepath.Join(dir, "mcp.json")
	require.NoError(t, os.WriteFile(mcpPath, []byte(`{"mcpServers":{}}`), 0o600))

	return commandcenter.NewRunner(commandcenter.RunnerConfig{
		ClaudeBin:    claudeBin,
		SessionPath:  filepath.Join(dir, "session.json"),
		HistoryPath:  filepath.Join(dir, "history.jsonl"),
		AllowedTools: commandcenter.AllowedTools(),
		BuildMCPConfig: func() (string, func(), error) {
			return mcpPath, func() {}, nil
		},
	})
}
