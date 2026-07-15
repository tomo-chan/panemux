package session

import (
	"fmt"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshRunTimeout bounds how long a single SSH exec-channel command (tmux
// display-message, git inspection, ps, cat, etc.) may block before the
// underlying *ssh.Session is force-closed to unblock the caller. Without
// this, ssh.Session.Output has no way to time out on its own: a wedged
// remote command (e.g. a stuck remote tmux server) leaves the exec channel
// open forever. Package-level so tests can shrink it.
var sshRunTimeout = 5 * time.Second

// sshInteractiveTimeout bounds user-triggered SSH commands (directory
// browsing, remote shell detection) that are not part of the background
// polling path and can tolerate a slightly longer bound than sshRunTimeout.
var sshInteractiveTimeout = 10 * time.Second

// runOutputWithTimeout runs outputFn(cmd) and returns its result, or a
// timeout error if it does not return within timeout. On timeout it calls
// closeFn to force the underlying session closed, which unblocks the
// still-running outputFn call (whose result is then discarded on the
// abandoned goroutine). outputFn/closeFn are accepted as plain funcs (rather
// than requiring a *ssh.Session directly) so the timeout race logic is
// testable without a real SSH connection.
func runOutputWithTimeout(
	outputFn func(cmd string) ([]byte, error),
	closeFn func() error,
	cmd string,
	timeout time.Duration,
) ([]byte, error) {
	type result struct {
		err error
		out []byte
	}
	done := make(chan result, 1)
	go func() {
		out, err := outputFn(cmd)
		done <- result{err: err, out: out}
	}()
	select {
	case r := <-done:
		return r.out, r.err
	case <-time.After(timeout):
		closeFn() //nolint:errcheck // best-effort unblock of the abandoned goroutine above
		return nil, fmt.Errorf("ssh command timed out after %s", timeout)
	}
}

// timeoutSessionRunner adapts *ssh.Session to sshSessionRunner, applying
// sshRunTimeout to every Output call, for call sites that go through the
// sshSessionRunner abstraction rather than calling sess.Output directly.
type timeoutSessionRunner struct {
	sess *ssh.Session
}

func (t *timeoutSessionRunner) Output(cmd string) ([]byte, error) {
	return runOutputWithTimeout(t.sess.Output, t.sess.Close, cmd, sshRunTimeout)
}

func (t *timeoutSessionRunner) Close() error {
	return t.sess.Close()
}
