package preview

import (
	"context"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"panemux/internal/session"
)

type transportStub struct {
	closedCh chan struct{}
	closed   atomic.Int32
}

func (s *transportStub) Dial(_ context.Context, _ session.PreviewLoopback) (net.Conn, error) {
	return nil, nil
}
func (s *transportStub) Close() error {
	if s.closed.Add(1) == 1 && s.closedCh != nil {
		close(s.closedCh)
	}
	return nil
}

func TestCaptureAndConnectKeepsTabAfterPaneRelease(t *testing.T) {
	pane := &paneStub{kind: session.TypeLocal, cwd: "/workspace/project"}
	source, err := CapturePane(context.Background(), pane, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	transport := &transportStub{closedCh: make(chan struct{})}
	connection, err := Open(context.Background(), source, func(_ context.Context, fixed Source) (Transport, error) {
		if fixed.Workdir() != "/workspace/project" {
			return nil, errors.New("wrong source")
		}
		return transport, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	paneLease, err := connection.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	tab, err := connection.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	pane.cwd = "/workspace/changed"
	paneLease.Close()
	paneLease.Close()
	if transport.closed.Load() != 0 {
		t.Fatal("pane closed tab's transport")
	}
	if _, err := tab.Dial(context.Background(), session.PreviewLoopback{}); err != nil {
		t.Fatal(err)
	}
	tab.Close()
	tab.Close()
	connection.Close()
	if transport.closed.Load() != 1 {
		t.Fatalf("closed %d times", transport.closed.Load())
	}
	if _, err := connection.Acquire(); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("acquired closed connection: %v", err)
	}
	if _, err := paneLease.Dial(context.Background(), session.PreviewLoopback{}); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("used released lease: %v", err)
	}
}

func TestOpenCancellationClosesLateResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	finish, release := previewBarrier(t)
	returned := make(chan error, 1)
	transport := &transportStub{closedCh: make(chan struct{})}
	go func() {
		_, err := Open(ctx, Source{}, func(context.Context, Source) (Transport, error) {
			close(started)
			<-finish
			return transport, nil
		})
		returned <- err
	}()
	awaitPreviewEvent(t, started)
	cancel()
	if err := awaitPreviewError(t, returned); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
	release()
	select {
	case <-transport.closedCh:
	case <-time.After(2 * time.Second):
		t.Fatal("late transport not reclaimed")
	}
	if transport.closed.Load() != 1 {
		t.Fatal("late transport not closed once")
	}
}

func TestConnectionShutdownAndFactoryError(t *testing.T) {
	failure := errors.New("dial failed")
	_, err := Open(context.Background(), Source{}, func(context.Context, Source) (Transport, error) {
		return nil, failure
	})
	if !errors.Is(err, failure) {
		t.Fatal(err)
	}
	transport := &transportStub{}
	connection, err := Open(context.Background(), Source{}, func(context.Context, Source) (Transport, error) {
		return transport, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, _ := connection.Acquire()
	connection.Close()
	connection.Close()
	lease.Close()
	if transport.closed.Load() != 1 {
		t.Fatal("shutdown did not close once")
	}
}
