package session

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	gossh "golang.org/x/crypto/ssh"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// hangingConn stands in for a transport whose peer has stopped answering: the
// handshake's first read never returns.
//
// deadlineErr is what SetDeadline reports, which is what distinguishes the
// three real transports from one another — and is exactly what must NOT be
// what bounds the handshake:
//
//   - a direct TCP conn honors deadlines,
//   - a ProxyJump hop is an SSH channel, whose SetDeadline returns
//     "ssh: deadline not supported",
//   - proxyCommandConn's SetDeadline is a no-op returning nil (see ssh.go).
type hangingConn struct {
	net.Conn
	release     chan struct{}
	closed      chan struct{}
	deadlineErr error
	closeBlocks bool
}

func newHangingConn(deadlineErr error) *hangingConn {
	return &hangingConn{
		deadlineErr: deadlineErr,
		release:     make(chan struct{}),
		closed:      make(chan struct{}),
	}
}

func (c *hangingConn) Read(_ []byte) (int, error) {
	<-c.release
	return 0, errors.New("closed")
}

func (c *hangingConn) Write(p []byte) (int, error) { return len(p), nil }

func (c *hangingConn) Close() error {
	if c.closeBlocks {
		// A transport whose own Close is unbounded: proxyCommandConn.Close
		// kills the subprocess and then waits on it.
		<-c.release
	}
	select {
	case <-c.closed:
	default:
		close(c.closed)
		close(c.release)
	}
	return nil
}

func (c *hangingConn) LocalAddr() net.Addr                { return proxyAddr("127.0.0.1:0") }
func (c *hangingConn) RemoteAddr() net.Addr               { return proxyAddr("remote:22") }
func (c *hangingConn) SetDeadline(_ time.Time) error      { return c.deadlineErr }
func (c *hangingConn) SetReadDeadline(_ time.Time) error  { return c.deadlineErr }
func (c *hangingConn) SetWriteDeadline(_ time.Time) error { return c.deadlineErr }

// handshakeFn is newClientConnFn's signature, named so the stubs below fit on
// one line.
type handshakeFn func(
	net.Conn, string, *gossh.ClientConfig,
) (gossh.Conn, <-chan gossh.NewChannel, <-chan *gossh.Request, error)

// readUntilClosed is the handshake of a peer that never answers: it blocks on
// the transport exactly as a real key exchange's first read would.
func readUntilClosed(c net.Conn, _ string, _ *gossh.ClientConfig) (
	gossh.Conn, <-chan gossh.NewChannel, <-chan *gossh.Request, error,
) {
	buf := make([]byte, 1)
	_, err := c.Read(buf)
	return nil, nil, nil, err
}

func stubHandshake(t *testing.T, fn handshakeFn) {
	t.Helper()
	orig := newClientConnFn
	newClientConnFn = fn
	t.Cleanup(func() { newClientConnFn = orig })
}

// TestHandshakeWithTimeout_HangingHandshakeIsBoundedOnEveryTransport is the
// regression test for issue #147: ssh.NewClientConn reads no timeout of its own
// and sets no deadline, so before this every transport could block a pane's
// reconnect forever after the transport dial itself had succeeded.
func TestHandshakeWithTimeout_HangingHandshakeIsBoundedOnEveryTransport(t *testing.T) {
	for _, tc := range []struct {
		deadlineErr error
		name        string
	}{
		{name: "direct tcp: deadlines work", deadlineErr: nil},
		{name: "proxy jump: ssh channel refuses deadlines", deadlineErr: errors.New("ssh: deadline not supported")},
		{name: "proxy command: deadlines are a silent no-op", deadlineErr: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conn := newHangingConn(tc.deadlineErr)
			stubHandshake(t, readUntilClosed)

			start := time.Now()
			_, _, _, err := handshakeWithTimeout(
				conn, "remote:22", &gossh.ClientConfig{}, 20*time.Millisecond,
			)

			require.Error(t, err)
			assert.Contains(t, err.Error(), "timed out")
			assert.Less(
				t, time.Since(start), 2*time.Second,
				"the handshake must be bounded by the timeout, not by the transport",
			)

			select {
			case <-conn.closed:
			case <-time.After(time.Second):
				t.Fatal("the transport was not closed, so the blocked handshake goroutine is never released")
			}
		})
	}
}

// TestHandshakeWithTimeout_ReturnsEvenWhenClosingTheTransportBlocks is why the
// bound cannot be "close the conn, then wait for the handshake to notice":
// proxyCommandConn.Close waits on the subprocess it just killed, and nothing
// bounds that either.
func TestHandshakeWithTimeout_ReturnsEvenWhenClosingTheTransportBlocks(t *testing.T) {
	conn := newHangingConn(nil)
	conn.closeBlocks = true
	stubHandshake(t, readUntilClosed)

	done := make(chan error, 1)
	go func() {
		_, _, _, err := handshakeWithTimeout(
			conn, "remote:22", &gossh.ClientConfig{}, 20*time.Millisecond,
		)
		done <- err
	}()

	select {
	case err := <-done:
		require.Error(t, err)
		assert.Contains(t, err.Error(), "timed out")
	case <-time.After(2 * time.Second):
		t.Fatal("handshakeWithTimeout waited on a transport Close that never returns")
	}
}

func TestHandshakeWithTimeout_SuccessPassesTheResultThroughAndKeepsTheTransport(t *testing.T) {
	conn := newHangingConn(nil)
	chans := make(chan gossh.NewChannel)
	reqs := make(chan *gossh.Request)
	stubHandshake(t, func(
		net.Conn, string, *gossh.ClientConfig,
	) (gossh.Conn, <-chan gossh.NewChannel, <-chan *gossh.Request, error) {
		return nil, chans, reqs, nil
	})

	_, gotChans, gotReqs, err := handshakeWithTimeout(conn, "remote:22", &gossh.ClientConfig{}, time.Minute)

	require.NoError(t, err)
	assert.Equal(t, (<-chan gossh.NewChannel)(chans), gotChans)
	assert.Equal(t, (<-chan *gossh.Request)(reqs), gotReqs)
	select {
	case <-conn.closed:
		t.Fatal("a successful handshake must not close the transport it is about to use")
	default:
	}
}

func TestHandshakeWithTimeout_FailedHandshakeClosesTheTransport(t *testing.T) {
	conn := newHangingConn(nil)
	stubHandshake(t, func(
		net.Conn, string, *gossh.ClientConfig,
	) (gossh.Conn, <-chan gossh.NewChannel, <-chan *gossh.Request, error) {
		return nil, nil, nil, errors.New("unable to authenticate")
	})

	_, _, _, err := handshakeWithTimeout(conn, "remote:22", &gossh.ClientConfig{}, time.Minute)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unable to authenticate")
	assert.NotContains(t, err.Error(), "timed out")
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("a failed handshake must not leak the transport")
	}
}

// TestDialSSHClientUntil_HangingHandshakeIsBounded drives the whole dial path,
// since the timeout being enforced somewhere is worth nothing unless the real
// caller passes one.
func TestDialSSHClientUntil_HangingHandshakeIsBounded(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(knownHosts, []byte{}, 0600))

	conn := newHangingConn(nil)
	origDial := dialTransportFn
	dialTransportFn = func(SSHConfig, string, int, time.Time) (net.Conn, *gossh.Client, error) {
		return conn, nil, nil
	}
	t.Cleanup(func() { dialTransportFn = origDial })

	stubHandshake(t, readUntilClosed)

	orig := sshHandshakeTimeout
	sshHandshakeTimeout = 20 * time.Millisecond
	t.Cleanup(func() { sshHandshakeTimeout = orig })

	cfg := SSHConfig{Host: "example.test", User: "demo", Password: "secret", KnownHostsFile: knownHosts}

	start := time.Now()
	_, _, err := dialSSHClientUntil(cfg, nowFn().Add(dialRetryBudget))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssh handshake")
	assert.Contains(t, err.Error(), "timed out")
	assert.Less(t, time.Since(start), 2*time.Second)
}

// TestDialSSHClientUntil_HandshakeSharesTheDialBudget is the second half of
// issue #147's bound, found in review of the first: a flat handshake timeout
// on top of a spent dial budget is not the ceiling dialRetryBudget documents.
//
// dialTransportWithRetry already clamps each attempt to what is left of the
// deadline precisely so the budget holds, and dialThroughJump hands the same
// deadline to every hop — so an unshared handshake window is added once per
// hop, and a wedged jump chain waits several times the documented ceiling.
func TestDialSSHClientUntil_HandshakeSharesTheDialBudget(t *testing.T) {
	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(knownHosts, []byte{}, 0600))

	conn := newHangingConn(nil)
	origDial := dialTransportFn
	dialTransportFn = func(SSHConfig, string, int, time.Time) (net.Conn, *gossh.Client, error) {
		return conn, nil, nil
	}
	t.Cleanup(func() { dialTransportFn = origDial })
	stubHandshake(t, readUntilClosed)

	// Three seconds rather than the real 30: long enough that an unclamped
	// handshake is unmistakable against the 20ms below, short enough that a
	// mutant removing the clamp fails this in seconds instead of timing the
	// mutation run out.
	orig := sshHandshakeTimeout
	sshHandshakeTimeout = 3 * time.Second
	t.Cleanup(func() { sshHandshakeTimeout = orig })

	cfg := SSHConfig{Host: "example.test", User: "demo", Password: "secret", KnownHostsFile: knownHosts}

	// A transport dial that consumed all but 20ms of the budget leaves the
	// handshake 20ms, not a fresh window.
	start := time.Now()
	_, _, err := dialSSHClientUntil(cfg, nowFn().Add(20*time.Millisecond))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "ssh handshake")
	assert.Less(t, time.Since(start), time.Second,
		"the handshake must be bounded by what is left of the dial budget, not by its own window")
}

// TestHandshakeWithTimeout_NoBudgetLeft_FailsWithoutStartingTheHandshake
// covers the arm the clamp above creates. A non-positive timeout reaches here
// when the dial consumed the whole budget between its own last check and this
// call; the dial layer refuses first in the ordinary case
// (TestDialTransport_ExhaustedBudget_NeverDials), so this is the narrow race,
// not the common path.
func TestHandshakeWithTimeout_NoBudgetLeft_FailsWithoutStartingTheHandshake(t *testing.T) {
	conn := newHangingConn(nil)
	handshakes := 0
	stubHandshake(t, func(
		c net.Conn, addr string, cfg *gossh.ClientConfig,
	) (gossh.Conn, <-chan gossh.NewChannel, <-chan *gossh.Request, error) {
		handshakes++
		return readUntilClosed(c, addr, cfg)
	})

	_, _, _, err := handshakeWithTimeout(conn, "remote:22", &gossh.ClientConfig{}, 0)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "budget")
	assert.NotContains(t, err.Error(), "timed out",
		"a handshake that never started did not time out")
	assert.Zero(t, handshakes, "with no budget left there is nothing to wait for")
	select {
	case <-conn.closed:
	case <-time.After(time.Second):
		t.Fatal("the transport must not be leaked when the budget is already spent")
	}
}
