package server

import (
	"embed"
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
	}
}

func TestNew_ReturnsServer(t *testing.T) {
	cfg := testConfig()
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)
	require.NotNil(t, srv)
}

func TestAddr_ReturnsConfiguredAddress(t *testing.T) {
	cfg := testConfig()
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)
	assert.Equal(t, "127.0.0.1:8080", srv.Addr())
}

func TestCorsMiddleware_SetsHeaders(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := corsMiddleware(inner)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Equal(t, "*", rr.Header().Get("Access-Control-Allow-Origin"))
	assert.NotEmpty(t, rr.Header().Get("Access-Control-Allow-Methods"))
	assert.NotEmpty(t, rr.Header().Get("Access-Control-Allow-Headers"))
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

func TestServer_APIRoutesWired(t *testing.T) {
	cfg := testConfig()
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)
	require.NotNil(t, srv)

	// Use the internal httpSrv handler to make requests without binding a port
	ts := httptest.NewServer(srv.httpSrv.Handler)
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/api/layout")
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}
