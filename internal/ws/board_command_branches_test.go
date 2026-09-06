package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/commandcenter"
)

// This socket is the command palette's only transport, and it is the one
// route in panemux that requires the bearer token. The arms below are what
// stands between a malformed or unexpected client and a palette that stops
// answering — or, worse, one that answers with nothing at all.

// A GET that carries the right subprotocol but not the upgrade headers gets
// past the token check and fails at the upgrade. It has to be logged: the
// dashboard shows a palette that never connects, and without this line there
// is nothing on the server side saying why.
func TestBoardCommandServeHTTPLogsAFailedUpgrade(t *testing.T) {
	logs := captureWSLog(t)
	srv := setupBoardCommandWSServer(&fakeBoardCommandRunner{}, "sample-token")
	defer srv.Close()

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL+"/ws/board-command", nil)
	require.NoError(t, err)
	req.Header.Set("Sec-WebSocket-Protocol", "sample-token")

	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close() //nolint:errcheck

	assert.Equal(t, http.StatusBadRequest, resp.StatusCode,
		"the token was accepted — this is the upgrade failing, not auth")
	assert.Contains(t, logs.String(), "board command ws upgrade error")
}

// The protocol is text frames carrying JSON. A binary frame is skipped rather
// than parsed or answered, and the connection stays open: a client that sends
// one by mistake must still be able to send a prompt afterwards, which is
// what separates skipping from closing.
func TestBoardCommandIgnoresBinaryFramesAndKeepsTheConnection(t *testing.T) {
	runner := &fakeBoardCommandRunner{
		queryFn: func(_ context.Context, _ string) (<-chan commandcenter.Event, error) {
			return closedEventsChan(commandcenter.Event{Type: commandcenter.EventDone}), nil
		},
	}
	srv := setupBoardCommandWSServer(runner, "sample-token")
	defer srv.Close()

	conn, _, err := dialBoardCommand(t, srv, "sample-token")
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	require.NoError(t, conn.WriteMessage(websocket.BinaryMessage, []byte("not a prompt")))
	require.NoError(t, conn.WriteJSON(map[string]string{"prompt": "how is the board"}))

	var frame boardCommandFrame
	require.NoError(t, conn.ReadJSON(&frame),
		"the binary frame must not have closed the socket or consumed the prompt behind it")
	assert.Equal(t, boardCommandFrameTypeDone, frame.Type)
}

// The runner's event types and this socket's frame types are two enumerations
// that have to stay in step. If one gains a member the other does not, the
// default arm is what keeps the client informed rather than silently dropping
// the event — a frame it can display beats a stream that just stops.
func TestUnknownEventTypeBecomesAnErrorFrameNamingIt(t *testing.T) {
	frame := eventToBoardCommandFrame(commandcenter.Event{Type: commandcenter.EventType("thinking")})

	assert.Equal(t, boardCommandFrameTypeError, frame.Type)
	assert.Contains(t, frame.Message, "thinking",
		"the unrecognized type has to be in the message, or nobody can tell which one drifted")
}

// A frame that cannot be encoded is reported as a write failure, which stops
// the stream rather than leaving the caller to write more frames the client
// will never be able to interpret.
//
// json.RawMessage validates on marshal rather than passing bytes through, so
// a line the subprocess emitted that is not valid JSON reaches this arm —
// this is not a hypothetical failure of encoding/json itself.
func TestWriteBoardCommandFrameFailsOnAnUnencodableFrame(t *testing.T) {
	conn := &fakeBoardCommandConn{}

	ok := writeBoardCommandFrame(conn, boardCommandFrame{
		Type: boardCommandFrameTypeLine,
		Raw:  json.RawMessage("not json"),
	})

	assert.False(t, ok)
	assert.Empty(t, conn.writtenFrames, "nothing may be written for a frame that failed to encode")
	assert.Empty(t, conn.deadlines,
		"and the deadline must not be set either — the failure is before the write is attempted")
}
