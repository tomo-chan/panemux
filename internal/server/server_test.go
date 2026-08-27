package server

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/session"
)

// emptyFS is an empty embed.FS used in place of the real frontend assets.
var emptyFS embed.FS

func testConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{
			Port: 8080,
			Host: "127.0.0.1",
		},
		Workspaces: config.WorkspacesConfig{
			Active:           "default",
			TabPosition:      "top",
			VerticalBarWidth: 280,
			Items: []config.WorkspaceConfig{
				{
					ID:    "default",
					Title: "Default",
					Layout: config.LayoutNode{
						Direction: "horizontal",
						Children: []config.LayoutChild{
							{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local"}},
						},
					},
				},
			},
		},
	}
}

func TestNew_ReturnsServer(t *testing.T) {
	cfg := testConfig()
	mgr := session.NewManager()
	srv := New(cfg, mgr, nil, nil, nil, emptyFS)
	require.NotNil(t, srv)
}

func TestAddr_ReturnsConfiguredAddress(t *testing.T) {
	cfg := testConfig()
	mgr := session.NewManager()
	srv := New(cfg, mgr, nil, nil, nil, emptyFS)
	assert.Equal(t, "127.0.0.1:8080", srv.Addr())
}

func TestCorsMiddleware_LocalhostOrigin_ReflectsOrigin(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := corsMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "http://localhost:5173", rr.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, rr.Header().Get("Access-Control-Allow-Methods"))
	assert.NotEmpty(t, rr.Header().Get("Access-Control-Allow-Headers"))
	assert.Equal(t, "Origin", rr.Header().Get("Vary"))
}

func TestCorsMiddleware_NonLocalhostOrigin_NoHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := corsMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Origin", "http://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestCorsMiddleware_NoOrigin_NoHeader(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := corsMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
}

func TestCorsMiddleware_OptionsReturns204(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Should not be called for OPTIONS
		w.WriteHeader(http.StatusOK)
	})
	handler := corsMiddleware(inner)

	req := httptest.NewRequest(http.MethodOptions, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusNoContent, rr.Code)
}

func TestSecurityHeadersMiddleware_SetsHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := securityHeadersMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))
	assert.Equal(t, "DENY", rr.Header().Get("X-Frame-Options"))
	assert.Equal(t, "strict-origin-when-cross-origin", rr.Header().Get("Referrer-Policy"))
}

func TestIsLocalhostOrigin(t *testing.T) {
	tests := []struct {
		origin string
		want   bool
	}{
		{"http://localhost:8080", true},
		{"http://localhost:5173", true},
		{"http://127.0.0.1:8080", true},
		{"http://[::1]:8080", true},
		{"http://evil.com", false},
		{"http://notlocalhost.com", false},
		{"://invalid", false},
	}
	for _, tc := range tests {
		assert.Equal(t, tc.want, isLocalhostOrigin(tc.origin), "origin: %s", tc.origin)
	}
}

// The two tests below are the deliberate exception to the consolidation in
// api_integration_test.go: they are not per-route wiring checks (those are all
// covered by the route table plus the integration table now), they are chi
// *precedence* checks. /api/workspaces/tab-position and
// /api/workspaces/vertical-bar-width are both matched by the
// /api/workspaces/{id} pattern registered after them, so the only thing
// separating "the settings route ran" from "a workspace was renamed to
// tab-position" is registration order. No exhaustiveness check over the route
// set can see that — both routes exist either way — so these stay as named
// tests. See issue #180's roadmap item 1.
func TestServer_WorkspaceTabPositionRouteWiredBeforeWorkspaceIDRoute(t *testing.T) {
	cfg := testConfig()
	mgr := session.NewManager()
	srv := New(cfg, mgr, nil, nil, nil, emptyFS)
	require.NotNil(t, srv)

	performWorkspaceSettingUpdate(t, srv, "/api/workspaces/tab-position", `{"tab_position":"right"}`)
	assert.Equal(t, "right", cfg.Workspaces.TabPosition)
	assert.Equal(t, "Default", cfg.Workspaces.Items[0].Title)
}

func TestServer_WorkspaceVerticalBarWidthRouteWiredBeforeWorkspaceIDRoute(t *testing.T) {
	cfg := testConfig()
	mgr := session.NewManager()
	srv := New(cfg, mgr, nil, nil, nil, emptyFS)
	require.NotNil(t, srv)

	performWorkspaceSettingUpdate(t, srv, "/api/workspaces/vertical-bar-width", `{"vertical_bar_width":320}`)
	assert.Equal(t, 320, cfg.Workspaces.VerticalBarWidth)
	assert.Equal(t, "Default", cfg.Workspaces.Items[0].Title)
}

func performWorkspaceSettingUpdate(t *testing.T, srv *Server, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	srv.httpSrv.Handler.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	return rec
}

// The loopback forward registry is process-wide state New() owns and Shutdown
// must release; this is about the server's lifecycle, not about any route, so
// it is not something the integration table can express.
func TestServer_ShutdownClosesPortForwards(t *testing.T) {
	srv := New(testConfig(), session.NewManager(), nil, nil, nil, emptyFS)
	require.NotNil(t, srv.forwards)

	require.NoError(t, srv.Shutdown(context.Background()))

	// The registry is closed: it refuses to open new forwards.
	_, err := srv.forwards.Ensure("pane-1", 45000, stubDialer{})
	assert.Error(t, err)
}

type stubDialer struct{}

func (stubDialer) DialLoopback(_ context.Context, _ int) (net.Conn, error) {
	return nil, errors.New("not used")
}
