package boardmcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:govet // fieldalignment: test fixture, padding cost is negligible
type fakeBoardAPIClient struct {
	statusRaw    json.RawMessage
	statusErr    error
	messagesRaw  json.RawMessage
	messagesErr  error
	broadcastRaw json.RawMessage
	broadcastErr error

	gotSince        int64
	gotBroadcastTo  []string
	gotBroadcastMsg string
}

func (f *fakeBoardAPIClient) Status(_ context.Context) (json.RawMessage, error) {
	return f.statusRaw, f.statusErr
}

func (f *fakeBoardAPIClient) Messages(_ context.Context, since int64) (json.RawMessage, error) {
	f.gotSince = since
	return f.messagesRaw, f.messagesErr
}

func (f *fakeBoardAPIClient) Broadcast(_ context.Context, to []string, body string) (json.RawMessage, error) {
	f.gotBroadcastTo = to
	f.gotBroadcastMsg = body
	return f.broadcastRaw, f.broadcastErr
}

// serveLines runs Server.Serve against the given request lines and returns
// the parsed response lines in order.
func serveLines(t *testing.T, client BoardAPIClient, lines ...string) []map[string]any {
	t.Helper()
	in := strings.NewReader(strings.Join(lines, "\n") + "\n")
	var out bytes.Buffer

	err := NewServer(client).Serve(context.Background(), in, &out)
	require.NoError(t, err)

	var responses []map[string]any
	scanner := bufio.NewScanner(&out)
	for scanner.Scan() {
		if scanner.Text() == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal(scanner.Bytes(), &m))
		responses = append(responses, m)
	}
	return responses
}

func TestServerInitializeReturnsProtocolVersionAndServerInfo(t *testing.T) {
	resp := serveLines(t, &fakeBoardAPIClient{}, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`)

	require.Len(t, resp, 1)
	result, ok := resp[0]["result"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, protocolVersion, result["protocolVersion"])
	serverInfo, ok := result["serverInfo"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, serverName, serverInfo["name"])
}

func TestServerNotificationsInitializedProducesNoResponse(t *testing.T) {
	resp := serveLines(t, &fakeBoardAPIClient{}, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	assert.Empty(t, resp)
}

func TestServerToolsListReturnsThreeBoardTools(t *testing.T) {
	resp := serveLines(t, &fakeBoardAPIClient{}, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)

	require.Len(t, resp, 1)
	result := resp[0]["result"].(map[string]any) //nolint:errcheck
	tools := result["tools"].([]any)             //nolint:errcheck
	require.Len(t, tools, 3)
	var names []string
	for _, tool := range tools {
		names = append(names, tool.(map[string]any)["name"].(string)) //nolint:errcheck
	}
	assert.ElementsMatch(t, []string{"board_status", "board_messages", "board_broadcast"}, names)
}

func TestServerToolsCallBoardStatusRelaysClientResponse(t *testing.T) {
	client := &fakeBoardAPIClient{statusRaw: json.RawMessage(`{"statuses":{"pane-a":{"state":"working"}}}`)}
	resp := serveLines(t, client,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"board_status","arguments":{}}}`)

	require.Len(t, resp, 1)
	result := resp[0]["result"].(map[string]any) //nolint:errcheck
	assert.Equal(t, false, result["isError"])
	content := result["content"].([]any)                 //nolint:errcheck
	text := content[0].(map[string]any)["text"].(string) //nolint:errcheck
	assert.JSONEq(t, `{"statuses":{"pane-a":{"state":"working"}}}`, text)
}

func TestServerToolsCallBoardMessagesParsesSinceArgument(t *testing.T) {
	client := &fakeBoardAPIClient{messagesRaw: json.RawMessage(`{"messages":[]}`)}
	serveLines(t, client,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"board_messages","arguments":{"since":17}}}`)

	assert.Equal(t, int64(17), client.gotSince)
}

func TestServerToolsCallBoardBroadcastParsesToAndBody(t *testing.T) {
	client := &fakeBoardAPIClient{broadcastRaw: json.RawMessage(`{"delivered":["pane-a"]}`)}
	resp := serveLines(t, client,
		`{"jsonrpc":"2.0","id":5,"method":"tools/call","params":`+
			`{"name":"board_broadcast","arguments":{"to":["pane-a"],"body":"hi"}}}`)

	assert.Equal(t, []string{"pane-a"}, client.gotBroadcastTo)
	assert.Equal(t, "hi", client.gotBroadcastMsg)
	result := resp[0]["result"].(map[string]any) //nolint:errcheck
	assert.Equal(t, false, result["isError"])
}

func TestServerToolsCallClientErrorReturnsIsErrorResult(t *testing.T) {
	client := &fakeBoardAPIClient{statusErr: errors.New("connection refused")}
	resp := serveLines(t, client,
		`{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"board_status","arguments":{}}}`)

	require.Len(t, resp, 1)
	result := resp[0]["result"].(map[string]any) //nolint:errcheck
	assert.Equal(t, true, result["isError"])
	content := result["content"].([]any)                 //nolint:errcheck
	text := content[0].(map[string]any)["text"].(string) //nolint:errcheck
	assert.Contains(t, text, "connection refused")
}

func TestServerToolsCallUnknownToolReturnsJSONRPCError(t *testing.T) {
	resp := serveLines(t, &fakeBoardAPIClient{},
		`{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"nonexistent","arguments":{}}}`)

	require.Len(t, resp, 1)
	require.NotNil(t, resp[0]["error"])
}

func TestServerUnknownMethodReturnsJSONRPCError(t *testing.T) {
	resp := serveLines(t, &fakeBoardAPIClient{}, `{"jsonrpc":"2.0","id":8,"method":"nonexistent/method"}`)

	require.Len(t, resp, 1)
	errObj := resp[0]["error"].(map[string]any) //nolint:errcheck
	assert.Contains(t, errObj["message"], "nonexistent/method")
}

func TestServerMalformedLineReturnsParseError(t *testing.T) {
	resp := serveLines(t, &fakeBoardAPIClient{}, `not json at all`)

	require.Len(t, resp, 1)
	require.NotNil(t, resp[0]["error"])
}

func TestServerProcessesMultipleLinesInOrder(t *testing.T) {
	resp := serveLines(t, &fakeBoardAPIClient{},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`,
	)

	require.Len(t, resp, 2)
	assert.InDelta(t, 1, resp[0]["id"], 0)
	assert.InDelta(t, 2, resp[1]["id"], 0)
}
