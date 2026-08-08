package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/session"
)

func boardTestConfig(boardEnabled bool) *config.Config {
	cfg := testConfig()
	if boardEnabled {
		cfg.Workspaces.Items[0].Layout.Children[0].Pane.AgentBoard = &config.PaneAgentBoardConfig{Enabled: true}
		cfg.AgentBoard.Team = "panemux"
		cfg.AgentBoard.AgmsgPath = "/opt/agmsg"
	}
	return cfg
}

func TestWireAgentBoard_NoBoardEnabledPanes_ReturnsNilCancel(t *testing.T) {
	cfg := boardTestConfig(false)
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)
	t.Cleanup(func() { srv.Shutdown(context.Background()) }) //nolint:errcheck

	assert.Nil(t, srv.cancelBoard)
}

func TestWireAgentBoard_BoardEnabledLocalPane_EnablesBoardEndpoints(t *testing.T) {
	cfg := boardTestConfig(true)
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)
	t.Cleanup(func() { srv.Shutdown(context.Background()) }) //nolint:errcheck

	require.NotNil(t, srv.cancelBoard)

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	rr := httptest.NewRecorder()
	srv.httpSrv.Handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusOK, rr.Code)
}

func TestWireAgentBoard_NoPanesEnabled_BoardEndpointReturns503(t *testing.T) {
	cfg := boardTestConfig(false)
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)
	t.Cleanup(func() { srv.Shutdown(context.Background()) }) //nolint:errcheck

	req := httptest.NewRequest(http.MethodGet, "/api/board/status", nil)
	rr := httptest.NewRecorder()
	srv.httpSrv.Handler.ServeHTTP(rr, req)
	assert.Equal(t, http.StatusServiceUnavailable, rr.Code)
}

func TestShutdown_CancelsBoardRelay(t *testing.T) {
	cfg := boardTestConfig(true)
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)

	require.NotNil(t, srv.cancelBoard)
	err := srv.Shutdown(context.Background())
	require.NoError(t, err)
	// A second Shutdown call must not panic on an already-canceled context.
	require.NoError(t, srv.Shutdown(context.Background()))
}

func TestBoardEnabledPanes_FiltersToEnabledOnly(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.Workspaces.Items[0].Layout.Children = append(cfg.Workspaces.Items[0].Layout.Children,
		config.LayoutChild{Size: 0, Pane: &config.PaneConfig{
			ID: "disabled-pane", Type: "local",
			AgentBoard: &config.PaneAgentBoardConfig{Enabled: false},
		}},
		config.LayoutChild{Size: 0, Pane: &config.PaneConfig{
			ID: "enabled-pane", Type: "local",
			AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
		}},
	)

	panes := boardEnabledPanes(cfg)
	require.Len(t, panes, 1)
	assert.Equal(t, "enabled-pane", panes[0].ID)
}

func TestPaneBoardHostID(t *testing.T) {
	assert.Equal(t, "local", paneBoardHostID(&config.PaneConfig{Type: "local"}))
	assert.Equal(t, "local", paneBoardHostID(&config.PaneConfig{Type: "tmux"}))
	assert.Equal(t, "build-host", paneBoardHostID(&config.PaneConfig{Type: "ssh", Connection: "build-host"}))
	assert.Equal(t, "build-host", paneBoardHostID(&config.PaneConfig{Type: "ssh_tmux", Connection: "build-host"}))
}

func TestRegisterRemoteBoardClient_NoLiveSession_LogsAndSkips(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.AgentBoard.Team = "panemux"
	pane := &config.PaneConfig{
		ID: "remote-pane", Type: "ssh", Connection: "build-host",
		AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
	}
	mgr := session.NewManager() // no session registered for "remote-pane"

	resolver := board.NewStaticPaneResolver(nil)
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

	// Must not panic even though no live session exists for the host.
	registerRemoteBoardClient(relay, cfg, mgr, []*config.PaneConfig{pane}, "build-host")
}
