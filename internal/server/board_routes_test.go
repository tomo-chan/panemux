package server

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/session"
)

func testConfigWithToken(token string) *config.Config {
	cfg := testConfig()
	cfg.Server.AuthToken = token
	return cfg
}

func TestServer_BoardRoutesWired_RequireAuth(t *testing.T) {
	cfg := testConfigWithToken("secret-token")
	mgr := session.NewManager()
	cache := board.NewBoardCache()
	srv := New(cfg, mgr, cache, nil, emptyFS)

	paths := []string{"/api/board/status", "/api/board/messages"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			srv.httpSrv.Handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusUnauthorized, rec.Code, "missing token must be rejected")
		})
	}
}

func TestServer_BoardRoutes_WrongToken_Rejected(t *testing.T) {
	cfg := testConfigWithToken("secret-token")
	mgr := session.NewManager()
	cache := board.NewBoardCache()
	srv := New(cfg, mgr, cache, nil, emptyFS)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	srv.httpSrv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestServer_BoardRoutes_CorrectToken_Reaches200(t *testing.T) {
	cfg := testConfigWithToken("secret-token")
	mgr := session.NewManager()
	cache := board.NewBoardCache()
	srv := New(cfg, mgr, cache, nil, emptyFS)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	req.Header.Set("Authorization", "Bearer secret-token")
	srv.httpSrv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestServer_ExistingAPIRoutes_RemainUnauthenticated(t *testing.T) {
	// Regression test: scoping bearerAuthMiddleware to /api/board must not
	// widen to any pre-existing route — the current frontend sends no
	// token at all.
	cfg := testConfigWithToken("secret-token")
	mgr := session.NewManager()
	srv := New(cfg, mgr, board.NewBoardCache(), nil, emptyFS)

	paths := []string{"/api/layout", "/api/workspaces", "/api/sessions", "/api/display"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, path, nil)
			srv.httpSrv.Handler.ServeHTTP(rec, req)
			assert.NotEqual(t, http.StatusUnauthorized, rec.Code,
				"existing routes must stay reachable with no Authorization header")
		})
	}
}

func TestServer_WSRoute_RemainsUnauthenticated(t *testing.T) {
	cfg := testConfigWithToken("secret-token")
	mgr := session.NewManager()
	srv := New(cfg, mgr, board.NewBoardCache(), nil, emptyFS)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/ws/nonexistent-session", nil)
	srv.httpSrv.Handler.ServeHTTP(rec, req)
	// 404 (unknown session) proves the request reached ws.Handler at all,
	// i.e. it was never intercepted by an auth check.
	require.Equal(t, http.StatusNotFound, rec.Code)
}
