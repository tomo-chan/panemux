package main

import (
	"context"
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
	"panemux/internal/agmsg"
	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/host"
	"panemux/internal/hostexec"
	"panemux/internal/session"
	"panemux/internal/tasks"
	"strings"
	"sync"
	"testing"
	"time"
)

type boardCommandConn struct {
	mu    sync.Mutex
	reads int
	calls int
}

func (c *boardCommandConn) Run(_ context.Context, cmd string, in io.Reader) ([]byte, error) {
	script, _ := io.ReadAll(in)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	switch {
	case strings.Contains(cmd, "board-send"):
		return nil, nil
	case strings.Contains(cmd, "board-agmsg-presence"):
		return []byte("yes"), nil
	case strings.Contains(cmd, "board-agmsg-version"):
		return []byte("1.2.0"), nil
	case strings.Contains(cmd, "$HOME"):
		return []byte("/remote/home/demo"), nil
	case strings.Contains(cmd, "--limit"):
		c.reads++
		return []byte("[]"), nil
	case strings.Contains(string(script), "::panemux-tasks"):
		var out strings.Builder
		out.WriteString("::panemux-tasks v2\n::now 1\n")
		for _, line := range strings.Split(string(script), "\n") {
			if section, ok := strings.CutPrefix(line, "echo '::section "); ok {
				out.WriteString("::section " + strings.TrimSuffix(section, "'") + "\n")
			}
		}
		out.WriteString("::end\n")
		return []byte(out.String()), nil
	default:
		return nil, errors.New("unexpected host execution")
	}
}
func (*boardCommandConn) Ping(context.Context) error { return nil }
func (*boardCommandConn) Close() error               { return nil }
func (*boardCommandConn) InspectGitContext(context.Context, string) (session.GitContext, error) {
	return session.GitContext{}, nil
}

func TestBoardHostWithoutPanesSharesTaskConnectionAndRecovers(t *testing.T) {
	isolatedHome(t)
	now := time.Unix(1, 0)
	dials := 0
	c := &boardCommandConn{}
	p := hostexec.New(hostexec.Options{Hosts: func() []string { return []string{"dev"} },
		Now: func() time.Time { return now },
		Dial: func(string) (hostexec.Conn, error) {
			dials++
			if dials == 1 {
				return nil, errors.New("offline")
			}
			return c, nil
		}})
	t.Cleanup(p.Close)
	cfg := boardEnabledConfig(paneConfig("local-agent"))
	cfg.SSHConnections = map[string]config.SSHConnection{"dev": {}}
	_, relay, _ := setupBoard(cfg, session.NewManager(), p)
	_, err := p.Connect(context.Background(), "dev")
	require.Error(t, err)
	relay.Poll(context.Background())
	require.Equal(t, 0, c.reads)
	require.Equal(t, 1, dials)
	now = now.Add(time.Minute)
	require.Eventually(t, func() bool {
		relay.Poll(context.Background())
		return c.reads == 1
	}, time.Second, time.Millisecond)
	require.Equal(t, 1, c.reads)
	require.Equal(t, 2, dials)
	svc := tasks.New(tasks.Options{Execution: p, Hosts: func() []string { return []string{"dev"} }})
	t.Cleanup(svc.Close)
	result, _ := svc.CollectHostLive(context.Background(), "dev")
	require.Equal(t, tasks.HostOK, result.Status, result.Error)
	relay.Poll(context.Background())
	require.Equal(t, 2, dials, "tasks and board must reuse the same connection without a remote pane")
}
func TestBoardPaneHostMatchesOnlyConfiguredName(t *testing.T) {
	cfg := &config.Config{Data: config.Data{SSHConnections: map[string]config.SSHConnection{"local": {}, "dev": {}}}}
	for _, kind := range []string{"ssh", "ssh_tmux"} {
		got, ok := boardHostForPane(cfg, &config.PaneConfig{Type: kind, Connection: "dev"})
		require.True(t, ok)
		assert.Equal(t, host.Host("dev"), got)
		got, ok = boardHostForPane(cfg, &config.PaneConfig{Type: kind, Connection: "local"})
		require.True(t, ok)
		assert.Equal(t, "ssh:local", got.BoardID())
		_, ok = boardHostForPane(cfg, &config.PaneConfig{Type: kind, Connection: "ssh-config-only"})
		assert.False(t, ok)
	}
	got, ok := boardHostForPane(cfg, &config.PaneConfig{Type: "tmux"})
	require.True(t, ok)
	assert.Equal(t, host.Local, got)
}
func TestBoardExcludedPaneWarnsWithConfigurationRemedy(t *testing.T) {
	isolatedHome(t)
	logs := captureBoardLog(t)
	pane := paneConfig("excluded")
	pane.Type = "ssh"
	pane.Connection = "ssh-config-only"
	cfg := boardEnabledConfig(pane)
	p := hostexec.New(hostexec.Options{})
	defer p.Close()
	_, relay, bootstrap := setupBoard(cfg, session.NewManager(), p)
	assert.True(t, relay.HasClients())
	assert.True(t, bootstrap.HasWork())
	require.NotContains(t, bootstrap.paneHosts, "excluded")
	assert.Contains(t, logs.String(), "ssh_connections")
	assert.Contains(t, logs.String(), "ssh-config-only")
}

func setupTestBoard(
	t *testing.T, cfg *config.Config, manager *session.Manager,
) (*board.BoardCache, *board.Relay, *bootstrapWatcher) {
	t.Helper()
	p := hostexec.New(hostexec.Options{})
	t.Cleanup(p.Close)
	return setupBoard(cfg, manager, p)
}

type hostRunnerFunc func(context.Context, string, ...string) ([]byte, error)

func (f hostRunnerFunc) Run(ctx context.Context, script string, args ...string) ([]byte, error) {
	return f(ctx, script, args...)
}

func TestBootstrapRecoversAfterPathResolutionFailure(t *testing.T) {
	sess := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager := session.NewManager()
	manager.Add(sess)
	attempts := 0
	watcher := newTestBootstrapWatcher(testBootstrapWatcherConfig{
		Manager: manager, PaneHosts: map[string]string{"pane-a": "ssh:dev"}, Team: "panemux",
		ResolvePath: func(context.Context, string) (string, error) {
			attempts++
			if attempts == 1 {
				return "", errors.New("offline")
			}
			return "/remote/home/demo/agmsg", nil
		},
		RunnerForHost: func(string) host.Runner {
			return hostRunnerFunc(func(context.Context, string, ...string) ([]byte, error) { return []byte("yes"), nil })
		},
	})
	watcher.pollOnce(context.Background())
	watcher.pollOnce(context.Background())
	require.Empty(t, sess.writes)
	watcher.pollOnce(context.Background())
	require.Len(t, sess.writes, 1)
	require.Contains(t, string(sess.writes[0]), "/remote/home/demo/agmsg")
	require.Equal(t, 2, attempts)
}

func TestHostPathResolutionUsesLocalRunner(t *testing.T) {
	home := isolatedHome(t)
	pool := hostexec.New(hostexec.Options{})
	defer pool.Close()
	path, err := agmsg.ResolveRemoteAgmsgPath(context.Background(), host.Bind(pool, host.Local), "~/agmsg")
	require.NoError(t, err)
	require.Equal(t, home+"/agmsg", path)
}

func TestBoardPendingDialDoesNotDelayHealthyHost(t *testing.T) {
	isolatedHome(t)
	blocked := make(chan struct{})
	defer close(blocked)
	live := &boardCommandConn{}
	pool := hostexec.New(hostexec.Options{
		Hosts: func() []string { return []string{"a-unused", "b-live"} },
		Dial: func(name string) (hostexec.Conn, error) {
			if name == "a-unused" {
				<-blocked
				return nil, errors.New("offline")
			}
			return live, nil
		},
	})
	defer pool.Close()
	_, err := pool.Connect(context.Background(), "b-live")
	require.NoError(t, err)
	cfg := boardEnabledConfig(paneConfig("local-agent"))
	cfg.SSHConnections = map[string]config.SSHConnection{"a-unused": {}, "b-live": {}}
	_, relay, _ := setupBoard(cfg, session.NewManager(), pool)
	for range 2 {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		started := time.Now()
		require.Error(t, relay.Poll(ctx))
		cancel()
		require.Less(t, time.Since(started), 250*time.Millisecond, "a pending dial must not consume the poll deadline")
	}
	require.Equal(t, 2, live.reads)
}

func TestTaskServiceBorrowsSharedExecutionLifetime(t *testing.T) {
	conn := &boardCommandConn{}
	pool := hostexec.New(hostexec.Options{Hosts: func() []string { return []string{"dev"} },
		Dial: func(string) (hostexec.Conn, error) { return conn, nil }})
	defer pool.Close()
	svc := tasks.New(tasks.Options{Execution: pool})
	svc.Close()
	_, err := pool.Run(context.Background(), "dev", "", "--limit")
	require.NoError(t, err, "closing a consumer must not close the caller-owned executor")
}

func TestBuildServerServicesWiresSharedHosts(t *testing.T) {
	isolatedHome(t)
	cfg := config.Default()
	srv, relay, watcher := buildServerServices(cfg, session.NewManager())
	require.NotNil(t, srv)
	require.NotNil(t, relay)
	require.NotNil(t, watcher)
	require.False(t, relay.HasClients())
	require.NoError(t, srv.Shutdown(context.Background()))
}

func TestSetupBoardBootstrapUsesSharedRunner(t *testing.T) {
	isolatedHome(t)
	sess := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager := session.NewManager()
	manager.Add(sess)
	installAgmsg(t, agmsg.TestedAgmsgVersion)
	cfg := boardEnabledConfig(paneConfig("pane-a"))
	srv, _, watcher := buildServerServices(cfg, manager)
	defer func() { require.NoError(t, srv.Shutdown(context.Background())) }()
	watcher.pollOnce(context.Background())
	watcher.pollOnce(context.Background())
	require.Len(t, sess.writes, 1)
	require.Contains(t, string(sess.writes[0]), "/.agents/skills/agmsg/scripts/join.sh")
}

func TestBootstrapWithoutHostRunnerDoesNotWrite(t *testing.T) {
	sess := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager := session.NewManager()
	manager.Add(sess)
	watcher := newTestBootstrapWatcher(testBootstrapWatcherConfig{
		Manager: manager, PaneHosts: map[string]string{"pane-a": "ssh:dev"},
		ResolvedPaths: map[string]string{"ssh:dev": "/opt/agmsg"}, Team: "panemux",
		RunnerForHost: func(string) host.Runner { return nil },
	})
	watcher.pollOnce(context.Background())
	watcher.pollOnce(context.Background())
	require.Empty(t, sess.writes)
}

func TestBoardSendWaitsForDialBeforePollAndAfterConnectionLoss(t *testing.T) {
	for _, state := range []string{"initial", "reconnect", "dropped"} {
		t.Run(state, func(t *testing.T) {
			isolatedHome(t)
			gate, started := make(chan struct{}), make(chan struct{}, 1)
			if state != "initial" {
				close(gate)
			}
			defer func() {
				select {
				case <-gate:
				default:
					close(gate)
				}
			}()
			conn := &boardCommandConn{}
			pool := hostexec.New(hostexec.Options{Hosts: func() []string { return []string{"dev"} },
				Dial: func(string) (hostexec.Conn, error) { started <- struct{}{}; <-gate; return conn, nil }})
			defer pool.Close()
			pane := paneConfig("remote-agent")
			pane.Type, pane.Connection = "ssh", "dev"
			cfg := boardEnabledConfig(pane)
			cfg.SSHConnections = map[string]config.SSHConnection{"dev": {}}
			_, relay, _ := setupBoard(cfg, session.NewManager(), pool)
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if state != "initial" {
				_, err := pool.Connect(ctx, "dev")
				require.NoError(t, err)
				<-started
				require.NoError(t, relay.Poll(ctx))
				if state == "reconnect" {
					require.NoError(t, pool.Reconnect("dev"))
				} else {
					_, err = pool.Run(ctx, "dev", "force transport failure")
					require.Error(t, err)
				}
				gate = make(chan struct{})
			}
			done := make(chan error, 1)
			go func() { _, err := relay.Broadcast(ctx, board.SystemID, []string{"remote-agent"}, "hello"); done <- err }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("send did not start a dial")
			}
			select {
			case err := <-done:
				t.Fatalf("send returned before dial completion: %v", err)
			default:
			}
			close(gate)
			select {
			case err := <-done:
				require.NoError(t, err)
			case <-ctx.Done():
				t.Fatal("send did not finish after dial completion")
			}
		})
	}
}

func TestGlobalBoardIncludesShellAddressesAndPublishesParticipation(t *testing.T) {
	isolatedHome(t)
	installAgmsg(t, agmsg.TestedAgmsgVersion)
	manager := session.NewManager()
	agentSession := &fakeAgentSession{id: "agent", detectType: "codex", detectOK: true}
	manager.Add(agentSession)
	cfg := boardEnabledConfig(paneConfig("agent"), paneConfig("shell"))
	remote := paneConfig("remote-shell")
	remote.Type, remote.Connection = "ssh", "build"
	cfg.Workspaces.Items[0].Layout.Children = append(cfg.Workspaces.Items[0].Layout.Children,
		config.LayoutChild{Pane: remote})
	cfg.SSHConnections = map[string]config.SSHConnection{"build": {}}
	cache, _, watcher := setupTestBoard(t, cfg, manager)
	require.Equal(t, map[string]string{"agent": "local", "shell": "local", "remote-shell": "ssh:build"}, watcher.paneHosts)
	watcher.pollOnce(context.Background())
	state := cache.ParticipationSnapshot()["agent"]
	require.Equal(t, "codex", state.AgentType)
	require.False(t, state.Instructed)
	watcher.pollOnce(context.Background())
	require.True(t, cache.ParticipationSnapshot()["agent"].Instructed)
	require.False(t, cache.ParticipationSnapshot()["shell"].Instructed)
	require.Empty(t, cache.ParticipationSnapshot()["shell"].AgentType)
	agentSession.detectOK = false
	watcher.pollOnce(context.Background())
	require.Empty(t, cache.ParticipationSnapshot()["agent"].AgentType)
	require.True(t, cache.ParticipationSnapshot()["agent"].Instructed)
	cfg.AgentBoard.Enabled = false
	_, relay, disabled := setupTestBoard(t, cfg, manager)
	require.False(t, relay.HasClients())
	require.False(t, disabled.HasWork())
}

func TestBoardTracksPanesAddedAndRemovedAfterStartup(t *testing.T) {
	isolatedHome(t)
	installAgmsg(t, agmsg.TestedAgmsgVersion)
	cfg := boardEnabledConfig()
	manager := session.NewManager()
	cache, relay, watcher := setupTestBoard(t, cfg, manager)
	require.True(t, watcher.HasWork(), "enabled board must keep watching an initially empty layout")
	require.True(t, relay.HasClients())
	added := &fakeAgentSession{id: "added", detectType: "codex", detectOK: true}
	manager.Add(added)
	cfg.Workspaces.Items[0].Layout.Children = []config.LayoutChild{{Pane: paneConfig("added")}}
	watcher.pollOnce(context.Background())
	watcher.pollOnce(context.Background())
	require.Len(t, added.writes, 1)
	require.True(t, cache.ParticipationSnapshot()["added"].Instructed)
	cfg.Workspaces.Items[0].Layout.Children = nil
	_, err := relay.Broadcast(context.Background(), board.SystemID, []string{"added"}, "removed")
	require.ErrorContains(t, err, "unknown")
	watcher.pollOnce(context.Background())
	require.NotContains(t, cache.ParticipationSnapshot(), "added")
}

func TestBootstrapForgetsInstructionWhenPaneChangesHost(t *testing.T) {
	for _, next := range []string{"host-a", "host-b", ""} {
		t.Run(next, func(t *testing.T) {
			current := map[string]string{}
			if next != "" {
				current["pane"] = next
			}
			watcher := newBootstrapWatcher(bootstrapWatcherConfig{
				PaneHosts:        map[string]string{"pane": "host-a"},
				CurrentPaneHosts: func() map[string]string { return current },
			})
			watcher.bootstrapped["pane"] = &fakeAgentSession{id: "pane"}
			watcher.refreshPanes()
			_, instructed := watcher.bootstrapped["pane"]
			require.Equal(t, next == "host-a", instructed)
			require.Equal(t, current, watcher.paneHosts)
		})
	}
}
