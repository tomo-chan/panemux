package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/commandcenter"
)

func setupCommandHistoryRouter(fn func() ([]commandcenter.HistoryEntry, error)) *Handler {
	h := setupBoardRouter(board.NewBoardCache(), nil)
	if fn != nil {
		h.commandHistoryFn = fn
	}
	return h
}

func TestGetBoardCommandHistory_Empty_ReturnsEmptyArray(t *testing.T) {
	h := setupCommandHistoryRouter(func() ([]commandcenter.HistoryEntry, error) { return nil, nil })
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/command/history", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp commandHistoryResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	assert.NotNil(t, resp.Entries)
	assert.Empty(t, resp.Entries)
}

func TestGetBoardCommandHistory_Populated_ReturnsEntriesInOrder(t *testing.T) {
	at := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)
	h := setupCommandHistoryRouter(func() ([]commandcenter.HistoryEntry, error) {
		return []commandcenter.HistoryEntry{
			{At: at, Raw: json.RawMessage(`{"type":"system","subtype":"init"}`)},
			{At: at.Add(time.Second), Raw: json.RawMessage(`{"type":"result","result":"done"}`)},
		}, nil
	})
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/command/history", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp commandHistoryResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Entries, 2)
	assert.Equal(t, at, resp.Entries[0].At.UTC())
	assert.JSONEq(t, `{"type":"system","subtype":"init"}`, string(resp.Entries[0].Raw))
	assert.JSONEq(t, `{"type":"result","result":"done"}`, string(resp.Entries[1].Raw))
}

func TestGetBoardCommandHistory_LoadError_500(t *testing.T) {
	h := setupCommandHistoryRouter(func() ([]commandcenter.HistoryEntry, error) {
		return nil, errors.New("disk read failed")
	})
	r := setupRouterWithHandler(h)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/command/history", nil)
	r.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}
