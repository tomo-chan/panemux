package hostexec

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"panemux/internal/host"
	"panemux/internal/session"
)

type testConn struct {
	fail              error
	commands, scripts []string
	mu                sync.Mutex
	closed            int
}

func (c *testConn) Run(_ context.Context, cmd string, in io.Reader) ([]byte, error) {
	b, _ := io.ReadAll(in)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commands = append(c.commands, cmd)
	c.scripts = append(c.scripts, string(b))
	return []byte("ok"), c.fail
}
func (*testConn) Ping(context.Context) error { return nil }
func (*testConn) InspectGitContext(context.Context, string) (session.GitContext, error) {
	return session.GitContext{}, nil
}
func (c *testConn) Close() error { c.mu.Lock(); defer c.mu.Unlock(); c.closed++; return nil }

func TestSharedConnectionAndReconnect(t *testing.T) {
	first, second := &testConn{}, &testConn{}
	dials := 0
	p := New(Options{Hosts: func() []string { return []string{"dev"} },
		Dial: func(string) (Conn, error) {
			dials++
			if dials == 1 {
				return first, nil
			}
			return second, nil
		}})
	defer p.Close()
	for _, script := range []string{"tasks", "board"} {
		out, err := p.Run(poolTestContext(t), host.Host("dev"), script)
		require.NoError(t, err)
		require.Equal(t, "ok", string(out))
	}
	require.Equal(t, 1, dials)
	require.NoError(t, p.Reconnect("dev"))
	_, err := p.Run(poolTestContext(t), host.Host("dev"), "board")
	require.NoError(t, err)
	require.Equal(t, 2, dials)
	require.Equal(t, 1, first.closed)
}
func TestFailedDialRetriesAfterCooldown(t *testing.T) {
	now := time.Unix(10, 0)
	calls := 0
	p := New(Options{
		Hosts: func() []string { return []string{"dev"} },
		Now:   func() time.Time { return now },
		Dial: func(string) (Conn, error) {
			calls++
			if calls == 1 {
				return nil, errors.New("offline")
			}
			return &testConn{}, nil
		}})
	defer p.Close()
	_, err := p.Run(poolTestContext(t), "dev", "board")
	require.Error(t, err)
	_, err = p.Run(poolTestContext(t), "dev", "board")
	require.Error(t, err)
	require.Equal(t, 1, calls)
	now = now.Add(time.Minute)
	_, err = p.Run(poolTestContext(t), "dev", "board")
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}
func TestRemoteArgumentsAreDataAndScriptUsesStdin(t *testing.T) {
	c := &testConn{}
	p := New(Options{Hosts: func() []string { return []string{"dev"} },
		Dial: func(string) (Conn, error) { return c, nil }})
	defer p.Close()
	_, err := p.Run(poolTestContext(t), "dev", `printf '%s' "$1"`, "it's; $(false)")
	require.NoError(t, err)
	require.Equal(t, []string{`printf '%s' "$1"`}, c.scripts)
	require.Equal(t, `sh -s -- 'it'\''s; $(false)'`, c.commands[0])
}
func TestLocalArgumentsAreData(t *testing.T) {
	p := New(Options{})
	defer p.Close()
	value := "it's; $(false)\nend"
	out, err := p.Run(poolTestContext(t), host.Local, `printf '%s' "$1"`, value)
	require.NoError(t, err)
	require.Equal(t, value, string(out))
	require.False(t, strings.Contains(string(out), "unexpected"))
}

func TestTransportFailureDropsSharedConnection(t *testing.T) {
	broken, healthy := &testConn{fail: errors.New("connection lost")}, &testConn{}
	dials := 0
	p := New(Options{Hosts: func() []string { return []string{"dev"} },
		Dial: func(string) (Conn, error) {
			dials++
			if dials == 1 {
				return broken, nil
			}
			return healthy, nil
		}})
	defer p.Close()
	_, err := p.Run(poolTestContext(t), "dev", "board")
	require.ErrorContains(t, err, "connection lost")
	_, err = p.Run(poolTestContext(t), "dev", "tasks")
	require.NoError(t, err)
	require.Equal(t, 2, dials, "an in-use failure has no dial cooldown")
	require.Equal(t, 1, broken.closed)
}

func TestRetainAndCloseDisposeConnections(t *testing.T) {
	c := &testConn{}
	p := New(Options{Hosts: func() []string { return []string{"dev"} },
		Dial: func(string) (Conn, error) { return c, nil }})
	require.Equal(t, []host.Host{host.Local, "dev"}, p.Names())
	require.ErrorIs(t, p.Reconnect("unknown"), ErrUnknownHost)
	_, err := p.Connect(poolTestContext(t), "dev")
	require.NoError(t, err)
	p.Retain([]string{"dev"})
	require.Equal(t, c, p.Current("dev"))
	p.Retain(nil)
	require.Nil(t, p.Current("dev"))
	require.Equal(t, 1, c.closed)
	p.Close()
	_, err = p.Connect(poolTestContext(t), "dev")
	require.Error(t, err)
}

func TestCloseOrReconnectDiscardsInflightDial(t *testing.T) {
	for _, action := range []string{"close", "reconnect", "remove"} {
		t.Run(action, func(t *testing.T) {
			started, release := make(chan struct{}), make(chan struct{})
			c := &testConn{}
			p := New(Options{Hosts: func() []string { return []string{"dev"} },
				Dial: func(string) (Conn, error) {
					close(started)
					<-release
					return c, nil
				}})
			defer p.Close()
			ctx, cancel := context.WithTimeout(poolTestContext(t), 5*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, err := p.Connect(ctx, "dev"); done <- err }()
			select {
			case <-started:
			case <-ctx.Done():
				t.Fatal("dial did not start")
			}
			switch action {
			case "close":
				p.Close()
			case "reconnect":
				require.NoError(t, p.Reconnect("dev"))
			case "remove":
				p.Retain(nil)
			}
			close(release)
			require.ErrorIs(t, <-done, ErrConnecting)
			require.Eventually(t, func() bool {
				c.mu.Lock()
				defer c.mu.Unlock()
				return c.closed == 1
			}, time.Second, time.Millisecond)
			require.Nil(t, p.Current("dev"))
		})
	}
}

func TestRemovedHostCannotRunOrDialAgain(t *testing.T) {
	names := []string{"dev"}
	dials := 0
	c := &testConn{}
	p := New(Options{Hosts: func() []string { return names }, Dial: func(string) (Conn, error) { dials++; return c, nil }})
	defer p.Close()
	_, err := p.Run(poolTestContext(t), "dev", "board")
	require.NoError(t, err)
	names = nil
	for range 2 {
		p.Retain(nil)
		_, err = p.Run(poolTestContext(t), "dev", "board")
		require.ErrorIs(t, err, ErrUnknownHost)
		require.Nil(t, p.Current("dev"))
	}
	require.Equal(t, 1, dials)
}

func TestLocalRunnerInjectionOnlyForNoArguments(t *testing.T) {
	calls := 0
	p := New(Options{RunLocal: func(context.Context, string) ([]byte, error) { calls++; return []byte("injected"), nil }})
	defer p.Close()
	out, err := p.Run(poolTestContext(t), host.Local, "ignored")
	require.NoError(t, err)
	require.Equal(t, "injected", string(out))
	out, err = p.Run(poolTestContext(t), host.Local, `printf '%s' "$1"`, "data")
	require.NoError(t, err)
	require.Equal(t, "data", string(out))
	require.Equal(t, 1, calls)
}

func TestRemoteNoArgumentsUsesFixedCommand(t *testing.T) {
	c := &testConn{}
	p := New(Options{Hosts: func() []string { return []string{"dev"} },
		Dial: func(string) (Conn, error) { return c, nil }})
	defer p.Close()
	_, err := p.Run(poolTestContext(t), "dev", "fixed")
	require.NoError(t, err)
	require.Equal(t, []string{"sh -s"}, c.commands)
}

// A broken wait condition must fail a test rather than hang the mutation run.
func poolTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestConnectWithoutDialerReportsFailure(t *testing.T) {
	p := New(Options{Hosts: func() []string { return []string{"dev"} }})
	defer p.Close()
	_, err := p.Connect(poolTestContext(t), "dev")
	require.ErrorContains(t, err, "no SSH dialer")
}

func TestLocalNoArgumentsWithoutInjection(t *testing.T) {
	p := New(Options{})
	defer p.Close()
	out, err := p.Run(poolTestContext(t), host.Local, "printf local")
	require.NoError(t, err)
	require.Equal(t, "local", string(out))
}
