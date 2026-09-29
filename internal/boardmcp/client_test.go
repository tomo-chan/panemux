package boardmcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHTTPBoardAPIClientStatusSendsBearerTokenAndReturnsBody(t *testing.T) {
	var gotAuth, gotPath, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		gotMethod = r.Method
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"statuses":{}}`))
	}))
	defer srv.Close()

	client := NewHTTPBoardAPIClient(srv.URL, "sekret")
	raw, err := client.Status(context.Background())

	require.NoError(t, err)
	assert.JSONEq(t, `{"statuses":{}}`, string(raw))
	assert.Equal(t, "Bearer sekret", gotAuth)
	assert.Equal(t, "/api/board/status", gotPath)
	assert.Equal(t, http.MethodGet, gotMethod)
}

func TestHTTPBoardAPIClientMessagesSetsSinceQueryParam(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"messages":[]}`))
	}))
	defer srv.Close()

	client := NewHTTPBoardAPIClient(srv.URL, "tok")
	_, err := client.Messages(context.Background(), 42)

	require.NoError(t, err)
	assert.Equal(t, "since=42", gotQuery)
}

func TestHTTPBoardAPIClientMessagesOmitsSinceWhenZero(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(`{"messages":[]}`))
	}))
	defer srv.Close()

	client := NewHTTPBoardAPIClient(srv.URL, "tok")
	_, err := client.Messages(context.Background(), 0)

	require.NoError(t, err)
	assert.Empty(t, gotQuery)
}

func TestHTTPBoardAPIClientBroadcastSendsJSONBody(t *testing.T) {
	var gotMethod, gotContentType, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		buf := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(buf)
		gotBody = string(buf)
		_, _ = w.Write([]byte(`{"delivered":["pane-a"]}`))
	}))
	defer srv.Close()

	client := NewHTTPBoardAPIClient(srv.URL, "tok")
	raw, err := client.Broadcast(context.Background(), []string{"pane-a"}, "hello")

	require.NoError(t, err)
	assert.JSONEq(t, `{"delivered":["pane-a"]}`, string(raw))
	assert.Equal(t, http.MethodPost, gotMethod)
	assert.Equal(t, "application/json", gotContentType)
	assert.JSONEq(t, `{"to":["pane-a"],"body":"hello"}`, gotBody)
}

func TestHTTPBoardAPIClientNon2xxReturnsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer srv.Close()

	client := NewHTTPBoardAPIClient(srv.URL, "wrong-token")
	_, err := client.Status(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "401")
}
