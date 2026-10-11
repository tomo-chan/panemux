package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Walk derives the scope from the shipped router, including every method and
// board route. Replacing only the final handler proves no effectful handler can
// run (disk writes, PTY/SSH creation and listener binding included).
func TestServer_AllAPIRoutesGuardBeforeHandler(t *testing.T) {
	e := newAPIEnv(t)
	router := chi.NewRouter()
	e.srv.api.Mount(router, bearerAuthMiddleware(integrationToken))
	count := 0
	walk := func(method, route string, _ http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		if !strings.HasPrefix(route, "/api/") {
			return nil
		}
		count++
		t.Run(method+route, func(t *testing.T) {
			called := false
			var handler http.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })
			for i := len(middlewares) - 1; i >= 0; i-- {
				handler = middlewares[i](handler)
			}
			req := httptest.NewRequest(method, "http://localhost:8080"+route, nil)
			req.Header.Set("Origin", "http://localhost:5173")
			req.Header.Set("Authorization", "Bearer "+integrationToken)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusForbidden, rec.Code)
			assert.False(t, called)
			assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
		})
		return nil
	}
	require.NoError(t, chi.Walk(router, walk))
	assert.Positive(t, count)
}

func TestServer_OriginGuardRealRouter(t *testing.T) {
	e := newAPIEnv(t)
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		paths := []string{
			"/api/layout", "/api/session-token", "/api/sessions",
			"/api/board/broadcast", "/ws/missing", "/ws/tasks/events",
		}
		for _, path := range paths {
			t.Run(method+path, func(t *testing.T) {
				body := strings.NewReader(`{"type":"local","command":"true"}`)
				req := httptest.NewRequest(method, "http://localhost:8080"+path, body)
				req.Header.Set("Origin", "http://localhost:5173")
				rec := httptest.NewRecorder()
				e.srv.httpSrv.Handler.ServeHTTP(rec, req)
				assert.Equal(t, http.StatusForbidden, rec.Code)
				assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
			})
		}
	}
	assert.Empty(t, e.mgr.List())
	assert.Equal(t, "Default", e.cfg.Workspaces.Items[0].Title)
}

func TestServer_OriginGuardProxyAndCLI(t *testing.T) {
	e := newAPIEnv(t)
	for _, headers := range []http.Header{
		{},
		{"Origin": {"http://localhost:5173"}},
		{"Origin": {"https://localhost:5173"}, "X-Forwarded-Proto": {"https"}},
		{"Sec-Fetch-Site": {"same-origin"}, "Referer": {"http://localhost:5173/tasks"}},
	} {
		req := httptest.NewRequest("GET", "http://localhost:5173/api/layout", nil)
		req.Header = headers
		rec := httptest.NewRecorder()
		e.srv.httpSrv.Handler.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code)
	}
	for _, headers := range []http.Header{
		{"Sec-Fetch-Site": {"same-origin"}},
		{"Referer": {"http://localhost:3000/tasks"}},
		{"Origin": {"null"}},
		{"Origin": {"http://localhost:5173", "http://localhost:5173"}},
		{"Origin": {"http://localhost:5173"}, "Sec-Fetch-Site": {"same-site"}},
		{"Origin": {"http://localhost:5173"}, "Referer": {"http://localhost:8080/tasks"}},
	} {
		for _, path := range []string{"/api/layout", "/api/session-token", "/api/board/status"} {
			req := httptest.NewRequest("GET", "http://localhost:5173"+path, nil)
			req.Header = headers
			rec := httptest.NewRecorder()
			e.srv.httpSrv.Handler.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusForbidden, rec.Code)
		}
	}
	for _, method := range []string{"OPTIONS", "GET"} {
		req := httptest.NewRequest(method, "http://localhost:5173/api/layout", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()
		e.srv.httpSrv.Handler.ServeHTTP(rec, req)
		want := http.StatusOK
		if method == "OPTIONS" {
			want = http.StatusNoContent
		}
		assert.Equal(t, want, rec.Code)
		assert.Equal(t, "http://localhost:5173", rec.Header().Get("Access-Control-Allow-Origin"))
	}
}
