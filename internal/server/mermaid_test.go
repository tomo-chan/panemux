package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/cachedir"
	"panemux/internal/homedir"
	"panemux/internal/session"
)

func TestServer_MermaidUsesOnlyTheBundledRuntime(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	cachedir.SetForTest(t, filepath.Join(home, ".cache"))
	manager := session.NewManager()
	srv := New(testConfig(), manager, nil, nil, nil, fstest.MapFS{
		"frontend/dist/preview-assets/mermaid.js": {Data: []byte("/* product script */")},
	})
	t.Cleanup(func() { _ = srv.Shutdown(t.Context()) })
	dir := realPreviewDir(t)
	source := []byte("```mermaid\nflowchart LR\nA-->B\n```")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "diagram.md"), source, 0o600))
	manager.Add(&previewFakeSession{wsFakeSession: newWSFakeSession("mermaid-pane"), cwd: dir})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:8080/api/sessions/mermaid-pane/preview",
		strings.NewReader(`{"path":"diagram.md"}`))
	srv.httpSrv.Handler.ServeHTTP(rr, req)
	require.Equal(t, 200, rr.Code)
	var result struct{ URL string }
	require.NoError(t, json.Unmarshal(rr.Body.Bytes(), &result))
	rr = httptest.NewRecorder()
	srv.httpSrv.Handler.ServeHTTP(rr, httptest.NewRequest("GET", "http://127.0.0.1:8080"+result.URL, nil))
	require.Equal(t, 200, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Security-Policy"), "sandbox allow-scripts")
	assert.NotContains(t, rr.Header().Get("Content-Security-Policy"), "allow-same-origin")
	assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
	scriptURL := strings.Split(strings.Split(rr.Body.String(), `src="`)[1], `"`)[0]
	for _, path := range []string{scriptURL, "/preview-assets/missing.js"} {
		req = httptest.NewRequest("GET", "http://127.0.0.1:8080"+path, nil)
		req.Header.Set("Origin", "null")
		req.Header.Set("Sec-Fetch-Site", "cross-site")
		rr = httptest.NewRecorder()
		srv.httpSrv.Handler.ServeHTTP(rr, req)
		if path == scriptURL {
			assert.Equal(t, 200, rr.Code)
			assert.Equal(t, "/* product script */", rr.Body.String())
		} else {
			assert.Equal(t, 404, rr.Code)
		}
		assert.Empty(t, rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func realPreviewDir(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	return dir
}
