package ws

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/session"
)

// wsMockSession implements session.Session for WebSocket handler tests.
type wsMockSession struct {
	id      string
	out     chan []byte   // data sent to WS client (session output)
	in      chan []byte   // data received from WS client (session input)
	resizes chan [2]uint16
	closed  bool
}

func newWsMock(id string) *wsMockSession {
	return &wsMockSession{
		id:      id,
		out:     make(chan []byte, 64),
		in:      make(chan []byte, 64),
		resizes: make(chan [2]uint16, 8),
	}
}

func (m *wsMockSession) ID() string           { return m.id }
func (m *wsMockSession) Type() session.Type   { return session.TypeLocal }
func (m *wsMockSession) Title() string        { return m.id }
func (m *wsMockSession) State() session.State { return session.StateConnected }

func (m *wsMockSession) Read(p []byte) (int, error) {
	data, ok := <-m.out
	if !ok {
		return 0, io.EOF
	}
	n := copy(p, data)
	return n, nil
}

func (m *wsMockSession) Write(p []byte) (int, error) {
	cp := make([]byte, len(p))
	copy(cp, p)
	m.in <- cp
	return len(p), nil
}

func (m *wsMockSession) Resize(cols, rows uint16) error {
	m.resizes <- [2]uint16{cols, rows}
	return nil
}

func (m *wsMockSession) Close() error {
	if !m.closed {
		m.closed = true
		close(m.out)
	}
	return nil
}

func setupWSServer(mgr *session.Manager) *httptest.Server {
	h := NewHandler(mgr)
	r := chi.NewRouter()
	r.Get("/ws/{sessionID}", h.ServeHTTP)
	return httptest.NewServer(r)
}

func wsURL(srv *httptest.Server, sessionID string) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/" + sessionID
}

func TestWS_NonexistentSession_404(t *testing.T) {
	srv := setupWSServer(session.NewManager())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/ws/missing")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestWS_Connect_ReceivesConnectedStatus(t *testing.T) {
	mgr := session.NewManager()
	sess := newWsMock("s1")
	mgr.Add(sess)

	srv := setupWSServer(mgr)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "s1"), nil)
	require.NoError(t, err)
	defer conn.Close()
	defer sess.Close() // unblocks Read() goroutine so handler exits without 2s wait

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.TextMessage, msgType)
	var msg ControlMessage
	require.NoError(t, json.Unmarshal(data, &msg))
	assert.Equal(t, "status", msg.Type)
	assert.Equal(t, "connected", msg.State)
}

func TestWS_BinaryFrame_WrittenToSession(t *testing.T) {
	mgr := session.NewManager()
	sess := newWsMock("s1")
	mgr.Add(sess)

	srv := setupWSServer(mgr)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "s1"), nil)
	require.NoError(t, err)
	defer conn.Close()
	defer sess.Close()

	// Drain the initial status message
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()

	input := []byte("hello terminal")
	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, input))

	select {
	case got := <-sess.in:
		assert.Equal(t, input, got)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for session input")
	}
}

func TestWS_SessionOutput_ReceivedAsBinary(t *testing.T) {
	mgr := session.NewManager()
	sess := newWsMock("s1")
	mgr.Add(sess)

	srv := setupWSServer(mgr)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "s1"), nil)
	require.NoError(t, err)
	defer conn.Close()
	defer sess.Close()

	// Drain the initial status message
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()

	// Send output from the session side
	sess.out <- []byte("terminal output")

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	msgType, data, err := conn.ReadMessage()
	require.NoError(t, err)
	assert.Equal(t, websocket.BinaryMessage, msgType)
	assert.Equal(t, []byte("terminal output"), data)
}

func TestWS_ResizeMessage_ResizesSession(t *testing.T) {
	mgr := session.NewManager()
	sess := newWsMock("s1")
	mgr.Add(sess)

	srv := setupWSServer(mgr)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "s1"), nil)
	require.NoError(t, err)
	defer conn.Close()
	defer sess.Close()

	// Drain status
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()

	msg := ControlMessage{Type: "resize", Cols: 120, Rows: 40}
	data, _ := json.Marshal(msg)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, data))

	select {
	case size := <-sess.resizes:
		assert.Equal(t, uint16(120), size[0])
		assert.Equal(t, uint16(40), size[1])
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for resize")
	}
}

func TestWS_ResizeWithZeroCols_Ignored(t *testing.T) {
	mgr := session.NewManager()
	sess := newWsMock("s1")
	mgr.Add(sess)

	srv := setupWSServer(mgr)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "s1"), nil)
	require.NoError(t, err)
	defer conn.Close()
	defer sess.Close()

	// Drain status
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()

	// Send resize with cols=0 and rows=0 — should be ignored
	msg := ControlMessage{Type: "resize", Cols: 0, Rows: 0}
	data, _ := json.Marshal(msg)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, data))

	select {
	case size := <-sess.resizes:
		t.Fatalf("unexpected resize received: %v", size)
	case <-time.After(100 * time.Millisecond):
		// expected: no resize was sent
	}
}

func TestWS_InvalidJSON_Ignored(t *testing.T) {
	mgr := session.NewManager()
	sess := newWsMock("s1")
	mgr.Add(sess)

	srv := setupWSServer(mgr)
	defer srv.Close()

	conn, _, err := websocket.DefaultDialer.Dial(wsURL(srv, "s1"), nil)
	require.NoError(t, err)
	defer conn.Close()
	defer sess.Close()

	// Drain status
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	conn.ReadMessage()

	// Invalid JSON should be silently ignored
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("not json at all")))

	// A valid resize sent after the bad frame must still be processed
	msg := ControlMessage{Type: "resize", Cols: 80, Rows: 24}
	data, _ := json.Marshal(msg)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, data))

	select {
	case size := <-sess.resizes:
		assert.Equal(t, uint16(80), size[0])
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for resize after invalid JSON")
	}
}
