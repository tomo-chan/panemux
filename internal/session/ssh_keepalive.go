package session

import (
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
// NewTmuxSSH: on transport death, close the primary client (and jump client,
// if any) so the session's existing monitorSSHSession/classifySSHWaitError
// machinery observes the resulting error and transitions state normally.
func sshKeepaliveOnDeadFn(client, jumpClient *ssh.Client) func() {
	return func() {
		client.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
	}
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
