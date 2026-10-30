package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/config"
	"panemux/internal/fileops"
	"panemux/internal/host"
	"panemux/internal/hosttools"
	"panemux/internal/session"
)

type manualAgmsgTool struct {
	err    error
	checks *atomic.Int32
}

func (t manualAgmsgTool) Check(context.Context, host.Host) hosttools.State {
	if t.checks != nil {
		t.checks.Add(1)
	}
	return hosttools.State{Status: hosttools.Missing}
}

func TestAgmsgAutoSettingPersistsGloballyAndRollsBackFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8080\nagent_board:\n  enabled: true\n"), 0600))
	cfg, err := config.Load(path)
	require.NoError(t, err)
	h := NewHandler(cfg, session.NewManager(), nil, nil)
	t.Cleanup(h.Close)
	// A missing fixture must never be installed by opting in.
	var checks atomic.Int32
	h.hostTools.Close()
	h.hostTools = hosttools.New(hosttools.Options{
		Tools:      map[hosttools.ID]hosttools.Tool{hosttools.AgmsgTool: manualAgmsgTool{checks: &checks}},
		GlobalAuto: map[hosttools.ID]func() bool{hosttools.AgmsgTool: h.agmsgAutoEnabled},
	})
	router := setupRouterWithHandler(h)
	post := func(body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/hosts/agmsg/auto-update", strings.NewReader(body)))
		return w
	}
	w := post(`{"enabled":true}`)
	require.Equal(t, 200, w.Code)
	require.JSONEq(t, `{"auto_update":true}`, w.Body.String())
	h.hostTools.Wait()
	require.Equal(t, int32(1), checks.Load())

	for _, step := range []struct {
		body  string
		count int32
	}{
		{`{"enabled":true}`, 1}, {`{"enabled":false}`, 1}, {`{"enabled":true}`, 2},
	} {
		require.Equal(t, 200, post(step.body).Code)
		h.hostTools.Wait()
		require.Equal(t, step.count, checks.Load())
	}
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"true"}`} {
		require.Equal(t, 400, post(body).Code)
	}
	loaded, err := config.Load(path)
	require.NoError(t, err)
	require.True(t, loaded.AgentBoard.AgmsgAutoUpdate)
	w = post(`{"host":"build-box","enabled":false}`)
	require.Equal(t, 400, w.Code)
	fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: errors.New("disk full")}).Ops())
	w = post(`{"enabled":false}`)
	require.Equal(t, 500, w.Code)
	require.True(t, cfg.AgentBoard.AgmsgAutoUpdate)
	cfg.AgentBoard.Enabled = false
	w = post(`{"enabled":true}`)
	require.Equal(t, 404, w.Code)
}
func (t manualAgmsgTool) Install(context.Context, host.Host) (hosttools.State, error) {
	return hosttools.State{Status: hosttools.Installed, Version: "1.2.0"}, t.err
}

func TestAgmsgToolInstallResponseAndFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		h := NewHandler(&config.Config{}, session.NewManager(), nil, nil)
		t.Cleanup(h.Close)
		h.cfg.AgentBoard.Enabled = true
		h.hostTools.Close()
		tool := manualAgmsgTool{}
		if fail {
			tool.err = errors.New("installer: exit status 7: recovery required")
		}
		h.hostTools = hosttools.New(hosttools.Options{Tools: map[hosttools.ID]hosttools.Tool{hosttools.AgmsgTool: tool}})
		w := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/hosts/agmsg", strings.NewReader(`{"host":""}`))
		setupRouterWithHandler(h).ServeHTTP(w, req)
		if fail {
			require.Equal(t, 502, w.Code)
			require.Contains(t, w.Body.String(), "exit status 7")
		} else {
			require.Equal(t, 200, w.Code)
			require.JSONEq(t, `{"status":"installed","version":"1.2.0","auto_update":false}`, w.Body.String())
		}
		require.Equal(t, uint64(1), h.hostTools.Revision(hosttools.AgmsgTool, host.Local))
	}
}

func TestAgmsgToolRoutesEnabledAndPause(t *testing.T) {
	h := NewHandler(&config.Config{}, session.NewManager(), nil, nil)
	t.Cleanup(h.Close)
	r := httptest.NewRequest(http.MethodGet, "/api/hosts/agmsg", nil)
	w := httptest.NewRecorder()
	h.GetAgmsgTool(w, r)
	require.Equal(t, 404, w.Code)
	h.cfg.AgentBoard.Enabled = true
	h.hostTools = hosttools.New(hosttools.Options{Tools: map[hosttools.ID]hosttools.Tool{
		hosttools.AgmsgTool: updatingHookTool{},
	}})
	w = httptest.NewRecorder()
	h.GetAgmsgTool(w, r)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "installed")
	w = httptest.NewRecorder()
	h.PostAgmsgTool(w, httptest.NewRequest(http.MethodPost, "/api/hosts/agmsg", strings.NewReader(`{"host":"unknown"}`)))
	require.Equal(t, 404, w.Code)
}
