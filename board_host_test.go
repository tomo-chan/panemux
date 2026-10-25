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
	relay.Poll(context.Background())
	require.Equal(t, 0, c.reads)
	require.Equal(t, 1, dials)
	now = now.Add(time.Minute)
	relay.Poll(context.Background())
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
