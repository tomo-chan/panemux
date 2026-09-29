package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func okHandler() (http.Handler, *bool) {
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	})
	return h, &called
}

func TestBearerAuthMiddleware_MissingHeader_Rejected(t *testing.T) {
	inner, called := okHandler()
	handler := bearerAuthMiddleware("secret-token")(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, *called, "the wrapped handler must never run when auth fails")
}

func TestBearerAuthMiddleware_WrongToken_Rejected(t *testing.T) {
	inner, called := okHandler()
	handler := bearerAuthMiddleware("secret-token")(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, *called)
}

func TestBearerAuthMiddleware_MissingBearerPrefix_Rejected(t *testing.T) {
	inner, called := okHandler()
	handler := bearerAuthMiddleware("secret-token")(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	req.Header.Set("Authorization", "secret-token") // no "Bearer " prefix
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, *called)
}

func TestBearerAuthMiddleware_EmptyAuthorizationHeader_Rejected(t *testing.T) {
	inner, called := okHandler()
	handler := bearerAuthMiddleware("secret-token")(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	req.Header.Set("Authorization", "")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, *called)
}

func TestBearerAuthMiddleware_ConfiguredTokenEmpty_AlwaysRejects(t *testing.T) {
	inner, called := okHandler()
	handler := bearerAuthMiddleware("")(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	req.Header.Set("Authorization", "Bearer ")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, *called, "an empty configured token must fail closed, never match any request")
}

func TestBearerAuthMiddleware_CorrectToken_PassesThrough(t *testing.T) {
	inner, called := okHandler()
	handler := bearerAuthMiddleware("secret-token")(inner)

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, *called)
}

// TestBearerAuthMiddleware_WSHandshakeStyleRequest_RejectedBeforeUpgrade
// proves the same middleware protects a WebSocket handshake: wrapping a
// handler that simulates ws.Handler's own upgrade attempt, an
// unauthenticated request carrying the standard WS upgrade headers is
// rejected before that handler — and therefore before any upgrade — runs.
func TestBearerAuthMiddleware_WSHandshakeStyleRequest_RejectedBeforeUpgrade(t *testing.T) {
	inner, called := okHandler()
	handler := bearerAuthMiddleware("secret-token")(inner)

	req := httptest.NewRequest(http.MethodGet, "/ws/board-command", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusUnauthorized, rr.Code)
	assert.False(t, *called, "the WS handler must never see the request, so no upgrade can happen")
}

func TestBearerAuthMiddleware_WSHandshakeStyleRequest_CorrectToken_PassesThrough(t *testing.T) {
	inner, called := okHandler()
	handler := bearerAuthMiddleware("secret-token")(inner)

	req := httptest.NewRequest(http.MethodGet, "/ws/board-command", nil)
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Authorization", "Bearer secret-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.True(t, *called)
}

func TestValidBearerToken_TableDriven(t *testing.T) {
	tests := []struct {
		name   string
		header string
		token  string
		want   bool
	}{
		{"exact match", "Bearer abc123", "abc123", true},
		{"wrong token", "Bearer wrong", "abc123", false},
		{"no prefix", "abc123", "abc123", false},
		{"empty header", "", "abc123", false},
		{"empty configured token", "Bearer ", "", false},
		{"case-sensitive prefix", "bearer abc123", "abc123", false},
		{"trailing whitespace differs", "Bearer abc123 ", "abc123", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, validBearerToken(tt.header, tt.token))
		})
	}
}
