package session

import (
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func previewSSHFixture(t *testing.T, upstream string, recorder *forwardRecorder) (SSHConfig, <-chan struct{}) {
	t.Helper()
	signer := testSSHSigner(t)
	server := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if string(password) != "fixture-password" {
			return nil, errors.New("bad password")
		}
		return nil, nil
	}}
	server.AddHostKey(signer)
	listener, err := net.Listen("tcp", loopbackHost+":0")
	if err != nil {
		t.Fatal(err)
	}
	transport := &testSSHTransport{forward: recorder, echoAddr: upstream}
	closed := make(chan struct{}, 32)
	t.Cleanup(func() { _ = listener.Close(); transport.closeConnections() })
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go func() { transport.serve(raw, server); closed <- struct{}{} }()
		}
	}()
	file := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey()) + "\n"
	if err := os.WriteFile(file, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	return SSHConfig{
		Host: loopbackHost, Port: listener.Addr().(*net.TCPAddr).Port,
		User: "demo", Password: "fixture-password", KnownHostsFile: file,
	}, closed
}

func awaitPreviewSSHClose(t *testing.T, closed <-chan struct{}) {
	t.Helper()
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("SSH transport leaked")
	}
}

func TestPreviewSSHIndependentConnectionAndProxyJump(t *testing.T) {
	for _, jump := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "jump"}[jump], func(t *testing.T) {
			targetRecorder := &forwardRecorder{}
			cfg, targetClosed := previewSSHFixture(t, startEchoServer(t), targetRecorder)
			var jumpClosed <-chan struct{}
			if jump {
				jumpCfg, closed := previewSSHFixture(t, net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port)), &forwardRecorder{})
				cfg.JumpHost = &jumpCfg
				jumpClosed = closed
			}
			ctx, cancel := context.WithCancel(context.Background())
			transport, err := OpenPreviewTransport(ctx, &cfg)
			if err != nil {
				t.Fatal(err)
			}
			defer transport.Close()
			cancel() // Only creation was owned by this caller; traffic must continue.
			for _, host := range []string{loopbackHost, "::1"} {
				addr, _ := NewPreviewLoopback(host, 8080)
				conn, err := transport.Dial(context.Background(), addr)
				if err != nil {
					t.Fatal(err)
				}
				assertLoopbackRoundTrip(t, conn, "preview")
			}
			addrs, ports := targetRecorder.snapshot()
			if len(addrs) != 2 || addrs[0] != loopbackHost || addrs[1] != "::1" || ports[0] != 8080 || ports[1] != 8080 {
				t.Fatalf("unexpected fixed loopback destinations: %v %v", addrs, ports)
			}
			if err := transport.Close(); err != nil {
				t.Fatal(err)
			}
			awaitPreviewSSHClose(t, targetClosed)
			if jump {
				awaitPreviewSSHClose(t, jumpClosed)
			}
		})
	}
}

func TestPreviewSSHCancelReclaimsLateSuccess(t *testing.T) {
	cfg, closed := previewSSHFixture(t, startEchoServer(t), &forwardRecorder{})
	entered, release := make(chan struct{}), make(chan struct{})
	original := dialTransportFn
	dialTransportFn = func(cfg SSHConfig, addr string, port int, deadline time.Time) (net.Conn, *ssh.Client, error) {
		close(entered)
		<-release
		return original(cfg, addr, port, deadline)
	}
	t.Cleanup(func() { dialTransportFn = original })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := OpenPreviewTransport(ctx, &cfg); done <- err }()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("cancel waited for dial")
	}
	close(release)
	awaitPreviewSSHClose(t, closed)
}

func TestPreviewSSHRefusesBadKnownHostAndAuthentication(t *testing.T) {
	for _, badKey := range []bool{false, true} {
		t.Run(map[bool]string{false: "password", true: "known-host"}[badKey], func(t *testing.T) {
			cfg, closed := previewSSHFixture(t, startEchoServer(t), &forwardRecorder{})
			if badKey {
				line := knownhosts.Line([]string{net.JoinHostPort(cfg.Host, strconv.Itoa(cfg.Port))}, testSSHSigner(t).PublicKey())
				if err := os.WriteFile(cfg.KnownHostsFile, []byte(line+"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				cfg.Password = "wrong"
			}
			if transport, err := OpenPreviewTransport(context.Background(), &cfg); err == nil || transport != nil {
				t.Fatal("unsafe SSH accepted")
			}
			awaitPreviewSSHClose(t, closed)
		})
	}
}
