package preview

import (
	"context"
	"errors"
	"net"
	"testing"

	"panemux/internal/session"
)

func TestDeliveredResultCancelledBeforeApplication(t *testing.T) {
	for _, hasTransport := range []bool{false, true} {
		t.Run(map[bool]string{false: "failed-dial", true: "late-success"}[hasTransport], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			transport := &transportStub{}
			var delivered Transport
			if hasTransport {
				delivered = transport
			}
			// The factory has already delivered; cancellation precedes application.
			cancel()
			connection, err := finishOpen(ctx, delivered, nil)
			if connection != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("applied canceled result: %v", err)
			}
			if hasTransport && transport.closed.Load() != 1 {
				t.Fatal("delivered transport was not closed once")
			}
		})
	}
}

func TestRetainedLeaseCannotDialAfterShutdown(t *testing.T) {
	transport := &transportStub{}
	connection, err := Open(context.Background(), Source{}, func(context.Context, Source) (Transport, error) {
		return transport, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	lease, err := connection.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	_ = connection.Close()
	if _, err := lease.Dial(context.Background(), session.PreviewLoopback{}); !errors.Is(err, net.ErrClosed) {
		t.Fatal(err)
	}
	_ = lease.Close()
	if transport.closed.Load() != 1 {
		t.Fatal("shutdown and release closed twice")
	}
}
