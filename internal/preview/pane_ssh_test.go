package preview

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
	"panemux/internal/config"
	"panemux/internal/session"
)

type paneSSHFixture struct {
	blockCWD           <-chan struct{}
	entered, cwdClosed chan<- struct{}
	label, upstream    string
}

func reviewHost(t *testing.T, label string, blockCWD <-chan struct{}, entered chan<- struct{},
	upstream string, cwdClosed chan<- struct{},
) session.SSHConfig {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	cfg := &ssh.ServerConfig{PasswordCallback: func(ssh.ConnMetadata, []byte) (*ssh.Permissions, error) {
		return nil, nil
	}}
	cfg.AddHostKey(signer)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	fixture := paneSSHFixture{blockCWD, entered, cwdClosed, label, upstream}
	servePaneSSHFixture(t, ln, cfg, fixture)
	f := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{ln.Addr().String()}, signer.PublicKey()) + "\n"
	if err := os.WriteFile(f, []byte(line), 0600); err != nil {
		t.Fatal(err)
	}
	return session.SSHConfig{
		Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port, User: "demo", Password: "fixture",
		KnownHostsFile: f, ConnectionName: "dev",
	}
}

func servePaneSSHFixture(t *testing.T, ln net.Listener, cfg *ssh.ServerConfig, fixture paneSSHFixture) {
	t.Helper()
	var mu sync.Mutex
	var raws []net.Conn
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		defer mu.Unlock()
		for _, c := range raws {
			c.Close()
		}
	})
	go func() {
		for {
			raw, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			raws = append(raws, raw)
			mu.Unlock()
			go fixture.serve(raw, cfg)
		}
	}()
}

func (f paneSSHFixture) serve(raw net.Conn, cfg *ssh.ServerConfig) {
	sc, channels, requests, err := ssh.NewServerConn(raw, cfg)
	if err != nil {
		raw.Close()
		return
	}
	defer sc.Close()
	go ssh.DiscardRequests(requests)
	for ch := range channels {
		go f.channel(ch)
	}
}

func (f paneSSHFixture) channel(ch ssh.NewChannel) {
	c, requests, err := ch.Accept()
	if err != nil {
		return
	}
	defer c.Close()
	if ch.ChannelType() == "direct-tcpip" {
		go ssh.DiscardRequests(requests)
		f.forward(c)
		return
	}
	for r := range requests {
		switch r.Type {
		case "exec":
			var payload struct{ Command string }
			ssh.Unmarshal(r.Payload, &payload)
			r.Reply(true, nil)
			terminal := strings.Contains(payload.Command, "PANEMUX_PANE_ID") ||
				(strings.Contains(payload.Command, "tmux") && !strings.HasPrefix(payload.Command, "tmux display-message "))
			if terminal {
				continue
			}
			f.cwd(c)
			return
		default:
			r.Reply(true, nil)
		}
	}
}

func (f paneSSHFixture) forward(c ssh.Channel) {
	if f.upstream == "" {
		io.WriteString(c, f.label)
		io.Copy(io.Discard, c)
		return
	}
	peer, err := net.Dial("tcp", f.upstream)
	if err != nil {
		return
	}
	defer peer.Close()
	go func() { io.Copy(c, peer); c.Close() }()
	io.Copy(peer, c)
}

func (f paneSSHFixture) cwd(c ssh.Channel) {
	if f.entered != nil {
		f.entered <- struct{}{}
	}
	if f.blockCWD != nil {
		closed := make(chan struct{})
		go func() {
			io.Copy(io.Discard, c)
			close(closed)
			if f.cwdClosed != nil {
				f.cwdClosed <- struct{}{}
			}
		}()
		select {
		case <-f.blockCWD:
		case <-closed:
			return
		}
	}
	io.WriteString(c, "/workspace/"+f.label+"\n")
	c.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
}

func newLivePane(t *testing.T, tmux bool, cfg session.SSHConfig) session.Session {
	t.Helper()
	var pane session.Session
	var err error
	if tmux {
		pane, err = session.NewTmuxSSH("fixture-pane", "fixture", "sample", cfg)
	} else {
		pane, err = session.NewSSH("fixture-pane", "fixture", cfg)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pane.Close() })
	return pane
}

func TestLivePanePreviewPreservesConnection(t *testing.T) {
	for _, tmux := range []bool{false, true} {
		for _, jump := range []bool{false, true} {
			t.Run(fmt.Sprintf("tmux=%t/jump=%t", tmux, jump), func(t *testing.T) {
				testLivePanePreviewPreservesConnection(t, tmux, jump)
			})
		}
	}
}

func testLivePanePreviewPreservesConnection(t *testing.T, tmux, jump bool) {
	a := reviewHost(t, "host-a", nil, nil, "", nil)
	b := reviewHost(t, "host-b", nil, nil, "", nil)
	if jump {
		jumpCfg := reviewHost(t, "jump-a", nil, nil, net.JoinHostPort(a.Host, strconv.Itoa(a.Port)), nil)
		a.JumpHost = &jumpCfg
	}
	pane := newLivePane(t, tmux, a)
	if got := pane.(session.SSHConfigGetter).SSHConfigSnapshot(); !reflect.DeepEqual(got, a) {
		t.Fatal("saved connection settings differ from live connection")
	}
	if jump {
		a.JumpHost.Host = "changed.test"
		a.JumpHost.Password = "changed"
	}
	snapshot := pane.(session.SSHConfigGetter).SSHConfigSnapshot()
	if snapshot.Host != a.Host || snapshot.Password != a.Password {
		t.Fatal("lost live settings")
	}
	if jump {
		snapshot.JumpHost.Host = "changed.test"
	}

	connections := map[string]config.SSHConnection{"dev": {
		Host: b.Host, Port: b.Port, User: b.User, Password: b.Password, KnownHostsFile: b.KnownHostsFile,
	}}
	source, err := CapturePane(context.Background(), pane, connections, "/missing/config")
	if err != nil {
		t.Fatal(err)
	}
	if source.Workdir() != "/workspace/host-a" {
		t.Fatalf("unexpected cwd: %q", source.Workdir())
	}
	pane.Close() // The captured source must own a fresh connection.
	connection, err := Open(context.Background(), source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	lease, err := connection.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	assertPanePreviewTraffic(t, lease)
}

func TestCaptureCancellationDuringLiveSSHCWD(t *testing.T) {
	for _, tmux := range []bool{false, true} {
		t.Run(fmt.Sprintf("tmux=%t", tmux), func(t *testing.T) { testCaptureCancellationDuringLiveSSHCWD(t, tmux) })
	}
}

func testCaptureCancellationDuringLiveSSHCWD(t *testing.T, tmux bool) {
	block := make(chan struct{})
	entered := make(chan struct{}, 1)
	cwdClosed := make(chan struct{}, 1)
	var once sync.Once
	unblock := func() { once.Do(func() { close(block) }) }
	defer unblock()
	a := reviewHost(t, "host-a", block, entered, "", cwdClosed)
	pane := newLivePane(t, tmux, a)
	// This tab already owns a separate connection before the next capture cancels.
	tab, err := Open(context.Background(), Source{ssh: &a, host: "dev"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tab.Close()
	lease, err := tab.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := CapturePane(ctx, pane, nil, "/missing/config")
		done <- e
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("cwd did not start")
	}
	cancel()
	select {
	case e := <-done:
		if !errors.Is(e, context.Canceled) {
			t.Fatal(e)
		}
	case <-time.After(250 * time.Millisecond):
		unblock()
		e := <-done
		t.Fatalf("capture waited for remote CWD after cancel; only returned after unblocking: %v", e)
	}
	select {
	case <-cwdClosed:
	case <-time.After(2 * time.Second):
		t.Fatal("CWD channel was not closed")
	}
	if pane.State() != session.StateConnected {
		t.Fatal("capture cancellation closed terminal")
	}
	assertPanePreviewTraffic(t, lease)
	unblock()
	// A fresh command channel on the terminal must still work.
	executor := pane.(session.BoardExecutor)
	if _, err := executor.RunBoardCommand(context.Background(), []string{"printf", "alive"}); err != nil {
		t.Fatal(err)
	}
}

func assertPanePreviewTraffic(t *testing.T, lease *Lease) {
	t.Helper()
	addr, _ := session.NewPreviewLoopback("127.0.0.1", 8080)
	conn, err := lease.Dial(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 6)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "host-a" {
		t.Fatalf("preview reached %s instead of host-a: %v", buf, err)
	}
}
