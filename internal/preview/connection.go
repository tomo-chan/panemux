package preview

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"

	"panemux/internal/session"
)

// Transport is owned by one preview, independently of any terminal or collector.
type Transport interface {
	Dial(context.Context, session.PreviewLoopback) (net.Conn, error)
	Close() error
}

type Factory func(context.Context, Source) (Transport, error)

// Connection owns one fixed source's transport. Its caller retains it for
// shutdown; each pane or tab owns a separate Lease. There is no global registry.
type Connection struct {
	transport Transport
	mu        sync.Mutex
	refs      int
	closed    bool
}

// Open uses ctx only for creation. Cancel a replaced UI request and compare its
// request ID before applying the result; an old result must never replace a new
// target. Once returned, the connection lives until last lease release or Close.
// A factory that returns after cancellation has its result closed here.
func Open(ctx context.Context, source Source, factory Factory) (*Connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("open preview: %w", err)
	}
	if factory == nil {
		factory = openTransport
	}
	type result struct {
		transport Transport
		err       error
	}
	results := make(chan result)
	go func() {
		transport, err := factory(ctx, source)
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
		return nil, fmt.Errorf("open preview: %w", ctx.Err())
	case result := <-results:
		if err := ctx.Err(); err != nil {
			if result.transport != nil {
				_ = result.transport.Close()
			}
			return nil, fmt.Errorf("open preview: %w", err)
		}
		if result.err != nil {
			if result.transport != nil {
				_ = result.transport.Close()
			}
			return nil, result.err
		}
		if result.transport == nil {
			return nil, errors.New("preview factory returned no transport")
		}
		return &Connection{transport: result.transport}, nil
	}
}

func openTransport(ctx context.Context, source Source) (Transport, error) {
	transport, err := session.OpenPreviewTransport(ctx, source.ssh)
	if err != nil {
		return nil, transportError("open transport", err)
	}
	return transport, nil
}

// Acquire retains this connection for a pane, tab, or active operation.
func (c *Connection) Acquire() (*Lease, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, net.ErrClosed
	}
	c.refs++
	return &Lease{connection: c}, nil
}

// Close releases the transport at server shutdown, including open tab leases.
func (c *Connection) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	c.mu.Unlock()
	return transportError("close", c.transport.Close())
}

// Lease belongs to one viewer or operation. Releasing a pane lease does not
// release a tab lease, and releasing twice has no additional effect.
type Lease struct {
	connection *Connection
	released   atomic.Bool
}

func (l *Lease) Dial(ctx context.Context, addr session.PreviewLoopback) (net.Conn, error) {
	if l.released.Load() {
		return nil, net.ErrClosed
	}
	c := l.connection
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return nil, net.ErrClosed
	}
	conn, err := c.transport.Dial(ctx, addr)
	c.mu.Lock()
	closed = c.closed
	c.mu.Unlock()
	if err == nil && (l.released.Load() || closed) {
		if conn != nil {
			_ = conn.Close()
		}
		return nil, net.ErrClosed
	}
	return conn, transportError("dial", err)
}

func (l *Lease) Close() error {
	if l.released.Swap(true) {
		return nil
	}
	c := l.connection
	c.mu.Lock()
	c.refs--
	last := c.refs == 0
	closeTransport := last && !c.closed
	if closeTransport {
		c.closed = true
	}
	c.mu.Unlock()
	if closeTransport {
		return transportError("release", c.transport.Close())
	}
	return nil
}

func transportError(action string, err error) error {
	if err != nil {
		return fmt.Errorf("preview %s: %w", action, err)
	}
	return nil
}
