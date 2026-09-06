package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/session"
)

// Start and Shutdown are the two calls main() makes around the whole
// process's lifetime, and what they return decides whether panemux exits
// quietly or reports a failure. A clean shutdown must be distinguishable from
// a server that could not listen at all — they arrive at the same place, the
// error return of Start, and only the value tells them apart.

// serverOnAFreePort returns a Server bound to a port nothing else is using,
// along with that port.
//
// The port is found by binding one, reading it back and releasing it, which
// leaves a window in which something else could take it. That window is why
// the callers below retry rather than assume: this repository has already had
// one flaky test from treating a released ephemeral port as reserved.
func serverOnAFreePort(t *testing.T) (*Server, int) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port //nolint:errcheck // a TCP listener always has a *TCPAddr
	require.NoError(t, ln.Close())

	cfg := testConfig()
	cfg.Server.Port = port
	return New(cfg, session.NewManager(), nil, nil, nil, emptyFS), port
}

// startListening starts srv and waits until it answers, retrying with a fresh
// port if the one it was given was taken in the meantime. It returns the
// running server, its port, and the channel Start's return value arrives on.
func startListening(t *testing.T) (*Server, int, <-chan error) {
	t.Helper()
	for attempt := range 5 {
		srv, port := serverOnAFreePort(t)
		errCh := make(chan error, 1)
		go func() { errCh <- srv.Start() }()

		if waitForListener(port) {
			return srv, port, errCh
		}
		// Start already returned, so the bind failed; take its error and try
		// another port rather than leaving a goroutine behind.
		select {
		case err := <-errCh:
			t.Logf("attempt %d: server did not come up: %v", attempt, err)
		case <-time.After(time.Second):
			t.Fatal("server neither came up nor reported why")
		}
	}
	t.Fatal("could not get a free port in five attempts")
	return nil, 0, nil
}

func waitForListener(port int) bool {
	for range 100 {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", port), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// A shutdown is not a failure. Start returns http.ErrServerClosed unwrapped
// so main() can tell "the operator stopped us" from "we could not listen",
// which is the difference between exiting 0 and exiting with a message.
func TestStartReturnsErrServerClosedAfterAShutdown(t *testing.T) {
	srv, _, errCh := startListening(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "a shutdown with no live requests must report nothing")

	select {
	case err := <-errCh:
		assert.ErrorIs(t, err, http.ErrServerClosed)
		assert.Equal(t, http.ErrServerClosed, err, //nolint:errorlint // the identity is the point
			"it must be the sentinel itself, not a wrap: main() compares it directly")
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}

// The other arm: a port already in use is a real failure and has to say so,
// with the wrap that names which step failed. Binding the port ourselves and
// keeping it is deterministic — unlike the free-port dance above, nothing can
// take it away.
func TestStartWrapsAFailureToListen(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close() //nolint:errcheck

	cfg := testConfig()
	cfg.Server.Port = ln.Addr().(*net.TCPAddr).Port //nolint:errcheck // a TCP listener always has a *TCPAddr
	srv := New(cfg, session.NewManager(), nil, nil, nil, emptyFS)

	startErr := srv.Start()

	require.Error(t, startErr)
	assert.NotErrorIs(t, startErr, http.ErrServerClosed,
		"a bind failure must not be mistaken for a clean shutdown")
	assert.Contains(t, startErr.Error(), "starting HTTP server",
		"the operator needs to know which step failed, not just the syscall")
	assert.Contains(t, startErr.Error(), "address already in use")
}

// Shutdown reports its own failure rather than swallowing it: a shutdown that
// gave up on live connections has left the process in a different state than
// one that drained cleanly, and main() logs the difference.
//
// The fixture is a connection that has been accepted but has sent no request,
// so net/http counts it as active and Shutdown cannot finish. With the
// context already canceled, Shutdown returns immediately with its error
// rather than waiting on a deadline.
func TestShutdownWrapsAFailureToDrain(t *testing.T) {
	srv, port, errCh := startListening(t)

	held, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	require.NoError(t, err)
	defer held.Close() //nolint:errcheck

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	shutdownErr := srv.Shutdown(ctx)

	require.Error(t, shutdownErr)
	assert.Contains(t, shutdownErr.Error(), "shutting down HTTP server")
	assert.ErrorIs(t, shutdownErr, context.Canceled,
		"the reason the drain was abandoned has to survive the wrap")

	select {
	case err := <-errCh:
		assert.True(t, errors.Is(err, http.ErrServerClosed),
			"the listener is closed either way, so Start still ends as a shutdown")
	case <-time.After(5 * time.Second):
		t.Fatal("Start did not return after Shutdown")
	}
}
