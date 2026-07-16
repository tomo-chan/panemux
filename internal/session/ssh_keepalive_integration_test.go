package session

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// startTestSSHServer starts a minimal in-process SSH server on loopback that
// accepts any password, completes the handshake, and satisfies the pty-req
// and shell requests NewSSH needs to establish a session. globalReqHandler
// receives every global request (e.g. the keepalive probe's
// "keepalive@panemux") so a test can control exactly which ones get a
// reply, simulating a transport that stops responding without a clean
// TCP close. Returns the server's host, port, and host public key.
func startTestSSHServer(
	t *testing.T,
	globalReqHandler func(reqs <-chan *gossh.Request),
) (host string, port int, hostKey gossh.PublicKey) {
	t.Helper()

	hostKeyPath := filepath.Join(t.TempDir(), "host_key")
	generateTestKeyFile(t, hostKeyPath)
	keyData, err := os.ReadFile(hostKeyPath)
	require.NoError(t, err)
	signer, err := gossh.ParsePrivateKey(keyData)
	require.NoError(t, err)

	config := &gossh.ServerConfig{
		PasswordCallback: func(conn gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			return nil, nil // accept any credentials; this is a local test fixture, not a real auth boundary
		},
	}
	config.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go serveTestSSHConn(conn, config, globalReqHandler)
		}
	}()

	hostStr, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	portNum, err := strconv.Atoi(portStr)
	require.NoError(t, err)

	return hostStr, portNum, signer.PublicKey()
}

func serveTestSSHConn(conn net.Conn, config *gossh.ServerConfig, globalReqHandler func(reqs <-chan *gossh.Request)) {
	sshConn, chans, reqs, err := gossh.NewServerConn(conn, config)
	if err != nil {
		conn.Close()
		return
	}
	defer sshConn.Close()

	go globalReqHandler(reqs)

	for newChannel := range chans {
		ch, requests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go serveTestSSHChannel(ch, requests)
	}
}

// serveTestSSHChannel answers just enough channel requests (pty-req, shell)
// for NewSSH's setup sequence (RequestPty + Shell) to succeed, and rejects
// anything else it doesn't recognize.
func serveTestSSHChannel(ch gossh.Channel, requests <-chan *gossh.Request) {
	defer ch.Close()
	for req := range requests {
		switch req.Type {
		case "pty-req", "shell":
			if req.WantReply {
				req.Reply(true, nil) //nolint:errcheck // best-effort test fixture reply
			}
		default:
			if req.WantReply {
				req.Reply(false, nil) //nolint:errcheck // best-effort test fixture reply
			}
		}
	}
}

// writeTestKnownHosts writes a known_hosts file with a single entry matching
// host:port -> hostKey, in the format golang.org/x/crypto/ssh/knownhosts
// expects, and returns its path.
func writeTestKnownHosts(t *testing.T, host string, port int, hostKey gossh.PublicKey) string {
	t.Helper()
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	line := knownhosts.Line([]string{knownhosts.Normalize(addr)}, hostKey)
	path := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(path, []byte(line+"\n"), 0600))
	return path
}

// TestSSHSession_KeepaliveDetectsDeadTransport is the end-to-end regression
// test for the reported pane-hang bug: golang.org/x/crypto/ssh has no
// ServerAliveInterval equivalent, so before the keepalive probe was added, a
// transport that silently stopped responding (no clean TCP close, no error)
// left the session in StateConnected forever. This test simulates exactly
// that — the server accepts the first keepalive probe, then goes silent —
// and verifies the session detects it and transitions to StateDisconnected
// within the shrunk keepalive window, instead of hanging indefinitely.
func TestSSHSession_KeepaliveDetectsDeadTransport(t *testing.T) {
	prevInterval := sshKeepaliveInterval
	prevProbeTimeout := sshKeepaliveProbeTimeout
	prevMaxFailures := sshKeepaliveMaxFailures
	sshKeepaliveInterval = 20 * time.Millisecond
	sshKeepaliveProbeTimeout = 50 * time.Millisecond
	sshKeepaliveMaxFailures = 2
	t.Cleanup(func() {
		sshKeepaliveInterval = prevInterval
		sshKeepaliveProbeTimeout = prevProbeTimeout
		sshKeepaliveMaxFailures = prevMaxFailures
	})

	var probeCount int32
	host, port, hostKey := startTestSSHServer(t, func(reqs <-chan *gossh.Request) {
		for req := range reqs {
			n := atomic.AddInt32(&probeCount, 1)
			if n == 1 && req.WantReply {
				// First probe succeeds — proves the happy path still works
				// before the transport goes silent.
				req.Reply(false, nil) //nolint:errcheck // best-effort test fixture reply
				continue
			}
			// Every subsequent probe is left unanswered: the transport is
			// still technically open (no TCP close), but nothing ever
			// replies, exactly like a network black hole.
		}
	})

	knownHostsPath := writeTestKnownHosts(t, host, port, hostKey)
	cfg := SSHConfig{
		Host:           host,
		Port:           port,
		User:           "test",
		Password:       "test",
		KnownHostsFile: knownHostsPath,
	}

	sess, err := NewSSH("keepalive-test", "keepalive test", cfg)
	require.NoError(t, err)
	defer sess.Close()

	require.Equal(t, StateConnected, sess.State())

	assert.Eventually(t, func() bool {
		return sess.State() == StateDisconnected
	}, 5*time.Second, 10*time.Millisecond, "keepalive should detect the dead transport and disconnect")

	assert.GreaterOrEqual(t, int(atomic.LoadInt32(&probeCount)), 2, "expected a success then a failing probe")
}

// TestSSHSession_KeepaliveDoesNotDisconnectHealthyTransport is the
// counterpart to the detection test above: a server that keeps answering
// every keepalive probe must never be marked disconnected by the keepalive
// loop on its own.
func TestSSHSession_KeepaliveDoesNotDisconnectHealthyTransport(t *testing.T) {
	prevInterval := sshKeepaliveInterval
	prevProbeTimeout := sshKeepaliveProbeTimeout
	sshKeepaliveInterval = 20 * time.Millisecond
	sshKeepaliveProbeTimeout = 50 * time.Millisecond
	t.Cleanup(func() {
		sshKeepaliveInterval = prevInterval
		sshKeepaliveProbeTimeout = prevProbeTimeout
	})

	host, port, hostKey := startTestSSHServer(t, func(reqs <-chan *gossh.Request) {
		for req := range reqs {
			if req.WantReply {
				req.Reply(false, nil) //nolint:errcheck // best-effort test fixture reply
			}
		}
	})

	knownHostsPath := writeTestKnownHosts(t, host, port, hostKey)
	cfg := SSHConfig{
		Host:           host,
		Port:           port,
		User:           "test",
		Password:       "test",
		KnownHostsFile: knownHostsPath,
	}

	sess, err := NewSSH("keepalive-healthy-test", "keepalive healthy test", cfg)
	require.NoError(t, err)
	defer sess.Close()

	time.Sleep(15 * sshKeepaliveInterval)
	assert.Equal(t, StateConnected, sess.State(), "a transport that keeps answering keepalive probes must stay connected")
}

// TestSSHSession_CloseDuringInFlightKeepaliveProbe_StaysExited is the
// regression test for the reviewer-flagged race in sshKeepaliveOnDeadFn:
// Close() sets StateExited and then closes the transport; if a keepalive
// probe was already blocked on SendRequest at that moment,
// close(keepaliveStop) doesn't reach the loop until its next iteration, but
// Close()'s own client.Close() call unblocks the in-flight SendRequest with
// an error first. With sshKeepaliveMaxFailures set to 1, that single
// failure is enough to fire onDead strictly after Close() already set
// StateExited. Without a guard in markDisconnected mirroring the one
// monitorSSHSession's callback already has, this would overwrite an
// intentional clean close with StateDisconnected.
func TestSSHSession_CloseDuringInFlightKeepaliveProbe_StaysExited(t *testing.T) {
	prevInterval := sshKeepaliveInterval
	prevProbeTimeout := sshKeepaliveProbeTimeout
	prevMaxFailures := sshKeepaliveMaxFailures
	sshKeepaliveInterval = 10 * time.Millisecond
	// Long enough that the test can reliably call Close() while this
	// probe is still blocked waiting for a reply that never comes.
	sshKeepaliveProbeTimeout = 300 * time.Millisecond
	sshKeepaliveMaxFailures = 1
	t.Cleanup(func() {
		sshKeepaliveInterval = prevInterval
		sshKeepaliveProbeTimeout = prevProbeTimeout
		sshKeepaliveMaxFailures = prevMaxFailures
	})

	probeStarted := make(chan struct{}, 1)
	host, port, hostKey := startTestSSHServer(t, func(reqs <-chan *gossh.Request) {
		for range reqs {
			select {
			case probeStarted <- struct{}{}:
			default:
			}
			// Never reply: this probe stays in flight until client.Close()
			// (called by SSHSession.Close(), below) unblocks it with an error.
		}
	})

	knownHostsPath := writeTestKnownHosts(t, host, port, hostKey)
	cfg := SSHConfig{
		Host:           host,
		Port:           port,
		User:           "test",
		Password:       "test",
		KnownHostsFile: knownHostsPath,
	}

	sess, err := NewSSH("close-race-test", "close race test", cfg)
	require.NoError(t, err)

	select {
	case <-probeStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("expected a keepalive probe to start")
	}

	// Close() sets StateExited synchronously before it closes the
	// transport, which is what will unblock the in-flight probe above.
	require.NoError(t, sess.Close())
	assert.Equal(t, StateExited, sess.State())

	// Give the keepalive loop time to actually observe the now-failing
	// probe and call onDead, proving the guard — not just timing luck —
	// is what keeps the state at StateExited.
	time.Sleep(5 * sshKeepaliveProbeTimeout)
	assert.Equal(
		t, StateExited, sess.State(),
		"an in-flight keepalive probe's failure must not overwrite an explicit Close()",
	)
}
