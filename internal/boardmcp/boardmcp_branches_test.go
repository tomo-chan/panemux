package boardmcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Branches `make coverage-blocks` reported as never entered. Issue #195.
//
// This package is the whole surface the command center's LLM subprocess can
// reach — three tools over a stdio JSON-RPC transport — so its error handling
// is what stands between a malformed model-generated call and the subprocess
// losing its connection to panemux entirely. Most of what was unentered is
// exactly that: the arms that turn bad input into an answer rather than a
// dropped stream.

// ── The stdio transport ──────────────────────────────────────────────────────

// A blank line between requests is skipped rather than answered with a parse
// error. Any writer that terminates its last line and then flushes produces
// one, and answering it would put an unsolicited error response on a stream
// the client correlates by id.
func TestServeSkipsBlankLinesBetweenRequests(t *testing.T) {
	responses := serveLines(t, &fakeBoardAPIClient{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize"}`,
		"",
		"   ",
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	)

	require.Len(t, responses, 2, "the blank lines must produce no response of their own")
	assert.EqualValues(t, 1, responses[0]["id"])
	assert.EqualValues(t, 2, responses[1]["id"])
}

// failingWriter fails every write, standing in for the subprocess's stdout
// pipe closing because `claude` exited while a response was in flight.
type failingWriter struct{ err error }

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

// A broken stdout ends the loop with the reason, rather than spinning through
// the remaining requests writing into a pipe nobody is reading.
func TestServeReportsAWriteFailure(t *testing.T) {
	in := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n")

	err := NewServer(&fakeBoardAPIClient{}).Serve(
		context.Background(), in, failingWriter{err: errors.New("broken pipe")},
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing mcp response")
	assert.Contains(t, err.Error(), "broken pipe")
}

// failingReader fails after handing back whatever it was primed with, so the
// scanner reports a read error rather than a clean end of input.
type failingReader struct {
	err       error
	remaining string
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.remaining == "" {
		return 0, f.err
	}
	n := copy(p, f.remaining)
	f.remaining = f.remaining[n:]
	return n, nil
}

// A read failure is distinguished from end-of-input: Serve returns nil when
// the client simply closed, and an error when the stream broke.
func TestServeReportsAReadFailure(t *testing.T) {
	r := &failingReader{
		remaining: `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n",
		err:       errors.New("connection reset"),
	}
	var out strings.Builder

	err := NewServer(&fakeBoardAPIClient{}).Serve(context.Background(), r, &out)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading mcp request")
	assert.Contains(t, err.Error(), "connection reset")
	assert.Contains(t, out.String(), `"id":1`, "the request read before the failure was still answered")
}

// ── Dispatch ─────────────────────────────────────────────────────────────────

// `notifications/initialized` normally arrives with no id and is answered with
// nothing at all. A client that sends it as a request anyway gets a response,
// not a "method not found": the method is known, it just has no result.
//
// The response carries neither key. `jsonrpcResponse.Result` is `omitempty`,
// so a nil result is omitted rather than serialized as `"result": null` —
// worth pinning by key presence rather than by value, since a lookup that
// returns nil cannot tell an absent key from a null one. Noted rather than
// changed: JSON-RPC 2.0 asks a response to carry exactly one of result/error,
// and this shape carries neither, but that is an implementation question and
// this branch changes no implementation.
func TestInitializedSentAsARequestIsAnsweredWithoutAResultOrAnError(t *testing.T) {
	responses := serveLines(t, &fakeBoardAPIClient{},
		`{"jsonrpc":"2.0","id":7,"method":"notifications/initialized"}`,
	)

	require.Len(t, responses, 1)
	assert.EqualValues(t, 7, responses[0]["id"])
	assert.Equal(t, "2.0", responses[0]["jsonrpc"])
	assert.NotContains(t, responses[0], "error", "the method is known, so this is not method-not-found")
	assert.NotContains(t, responses[0], "result", "and it has no result to report")
}

// ── Malformed tool calls ─────────────────────────────────────────────────────

// The three shapes a model can get wrong, each answered as a JSON-RPC error
// rather than crashing the handler or returning a tool result that looks like
// success.
func TestMalformedToolCallsReturnInvalidParams(t *testing.T) {
	const invalidParams = -32602

	tests := []struct {
		name string
		line string
		want string
	}{
		{
			name: "tools/call params are not an object",
			line: `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":"not-an-object"}`,
			want: "invalid params:",
		},
		{
			name: "board_messages arguments are not an object",
			line: `{"jsonrpc":"2.0","id":2,"method":"tools/call",` +
				`"params":{"name":"board_messages","arguments":"not-an-object"}}`,
			want: "invalid arguments:",
		},
		{
			name: "board_broadcast arguments are not an object",
			line: `{"jsonrpc":"2.0","id":3,"method":"tools/call",` +
				`"params":{"name":"board_broadcast","arguments":42}}`,
			want: "invalid arguments:",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := serveLines(t, &fakeBoardAPIClient{}, tt.line)

			require.Len(t, responses, 1)
			rpcErr, ok := responses[0]["error"].(map[string]any)
			require.True(t, ok, "expected a JSON-RPC error, got %v", responses[0])
			assert.EqualValues(t, invalidParams, rpcErr["code"])
			assert.Contains(t, rpcErr["message"], tt.want)
			assert.Nil(t, responses[0]["result"])
		})
	}
}

// A failure from panemux's own REST API is reported as a tool result with
// isError set, naming the tool. It is deliberately not a JSON-RPC error: the
// call itself was well-formed, so the model should see the failure as
// something it can react to rather than a protocol fault.
func TestClientFailuresSurfaceAsToolErrorsNamingTheTool(t *testing.T) {
	tests := []struct {
		name   string
		client *fakeBoardAPIClient
		line   string
		want   string
	}{
		{
			name:   "board_messages",
			client: &fakeBoardAPIClient{messagesErr: errors.New("relay unreachable")},
			line: `{"jsonrpc":"2.0","id":1,"method":"tools/call",` +
				`"params":{"name":"board_messages","arguments":{"since":5}}}`,
			want: "board_messages: relay unreachable",
		},
		{
			name:   "board_broadcast",
			client: &fakeBoardAPIClient{broadcastErr: errors.New("no board-enabled panes")},
			line: `{"jsonrpc":"2.0","id":2,"method":"tools/call",` +
				`"params":{"name":"board_broadcast","arguments":{"to":["pane-a"],"body":"hi"}}}`,
			want: "board_broadcast: no board-enabled panes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			responses := serveLines(t, tt.client, tt.line)

			require.Len(t, responses, 1)
			assert.Nil(t, responses[0]["error"], "a backend failure is a tool result, not a protocol error")
			result, ok := responses[0]["result"].(map[string]any)
			require.True(t, ok, "expected a tool result, got %v", responses[0])
			assert.Equal(t, true, result["isError"])
			content, ok := result["content"].([]any)
			require.True(t, ok)
			require.Len(t, content, 1)
			text, ok := content[0].(map[string]any)["text"].(string)
			require.True(t, ok)
			assert.Equal(t, tt.want, text)
		})
	}
}

// jsonrpcError doubles as an ordinary error so callTool can return one
// directly; Error() is what any %v or errors.Is path would print.
func TestJSONRPCErrorImplementsError(t *testing.T) {
	var err error = &jsonrpcError{Code: -32602, Message: "invalid arguments: bad json"}

	assert.Equal(t, "invalid arguments: bad json", err.Error())
}

// ── The REST client ──────────────────────────────────────────────────────────

// The base URL arrives from the environment (PANEMUX_BOARD_BASE_URL, read by
// runBoardMCPServer), so a value http.NewRequest cannot parse is real input
// rather than a hypothetical. It fails at request construction, before
// anything is dialed — this says nothing about whether the host is loopback,
// which is not checked here.
func TestHTTPBoardAPIClientUnbuildableRequestIsReported(t *testing.T) {
	client := NewHTTPBoardAPIClient("://not-a-url", "token")

	raw, err := client.Status(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "building request")
	assert.Nil(t, raw)
}

// panemux not listening is the ordinary failure here: the command center
// subprocess outlives a server shutdown by however long its query takes.
func TestHTTPBoardAPIClientUnreachableServerIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	baseURL := server.URL
	server.Close() // nothing is listening on that port any more

	client := NewHTTPBoardAPIClient(baseURL, "token")

	raw, err := client.Status(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "calling panemux board api")
	assert.Nil(t, raw)
}

// A response whose body ends early is reported as a read failure rather than
// parsed as a short but valid body.
func TestHTTPBoardAPIClientTruncatedBodyIsReported(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "64")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"panes":`))
		// Returning without writing the promised 64 bytes makes net/http close
		// the connection, which the reader sees as an unexpected EOF.
	}))
	defer server.Close()

	client := NewHTTPBoardAPIClient(server.URL, "token")

	raw, err := client.Status(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading panemux board api response")
	assert.Nil(t, raw)
}

// A trailing slash on the base URL must not produce a double slash in the
// request path, which some routers treat as a different route entirely.
func TestHTTPBoardAPIClientTrimsATrailingSlashFromTheBaseURL(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := NewHTTPBoardAPIClient(server.URL+"/", "token")

	raw, err := client.Status(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "/api/board/status", gotPath)
	assert.Equal(t, json.RawMessage(`{}`), raw)
}
