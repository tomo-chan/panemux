package ws

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"net/http/httptest"

	"panemux/internal/commandcenter"
)

type fakeBoardCommandRunner struct {
	queryFn func(ctx context.Context, prompt string) (<-chan commandcenter.Event, error)
}

func (f *fakeBoardCommandRunner) Query(ctx context.Context, prompt string) (<-chan commandcenter.Event, error) {
	return f.queryFn(ctx, prompt)
}

func setupBoardCommandWSServer(runner boardCommandRunner, token string) *httptest.Server {
	h := NewBoardCommandHandler(runner, token)
	r := chi.NewRouter()
	r.Get("/ws/board-command", h.ServeHTTP)
	return httptest.NewServer(r)
}

func boardCommandWSURL(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/board-command"
}

//nolint:wrapcheck // test helper, callers assert on err directly
func dialBoardCommand(t *testing.T, srv *httptest.Server, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	header := http.Header{}
	if token != "" {
		header.Set("Sec-WebSocket-Protocol", token)
	}
	return websocket.DefaultDialer.Dial(boardCommandWSURL(srv), header)
}

func closedEventsChan(events ...commandcenter.Event) <-chan commandcenter.Event {
	ch := make(chan commandcenter.Event, len(events))
	for _, ev := range events {
		ch <- ev
	}
	close(ch)
	return ch
}

func TestBoardCommandWS_MissingToken_Rejected(t *testing.T) {
	srv := setupBoardCommandWSServer(&fakeBoardCommandRunner{}, "secret")
	defer srv.Close()

	_, resp, err := dialBoardCommand(t, srv, "")

	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestBoardCommandWS_WrongToken_Rejected(t *testing.T) {
	srv := setupBoardCommandWSServer(&fakeBoardCommandRunner{}, "secret")
	defer srv.Close()

	_, resp, err := dialBoardCommand(t, srv, "wrong-token")

	require.Error(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
}

func TestBoardCommandWS_CorrectToken_StreamsLineAndDoneEvents(t *testing.T) {
	runner := &fakeBoardCommandRunner{queryFn: func(_ context.Context, prompt string) (<-chan commandcenter.Event, error) {
		assert.Equal(t, "hi", prompt)
		return closedEventsChan(
			commandcenter.Event{Type: commandcenter.EventLine, Raw: json.RawMessage(`{"type":"result"}`)},
			commandcenter.Event{Type: commandcenter.EventDone},
		), nil
	}}
	srv := setupBoardCommandWSServer(runner, "secret")
	defer srv.Close()

	conn, resp, err := dialBoardCommand(t, srv, "secret")
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck
	assert.Equal(t, "secret", resp.Header.Get("Sec-WebSocket-Protocol"))

	require.NoError(t, conn.WriteJSON(boardCommandRequest{Prompt: "hi"}))

	var f1, f2 boardCommandFrame
	require.NoError(t, conn.ReadJSON(&f1))
	require.NoError(t, conn.ReadJSON(&f2))

	assert.Equal(t, "line", f1.Type)
	assert.JSONEq(t, `{"type":"result"}`, string(f1.Raw))
	assert.Equal(t, "done", f2.Type)
}

func TestBoardCommandWS_Busy_SendsBusyFrame(t *testing.T) {
	runner := &fakeBoardCommandRunner{queryFn: func(context.Context, string) (<-chan commandcenter.Event, error) {
		return nil, commandcenter.ErrBusy
	}}
	srv := setupBoardCommandWSServer(runner, "secret")
	defer srv.Close()

	conn, _, err := dialBoardCommand(t, srv, "secret")
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	require.NoError(t, conn.WriteJSON(boardCommandRequest{Prompt: "hi"}))

	var frame boardCommandFrame
	require.NoError(t, conn.ReadJSON(&frame))
	assert.Equal(t, "busy", frame.Type)
}

func TestBoardCommandWS_RunnerError_SendsErrorFrame(t *testing.T) {
	runner := &fakeBoardCommandRunner{queryFn: func(context.Context, string) (<-chan commandcenter.Event, error) {
		return nil, errors.New("mcp config build failed")
	}}
	srv := setupBoardCommandWSServer(runner, "secret")
	defer srv.Close()

	conn, _, err := dialBoardCommand(t, srv, "secret")
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	require.NoError(t, conn.WriteJSON(boardCommandRequest{Prompt: "hi"}))

	var frame boardCommandFrame
	require.NoError(t, conn.ReadJSON(&frame))
	assert.Equal(t, "error", frame.Type)
	assert.Contains(t, frame.Message, "mcp config build failed")
}

func TestBoardCommandWS_QueryEventError_ForwardedAsErrorFrame(t *testing.T) {
	runner := &fakeBoardCommandRunner{queryFn: func(context.Context, string) (<-chan commandcenter.Event, error) {
		ev := commandcenter.Event{Type: commandcenter.EventError, Err: "claude exited with error: exit status 1"}
		return closedEventsChan(ev), nil
	}}
	srv := setupBoardCommandWSServer(runner, "secret")
	defer srv.Close()

	conn, _, err := dialBoardCommand(t, srv, "secret")
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	require.NoError(t, conn.WriteJSON(boardCommandRequest{Prompt: "hi"}))

	var frame boardCommandFrame
	require.NoError(t, conn.ReadJSON(&frame))
	assert.Equal(t, "error", frame.Type)
	assert.Contains(t, frame.Message, "exit status 1")
}

func TestBoardCommandWS_InvalidJSONRequest_SendsErrorFrame(t *testing.T) {
	srv := setupBoardCommandWSServer(&fakeBoardCommandRunner{}, "secret")
	defer srv.Close()

	conn, _, err := dialBoardCommand(t, srv, "secret")
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	require.NoError(t, conn.WriteMessage(websocket.TextMessage, []byte("not json")))

	var frame boardCommandFrame
	require.NoError(t, conn.ReadJSON(&frame))
	assert.Equal(t, "error", frame.Type)
}

func TestBoardCommandWS_MultiplePromptsSequentially(t *testing.T) {
	calls := 0
	runner := &fakeBoardCommandRunner{queryFn: func(_ context.Context, prompt string) (<-chan commandcenter.Event, error) {
		calls++
		return closedEventsChan(commandcenter.Event{Type: commandcenter.EventDone}), nil
	}}
	srv := setupBoardCommandWSServer(runner, "secret")
	defer srv.Close()

	conn, _, err := dialBoardCommand(t, srv, "secret")
	require.NoError(t, err)
	defer conn.Close() //nolint:errcheck

	require.NoError(t, conn.WriteJSON(boardCommandRequest{Prompt: "first"}))
	var f1 boardCommandFrame
	require.NoError(t, conn.ReadJSON(&f1))
	assert.Equal(t, "done", f1.Type)

	require.NoError(t, conn.WriteJSON(boardCommandRequest{Prompt: "second"}))
	var f2 boardCommandFrame
	require.NoError(t, conn.ReadJSON(&f2))
	assert.Equal(t, "done", f2.Type)

	assert.Equal(t, 2, calls)
}

func TestBoardCommandWS_ClientDisconnectMidStream_DrainsEventsWithoutBlocking(t *testing.T) {
	// A large pre-buffered, already-closed channel standing in for a long
	// Runner stream. The handler must drain every event even once writes to
	// the (now-disconnected) client start failing — otherwise the Runner's
	// own goroutine would stay blocked sending into a channel no one reads,
	// permanently wedging its busy flag.
	ch := make(chan commandcenter.Event, 300)
	for i := 0; i < 200; i++ {
		ch <- commandcenter.Event{Type: commandcenter.EventLine, Raw: json.RawMessage(`{"n":1}`)}
	}
	ch <- commandcenter.Event{Type: commandcenter.EventDone}
	close(ch)

	runner := &fakeBoardCommandRunner{queryFn: func(context.Context, string) (<-chan commandcenter.Event, error) {
		return ch, nil
	}}
	srv := setupBoardCommandWSServer(runner, "secret")
	defer srv.Close()

	conn, _, err := dialBoardCommand(t, srv, "secret")
	require.NoError(t, err)

	require.NoError(t, conn.WriteJSON(boardCommandRequest{Prompt: "hi"}))
	// Read exactly one frame then disconnect abruptly without draining the rest.
	var frame boardCommandFrame
	require.NoError(t, conn.ReadJSON(&frame))
	require.NoError(t, conn.Close())

	require.Eventually(t, func() bool {
		return len(ch) == 0
	}, 2*time.Second, 10*time.Millisecond, "handler must drain all events even after the client disconnects")
}
