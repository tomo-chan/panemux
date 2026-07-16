package session

import (
	"io"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// sshKeepaliveInterval, sshKeepaliveProbeTimeout, and sshKeepaliveMaxFailures
// are package-level so tests can shrink them. golang.org/x/crypto/ssh has no
// equivalent to OpenSSH's ServerAliveInterval/ServerAliveCountMax: a
// half-dead TCP connection (packets black-holed in one or both directions,
// no RST/FIN) otherwise leaves Read/Write/NewSession/Session.Wait blocked
// forever with no signal from the standard library. This probe loop detects
// that and forces the transport closed so the rest of the session lifecycle
// (monitorSSHSession/classifySSHWaitError) can react normally.
var (
	sshKeepaliveInterval     = 15 * time.Second
	sshKeepaliveProbeTimeout = 10 * time.Second
	sshKeepaliveMaxFailures  = 2
)

// sshKeepaliveSender is the subset of *ssh.Client used by the keepalive
// probe, extracted so tests can inject a fake instead of standing up a real
// SSH server for every failure-path test.
type sshKeepaliveSender interface {
	SendRequest(name string, wantReply bool, payload []byte) (bool, []byte, error)
}

// sshKeepaliveOnDeadFn builds the onDead callback shared by NewSSH and
// NewTmuxSSH. It marks the session disconnected *before* closing the
// client, then closes the primary client (and jump client, if any).
//
// Ordering matters here: forcing the transport closed makes the still-
// pending monitorSSHSession goroutine's sess.Wait() return
// *ssh.ExitMissingError — the same error golang.org/x/crypto/ssh returns
// when a remote process's channel closes without ever sending a clean SSH
// exit-status message, which is indistinguishable from "the transport was
// severed out from under an in-flight session." classifySSHWaitError
// therefore classifies it as StateExited, not StateDisconnected, which
// would misreport a keepalive-detected network failure as a normal session
// exit and could suppress the frontend's reconnect behavior. Setting
// StateDisconnected first, combined with the guard in the markExited
// closure passed to monitorSSHSession, ensures that later, less specific
// classification never overwrites this one.
//
// markDisconnected itself must also refuse to overwrite an explicit
// StateExited — see wireSSHSessionLifecycle's symmetric guard for why:
// Close() can already have committed to StateExited before a probe that was
// already in flight (blocked on SendRequest) unblocks via Close()'s own
// client.Close() call and reports a failure, racing onDead in after the
// fact.
func sshKeepaliveOnDeadFn(markDisconnected func(), client, jumpClient *ssh.Client) func() {
	return func() {
		markDisconnected()
		client.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
	}
}

// wireSSHSessionLifecycle wires monitorSSHSession and startSSHKeepalive
// together for a *ssh.Session-backed session (SSHSession, TmuxSSHSession).
// Both directions of state transition are mutually guarded so neither can
// downgrade the other's more specific classification:
//
//   - monitorSSHSession's callback won't downgrade a keepalive-detected
//     StateDisconnected back to StateExited when the session's own Wait()
//     later returns (see sshKeepaliveOnDeadFn's doc comment).
//   - the keepalive onDead callback won't overwrite an explicit
//     Close()-driven StateExited with StateDisconnected. Close() sets
//     StateExited and then closes the transport; if a keepalive probe was
//     already blocked on SendRequest at that moment, close(keepaliveStop)
//     doesn't reach it until the next loop iteration, but Close()'s own
//     client.Close() call unblocks the in-flight SendRequest with an error
//     first — which can push the loop's failure count over
//     sshKeepaliveMaxFailures and fire onDead strictly after Close() already
//     set StateExited. Without this guard, an intentional clean close could
//     be reported as an unexpected disconnect.
//
// mu/state are the caller's own mutex-protected state field.
func wireSSHSessionLifecycle(
	sess *ssh.Session, pw *io.PipeWriter,
	client, jumpClient *ssh.Client,
	keepaliveStop chan struct{},
	mu *sync.RWMutex, state *State,
) {
	monitorSSHSession(sess, pw, func(newState State) {
		mu.Lock()
		defer mu.Unlock()
		if *state == StateDisconnected {
			return
		}
		*state = newState
	})

	startSSHKeepalive(client, keepaliveStop, sshKeepaliveOnDeadFn(func() {
		mu.Lock()
		defer mu.Unlock()
		if *state == StateExited {
			return
		}
		*state = StateDisconnected
	}, client, jumpClient))
}

// startSSHKeepalive runs a background probe loop and calls onDead once
// sshKeepaliveMaxFailures consecutive probes fail or time out. It exits
// after calling onDead, or immediately once stop is closed — whichever
// happens first.
func startSSHKeepalive(client sshKeepaliveSender, stop <-chan struct{}, onDead func()) {
	go sshKeepaliveLoop(client, stop, onDead, sshKeepaliveInterval, sshKeepaliveProbeTimeout, sshKeepaliveMaxFailures)
}

// sshKeepaliveLoop is startSSHKeepalive's body, with the timing/threshold
// values passed explicitly rather than read from the package-level vars.
// Extracted so tests can run the loop synchronously (or in a goroutine they
// can wait on) with test-local values, instead of racing a background
// goroutine against a t.Cleanup that mutates the shared package vars.
func sshKeepaliveLoop(
	client sshKeepaliveSender,
	stop <-chan struct{},
	onDead func(),
	interval, probeTimeout time.Duration,
	maxFailures int,
) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	failures := 0
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			if sshKeepaliveProbe(client, probeTimeout) {
				failures = 0
				continue
			}
			failures++
			if failures >= maxFailures {
				onDead()
				return
			}
		}
	}
}

// sshKeepaliveProbe sends an unrecognized global request and treats any
// reply (including the SSH_MSG_REQUEST_FAILURE a compliant server sends for
// an unknown request name) as proof the transport is alive — the same trick
// OpenSSH's own ServerAliveInterval uses. Returns false if SendRequest
// returns an error or does not return within timeout.
func sshKeepaliveProbe(client sshKeepaliveSender, timeout time.Duration) bool {
	done := make(chan bool, 1)
	go func() {
		_, _, err := client.SendRequest("keepalive@panemux", true, nil)
		done <- err == nil
	}()
	select {
	case ok := <-done:
		return ok
	case <-time.After(timeout):
		return false // the abandoned probe goroutine above unblocks once the
		// caller's onDead() closes the underlying client; harmless until then.
	}
}
