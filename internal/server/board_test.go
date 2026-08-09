package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

// Regression test for wireAgentBoard's defensive empty-team fallback: a
// caller-constructed Config that skipped config.Load()/config.Default()'s
// normalization must still end up relaying under config.DefaultAgentBoardTeam,
// not an empty agmsg team. Verified end-to-end via a fake local send.sh that
// records the team argument it was actually invoked with, since the team
// value is otherwise internal to the unexported Relay it's threaded through.
func TestWireAgentBoard_EmptyTeam_FallsBackToDefaultTeam(t *testing.T) {
	agmsgDir := t.TempDir()
	scriptsDir := filepath.Join(agmsgDir, "scripts")
	require.NoError(t, os.MkdirAll(scriptsDir, 0750))
	logPath := filepath.Join(t.TempDir(), "send-args.log")
	script := "#!/bin/sh\nprintf '%s\\n' \"$1\" > \"" + logPath + "\"\n"
	sendPath := filepath.Join(scriptsDir, "send.sh")
	apiPath := filepath.Join(scriptsDir, "api.sh")
	//nolint:gosec // G306: test-only fixture scripts need the executable bit
	require.NoError(t, os.WriteFile(sendPath, []byte(script), 0700))
	//nolint:gosec // G306: test-only fixture scripts need the executable bit
	require.NoError(t, os.WriteFile(apiPath, []byte("#!/bin/sh\nexit 0\n"), 0700))

	cfg := boardTestConfig(true)
	cfg.AgentBoard.Team = "" // exercise wireAgentBoard's empty-team defensive fallback
	cfg.AgentBoard.AgmsgPath = agmsgDir
	mgr := session.NewManager()
	srv := New(cfg, mgr, emptyFS)
	t.Cleanup(func() { srv.Shutdown(context.Background()) }) //nolint:errcheck

	localPaneID := cfg.Workspaces.Items[0].Layout.Children[0].Pane.ID
	body := `{"to":["` + localPaneID + `"],"body":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/board/broadcast", strings.NewReader(body))
	rr := httptest.NewRecorder()
	srv.httpSrv.Handler.ServeHTTP(rr, req)
	require.Equal(t, http.StatusOK, rr.Code)

	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	assert.Equal(t, config.DefaultAgentBoardTeam+"\n", string(data))
}

// Regression test for wireAgentBoard's DefaultCursorPath failure fallback:
// when the home directory cannot be resolved (simulated here via an unset
// $HOME, which is what the real os.UserHomeDir() consults), the relay must
// still start with an in-memory CursorStore rather than failing server
// startup — Agent Board is additive, never load-bearing.
func TestWireAgentBoard_DefaultCursorPathFailure_FallsBackToMemoryCursors(t *testing.T) {
	t.Setenv("HOME", "")

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

// Regression test for wireAgentBoard's relay.LoadCursors() failure handling:
// a corrupted (but present) cursor file must not fail server startup — the
// relay must still start, from an empty cursor state, logging the failure
// instead.
func TestWireAgentBoard_LoadCursorsFailure_StartsFromEmptyCursors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cursorDir := filepath.Join(home, ".config", "panemux")
	require.NoError(t, os.MkdirAll(cursorDir, 0750))
	require.NoError(t, os.WriteFile(filepath.Join(cursorDir, "board-relay-cursor.json"), []byte("not json"), 0600))

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

func TestRegisterBoardClients_LocalAgmsgPathExpandFailure_LogsAndSkips(t *testing.T) {
	t.Setenv("HOME", "") // makes ExpandLocalAgmsgPath fail for a "~"-relative path
	cfg := boardTestConfig(false)
	cfg.AgentBoard.Team = "panemux"
	cfg.AgentBoard.AgmsgPath = "~/.agents/skills/agmsg"
	pane := &config.PaneConfig{ID: "local-pane", Type: "local", AgentBoard: &config.PaneAgentBoardConfig{Enabled: true}}
	mgr := session.NewManager()

	resolver := board.NewStaticPaneResolver([]board.PaneRef{{ID: "local-pane", HostID: board.LocalHostID}})
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

	registerBoardClients(relay, cfg, mgr, []*config.PaneConfig{pane}, map[string]bool{board.LocalHostID: true})

	assert.False(t, relay.HasClient(board.LocalHostID))
}

// Exercises registerBoardClients' own loop into registerRemoteBoardClient
// for a remote host (existing tests call registerRemoteBoardClient
// directly, but never through registerBoardClients itself).
func TestRegisterBoardClients_RemoteHostInSet_RegistersRemoteClient(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.AgentBoard.Team = "panemux"
	cfg.AgentBoard.AgmsgPath = "/opt/agmsg"
	pane := &config.PaneConfig{
		ID: "remote-pane", Type: paneTypeSSH, Connection: "build-host",
		AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
	}
	mgr := session.NewManager()
	mgr.Add(&fakeRemoteSession{id: "remote-pane"})

	resolver := board.NewStaticPaneResolver([]board.PaneRef{{ID: "remote-pane", HostID: "build-host"}})
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

	registerBoardClients(relay, cfg, mgr, []*config.PaneConfig{pane}, map[string]bool{"build-host": true})

	assert.True(t, relay.HasClient("build-host"))
}

// Regression test for registerRemoteBoardClient's per-pane host filter: a
// pane on a different host must be skipped (not matched, not error) before
// the loop reaches the pane that actually belongs to the target host.
func TestRegisterRemoteBoardClient_SkipsPanesOnOtherHosts(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.AgentBoard.Team = "panemux"
	cfg.AgentBoard.AgmsgPath = "/opt/agmsg"
	otherPane := &config.PaneConfig{
		ID: "other-pane", Type: paneTypeSSH, Connection: "other-host",
		AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
	}
	targetPane := &config.PaneConfig{
		ID: "remote-pane", Type: paneTypeSSH, Connection: "build-host",
		AgentBoard: &config.PaneAgentBoardConfig{Enabled: true},
	}
	mgr := session.NewManager()
	mgr.Add(&fakeRemoteSession{id: "other-pane"})
	mgr.Add(&fakeRemoteSession{id: "remote-pane"})

	resolver := board.NewStaticPaneResolver([]board.PaneRef{{ID: "remote-pane", HostID: "build-host"}})
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

	registerRemoteBoardClient(relay, cfg, mgr, []*config.PaneConfig{otherPane, targetPane}, "build-host")

	assert.True(t, relay.HasClient("build-host"))
}

// fakeExecutorOnlySession implements session.Session and
// session.BoardExecutor but deliberately not session.BoardHomeDirer, for
// exercising registerRemoteClientFromSession's missing-capability branch.
type fakeExecutorOnlySession struct{ id string }

func (f *fakeExecutorOnlySession) ID() string                     { return f.id }
func (f *fakeExecutorOnlySession) Type() session.Type             { return session.TypeSSH }
func (f *fakeExecutorOnlySession) Title() string                  { return f.id }
func (f *fakeExecutorOnlySession) State() session.State           { return session.StateConnected }
func (f *fakeExecutorOnlySession) Read(p []byte) (int, error)     { return 0, nil }
func (f *fakeExecutorOnlySession) Write(p []byte) (int, error)    { return len(p), nil }
func (f *fakeExecutorOnlySession) Resize(cols, rows uint16) error { return nil }
func (f *fakeExecutorOnlySession) Close() error                   { return nil }

func (f *fakeExecutorOnlySession) RunBoardCommand(_ context.Context, _ string, _ []string) ([]byte, error) {
	return nil, nil
}

func TestRegisterRemoteClientFromSession_MissingBoardHomeDirer_ReturnsFalse(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.AgentBoard.AgmsgPath = "/opt/agmsg"
	resolver := board.NewStaticPaneResolver(nil)
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

	ok := registerRemoteClientFromSession(relay, cfg, "build-host", &fakeExecutorOnlySession{id: "remote-pane"})

	assert.False(t, ok)
	assert.False(t, relay.HasClient("build-host"))
}

// fakeRemoteSessionHomeDirError implements the full remote board capability
// set but always fails to resolve its home directory, for exercising
// registerRemoteClientFromSession's "~"-expansion failure branch.
type fakeRemoteSessionHomeDirError struct{ id string }

func (f *fakeRemoteSessionHomeDirError) ID() string                     { return f.id }
func (f *fakeRemoteSessionHomeDirError) Type() session.Type             { return session.TypeSSH }
func (f *fakeRemoteSessionHomeDirError) Title() string                  { return f.id }
func (f *fakeRemoteSessionHomeDirError) State() session.State           { return session.StateConnected }
func (f *fakeRemoteSessionHomeDirError) Read(p []byte) (int, error)     { return 0, nil }
func (f *fakeRemoteSessionHomeDirError) Write(p []byte) (int, error)    { return len(p), nil }
func (f *fakeRemoteSessionHomeDirError) Resize(cols, rows uint16) error { return nil }
func (f *fakeRemoteSessionHomeDirError) Close() error                   { return nil }
func (f *fakeRemoteSessionHomeDirError) BoardHostID() string            { return "build-host" }

func (f *fakeRemoteSessionHomeDirError) BoardHomeDir(_ context.Context) (string, error) {
	return "", errors.New("home dir lookup failed")
}

func (f *fakeRemoteSessionHomeDirError) RunBoardCommand(_ context.Context, _ string, _ []string) ([]byte, error) {
	return nil, nil
}

func TestRegisterRemoteClientFromSession_BoardHomeDirFailure_ReturnsFalse(t *testing.T) {
	cfg := boardTestConfig(false)
	cfg.AgentBoard.AgmsgPath = "~/.agents/skills/agmsg"
	resolver := board.NewStaticPaneResolver(nil)
	relay := board.NewRelay(board.NewBoardCache(), resolver, board.NewMemCursorStore(), nil)

	ok := registerRemoteClientFromSession(relay, cfg, "build-host", &fakeRemoteSessionHomeDirError{id: "remote-pane"})

	assert.False(t, ok)
	assert.False(t, relay.HasClient("build-host"))
}
