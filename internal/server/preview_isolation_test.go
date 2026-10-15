package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// previewFakeSession is a local pane whose working directory is known
// without a real shell.
type previewFakeSession struct {
	*wsFakeSession
	cwd string
}

func (s *previewFakeSession) GetCWD() (string, error) { return s.cwd, nil }

// openFakePreview opens dir/index.html in a fake local pane and returns the
// address the real router serves it at.
func openFakePreview(t *testing.T, e *apiEnv, html string) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(dir, "index.html"), []byte(html), 0o600))
	e.mgr.Add(&previewFakeSession{wsFakeSession: newWSFakeSession("pane-preview"), cwd: dir})

	rr := e.do(t, http.MethodPost, "/api/sessions/pane-preview/preview", `{"path":"index.html"}`)
	require.Equal(t, http.StatusOK, rr.Code, rr.Body.String())
	var resp struct {
		URL string `json:"url"`
	}
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &resp))
	return resp.URL
}

// TestServer_PreviewIsIsolatedFromTheDashboardOrigin pins the isolation as
// the shipped router delivers it, middleware included: the page is framed by
// the dashboard but runs in an opaque origin, and the requests such a page
// makes to the dashboard's API are refused.
func TestServer_PreviewIsIsolatedFromTheDashboardOrigin(t *testing.T) {
	e := newAPIEnv(t)
	url := openFakePreview(t, e, "<script>fetch('/api/layout')</script>")

	// What a sandboxed page sends for its own subresources: cross-site, and
	// for a CORS-mode request an Origin of "null".
	req := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080"+url, nil)
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rr := httptest.NewRecorder()
	e.srv.httpSrv.Handler.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	csp := rr.Header().Get("Content-Security-Policy")
	assert.True(t, strings.HasPrefix(csp, "sandbox "), csp)
	assert.NotContains(t, csp, "allow-same-origin")
	assert.NotContains(t, csp, "allow-top-navigation")
	assert.Contains(t, csp, "frame-ancestors 'self'")
	assert.Empty(t, rr.Header().Get("X-Frame-Options"), "the dashboard frames the preview")
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "nosniff", rr.Header().Get("X-Content-Type-Options"))

	// Everything else keeps refusing to be framed.
	page := httptest.NewRecorder()
	e.srv.httpSrv.Handler.ServeHTTP(page, httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/", nil))
	assert.Equal(t, "DENY", page.Header().Get("X-Frame-Options"))

	for _, tc := range []struct {
		headers map[string]string
		name    string
		method  string
		path    string
	}{
		{name: "fetch from the opaque origin", method: http.MethodGet, path: "/api/layout",
			headers: map[string]string{"Origin": "null", "Sec-Fetch-Site": "cross-site"}},
		{name: "POST from the opaque origin", method: http.MethodPost, path: "/api/sessions",
			headers: map[string]string{"Origin": "null", "Sec-Fetch-Site": "cross-site"}},
		{name: "no-cors image request", method: http.MethodGet, path: "/api/layout",
			headers: map[string]string{"Sec-Fetch-Site": "cross-site", "Sec-Fetch-Mode": "no-cors"}},
		{name: "WebSocket to a pane", method: http.MethodGet, path: "/ws/pane-preview",
			headers: map[string]string{"Origin": "null"}},
		{name: "session token", method: http.MethodGet, path: "/api/session-token",
			headers: map[string]string{"Origin": "null"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "http://127.0.0.1:8080"+tc.path, nil)
			req.RemoteAddr = "127.0.0.1:54321"
			for k, v := range tc.headers {
				req.Header.Set(k, v)
			}
			rr := httptest.NewRecorder()
			e.srv.httpSrv.Handler.ServeHTTP(rr, req)
			assert.Equal(t, http.StatusForbidden, rr.Code)
			assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
		})
	}
}

// The request that opens a preview is an API call like any other: the
// dashboard's own page can make it, and a page on another site cannot.
func TestServer_PreviewOpenRefusesCrossSite(t *testing.T) {
	e := newAPIEnv(t)
	require.NoError(t, os.WriteFile(filepath.Join(e.home, "index.html"), []byte("<p>x</p>"), 0o600))
	e.mgr.Add(&previewFakeSession{wsFakeSession: newWSFakeSession("pane-preview"), cwd: e.home})

	open := func(origin string) int {
		req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/sessions/pane-preview/preview",
			strings.NewReader(`{"path":"index.html"}`))
		req.Header.Set("Origin", origin)
		rr := httptest.NewRecorder()
		e.srv.httpSrv.Handler.ServeHTTP(rr, req)
		return rr.Code
	}
	assert.Equal(t, http.StatusOK, open("http://127.0.0.1:8080"))
	assert.Equal(t, http.StatusForbidden, open("http://attacker.example"))
}
