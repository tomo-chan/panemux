package api

import (
	"context"
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
	"panemux/internal/host"
	"panemux/internal/hosttools"
	"panemux/internal/session"
	"panemux/internal/tasks"
	"time"
)

type updatingHookTool struct{ started, finish chan struct{} }

func (updatingHookTool) Check(context.Context, host.Host) hosttools.State {
	return hosttools.State{Status: hosttools.Installed}
}
func (f updatingHookTool) Install(ctx context.Context, _ host.Host) (hosttools.State, error) {
	close(f.started)
	select {
	case <-f.finish:
		return hosttools.State{Status: hosttools.Installed}, nil
	case <-ctx.Done():
		return hosttools.State{}, ctx.Err()
	}
}

func TestCodexHookRoutesDuringUpdate(t *testing.T) {
	h, _ := codexHookHandler(t, "")
	h.hostTools.Close()
	tool := updatingHookTool{started: make(chan struct{}), finish: make(chan struct{})}
	h.hostTools = hosttools.New(hosttools.Options{Tools: map[hosttools.ID]hosttools.Tool{hosttools.CodexHookTool: tool}})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		req := httptest.NewRequest(http.MethodPost, "/api/tasks/codex-hook", strings.NewReader(`{"host":""}`))
		req = req.WithContext(ctx)
		rec := httptest.NewRecorder()
		setupRouterWithHandler(h).ServeHTTP(rec, req)
		done <- rec
	}()
	select {
	case <-tool.started:
	case <-ctx.Done():
		t.Fatal("install did not start")
	}
	rec := doCodexHook(h, http.MethodGet, "/api/tasks/codex-hook", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"unknown","error":"tool is updating","auto_update":false}`, rec.Body.String())
	rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook", `{"host":""}`)
	require.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "tool is updating")
	rec = doCodexHook(h, http.MethodGet, "/api/tasks/codex-hook?host=unknown", "")
	require.Equal(t, http.StatusNotFound, rec.Code)
	close(tool.finish)
	select {
	case rec = <-done:
		require.Equal(t, http.StatusOK, rec.Code)
	case <-ctx.Done():
		t.Fatal("install did not finish")
	}
	assert.Equal(t, http.StatusOK, doCodexHook(h, http.MethodGet, "/api/tasks/codex-hook", "").Code)
}

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
	assert.JSONEq(t, `{"status":"missing","auto_update":false}`, rec.Body.String())

	rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook", `{"host":""}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"installed","auto_update":false}`, rec.Body.String())

	data, err := os.ReadFile(filepath.Join(home, ".codex", "hooks.json"))
	require.NoError(t, err)
	var doc map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(data, &doc))
	assert.JSONEq(t, `"x"`, string(doc["model"]))
	assert.Contains(t, string(doc["hooks"]), "panemux-codex-permission-hook.sh")

	rec = doCodexHook(h, http.MethodGet, "/api/tasks/codex-hook", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"status":"installed","auto_update":false}`, rec.Body.String())
}

func TestCodexHookRoutesRefuse(t *testing.T) {
	h, home := codexHookHandler(t, `{"hooks":[]}`)

	rec := doCodexHook(h, http.MethodGet, "/api/tasks/codex-hook?host=", "")
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t,
		`{"status":"invalid","auto_update":false,`+
			`"error":"~/.codex/hooks.json is not as codex reads it: \"hooks\" is not an object"}`,
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

func TestCodexHookAutoUpdateRoute(t *testing.T) {
	h, home := codexHookHandler(t, "")

	rec := doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook/auto-update", `{"host":"build-box","enabled":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"auto_update":true}`, rec.Body.String())
	data, err := os.ReadFile(filepath.Join(home, ".config", "panemux", "codex-hook.json"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"version":1,"auto_update":["build-box"]}`, string(data))

	rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook/auto-update", `{"host":"","enabled":true}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	rec = doCodexHook(h, http.MethodGet, "/api/tasks/codex-hook?host=", "")
	assert.JSONEq(t, `{"status":"missing","auto_update":true}`, rec.Body.String())
	rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook/auto-update", `{"host":"","enabled":false}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.JSONEq(t, `{"auto_update":false}`, rec.Body.String())

	rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook/auto-update", `{"host":"elsewhere","enabled":true}`)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	for _, body := range []string{`{"host":"","enabled":true,"path":"/etc"}`, `not json`} {
		rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook/auto-update", body)
		assert.Equal(t, http.StatusBadRequest, rec.Code, body)
	}

	require.NoError(t, os.WriteFile(filepath.Join(home, ".config", "panemux", "codex-hook.json"), []byte("{"), 0o600))
	rec = doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook/auto-update", `{"host":"","enabled":true}`)
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "codex hook settings file")
}

func TestCodexHookAutoUpdateFollowsSuccessfulTaskCollections(t *testing.T) {
	for _, live := range []bool{false, true} {
		name := "full"
		if live {
			name = "live"
		}
		t.Run(name, func(t *testing.T) {
			h, home := codexHookHandler(t, "")
			installed := doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook", `{"host":""}`)
			require.Equal(t, http.StatusOK, installed.Code, installed.Body.String())
			path := filepath.Join(home, ".local", "bin", "panemux-codex-permission-hook.sh")
			original, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(path, []byte("#!/bin/sh\n"), 0o600))
			enabled := doCodexHook(h, http.MethodPost, "/api/tasks/codex-hook/auto-update", `{"host":"","enabled":true}`)
			require.Equal(t, http.StatusOK, enabled.Code)
			options := taskServiceOptions(h)
			options.Hosts = func() []string { return nil }
			options.RunLocal = func(context.Context, string) ([]byte, error) {
				return taskCollection("session", "/workspace/user/project"), nil
			}
			h.SetTaskService(tasks.New(options))
			if live {
				h.tasks.CollectAttention(context.Background())
			} else {
				h.tasks.Collect(context.Background())
			}
			h.hostTools.Wait()
			got, err := os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, original, got)
			// A second successful collection does not repeat the startup check.
			changed := []byte("#!/bin/sh\nexit 0\n")
			require.NoError(t, os.WriteFile(path, changed, 0o600))
			h.tasks.CollectAttention(context.Background())
			h.hostTools.Wait()
			got, err = os.ReadFile(path)
			require.NoError(t, err)
			require.Equal(t, changed, got)
		})
	}
}
