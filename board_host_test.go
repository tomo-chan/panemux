package main

import (
	"context"
	"errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"io"
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
	cfg := boardEnabledConfig("/opt/agmsg", paneConfig("local-agent", config.PaneAgentBoardConfig{Enabled: boolPtr(true)}))
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
	pane := paneConfig("excluded", config.PaneAgentBoardConfig{Enabled: boolPtr(true)})
	pane.Type = "ssh"
	pane.Connection = "ssh-config-only"
	cfg := boardEnabledConfig("/opt/agmsg", pane)
	p := hostexec.New(hostexec.Options{})
	defer p.Close()
	_, relay, bootstrap := setupBoard(cfg, session.NewManager(), p)
	assert.False(t, relay.HasClients())
	assert.False(t, bootstrap.HasWork())
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

func TestHostAgmsgClientRetriesProbesAndSends(t *testing.T) {
	for _, failure := range []string{"home", "presence", "version", "none", "absent"} {
		t.Run(failure, func(t *testing.T) {
			logs := captureBoardLog(t)
			failed, sends := false, 0
			runner := hostRunnerFunc(func(_ context.Context, script string, args ...string) ([]byte, error) {
				require.Equal(t, `exec "$@"`, script)
				joined := strings.Join(args, " ")
				stage := "home"
				switch {
				case strings.Contains(joined, "board-agmsg-presence"):
					stage = "presence"
				case strings.Contains(joined, "board-agmsg-version"):
					stage = "version"
				case strings.Contains(joined, "board-send"):
					sends++
					require.Contains(t, joined, "/remote/home/demo/agmsg/scripts/send.sh")
					return nil, nil
				case strings.Contains(joined, "--limit"):
					return []byte("[]"), nil
				}
				if stage == failure && !failed {
					failed = true
					return nil, errors.New("temporarily offline")
				}
				switch stage {
				case "home":
					return []byte("/remote/home/demo"), nil
				case "presence":
					if failure == "absent" {
						return []byte("no"), nil
					}
					return []byte("yes"), nil
				default:
					return []byte(board.TestedAgmsgVersion), nil
				}
			})
			c := &hostAgmsgClient{id: "ssh:dev", path: "~/agmsg", runner: runner}
			_, err := c.Since(context.Background(), "panemux", "", 10)
			if failure == "home" || failure == "presence" {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			_, err = c.Since(context.Background(), "panemux", "", 10)
			require.NoError(t, err)
			err = c.Send(context.Background(), "panemux", "pane-a", "pane-b", "hello; $(false)")
			if failure == "absent" {
				require.ErrorContains(t, err, "not installed")
				require.Contains(t, logs.String(), "no agmsg installation")
				require.Zero(t, sends)
			} else {
				require.NoError(t, err)
				require.Equal(t, 1, sends)
			}

			if failure == "version" {
				require.Contains(t, logs.String(), "reading agmsg version")
			}
		})
	}
}

func TestBootstrapRecoversAfterPathResolutionFailure(t *testing.T) {
	sess := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager := session.NewManager()
	manager.Add(sess)
	attempts := 0
	watcher := newBootstrapWatcher(bootstrapWatcherConfig{
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
	path, err := resolveHostAgmsgPath(context.Background(), host.Bind(pool, host.Local), "~/agmsg")
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
	cfg := boardEnabledConfig("/opt/agmsg", paneConfig("local-agent", config.PaneAgentBoardConfig{Enabled: boolPtr(true)}))
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

func TestHostAgmsgVersionWarningOnce(t *testing.T) {
	for _, version := range []string{board.TestedAgmsgVersion, "0.0.1", ""} {
		t.Run(version, func(t *testing.T) {
			logs := captureBoardLog(t)
			probes := 0
			runner := hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				switch {
				case strings.Contains(joined, "board-agmsg-presence"):
					return []byte("yes"), nil
				case strings.Contains(joined, "board-agmsg-version"):
					probes++
					return []byte(version), nil
				default:
					return nil, nil
				}
			})
			client := &hostAgmsgClient{id: "ssh:dev", path: "/opt/agmsg", runner: runner}
			require.Equal(t, "ssh:dev", client.HostID())
			for range 2 {
				_, err := client.Since(context.Background(), "panemux", "", 10)
				require.NoError(t, err)
			}
			require.Equal(t, 1, probes)
			warning := board.VersionMismatchWarning("ssh:dev", version)
			if version == "0.0.1" {
				require.NotEmpty(t, warning)
				require.Equal(t, 1, strings.Count(logs.String(), warning))
			} else {
				require.Empty(t, logs.String())
			}
		})
	}
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

func TestHostAgmsgOperationErrors(t *testing.T) {
	for _, operation := range []string{"ready", "read", "send"} {
		t.Run(operation, func(t *testing.T) {
			sentinel := errors.New("command failed")
			runner := hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
				joined := strings.Join(args, " ")
				if operation == "ready" {
					return nil, sentinel
				}
				if strings.Contains(joined, "board-agmsg-presence") {
					return []byte("yes"), nil
				}
				if strings.Contains(joined, "board-agmsg-version") {
					return []byte(board.TestedAgmsgVersion), nil
				}
				return nil, sentinel
			})
			c := &hostAgmsgClient{id: "ssh:dev", path: "/opt/agmsg", runner: runner}
			if operation == "read" {
				_, err := c.Since(context.Background(), "panemux", "", 10)
				require.ErrorIs(t, err, sentinel)
				require.ErrorContains(t, err, "read host messages")
			} else {
				err := c.Send(context.Background(), "panemux", "pane-a", "pane-b", "body")
				require.ErrorIs(t, err, sentinel)
				if operation == "send" {
					require.ErrorContains(t, err, "send host message")
				}
			}
		})
	}
}

func TestSetupBoardBootstrapUsesSharedRunner(t *testing.T) {
	isolatedHome(t)
	sess := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager := session.NewManager()
	manager.Add(sess)
	cfg := boardEnabledConfig(installAgmsg(t, t.TempDir(), board.TestedAgmsgVersion),
		paneConfig("pane-a", config.PaneAgentBoardConfig{Enabled: boolPtr(true)}))
	srv, _, watcher := buildServerServices(cfg, manager)
	defer func() { require.NoError(t, srv.Shutdown(context.Background())) }()
	watcher.pollOnce(context.Background())
	watcher.pollOnce(context.Background())
	require.Len(t, sess.writes, 1)
	require.Contains(t, string(sess.writes[0]), cfg.AgentBoard.AgmsgPath)
}

func TestBootstrapWithoutHostRunnerDoesNotWrite(t *testing.T) {
	sess := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager := session.NewManager()
	manager.Add(sess)
	watcher := newBootstrapWatcher(bootstrapWatcherConfig{
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
			pane := paneConfig("remote-agent", config.PaneAgentBoardConfig{Enabled: boolPtr(true)})
			pane.Type, pane.Connection = "ssh", "dev"
			cfg := boardEnabledConfig("/opt/agmsg", pane)
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

func TestPollDoesNotWaitForSendInitialization(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()
	sender := hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "board-agmsg-presence") {
			close(started)
			<-release
			return []byte("yes"), nil
		}
		return nil, nil
	})
	poller := hostRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
		return nil, hostexec.ErrConnecting
	})
	c := &hostAgmsgClient{id: "ssh:dev", path: "/opt/agmsg", runner: poller, sendRunner: sender}
	sent := make(chan error, 1)
	go func() { sent <- c.Send(context.Background(), "panemux", "pane-a", "pane-b", "hello") }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("send probe did not start")
	}
	polled := make(chan error, 1)
	go func() { _, err := c.Since(context.Background(), "panemux", "", 10); polled <- err }()
	finished := false
	select {
	case err := <-polled:
		finished = true
		require.ErrorIs(t, err, hostexec.ErrConnecting)
	case <-time.After(100 * time.Millisecond):
		t.Error("poll waited for the send initialization")
	}
	close(release)
	require.NoError(t, <-sent)
	if !finished {
		<-polled
	}
}

func TestConcurrentHostDiscoveryPublishesOneVersionWarning(t *testing.T) {
	logs := captureBoardLog(t)
	entered, release := make(chan struct{}, 2), make(chan struct{})
	runner := hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "board-agmsg-presence") {
			entered <- struct{}{}
			<-release
			return []byte("yes"), nil
		}
		return []byte("0.0.1"), nil
	})
	c := &hostAgmsgClient{id: "ssh:dev", path: "/opt/agmsg", runner: runner}
	done := make(chan error, 2)
	for range 2 {
		go func() { _, err := c.Since(context.Background(), "panemux", "", 10); done <- err }()
	}
	<-entered
	<-entered
	close(release)
	for range 2 {
		require.NoError(t, <-done)
	}
	warning := board.VersionMismatchWarning("ssh:dev", "0.0.1")
	require.Equal(t, 1, strings.Count(logs.String(), warning))
}
