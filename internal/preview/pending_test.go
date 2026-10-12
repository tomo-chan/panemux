package preview

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"panemux/internal/session"
)

type pendingTransport struct {
	conn             net.Conn
	entered, release chan struct{}
	transportStub
}

func (p *pendingTransport) Dial(context.Context, session.PreviewLoopback) (net.Conn, error) {
	close(p.entered)
	<-p.release
	return p.conn, nil
}

func TestLeasePendingDialClosesLateResultAfterShutdown(t *testing.T) {
	for _, releaseLease := range []bool{false, true} {
		t.Run(map[bool]string{false: "shutdown", true: "release"}[releaseLease], func(t *testing.T) {
			left, right := net.Pipe()
			defer left.Close()
			defer right.Close()
			transport := &pendingTransport{conn: left, entered: make(chan struct{}), release: make(chan struct{})}
			connection, err := Open(context.Background(), Source{}, func(context.Context, Source) (Transport, error) {
				return transport, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			lease, _ := connection.Acquire()
			done := make(chan error, 1)
			go func() { _, err := lease.Dial(context.Background(), session.PreviewLoopback{}); done <- err }()
			<-transport.entered
			if releaseLease {
				_ = lease.Close()
			} else {
				_ = connection.Close()
			}
			close(transport.release)
			if err := <-done; !errors.Is(err, net.ErrClosed) {
				t.Fatalf("late dial returned after close: %v", err)
			}
			if _, err := left.Write([]byte("closed")); !errors.Is(err, io.ErrClosedPipe) {
				t.Fatalf("late channel leaked: %v", err)
			}
			_ = lease.Close()
			if transport.closed.Load() != 1 {
				t.Fatal("transport closed twice")
			}
		})
	}
}
