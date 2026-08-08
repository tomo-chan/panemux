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
	assert.Equal(t, "build-host", paneBoardHostID(&config.PaneConfig{Type: paneTypeSSH, Connection: "build-host"}))
	assert.Equal(t, "build-host", paneBoardHostID(&config.PaneConfig{Type: "ssh_tmux", Connection: "build-host"}))
}

// fakeRemoteSession implements session.Session plus session.BoardExecutor
// and session.BoardHomeDirer, so registerRemoteBoardClient can find it via
// manager.Get and build a RemoteAgmsgClient from it.
type fakeRemoteSession struct {
	id string
}

func (f *fakeRemoteSession) ID() string                     { return f.id }
func (f *fakeRemoteSession) Type() session.Type             { return session.TypeSSH }
func (f *fakeRemoteSession) Title() string                  { return f.id }
func (f *fakeRemoteSession) State() session.State           { return session.StateConnected }
func (f *fakeRemoteSession) Read(p []byte) (int, error)     { return 0, nil }
func (f *fakeRemoteSession) Write(p []byte) (int, error)    { return len(p), nil }
func (f *fakeRemoteSession) Resize(cols, rows uint16) error { return nil }
func (f *fakeRemoteSession) Close() error                   { return nil }
func (f *fakeRemoteSession) BoardHostID() string            { return "build-host" }
func (f *fakeRemoteSession) BoardHomeDir(_ context.Context) (string, error) {
	return "/home/build-user", nil
}

func (f *fakeRemoteSession) RunBoardCommand(_ context.Context, _ string, _ []string) ([]byte, error) {
	return nil, nil
}

func TestRegisterRemoteBoardClient_LiveCapableSession_RegistersClient(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.AgentBoard.Team = "panemux"
	cfg.AgentBoard.AgmsgPath = "~/.agents/skills/agmsg"
	pane := &config.PaneConfig{
		ID: "remote-pane", Type: paneTypeSSH, Connection: "build-host",
		AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
	}
	mgr := session.NewManager()
	mgr.Add(&fakeRemoteSession{id: "remote-pane"})

	resolver := board.NewStaticPaneResolver([]board.PaneRef{{ID: "remote-pane", HostID: "build-host"}})
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

	registerRemoteBoardClient(relay, cfg, mgr, []*config.PaneConfig{pane}, "build-host")

	assert.True(t, relay.HasClient("build-host"))
}

func TestRegisterRemoteBoardClient_NoLiveSession_LogsAndSkips(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.AgentBoard.Team = "panemux"
	pane := &config.PaneConfig{
		ID: "remote-pane", Type: paneTypeSSH, Connection: "build-host",
		AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
	}
	mgr := session.NewManager() // no session registered for "remote-pane"

	resolver := board.NewStaticPaneResolver(nil)
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

	// Must not panic even though no live session exists for the host.
	registerRemoteBoardClient(relay, cfg, mgr, []*config.PaneConfig{pane}, "build-host")
}

// bareSession implements session.Session only — no board capabilities —
// for asserting reregisterBoardClientAfterRestart no-ops on a session that
// can't satisfy BoardExecutor/BoardHomeDirer (e.g. a local session type).
type bareSession struct{ id string }

func (f *bareSession) ID() string                     { return f.id }
func (f *bareSession) Type() session.Type             { return session.TypeLocal }
func (f *bareSession) Title() string                  { return f.id }
func (f *bareSession) State() session.State           { return session.StateConnected }
func (f *bareSession) Read(p []byte) (int, error)     { return 0, nil }
func (f *bareSession) Write(p []byte) (int, error)    { return len(p), nil }
func (f *bareSession) Resize(cols, rows uint16) error { return nil }
func (f *bareSession) Close() error                   { return nil }

// Regression tests for the PR #163 review finding: wireAgentBoard only ever
// registered a host's AgmsgClient once, at server startup, using whatever
// session happened to already be live in the manager. A host whose only
// session dialed successfully only *after* startup (e.g. a transient SSH
// failure followed by a later POST /api/sessions/{id}/restart) never got a
// second chance at registration, silently losing board relay for the rest
// of the process's life. reregisterBoardClientAfterRestart is the hook
// RestartSession now invokes on every successful restart so that gap closes
// without needing a full panemux restart.
func TestReregisterBoardClientAfterRestart_BoardEnabledSSHPane_RegistersClient(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.AgentBoard.Team = "panemux"
	cfg.AgentBoard.AgmsgPath = "/opt/agmsg"
	pane := &config.PaneConfig{
		ID: "remote-pane", Type: paneTypeSSH, Connection: "build-host",
		AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
	}
	resolver := board.NewStaticPaneResolver([]board.PaneRef{{ID: "remote-pane", HostID: "build-host"}})
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)
	require.False(t, relay.HasClient("build-host"))

	reregisterBoardClientAfterRestart(relay, cfg, pane, &fakeRemoteSession{id: "remote-pane"})

	assert.True(t, relay.HasClient("build-host"))
}

// reregisterBoardClientAfterRestart no-ops for two distinct reasons — a
// board-disabled pane, or a session that doesn't implement the required
// board capabilities — table-driven since both cases assert the same
// outcome (no client registered) from the same setup shape.
func TestReregisterBoardClientAfterRestart_NoOpCases(t *testing.T) {
	tests := []struct { //nolint:govet // fieldalignment: clarity preferred
		name    string
		pane    *config.PaneConfig
		session session.Session
	}{
		{
			name: "board disabled pane",
			pane: &config.PaneConfig{
				ID: "remote-pane", Type: paneTypeSSH, Connection: "build-host",
				AgentBoard: &config.PaneAgentBoardConfig{Enabled: false},
			},
			session: &fakeRemoteSession{id: "remote-pane"},
		},
		{
			// bareSession doesn't implement BoardExecutor/BoardHomeDirer —
			// must not panic and must not register a client.
			name: "session missing board capabilities",
			pane: &config.PaneConfig{
				ID: "remote-pane", Type: paneTypeSSH, Connection: "build-host",
				AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
			},
			session: &bareSession{id: "remote-pane"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := boardTestConfig(false)
			cfg.AgentBoard.Team = "panemux"
			cfg.AgentBoard.AgmsgPath = "/opt/agmsg"
			resolver := board.NewStaticPaneResolver([]board.PaneRef{{ID: "remote-pane", HostID: "build-host"}})
			relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

			reregisterBoardClientAfterRestart(relay, cfg, tt.pane, tt.session)

			assert.False(t, relay.HasClient("build-host"))
		})
	}
}

func TestReregisterBoardClientAfterRestart_LocalPane_NoOp(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.AgentBoard.Team = "panemux"
	cfg.AgentBoard.AgmsgPath = "/opt/agmsg"
	pane := &config.PaneConfig{
		ID: "local-pane", Type: "local",
		AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
	}
	resolver := board.NewStaticPaneResolver([]board.PaneRef{{ID: "local-pane", HostID: board.LocalHostID}})
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

	// The local AgmsgClient is registered once at startup and lives for the
	// process's whole lifetime — a local pane restarting must not touch it.
	reregisterBoardClientAfterRestart(relay, cfg, pane, &bareSession{id: "local-pane"})

	assert.False(t, relay.HasClient(board.LocalHostID))
}

func TestWireAgentBoard_BoardEnabled_ConfiguresSessionRestartHook(t *testing.T) {
	cfg := boardTestConfig(true)
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)
	t.Cleanup(func() { srv.Shutdown(context.Background()) }) //nolint:errcheck

	assert.True(t, srv.apiHandler.BoardSessionRestartHookConfigured())
}

func TestWireAgentBoard_NoBoardEnabledPanes_DoesNotConfigureSessionRestartHook(t *testing.T) {
	cfg := boardTestConfig(false)
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)
	t.Cleanup(func() { srv.Shutdown(context.Background()) }) //nolint:errcheck

	assert.False(t, srv.apiHandler.BoardSessionRestartHookConfigured())
}
