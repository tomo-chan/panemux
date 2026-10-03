package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/session"
	"panemux/internal/tasks"
)

// waitingCollection is one host's collection with one claude task waiting
// for input.
func waitingCollection(sessionID string) []byte {
	return []byte(strings.Join([]string{
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"` + sessionID + `","cwd":"/workspace/user/project","status":"waiting",` +
			`"waitingFor":"input needed","statusUpdatedAt":990000}`,
		"::section ps",
		"7 1 claude",
		"::end",
	}, "\n") + "\n")
}

// taskEventsServer serves GET /ws/tasks/events from a handler whose panemux
// host answers local, counting the collections it runs.
func taskEventsServer(t *testing.T, local []byte) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	var collections atomic.Int32
	h.tasks = tasks.New(tasks.Options{
		Hosts: h.taskHostNames,
		RunLocal: func(context.Context, string) ([]byte, error) {
			collections.Add(1)
			return local, nil
		},
	})
	ts := httptest.NewServer(http.HandlerFunc(h.GetTaskEvents))
	t.Cleanup(func() {
		ts.Close()
		h.Close()
	})
	return ts, &collections
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
func TestGetTaskEvents_SnapshotThenChanges(t *testing.T) {
	ts, _ := taskEventsServer(t, waitingCollection("local-sess"))
	conn, _, err := dialTaskEvents(t, ts, nil)
	require.NoError(t, err)

	snap := readTaskFrame(t, conn)
	assert.Equal(t, "snapshot", snap["type"])
	assert.Equal(t, []any{map[string]any{"name": "", "status": "pending"}}, snap["hosts"])
	assert.Equal(t, []any{}, snap["tasks"])
	seq := snap["seq"].(float64)

	host := readTaskFrame(t, conn)
	assert.Equal(t, "host", host["type"])
	assert.Equal(t, seq+1, host["seq"])
	assert.Equal(t, snap["epoch"], host["epoch"])
	assert.Equal(t, map[string]any{"name": "", "status": "ok"}, host["host"])

	task := readTaskFrame(t, conn)
	assert.Equal(t, "task", task["type"])
	assert.Equal(t, "added", task["op"])
	view := task["task"].(map[string]any)
	assert.Equal(t, "local:claude:local-sess", view["id"])
	assert.Equal(t, "wait", view["state"])
	assert.Equal(t, "input needed", view["waiting_for"])
	assert.True(t, strings.HasPrefix(view["wait_id"].(string), "w1-"), "a signed wait: %v", view["wait_id"])
	assert.NotContains(t, view, "pid")
}

// A cross-site request is refused before upgrading and before anything is
// collected, whichever of the two checks catches it.
func TestGetTaskEvents_RefusesCrossSiteBeforeCollecting(t *testing.T) {
	for name, header := range map[string]http.Header{
		"cross-site fetch":      {"Sec-Fetch-Site": {secFetchSiteCrossSite}},
		"same-site fetch":       {"Sec-Fetch-Site": {secFetchSiteSameSite}},
		"foreign origin":        {"Origin": {"https://evil.example"}},
		"unparseable origin":    {"Origin": {"://"}},
		"foreign origin + same": {"Origin": {"https://evil.example"}, "Sec-Fetch-Site": {"same-origin"}},
	} {
		t.Run(name, func(t *testing.T) {
			ts, collections := taskEventsServer(t, waitingCollection("local-sess"))
			conn, resp, err := dialTaskEvents(t, ts, header)
			require.Error(t, err)
			require.Nil(t, conn)
			require.NotNil(t, resp)
			assert.Equal(t, http.StatusForbidden, resp.StatusCode)
			time.Sleep(20 * time.Millisecond)
			assert.Zero(t, collections.Load(), "no host is observed for a refused request")
		})
	}
}

// The upgrade's own Origin check runs too: a loopback origin, as the Vite
// dev server's proxy sends, is accepted like on /ws/{sessionID}.
func TestGetTaskEvents_AcceptsOwnAndLoopbackOrigins(t *testing.T) {
	ts, _ := taskEventsServer(t, waitingCollection("local-sess"))
	for _, origin := range []string{ts.URL, "http://localhost:5173"} {
		conn, _, err := dialTaskEvents(t, ts, http.Header{"Origin": {origin}, "Sec-Fetch-Site": {"same-origin"}})
		require.NoError(t, err, origin)
		assert.Equal(t, "snapshot", readTaskFrame(t, conn)["type"])
	}
}

// What the client sends is ignored, up to 512 bytes a frame; a larger one
// closes the connection.
func TestGetTaskEvents_ClientFramesAreIgnoredAndBounded(t *testing.T) {
	ts, _ := taskEventsServer(t, waitingCollection("local-sess"))
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

// SetTaskEventOptions replaces the publisher: the stream then observes on
// the new one's cycle.
func TestSetTaskEventOptions_ReplacesThePublisher(t *testing.T) {
	h := NewHandler(defaultTestConfig(), session.NewManager(), nil, nil)
	var collections atomic.Int32
	h.tasks = tasks.New(tasks.Options{
		Hosts: h.taskHostNames,
		RunLocal: func(context.Context, string) ([]byte, error) {
			collections.Add(1)
			return waitingCollection("local-sess"), nil
		},
	})
	h.SetTaskEventOptions(tasks.PublisherOptions{Interval: time.Millisecond})
	ts := httptest.NewServer(http.HandlerFunc(h.GetTaskEvents))
	t.Cleanup(func() {
		ts.Close()
		h.Close()
	})

	conn, _, err := dialTaskEvents(t, ts, nil)
	require.NoError(t, err)
	readTaskFrame(t, conn)
	assert.Eventually(t, func() bool { return collections.Load() >= 3 }, 5*time.Second, time.Millisecond,
		"observed every millisecond rather than every 5 seconds")
}
