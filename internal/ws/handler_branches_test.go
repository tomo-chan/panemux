package ws

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/session"
)

// The WebSocket handler has no caller to return an error to: a pane's
// terminal stream either keeps flowing or it does not, and every failure it
// meets is either logged or silently ends a goroutine. These tests cover the
// arms that make that choice.
//
// Several of them call forwardTerminalOutput directly with a *client-side*
// *websocket.Conn. That is the only way to reach its write-failure arms
// deterministically: it takes the concrete type rather than the package's own
// messageWriter interface, and closing a conn locally makes every subsequent
// WriteMessage fail with no timing involved — unlike closing the peer, which
// races the kernel's send buffer.

// captureWSLog redirects the standard logger for the duration of a test.
//
// It restores the previous writer rather than nil: session pump goroutines
// from other tests in this package log while it is swapped, and log.Printf
// against a nil writer panics rather than being a no-op.
func captureWSLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// registerWSSession puts a mock session in a manager and serves it.
//
// Closing the session is not optional bookkeeping: Manager.Add starts a pump
// goroutine that blocks in the mock's Read until its out channel closes, so
// without this the goroutine outlives the test and accumulates across -count
// runs.
func registerWSSession(t *testing.T, id string) (*httptest.Server, *wsMockSession) {
	t.Helper()
	mgr := session.NewManager()
	sess := newWsMock(id)
	mgr.Add(sess)
	srv := setupWSServer(mgr)
	t.Cleanup(func() {
		srv.Close()
		mgr.CloseAll()
	})
	return srv, sess
}

// dialDiscardingPeer stands up a WebSocket server that upgrades and then
// reads until the client goes away, and returns the client end of the
// connection. Writes on it are real WebSocket writes; nothing inspects them.
func dialDiscardingPeer(t *testing.T) *websocket.Conn {
	t.Helper()
	var peer = websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := peer.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer c.Close() //nolint:errcheck
		for {
			if _, _, err := c.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)

	conn, _, err := websocket.DefaultDialer.Dial("ws"+srv.URL[len("http"):], nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// A plain HTTP GET reaches the handler, passes the session lookup and the
// subscribe, and then fails the upgrade — the shape of a browser (or a probe)
// hitting the WS path without the upgrade headers. It must be logged rather
// than left silent: from the operator's side an unupgradable request and a
// pane that simply never connects look identical.
func TestServeHTTPLogsAFailedUpgrade(t *testing.T) {
	logs := captureWSLog(t)
	srv, _ := registerWSSession(t, "s1")

	resp, err := http.Get(srv.URL + "/ws/s1") //nolint:noctx // a local httptest server
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"the upgrade fails, and the session lookup before it must have succeeded to get this far")
	assert.Contains(t, logs.String(), "ws upgrade error for session s1",
		"the pane id has to be in the line, or an operator cannot tell which pane failed")
}

// An empty chunk produces no frame of its own and does not end the stream.
// Asserting only the absence of an empty frame would be satisfied by a
// `break` too, which drops everything after it, so what is pinned here is
// that the chunk *after* the empty one is still written.
//
// This is reached by calling forwardTerminalOutput directly because the
// Manager cannot produce an empty chunk: managedSession.pump publishes only
// when its Read returned n > 0.
func TestForwardTerminalOutputSkipsEmptyChunksAndKeepsForwarding(t *testing.T) {
	conn := dialDiscardingPeer(t)
	sess := newWsMock("s1")
	updates := make(chan []byte, 3)
	updates <- []byte{}
	updates <- []byte("after the empty chunk")
	close(updates)

	NewHandler(session.NewManager()).forwardTerminalOutput(conn, sess, nil, updates, "s1")

	assert.Empty(t, updates, "the loop must run to the end of the channel, not stop at the empty chunk")
}

// A failed write ends the goroutine rather than spinning through the rest of
// the stream into a socket nobody is reading. The undelivered chunk left in
// the channel is what distinguishes returning from continuing: a `continue`
// would drain the channel to the end.
func TestForwardTerminalOutputStopsOnAFailedChunkWrite(t *testing.T) {
	conn := dialDiscardingPeer(t)
	require.NoError(t, conn.Close())
	sess := newWsMock("s1")
	updates := make(chan []byte, 3)
	updates <- []byte("this write fails")
	updates <- []byte("and this one is never attempted")
	close(updates)

	NewHandler(session.NewManager()).forwardTerminalOutput(conn, sess, nil, updates, "s1")

	assert.Len(t, updates, 1, "the loop must stop at the failed write, leaving the rest unread")
}

// The replay preamble is guarded the same way: if the client is already gone,
// the snapshot is not written and the update loop is never entered.
//
// What this protects is the three guards as a set, not each one. A conn that
// is closed fails every write, so removing any single return just lands on
// the next one and the function still stops before the loop — verified, and
// worth saying rather than implying more. Removing all three does reach the
// loop and does fail this test. Telling them apart would need a writer whose
// Nth write fails, which the concrete *websocket.Conn this function takes
// cannot give: writeControlMessage already accepts the package's
// messageWriter interface, and widening this function to it would make the
// recordingConn fixture usable here — an implementation change, so it is
// tracked separately rather than done in a test-only branch.
func TestForwardTerminalOutputStopsWhenTheReplayPreambleCannotBeSent(t *testing.T) {
	conn := dialDiscardingPeer(t)
	require.NoError(t, conn.Close())
	sess := newWsMock("s1")
	updates := make(chan []byte, 2)
	updates <- []byte("never forwarded")
	close(updates)

	NewHandler(session.NewManager()).forwardTerminalOutput(
		conn, sess, []byte("buffered terminal output"), updates, "s1",
	)

	assert.Len(t, updates, 1,
		"a client that cannot receive the replay start must not be streamed to either")
}

// A write to the pane's shell can fail while the WebSocket is perfectly
// healthy — the process died between the state check above and the write.
// It is logged and the connection is kept, because the client has no way to
// act on it and dropping the socket would lose the pane's output too.
func TestHandleWebSocketMessageLogsAFailedSessionWrite(t *testing.T) {
	logs := captureWSLog(t)
	sess := newWsMock("s1")
	sess.writeErr = io.ErrClosedPipe

	NewHandler(session.NewManager()).handleWebSocketMessage(
		nil, sess, "s1", websocket.BinaryMessage, []byte("ls\n"),
	)

	assert.Contains(t, logs.String(), "session s1 write error",
		"the pane id has to be in the line — a write failure is per-pane, not global")
	assert.Contains(t, logs.String(), io.ErrClosedPipe.Error())
}

// A resize failing is not a reason to tear anything down: the pane keeps
// working at its old size. It is logged so a pane that silently stops
// resizing has something behind it.
func TestHandleControlLogsAFailedResize(t *testing.T) {
	logs := captureWSLog(t)
	sess := newWsMock("s1")
	sess.resizeErr = io.ErrClosedPipe

	NewHandler(session.NewManager()).handleControl(
		nil, sess, ControlMessage{Type: controlMessageTypeResize, Cols: 80, Rows: 24},
	)

	assert.Contains(t, logs.String(), "resize error")
	assert.Contains(t, logs.String(), io.ErrClosedPipe.Error())
}

// waitForTerminalPipe bounds how long ServeHTTP blocks on its output
// goroutine after the read loop ends. Both arms matter and they fail in
// opposite directions: without the timeout a pane whose goroutine is wedged
// holds the HTTP handler open for the life of the process, and without the
// done arm every single disconnect costs two seconds.
//
// The bound below pins that a timeout exists, not what it is. A perturbation
// that lengthens the two seconds is caught only by the suite's own timeout
// rather than promptly, because the test has to wait it out — pinning the
// duration quickly would need it injectable. The done arm is the half that
// is verified fast, and it is the one an ordinary disconnect takes.
func TestWaitForTerminalPipeReturnsOnWhicheverComesFirst(t *testing.T) {
	closed := make(chan struct{})
	close(closed)

	start := time.Now()
	waitForTerminalPipe(closed)
	assert.Less(t, time.Since(start), time.Second,
		"a goroutine that has already finished must not cost the timeout")

	start = time.Now()
	waitForTerminalPipe(make(chan struct{}))
	elapsed := time.Since(start)
	assert.GreaterOrEqual(t, elapsed, 2*time.Second,
		"a goroutine that never finishes must be given up on rather than waited for forever")
	assert.Less(t, elapsed, 10*time.Second, "and given up on promptly, not eventually")
}
