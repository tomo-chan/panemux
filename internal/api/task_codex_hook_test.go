package api

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

	"panemux/internal/config"
	"panemux/internal/homedir"
	"panemux/internal/session"
	"panemux/internal/tasks"
)

// codexHookHandler is a panemux host whose HOME has ~/.codex and the
// hooks.json given ("" for none), and one ssh_connections host.
func codexHookHandler(t *testing.T, hooksJSON string) (*Handler, string) {
	t.Helper()
	home := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(home, ".codex"), 0o700))
	if hooksJSON != "" {
		require.NoError(t, os.WriteFile(filepath.Join(home, ".codex", "hooks.json"), []byte(hooksJSON), 0o600))
	}
	homedir.SetForTest(t, home)
	cfg := defaultTestConfig()
	cfg.SSHConnections = map[string]config.SSHConnection{"build-box": {Host: "build.invalid"}}
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	h.SetTaskService(tasks.New(taskServiceOptions(h)))
	t.Cleanup(h.Close)
	return h, home
}

func doCodexHook(h *Handler, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, req)
	return rec
}

func TestCodexHookRoutes(t *testing.T) {
	h, home := codexHookHandler(t, `{"model":"x"}`)

	rec := doCodexHook(h, http.MethodGet, "/api/tasks/codex-hook?host=", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"missing"}`, rec.Body.String())

	rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook", `{"host":""}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"installed"}`, rec.Body.String())

	data, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	require.NoError(t, err)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.JSONEq(t, `"x"`, string(doc["model"]))
	assert.Contains(t, string(doc["hooks"]), "panemux-codex-permission-hook.sh")

	rec = doCodexHook(h, http.MethodGet, "/api/tasks/codex-hook", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"installed"}`, rec.Body.String())
}

func TestCodexHookRoutesRefuse(t *testing.T) {
	h, home := codexHookHandler(t, `{"hooks":[]}`)

	rec := doCodexHook(h, http.MethodGet, "/api/tasks/codex-hook?host=", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t,
		`{"status":"invalid","error":"~/.codex/hooks.json is not as codex reads it: \"hooks\" is not an object"}`,
		rec.Body.String())

	rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook", `{"host":""}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), `"hooks" is not an object`)
	data, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	require.NoError(t, err)
	assert.Equal(t, `{"hooks":[]}`, string(data))

	for _, tc := range []struct{ method, target, body string }{
		{http.MethodGet, "/api/tasks/codex-hook?host=elsewhere", ""},
		{http.MethodPost, "/api/tasks/codex-hook", `{"host":"elsewhere"}`},
	} {
		rec = doCodexHook(h, tc.method, tc.target, tc.body)
		assert.Equal(t, http.StatusNotFound, rec.Code, tc.method)
	}
	for _, body := range []string{`{"host":"","path":"/etc"}`, `not json`} {
		rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}
}
