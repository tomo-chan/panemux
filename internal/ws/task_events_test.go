package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/taskevents"
	"panemux/internal/tasks"
)

// stubTaskSource is the panemux host alone, answering with one claude task
// waiting for input, and counting its observations.
type stubTaskSource struct {
	observations atomic.Int32
}

func (s *stubTaskSource) Hosts() []string { return nil }

func (s *stubTaskSource) CollectHostLive(context.Context, string) (tasks.HostResult, []tasks.Task) {
	s.observations.Add(1)
	return tasks.HostResult{Status: tasks.HostOK}, []tasks.Task{{
		ID: "local:claude:local-sess", Agent: "claude", SessionID: "local-sess", CWD: "/workspace/user/project",
		State: tasks.StateWait, WaitingFor: "input needed", WaitSignature: "w1-abc", PID: 7,
		Location: tasks.Location{Kind: tasks.LocationNone},
	}}
}

// refuseMarked stands in for internal/api's cross-site rule: it refuses a
// request carrying X-Test-Cross-Site.
func refuseMarked(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("X-Test-Cross-Site") != "" {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return true
	}
	return false
}

// taskEventsServer serves the stream of a publisher over src.
func taskEventsServer(t *testing.T, src *stubTaskSource) *httptest.Server {
	t.Helper()
	publisher := taskevents.New(src, taskevents.Options{Interval: time.Hour})
	ts := httptest.NewServer(NewTaskEventsHandler(publisher, refuseMarked))
	t.Cleanup(func() {
		ts.Close()
		publisher.Close()
	})
	return ts
}

func dialTaskEvents(t *testing.T, ts *httptest.Server, header http.Header) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, resp, err := dialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), header)
	if conn != nil {
		t.Cleanup(func() { _ = conn.Close() })
	}
	return conn, resp, err
}

func readTaskFrame(t *testing.T, conn *websocket.Conn) map[string]any {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	msgType, data, err := conn.ReadMessage()
	require.NoError(t, err)
	require.Equal(t, websocket.TextMessage, msgType)
	var frame map[string]any
	require.NoError(t, json.Unmarshal(data, &frame))
	return frame
}

// The snapshot comes first, then one frame per change: the panemux host
// answering, then its waiting task.
func TestTaskEvents_SnapshotThenChanges(t *testing.T) {
	ts := taskEventsServer(t, &stubTaskSource{})
	conn, _, err := dialTaskEvents(t, ts, nil)
	require.NoError(t, err)

	snap := readTaskFrame(t, conn)
	assert.Equal(t, "snapshot", snap["type"])
	hosts, err := json.Marshal(snap["hosts"])
	require.NoError(t, err)
	assert.JSONEq(t, `[{"name":"","status":"pending"}]`, string(hosts))
	assert.Equal(t, []any{}, snap["tasks"])
	seq := snap["seq"].(float64)

	host := readTaskFrame(t, conn)
	assert.Equal(t, "host", host["type"])
	assert.Equal(t, seq+1, host["seq"])
	assert.Equal(t, snap["epoch"], host["epoch"])
	answered, err := json.Marshal(host["host"])
	require.NoError(t, err)
	assert.JSONEq(t, `{"name":"","status":"ok"}`, string(answered))

	task := readTaskFrame(t, conn)
	assert.Equal(t, "task", task["type"])
	assert.Equal(t, "added", task["op"])
	view := task["task"].(map[string]any)
	assert.Equal(t, "local:claude:local-sess", view["id"])
	assert.Equal(t, "wait", view["state"])
	assert.Equal(t, "w1-abc", view["wait_id"])
	assert.NotContains(t, view, "pid")
}

// A refused request is answered 403 before upgrading and before anything is
// observed, whichever of the two checks refuses it.
func TestTaskEvents_RefusesBeforeObserving(t *testing.T) {
	for name, header := range map[string]http.Header{
		"cross-site rule":    {"X-Test-Cross-Site": {"1"}},
		"foreign origin":     {"Origin": {"https://evil.example"}},
		"unparseable origin": {"Origin": {"://"}},
	} {
		t.Run(name, func(t *testing.T) {
			src := &stubTaskSource{}
			ts := taskEventsServer(t, src)
			conn, resp, err := dialTaskEvents(t, ts, header)
			require.Error(t, err)
			require.Nil(t, conn)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
			time.Sleep(20 * time.Millisecond)
			assert.Zero(t, src.observations.Load(), "no host is observed for a refused request")
		})
	}
}

// The upgrade's Origin check is /ws/{sessionID}'s: the server's own origin
// and a loopback one, as the Vite dev server's proxy sends, are accepted.
func TestTaskEvents_AcceptsOwnAndLoopbackOrigins(t *testing.T) {
	ts := taskEventsServer(t, &stubTaskSource{})
	for _, origin := range []string{ts.URL, "http://localhost:5173"} {
		conn, _, err := dialTaskEvents(t, ts, http.Header{"Origin": {origin}})
		require.NoError(t, err, origin)
		assert.Equal(t, "snapshot", readTaskFrame(t, conn)["type"])
	}
}

// What the client sends is ignored, up to 512 bytes a frame; a larger one
// closes the connection.
func TestTaskEvents_ClientFramesAreIgnoredAndBounded(t *testing.T) {
	ts := taskEventsServer(t, &stubTaskSource{})
	conn, _, err := dialTaskEvents(t, ts, nil)
	require.NoError(t, err)
	readTaskFrame(t, conn)

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 512))))
	assert.Equal(t, "host", readTaskFrame(t, conn)["type"], "a frame within the limit is ignored")
	readTaskFrame(t, conn)

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte(strings.Repeat("x", 513))))
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	_, _, err = conn.ReadMessage()
	require.Error(t, err)
	var netErr net.Error
	assert.False(t, errors.As(err, &netErr) && netErr.Timeout(),
		"the server closes the connection rather than leaving it to time out: %v", err)
}

// A connection whose subscription the publisher closed — it fell behind, or
// the server is shutting down — is closed, so the client reconnects from a
// fresh snapshot.
func TestTaskEvents_ClosedSubscriptionClosesTheConnection(t *testing.T) {
	src := &stubTaskSource{}
	publisher := taskevents.New(src, taskevents.Options{Interval: time.Hour})
	ts := httptest.NewServer(NewTaskEventsHandler(publisher, refuseMarked))
	t.Cleanup(ts.Close)
	conn, _, err := dialTaskEvents(t, ts, nil)
	require.NoError(t, err)
	readTaskFrame(t, conn)

	publisher.Close()

	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	for {
		if _, _, err = conn.ReadMessage(); err != nil {
			break
		}
	}
	var netErr net.Error
	assert.False(t, errors.As(err, &netErr) && netErr.Timeout(), "closed by the server: %v", err)
}

// churningTaskSource answers every observation with a task whose large
// field changes each time, so each observation is one large frame.
// With next set, each observation first waits for a value from it.
type churningTaskSource struct {
	next         chan struct{}
	observations atomic.Int32
}

func (s *churningTaskSource) Hosts() []string { return nil }

func (s *churningTaskSource) CollectHostLive(ctx context.Context, _ string) (tasks.HostResult, []tasks.Task) {
	if s.next != nil {
		select {
		case <-s.next:
		case <-ctx.Done():
		}
	}
	n := s.observations.Add(1)
	return tasks.HostResult{Status: tasks.HostOK}, []tasks.Task{{
		ID: "local:claude:local-sess", Agent: "claude", SessionID: "local-sess",
		CWD:   "/workspace/user/project/" + strings.Repeat(string(rune('a'+n%2)), 64<<10),
		State: tasks.StateBusy, PID: 7, Location: tasks.Location{Kind: tasks.LocationNone},
	}}
}

// handlerGoroutines counts the goroutines running a task event handler: two
// per open connection, the handler and its reader.
func handlerGoroutines() int {
	buf := make([]byte, 1<<22)
	buf = buf[:runtime.Stack(buf, true)]
	n := 0
	for _, g := range strings.Split(string(buf), "\n\n") {
		if strings.Contains(g, "(*TaskEventsHandler).ServeHTTP") {
			n++
		}
	}
	return n
}

// A client that completes the handshake and then never reads stalls only
// its own connection: once its subscription's buffer fills the publisher
// drops it instead of waiting, every other subscriber keeps receiving and
// observation goes on, and closing the stalled connection leaves none of
// its goroutines behind (decision G in docs/DECISIONLOG.md).
func TestTaskEvents_ClientThatStopsReadingStallsOnlyItsOwnConnection(t *testing.T) {
	// The test steps observation one at a time, so the reader never falls
	// behind however slowly it runs.
	src := &churningTaskSource{next: make(chan struct{})}
	publisher := taskevents.New(src, taskevents.Options{Interval: time.Millisecond, Buffer: 4})
	ts := httptest.NewServer(NewTaskEventsHandler(publisher, refuseMarked))
	t.Cleanup(func() {
		ts.Close()
		publisher.Close()
	})
	// drained never reads until the end; closed never reads at all.
	drained, _, err := dialTaskEvents(t, ts, nil)
	require.NoError(t, err)
	closed, _, err := dialTaskEvents(t, ts, nil)
	require.NoError(t, err)
	reader, _, err := dialTaskEvents(t, ts, nil)
	require.NoError(t, err)

	// 300 frames of 64 KiB each is far more than a stalled connection's
	// socket buffers and its subscription's buffer of 4 hold together, so
	// both stalled handlers are blocked writing and both subscriptions
	// overflow long before the reader is done. The reader still receives
	// each observation's frame, and observation goes on.
	readTaskFrame(t, reader) // snapshot
	src.next <- struct{}{}
	assert.Equal(t, "host", readTaskFrame(t, reader)["type"])
	for i := range 300 {
		if i > 0 {
			src.next <- struct{}{}
		}
		readTaskFrame(t, reader)
	}

	// The publisher dropped drained's subscription: reading now yields what
	// was already in flight, then the server closes the connection, while
	// observation keeps producing frames it is no longer sent.
	require.NoError(t, drained.SetReadDeadline(time.Now().Add(10*time.Second)))
	got := 0
	for {
		if _, _, err = drained.ReadMessage(); err != nil {
			break
		}
		got++
	}
	var netErr net.Error
	require.False(t, errors.As(err, &netErr) && netErr.Timeout(), "closed by the server: %v", err)
	assert.Less(t, got, 300, "the dropped connection was sent no frame after its subscription closed")

	// Closing the connection that never read unblocks its handler's write.
	require.NoError(t, closed.Close())
	assert.Eventually(t, func() bool { return handlerGoroutines() == 2 }, 5*time.Second,
		10*time.Millisecond, "only the reader's handler and its reader goroutine remain")
	require.NoError(t, reader.Close())
	assert.Eventually(t, func() bool { return handlerGoroutines() == 0 }, 5*time.Second,
		10*time.Millisecond, "no handler goroutine outlives its connection")
}
