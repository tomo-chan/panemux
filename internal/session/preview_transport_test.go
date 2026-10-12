package session

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
)

func TestPreviewLoopback(t *testing.T) {
	for _, host := range []string{"127.0.0.1", "::1"} {
		for _, port := range []int{1, 8080, 65535} {
			addr, err := NewPreviewLoopback(host, port)
			if err != nil || addr.address() != net.JoinHostPort(host, strconv.Itoa(port)) {
				t.Fatalf("address %s:%d: %v", host, port, err)
			}
		}
	}
	for _, host := range []string{"localhost", "127.0.0.2", "example.test", "0.0.0.0", "::"} {
		if _, err := NewPreviewLoopback(host, 8080); err == nil {
			t.Fatalf("accepted %s", host)
		}
	}
	for _, port := range []int{-1, 0, 65536} {
		if _, err := NewPreviewLoopback("127.0.0.1", port); err == nil {
			t.Fatalf("accepted %d", port)
		}
	}
}

func TestPreviewTransportLocalClose(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	transport, err := OpenPreviewTransport(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transport.Close()
	addr, _ := NewPreviewLoopback("127.0.0.1", listener.Addr().(*net.TCPAddr).Port)
	conn, err := transport.Dial(context.Background(), addr)
	if err != nil {
		t.Fatal(err)
	}
	peer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write([]byte("closed")); err == nil {
		t.Fatal("connection survived transport close")
	}
	if _, err := transport.Dial(context.Background(), addr); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("dial after close: %v", err)
	}
	if _, err := transport.Dial(context.Background(), PreviewLoopback{}); err == nil {
		t.Fatal("zero capability accepted")
	}
	if err := transport.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestPreviewTransportCancelledOpen(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := OpenPreviewTransport(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancel: %v", err)
	}
}
