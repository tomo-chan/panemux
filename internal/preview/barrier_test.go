package preview

import (
	"net"
	"sync"
	"testing"
	"time"
)

// Every controlled wait is released even when an assertion fails under a mutant.
func previewBarrier(t *testing.T) (chan struct{}, func()) {
	t.Helper()
	barrier := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(barrier) }) }
	t.Cleanup(release)
	return barrier, release
}

func awaitPreviewError(t *testing.T, results <-chan error) error {
	t.Helper()
	select {
	case err := <-results:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("preview fixture did not return")
		return nil
	}
}

type observedPreviewConn struct {
	net.Conn
	closed chan struct{}
	once   sync.Once
}

func (c *observedPreviewConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() { close(c.closed) })
	return err
}

func awaitPreviewEvent(t *testing.T, event <-chan struct{}) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(2 * time.Second):
		t.Fatal("preview fixture did not reach its barrier")
	}
}
