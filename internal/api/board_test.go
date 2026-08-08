package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/session"
)

type fakeBroadcaster struct { //nolint:govet // fieldalignment: clarity preferred
	calls    int
	lastTo   []string
	lastBody string
	results  []board.BroadcastResult
}

func (f *fakeBroadcaster) Broadcast(_ context.Context, _ string, to []string, body string) []board.BroadcastResult {
	f.calls++
	f.lastTo = to
	f.lastBody = body
	if f.results != nil {
		return f.results
	}
	out := make([]board.BroadcastResult, 0, len(to))
	for _, p := range to {
		out = append(out, board.BroadcastResult{Pane: p})
	}
	return out
}

func newBoardTestHandler(t *testing.T) *Handler {
	t.Helper()
	return NewHandler(config.Default(), session.NewManager())
}

func TestGetBoardStatus_NotConfigured_ReturnsServiceUnavailable(t *testing.T) {
	h := newBoardTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	rr := httptest.NewRecorder()
	h.GetBoardStatus(rr, req)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestGetBoardStatus_ReturnsSnapshot(t *testing.T) {
	h := newBoardTestHandler(t)
	cache := board.NewBoardCache()
	cache.RecordStatus("pane-a", board.Status{State: "working", Branch: "feature/x"})
	h.EnableBoard(cache, &fakeBroadcaster{}, "panemux")

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	rr := httptest.NewRecorder()
	h.GetBoardStatus(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp boardStatusResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Contains(t, resp.Statuses, "pane-a")
	assert.Equal(t, "working", resp.Statuses["pane-a"].State)
	assert.Equal(t, "feature/x", resp.Statuses["pane-a"].Branch)
}

func TestGetBoardStatus_EmptyCache_ReturnsEmptyMap(t *testing.T) {
	h := newBoardTestHandler(t)
	h.EnableBoard(board.NewBoardCache(), &fakeBroadcaster{}, "panemux")

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	rr := httptest.NewRecorder()
	h.GetBoardStatus(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp boardStatusResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Statuses)
}

func TestGetBoardMessages_NotConfigured_ReturnsServiceUnavailable(t *testing.T) {
	h := newBoardTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "/api/board/messages", nil)
	rr := httptest.NewRecorder()
	h.GetBoardMessages(rr, req)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestGetBoardMessages_ReturnsHistoryAndSeq(t *testing.T) {
	h := newBoardTestHandler(t)
	cache := board.NewBoardCache()
	cache.AppendMessage(board.Row{Host: "local", From: "pane-a", To: "pane-b", Body: "hi"})
	cache.AppendMessage(board.Row{Host: "local", From: "pane-b", To: "pane-a", Body: "hi back"})
	h.EnableBoard(cache, &fakeBroadcaster{}, "panemux")

	req := httptest.NewRequest(http.MethodGet, "/api/board/messages", nil)
	rr := httptest.NewRecorder()
	h.GetBoardMessages(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp boardMessagesResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Messages, 2)
	assert.Equal(t, int64(2), resp.Seq)
}

func TestGetBoardMessages_SinceFiltersEarlierRows(t *testing.T) {
	h := newBoardTestHandler(t)
	cache := board.NewBoardCache()
	cache.AppendMessage(board.Row{Body: "one"})
	cache.AppendMessage(board.Row{Body: "two"})
	h.EnableBoard(cache, &fakeBroadcaster{}, "panemux")

	req := httptest.NewRequest(http.MethodGet, "/api/board/messages?since=1", nil)
	rr := httptest.NewRecorder()
	h.GetBoardMessages(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp boardMessagesResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Messages, 1)
	assert.Equal(t, "two", resp.Messages[0].Body)
}

func TestGetBoardMessages_InvalidSince_BadRequest(t *testing.T) {
	h := newBoardTestHandler(t)
	h.EnableBoard(board.NewBoardCache(), &fakeBroadcaster{}, "panemux")

	req := httptest.NewRequest(http.MethodGet, "/api/board/messages?since=not-a-number", nil)
	rr := httptest.NewRecorder()
	h.GetBoardMessages(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

func TestGetBoardMessages_EmptyHistory_ReturnsEmptyList(t *testing.T) {
	h := newBoardTestHandler(t)
	h.EnableBoard(board.NewBoardCache(), &fakeBroadcaster{}, "panemux")

	req := httptest.NewRequest(http.MethodGet, "/api/board/messages", nil)
	rr := httptest.NewRecorder()
	h.GetBoardMessages(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp boardMessagesResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	assert.Empty(t, resp.Messages)
	assert.Equal(t, int64(0), resp.Seq)
}

func TestPostBoardBroadcast_NotConfigured_ReturnsServiceUnavailable(t *testing.T) {
	h := newBoardTestHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/board/broadcast", strings.NewReader(`{"to":["pane-a"],"body":"hi"}`))
	rr := httptest.NewRecorder()
	h.PostBoardBroadcast(rr, req)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestPostBoardBroadcast_SendsToEachTarget(t *testing.T) {
	h := newBoardTestHandler(t)
	fb := &fakeBroadcaster{}
	h.EnableBoard(board.NewBoardCache(), fb, "panemux")

	body := `{"to":["pane-a","pane-b"],"body":"please review"}`
	req := httptest.NewRequest(http.MethodPost, "/api/board/broadcast", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.PostBoardBroadcast(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	assert.Equal(t, 1, fb.calls)
	assert.Equal(t, []string{"pane-a", "pane-b"}, fb.lastTo)
	assert.Equal(t, "please review", fb.lastBody)

	var resp boardBroadcastResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Results, 2)
}

func TestPostBoardBroadcast_PropagatesPerTargetErrors(t *testing.T) {
	h := newBoardTestHandler(t)
	fb := &fakeBroadcaster{results: []board.BroadcastResult{{Pane: "no-such-pane", Error: "unknown pane"}}}
	h.EnableBoard(board.NewBoardCache(), fb, "panemux")

	body := `{"to":["no-such-pane"],"body":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/board/broadcast", strings.NewReader(body))
	rr := httptest.NewRecorder()
	h.PostBoardBroadcast(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	var resp boardBroadcastResponse
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	require.Len(t, resp.Results, 1)
	assert.Equal(t, "unknown pane", resp.Results[0].Error)
}

func TestPostBoardBroadcast_EmptyTo_UnprocessableEntity(t *testing.T) {
	h := newBoardTestHandler(t)
	h.EnableBoard(board.NewBoardCache(), &fakeBroadcaster{}, "panemux")

	req := httptest.NewRequest(http.MethodPost, "/api/board/broadcast", strings.NewReader(`{"to":[],"body":"hi"}`))
	rr := httptest.NewRecorder()
	h.PostBoardBroadcast(rr, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
}

func TestPostBoardBroadcast_EmptyBody_UnprocessableEntity(t *testing.T) {
	h := newBoardTestHandler(t)
	h.EnableBoard(board.NewBoardCache(), &fakeBroadcaster{}, "panemux")

	req := httptest.NewRequest(http.MethodPost, "/api/board/broadcast", strings.NewReader(`{"to":["pane-a"],"body":""}`))
	rr := httptest.NewRecorder()
	h.PostBoardBroadcast(rr, req)
	assert.Equal(t, http.StatusUnprocessableEntity, rr.Code)
}

func TestPostBoardBroadcast_MalformedBody_BadRequest(t *testing.T) {
	h := newBoardTestHandler(t)
	h.EnableBoard(board.NewBoardCache(), &fakeBroadcaster{}, "panemux")

	req := httptest.NewRequest(http.MethodPost, "/api/board/broadcast", strings.NewReader(`not json`))
	rr := httptest.NewRecorder()
	h.PostBoardBroadcast(rr, req)
	assert.Equal(t, http.StatusBadRequest, rr.Code)
}

// --- Auth middleware ---

func TestRequireBoardAuth_NoTokenConfigured_AllowsRequest(t *testing.T) {
	h := newBoardTestHandler(t) // config.Default() has an empty auth_token
	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	rr := httptest.NewRecorder()
	assert.True(t, h.RequireBoardAuth(rr, req))
}

func TestRequireBoardAuth_TokenConfigured_MissingHeader_Unauthorized(t *testing.T) {
	cfg := config.Default()
	cfg.Server.AuthToken = "secret-token"
	h := NewHandler(cfg, session.NewManager())

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	rr := httptest.NewRecorder()
	assert.False(t, h.RequireBoardAuth(rr, req))
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestRequireBoardAuth_TokenConfigured_WrongToken_Unauthorized(t *testing.T) {
	cfg := config.Default()
	cfg.Server.AuthToken = "secret-token"
	h := NewHandler(cfg, session.NewManager())

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	assert.False(t, h.RequireBoardAuth(rr, req))
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestRequireBoardAuth_TokenConfigured_CorrectToken_Allowed(t *testing.T) {
	cfg := config.Default()
	cfg.Server.AuthToken = "secret-token"
	h := NewHandler(cfg, session.NewManager())

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rr := httptest.NewRecorder()
	assert.True(t, h.RequireBoardAuth(rr, req))
}

func TestBoardAuthMiddleware_RejectsThenAllows(t *testing.T) {
	cfg := config.Default()
	cfg.Server.AuthToken = "secret-token"
	h := NewHandler(cfg, session.NewManager())
	h.EnableBoard(board.NewBoardCache(), &fakeBroadcaster{}, "panemux")

	mux := http.NewServeMux()
	mux.Handle("/api/board/status", BoardAuthMiddleware(h)(http.HandlerFunc(h.GetBoardStatus)))

	unauthed := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, unauthed)
	assert.Equal(t, http.StatusUnauthorized, rr.Code)

	authed := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	authed.Header.Set("Authorization", "Bearer secret-token")
	rr2 := httptest.NewRecorder()
	mux.ServeHTTP(rr2, authed)
	assert.Equal(t, http.StatusOK, rr2.Code)
}

func TestBoardEnabled(t *testing.T) {
	h := newBoardTestHandler(t)
	assert.False(t, h.BoardEnabled())
	h.EnableBoard(board.NewBoardCache(), &fakeBroadcaster{}, "panemux")
	assert.True(t, h.BoardEnabled())
}
