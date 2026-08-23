package server

import (
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/commandcenter"
	"panemux/internal/session"
)

// These tests are the exhaustiveness gate over the HTTP route table. They
// exist because the api package's handler tests used to build their own copy
// of the router, so a route could be renamed, reordered, or moved behind
// different middleware in internal/server without a single one of them
// failing — and the /api/board/* routes had already drifted apart, registered
// flat and unauthenticated in the test copy. See docs/quality-gateway.md's
// gate G3, and issue #178.
//
// Adding a route to the server is now expected to fail these tests until the
// expected sets below are updated. That is the point: the table is stated in
// exactly two places, the code and this test, and they are compared.

// walkRoutes returns every route registered on the real server.New() router,
// as "METHOD /pattern", sorted. It reads the router the server actually
// serves, not a reconstruction of it.
func walkRoutes(t *testing.T, srv *Server) []string {
	t.Helper()

	mux, ok := srv.httpSrv.Handler.(*chi.Mux)
	require.True(t, ok, "the server's handler must be the chi router itself")

	var routes []string
	err := chi.Walk(mux, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		routes = append(routes, method+" "+route)
		return nil
	})
	require.NoError(t, err)

	sort.Strings(routes)
	return routes
}

// expectedRoutes is every route registered when the command center is
// disabled. Keep it sorted; the comparison is order-independent but a sorted
// literal is easier to diff by eye.
var expectedRoutes = []string{
	"DELETE /api/sessions/{id}",
	"DELETE /api/workspaces/{id}",
	"GET /*",
	"GET /api/board/command/history",
	"GET /api/board/messages",
	"GET /api/board/status",
	"GET /api/detect-shell",
	"GET /api/directories",
	"GET /api/display",
	"GET /api/layout",
	"GET /api/session-token",
	"GET /api/sessions",
	"GET /api/sessions/{id}/git-info",
	"GET /api/ssh-config/hosts",
	"GET /api/ssh-connections",
	"GET /api/workspaces",
	"GET /ws/{sessionID}",
	"POST /api/board/broadcast",
	"POST /api/sessions",
	"POST /api/sessions/{id}/open-url",
	"POST /api/sessions/{id}/open-vscode",
	"POST /api/sessions/{id}/restart",
	"POST /api/ssh-config/hosts",
	"POST /api/workspaces",
	"PUT /api/layout",
	"PUT /api/workspaces/active",
	"PUT /api/workspaces/tab-position",
	"PUT /api/workspaces/vertical-bar-width",
	"PUT /api/workspaces/{id}",
	"PUT /api/workspaces/{id}/layout",
}

func TestServer_RouteTable_CommandCenterDisabled(t *testing.T) {
	srv := New(testConfig(), session.NewManager(), nil, nil, nil, emptyFS)

	assert.Equal(t, expectedRoutes, walkRoutes(t, srv))
}

// With a runner present the table gains exactly one route. Asserting the
// difference rather than a second full literal keeps the two lists from
// drifting against each other.
func TestServer_RouteTable_CommandCenterEnabled_AddsOnlyBoardCommandWS(t *testing.T) {
	runner := commandcenter.NewRunner(commandcenter.RunnerConfig{})
	srv := New(testConfigWithToken("secret-token"), session.NewManager(), nil, nil, runner, emptyFS)

	want := append(append([]string{}, expectedRoutes...), "GET /ws/board-command")
	sort.Strings(want)

	assert.Equal(t, want, walkRoutes(t, srv))
}

// boardRoutePrefix is the path prefix chi mounts behind the bearer-token
// middleware. Everything under it must be authenticated; nothing outside it
// is, today.
const boardRoutePrefix = "/api/board/"

// routeRequestPath turns a chi pattern into a concrete request path. Board
// routes carry no URL parameters today, but substituting rather than skipping
// keeps the test working if one gains a parameter later.
func routeRequestPath(pattern string) string {
	var out []string
	for _, seg := range strings.Split(pattern, "/") {
		if strings.HasPrefix(seg, "{") && strings.HasSuffix(seg, "}") {
			out = append(out, "placeholder")
			continue
		}
		out = append(out, seg)
	}
	return strings.Join(out, "/")
}

// Every board route is derived from the router itself rather than listed by
// hand, so a board route added later is covered by this test the day it is
// registered — the failure mode being guarded against is a new /api/board/*
// route registered outside the authenticated sub-router.
func TestServer_EveryBoardRouteRequiresAuth(t *testing.T) {
	cache := board.NewBoardCache()
	srv := New(testConfigWithToken("secret-token"), session.NewManager(), cache, nil, nil, emptyFS)

	var checked int
	for _, route := range walkRoutes(t, srv) {
		method, pattern, _ := strings.Cut(route, " ")
		if !strings.HasPrefix(pattern, boardRoutePrefix) {
			continue
		}
		checked++

		t.Run(route, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, routeRequestPath(pattern), nil)
			srv.httpSrv.Handler.ServeHTTP(rec, req)

			assert.Equal(t, http.StatusUnauthorized, rec.Code,
				"a route under %s must reject a request carrying no bearer token", boardRoutePrefix)
		})
	}

	require.Positive(t, checked, "no board routes were found — the prefix or the router changed")
}

// The complement of the test above: the routes outside /api/board/ follow the
// unauthenticated posture the frontend still depends on. Restricted to GET so
// the check cannot start a session or write config as a side effect.
func TestServer_NonBoardAPIRoutesStayUnauthenticated(t *testing.T) {
	srv := New(testConfigWithToken("secret-token"), session.NewManager(), nil, nil, nil, emptyFS)

	var checked int
	for _, route := range walkRoutes(t, srv) {
		method, pattern, _ := strings.Cut(route, " ")
		if method != http.MethodGet || !strings.HasPrefix(pattern, "/api/") {
			continue
		}
		if strings.HasPrefix(pattern, boardRoutePrefix) {
			continue
		}
		checked++

		t.Run(route, func(t *testing.T) {
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(method, routeRequestPath(pattern), nil)
			srv.httpSrv.Handler.ServeHTTP(rec, req)

			assert.NotEqual(t, http.StatusUnauthorized, rec.Code,
				"widening bearer auth beyond %s would break every existing frontend request", boardRoutePrefix)
		})
	}

	require.Positive(t, checked, "no unauthenticated GET routes were found — the router changed")
}
