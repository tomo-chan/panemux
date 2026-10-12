package session

import (
	"context"
	"errors"
	"net"
	"strconv"
	"sync"
)

// PreviewLoopback is a literal IPv4/IPv6 loopback destination, not a hostname
// interpreted by DNS. Its zero value is invalid.
type PreviewLoopback struct {
	host string
	port int
}

const previewIPv6Loopback = "::1"

func NewPreviewLoopback(host string, port int) (PreviewLoopback, error) {
	if (host != loopbackHost && host != previewIPv6Loopback) || port < 1 || port > 65535 {
		return PreviewLoopback{}, errors.New("invalid preview loopback destination")
	}
	return PreviewLoopback{host: host, port: port}, nil
}

func (a PreviewLoopback) address() string { return net.JoinHostPort(a.host, strconv.Itoa(a.port)) }

// PreviewTransport exposes only loopback forwarding and owns all its channels.
// SSH transports use a fresh SSH client; no PTY, shell, or helper is started.
type PreviewTransport struct {
	connections map[*previewConn]struct{}
	dial        func(context.Context, string, string) (net.Conn, error)
	closeHost   func() error
	mu          sync.Mutex
	closed      bool
}

// OpenPreviewTransport reuses pane authentication, known_hosts and routing.
// A canceled pending SSH dial is reclaimed when the existing bounded dialer
// returns. Successful transports have no dependency on the creation context.
func OpenPreviewTransport(ctx context.Context, cfg *SSHConfig) (*PreviewTransport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg == nil {
		return &PreviewTransport{
			connections: make(map[*previewConn]struct{}), dial: (&net.Dialer{}).DialContext,
			closeHost: func() error { return nil },
		}, nil
	}
	type result struct {
		transport *PreviewTransport
		err       error
	}
	results := make(chan result)
	go func() {
		client, jump, err := dialSSHClient(*cfg)
		var transport *PreviewTransport
		if err == nil {
			transport = &PreviewTransport{
				connections: make(map[*previewConn]struct{}), dial: client.DialContext, closeHost: func() error {
					closeErr := client.Close()
					if jump != nil {
						_ = jump.Close()
					}
					return closeErr
				}}
		}
		select {
		case results <- result{transport, err}:
		case <-ctx.Done():
			if transport != nil {
				_ = transport.Close()
			}
		}
	}()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-results:
		if err := ctx.Err(); err != nil {
			if result.transport != nil {
				_ = result.transport.Close()
			}
			return nil, err
		}
		return result.transport, result.err
	}
}

func (t *PreviewTransport) Dial(ctx context.Context, addr PreviewLoopback) (net.Conn, error) {
	if _, err := NewPreviewLoopback(addr.host, addr.port); err != nil {
		return nil, err
	}
	t.mu.Lock()
	closed := t.closed
	t.mu.Unlock()
	if closed {
		return nil, net.ErrClosed
	}
	conn, err := t.dial(ctx, "tcp", addr.address())
	if err != nil {
		return nil, err
	}
	t.mu.Lock()
	if t.closed || ctx.Err() != nil {
		t.mu.Unlock()
		_ = conn.Close()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, net.ErrClosed
	}
	tracked := &previewConn{Conn: conn, transport: t}
	t.connections[tracked] = struct{}{}
	t.mu.Unlock()
	return tracked, nil
}

func (t *PreviewTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	connections := t.connections
	t.connections = nil
	t.mu.Unlock()
	for conn := range connections {
		_ = conn.Close()
	}
	return t.closeHost()
}

type previewConn struct {
	net.Conn
	transport *PreviewTransport
	err       error
	once      sync.Once
}

func (c *previewConn) Close() error {
	c.once.Do(func() {
		c.err = c.Conn.Close()
		c.transport.mu.Lock()
		delete(c.transport.connections, c)
		c.transport.mu.Unlock()
	})
	return c.err
}
