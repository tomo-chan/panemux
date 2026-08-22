package session

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
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

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate host key: %v", err)
	}
	signer, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		t.Fatalf("signer: %v", err)
	}
	serverCfg := &ssh.ServerConfig{NoClientAuth: true}
	serverCfg.AddHostKey(signer)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen ssh: %v", err)
	}
	t.Cleanup(func() { ln.Close() })

	go func() {
		for {
			raw, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			go serveTestSSHConn(raw, serverCfg, echoAddr, rec)
		}
	}()

	client, err := ssh.Dial("tcp", ln.Addr().String(), &ssh.ClientConfig{
		User:            "tester",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // G106: ephemeral in-process test server
		Timeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("ssh dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func serveTestSSHConn(raw net.Conn, cfg *ssh.ServerConfig, echoAddr string, rec *forwardRecorder) {
	conn, chans, reqs, err := ssh.NewServerConn(raw, cfg)
	if err != nil {
		raw.Close()
		return
	}
	defer conn.Close()
	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		if newChannel.ChannelType() != "direct-tcpip" {
			newChannel.Reject(ssh.UnknownChannelType, "unsupported")
			continue
		}
		var payload directTCPIPPayload
		if err := ssh.Unmarshal(newChannel.ExtraData(), &payload); err != nil {
			newChannel.Reject(ssh.ConnectionFailed, "bad payload")
			continue
		}
		rec.record(payload.DestAddr, payload.DestPort)

		ch, chReqs, err := newChannel.Accept()
		if err != nil {
			continue
		}
		go ssh.DiscardRequests(chReqs)
		go pipeTestChannel(ch, echoAddr)
	}
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
