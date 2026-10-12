package preview

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"panemux/internal/config"
	"panemux/internal/session"
)

func TestDefaultFactoryKeepsLocalTrafficAfterCreationCancel(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	peerDone := make(chan struct{})
	go func() {
		defer close(peerDone)
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_, _ = io.Copy(conn, conn)
	}()
	ctx, cancel := context.WithCancel(context.Background())
	source, err := CapturePane(ctx, &paneStub{kind: session.TypeLocal, cwd: "/workspace/project"}, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	connection, err := Open(ctx, source, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	pane, _ := connection.Acquire()
	tab, _ := connection.Acquire()
	cancel()
	_ = pane.Close()
	addr, _ := session.NewPreviewLoopback("127.0.0.1", listener.Addr().(*net.TCPAddr).Port)
	conn, err := tab.Dial(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	assertPreviewDeadline(t, conn)
	if _, err := conn.Write([]byte("tab")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "tab" {
		t.Fatalf("tab traffic: %s %v", buf, err)
	}
	_ = tab.Close()
	if _, err := conn.Write([]byte("closed")); err == nil {
		t.Fatal("last reference did not close traffic")
	}
	select {
	case <-peerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("local channel leaked")
	}
}

func TestDefaultFactoryReportsSSHSetupFailure(t *testing.T) {
	connections := map[string]config.SSHConnection{
		"dev": {Host: "host.test", KnownHostsFile: "relative/known_hosts", Password: "fixture"},
	}
	pane := &paneStub{kind: session.TypeSSH, host: "dev"}
	source, err := CapturePane(context.Background(), pane, connections, "/missing/config")
	if err != nil {
		t.Fatal(err)
	}
	if conn, err := Open(context.Background(), source, nil); err == nil || conn != nil {
		t.Fatalf("SSH setup failure: %v", err)
	}
}

func TestReplacedRequestCannotReturnAnOldTarget(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	oldTransport := &transportStub{closedCh: make(chan struct{})}
	oldResult := make(chan error, 1)
	go func() {
		_, err := Open(ctx, Source{workdir: "/workspace/old"}, func(context.Context, Source) (Transport, error) {
			close(entered)
			<-release
			return oldTransport, nil
		})
		oldResult <- err
	}()
	<-entered
	cancel()
	newSource := Source{workdir: "/workspace/new"}
	newTransport := &transportStub{}
	current, err := Open(context.Background(), newSource, func(_ context.Context, source Source) (Transport, error) {
		if source.Workdir() != "/workspace/new" {
			return nil, errors.New("wrong target")
		}
		return newTransport, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer current.Close()
	close(release)
	if err := <-oldResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("old response: %v", err)
	}
	select {
	case <-oldTransport.closedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("old transport leaked")
	}
	if newTransport.closed.Load() != 0 {
		t.Fatal("old request closed new target")
	}
}

func TestConnectionConcurrentAcquireReleaseAndShutdown(t *testing.T) {
	transport := &transportStub{}
	connection, err := Open(context.Background(), Source{}, func(context.Context, Source) (Transport, error) {
		return transport, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	base, _ := connection.Acquire()
	var group sync.WaitGroup
	for range 32 {
		group.Go(func() {
			lease, err := connection.Acquire()
			if err == nil {
				_ = lease.Close()
				_ = lease.Close()
			} else if !errors.Is(err, net.ErrClosed) {
				t.Error(err)
			}
		})
	}
	group.Go(func() { _ = connection.Close() })
	group.Go(func() { _ = base.Close() })
	group.Wait()
	if transport.closed.Load() != 1 {
		t.Fatalf("close count=%d", transport.closed.Load())
	}
}

func TestOpenFactoryBoundaryResults(t *testing.T) {
	failure := errors.New("factory failure")
	for _, tc := range []struct {
		transport Transport
		err       error
		name      string
	}{
		{name: "nil-success"}, {name: "error", err: failure},
		{name: "transport-and-error", transport: &transportStub{}, err: failure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Open(context.Background(), Source{}, func(context.Context, Source) (Transport, error) {
				return tc.transport, tc.err
			})
			if err == nil {
				t.Fatal("bad factory result accepted")
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatal(err)
			}
			if tc.transport != nil && tc.transport.(*transportStub).closed.Load() != 1 {
				t.Fatal("failed transport leaked")
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := Open(ctx, Source{}, func(context.Context, Source) (Transport, error) {
		t.Error("canceled request dialed")
		return nil, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func assertPreviewDeadline(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
}
