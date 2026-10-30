package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/config"
	"panemux/internal/host"
	"panemux/internal/hosttools"
	"panemux/internal/session"
)

type manualAgmsgTool struct{ err error }

func (manualAgmsgTool) Check(context.Context, host.Host) hosttools.State {
	return hosttools.State{Status: hosttools.Missing}
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
			require.JSONEq(t, `{"status":"installed","version":"1.2.0"}`, w.Body.String())
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
