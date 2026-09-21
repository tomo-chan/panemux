package session

import (
	"context"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// directTCPIPPayload is the channel-open payload an SSH client sends for a
// client-side ("-L" style) forward.
//
//nolint:govet // fieldalignment: the field order is the SSH wire format for direct-tcpip
type directTCPIPPayload struct {
	DestAddr string
	DestPort uint32
	SrcAddr  string
	SrcPort  uint32
}

// forwardRecorder captures the destinations a test SSH server was asked to
// connect to on the client's behalf.
type forwardRecorder struct {
	addrs []string
	ports []uint32
	mu    sync.Mutex
}

func (r *forwardRecorder) record(addr string, port uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.addrs = append(r.addrs, addr)
	r.ports = append(r.ports, port)
}

func (r *forwardRecorder) snapshot() ([]string, []uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.addrs...), append([]uint32(nil), r.ports...)
}

// startEchoServer runs a TCP echo server, standing in for the OAuth callback
// listener a CLI starts on the pane host's loopback interface.
func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// startTestSSHServer runs an in-process SSH server that services direct-tcpip
// channels by connecting to echoAddr, and returns a connected client.
func startTestSSHServer(t *testing.T, echoAddr string, rec *forwardRecorder) *ssh.Client {
	t.Helper()
	client, _ := startTestSSHTransport(t, nil, echoAddr, rec)
	return client
}

func pipeTestChannel(ch ssh.Channel, echoAddr string) {
	defer ch.Close()
	upstream, err := net.Dial("tcp", echoAddr)
	if err != nil {
		return
	}
	defer upstream.Close()
	done := make(chan struct{}, 2)
	go func() { io.Copy(upstream, ch); done <- struct{}{} }()
	go func() { io.Copy(ch, upstream); done <- struct{}{} }()
	<-done
}

func assertLoopbackRoundTrip(t *testing.T, conn net.Conn, payload string) {
	t.Helper()
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.WriteString(conn, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(buf) != payload {
		t.Fatalf("round trip = %q, want %q", buf, payload)
	}
}

func TestSSHSessionDialLoopbackReachesTheRemoteLoopbackPort(t *testing.T) {
	rec := &forwardRecorder{}
	client := startTestSSHServer(t, startEchoServer(t), rec)
	sess := &SSHSession{id: "pane-1", client: client, state: StateConnected}

	conn, err := sess.DialLoopback(context.Background(), 51234)
	if err != nil {
		t.Fatalf("DialLoopback: %v", err)
	}
	assertLoopbackRoundTrip(t, conn, "callback")

	addrs, ports := rec.snapshot()
	if len(addrs) != 1 || addrs[0] != "127.0.0.1" {
		t.Fatalf("dest addrs = %v, want [127.0.0.1]", addrs)
	}
	if len(ports) != 1 || ports[0] != 51234 {
		t.Fatalf("dest ports = %v, want [51234]", ports)
	}
}

func TestTmuxSSHSessionDialLoopbackReachesTheRemoteLoopbackPort(t *testing.T) {
	rec := &forwardRecorder{}
	client := startTestSSHServer(t, startEchoServer(t), rec)
	sess := &TmuxSSHSession{id: "pane-2", client: client, state: StateConnected}

	conn, err := sess.DialLoopback(context.Background(), 45678)
	if err != nil {
		t.Fatalf("DialLoopback: %v", err)
	}
	assertLoopbackRoundTrip(t, conn, "tmux-callback")

	_, ports := rec.snapshot()
	if len(ports) != 1 || ports[0] != 45678 {
		t.Fatalf("dest ports = %v, want [45678]", ports)
	}
}

func TestDialLoopbackWithoutAnEstablishedConnection(t *testing.T) {
	sess := &SSHSession{id: "pane-3"}
	if _, err := sess.DialLoopback(context.Background(), 51234); err == nil {
		t.Fatal("DialLoopback with no client = nil, want error")
	}
	tmuxSess := &TmuxSSHSession{id: "pane-4"}
	if _, err := tmuxSess.DialLoopback(context.Background(), 51234); err == nil {
		t.Fatal("DialLoopback with no client = nil, want error")
	}
}

func TestDialLoopbackRejectsPortsOutsideTheValidRange(t *testing.T) {
	rec := &forwardRecorder{}
	client := startTestSSHServer(t, startEchoServer(t), rec)
	sess := &SSHSession{id: "pane-5", client: client, state: StateConnected}

	for _, port := range []int{0, -1, 65536} {
		if _, err := sess.DialLoopback(context.Background(), port); err == nil {
			t.Fatalf("DialLoopback(%d) = nil, want error", port)
		}
	}
	if addrs, _ := rec.snapshot(); len(addrs) != 0 {
		t.Fatalf("invalid ports still reached the remote: %v", addrs)
	}
}

// TestDialLoopbackAcceptsThePortRangeBoundaries pins the two ports the range
// check must let through. The rejection test above uses 0, -1 and 65536 —
// every one of them outside the range — so `port <= 1 || port >= 65535` would
// have refused to forward both ends of the legal range with the suite still
// green. Issue #190.
// Nothing under this test changed on this branch, so the red-check could never
// see it go red: it pins behavior that was already correct and merely
// unasserted. See docs/quality-gateway.md's "Clearing the boundary-value class".
//
//efficacy:exempt pins pre-existing behavior; no implementation under it changed
func TestDialLoopbackAcceptsThePortRangeBoundaries(t *testing.T) {
	for _, port := range []int{1, 2, 65534, 65535} {
		rec := &forwardRecorder{}
		client := startTestSSHServer(t, startEchoServer(t), rec)
		sess := &SSHSession{id: "pane-boundary", client: client, state: StateConnected}

		conn, err := sess.DialLoopback(context.Background(), port)
		if err != nil {
			t.Fatalf("DialLoopback(%d) = %v, want the port to be forwarded", port, err)
		}
		conn.Close()

		_, ports := rec.snapshot()
		if len(ports) != 1 || int(ports[0]) != port {
			t.Fatalf("DialLoopback(%d) reached the remote as %v, want [%d]", port, ports, port)
		}
	}
}

func TestDialLoopbackFailsWhenTheRemotePortIsClosed(t *testing.T) {
	rec := &forwardRecorder{}
	// An address nothing listens on: the test server's dial to it fails, so
	// the channel is closed immediately after being accepted.
	client := startTestSSHServer(t, "127.0.0.1:1", rec)
	sess := &SSHSession{id: "pane-6", client: client, state: StateConnected}

	conn, err := sess.DialLoopback(context.Background(), 51234)
	if err != nil {
		t.Fatalf("DialLoopback: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("read from a channel with no upstream: %v", err)
	}
}

// Only SSH-backed panes need forwarding: a local or local-tmux pane's
// callback listener is already on the panemux host.
func TestLoopbackDialerIsImplementedOnlyBySSHBackedSessions(t *testing.T) {
	var _ LoopbackDialer = (*SSHSession)(nil)
	var _ LoopbackDialer = (*TmuxSSHSession)(nil)

	if _, ok := any(&LocalSession{}).(LoopbackDialer); ok {
		t.Fatal("LocalSession implements LoopbackDialer; local panes must not need forwarding")
	}
	if _, ok := any(&TmuxLocalSession{}).(LoopbackDialer); ok {
		t.Fatal("TmuxLocalSession implements LoopbackDialer; local tmux panes must not need forwarding")
	}
}
