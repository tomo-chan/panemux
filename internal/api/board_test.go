package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/session"
)

type broadcastFunc func(ctx context.Context, to []string, body string) ([]string, error)

func setupBoardRouter(cache *board.BoardCache, broadcastFn broadcastFunc) *Handler {
	h := NewHandler(defaultTestConfig(), session.NewManager(), cache, nil)
	if broadcastFn != nil {
		h.boardBroadcastFn = broadcastFn
	}
	return h
}

func TestGetBoardStatus_EmptyCache_ReturnsEmptyObject(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardStatusResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Empty(t, resp.Statuses)
}

func TestGetBoardStatus_PopulatedCache_ReturnsEntries(t *testing.T) {
	cache := board.NewBoardCache()
	cache.RecordStatus("pane-a", board.Status{State: "working", Branch: "feature/x"})
	h := setupBoardRouter(cache, nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardStatusResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Contains(t, resp.Statuses, "pane-a")
	assert.Equal(t, "working", resp.Statuses["pane-a"].State)
	assert.Equal(t, "feature/x", resp.Statuses["pane-a"].Branch)
}

func TestGetBoardMessages_EmptyCache_ReturnsEmptyArray(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/messages", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardMessagesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotNil(t, resp.Messages)
	assert.Empty(t, resp.Messages)
}

func TestGetBoardMessages_MissingSince_DefaultsToZero(t *testing.T) {
	cache := board.NewBoardCache()
	cache.AppendMessage(board.Row{From: "a", To: "b", Body: "hi", At: time.Now()})
	h := setupBoardRouter(cache, nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/messages", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardMessagesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Messages, 1)
	assert.Equal(t, int64(1), resp.Messages[0].Seq)
	assert.Equal(t, "hi", resp.Messages[0].Body)
}

func TestGetBoardMessages_NonNumericSince_400(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/messages?since=not-a-number", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestGetBoardMessages_SinceExcludesEverything_EmptyResult(t *testing.T) {
	cache := board.NewBoardCache()
	cache.AppendMessage(board.Row{Body: "one"})
	h := setupBoardRouter(cache, nil)
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/messages?since=1", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardMessagesResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Empty(t, resp.Messages)
}

func postBoardBroadcast(t *testing.T, r http.Handler, body any) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	switch v := body.(type) {
	case string:
		reader = bytes.NewReader([]byte(v))
	default:
		data, err := json.Marshal(body)
		require.NoError(t, err)
		reader = bytes.NewReader(data)
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/board/broadcast", reader)
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(rec, req)
	return rec
}

func TestPostBoardBroadcast_MissingTo_422(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(context.Context, []string, string) ([]string, error) {
		t.Fatal("broadcast fn must not be called for a validation failure")
		return nil, nil
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{Body: "hello"})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostBoardBroadcast_EmptyBody_422(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(context.Context, []string, string) ([]string, error) {
		t.Fatal("broadcast fn must not be called for a validation failure")
		return nil, nil
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{To: []string{"pane-a"}})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
}

func TestPostBoardBroadcast_MalformedJSON_400(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, "not json")
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPostBoardBroadcast_UnknownPaneError_422(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(context.Context, []string, string) ([]string, error) {
		return nil, &board.UnknownPaneError{IDs: []string{"no-such-pane"}}
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{To: []string{"no-such-pane"}, Body: "hello"})
	assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	var body map[string]string
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	assert.Contains(t, body["error"], "no-such-pane")
}

func TestPostBoardBroadcast_GenericError_502(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(context.Context, []string, string) ([]string, error) {
		return nil, errors.New("ssh: connection lost")
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{To: []string{"pane-a"}, Body: "hello"})
	assert.Equal(t, http.StatusBadGateway, rec.Code)
}

func TestPostBoardBroadcast_Success_200WithDelivered(t *testing.T) {
	h := setupBoardRouter(board.NewBoardCache(), func(_ context.Context, to []string, body string) ([]string, error) {
		assert.Equal(t, []string{"pane-a", "pane-b"}, to)
		assert.Equal(t, "hello", body)
		return to, nil
	})
	r := setupRouterWithHandler(h)

	rec := postBoardBroadcast(t, r, boardBroadcastRequest{To: []string{"pane-a", "pane-b"}, Body: "hello"})
	assert.Equal(t, http.StatusOK, rec.Code)
	var resp boardBroadcastResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.Equal(t, []string{"pane-a", "pane-b"}, resp.Delivered)
}
