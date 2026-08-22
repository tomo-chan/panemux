package portforward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"
)

// echoDialer stands in for a pane's SSH connection: DialLoopback hands back a
// connection to a local echo server, recording which port was requested.
type echoDialer struct {
	err         error
	addr        string
	askedPorts  []int
	mu          sync.Mutex
	dialCallCnt int
}

func (d *echoDialer) DialLoopback(ctx context.Context, port int) (net.Conn, error) {
	d.mu.Lock()
	d.askedPorts = append(d.askedPorts, port)
	d.dialCallCnt++
	d.mu.Unlock()
	if d.err != nil {
		return nil, d.err
	}
	var dialer net.Dialer
	conn, err := dialer.DialContext(ctx, "tcp", d.addr)
	if err != nil {
		return nil, fmt.Errorf("dialing echo server: %w", err)
	}
	return conn, nil
}

func (d *echoDialer) ports() []int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]int(nil), d.askedPorts...)
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen echo: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

// freePort returns a loopback port that was free a moment ago.
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	return port
}

// fakeClock is a mutex-guarded clock so the registry's own goroutines can read
// it while the test advances it.
type fakeClock struct {
	now time.Time
	mu  sync.Mutex
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func newTestRegistry(t *testing.T, opts Options) *Registry {
	t.Helper()
	r := New(opts)
	t.Cleanup(r.Close)
	return r
}

func roundTrip(t *testing.T, port int, payload string) string {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarded port %d: %v", port, err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.WriteString(conn, payload); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, len(payload))
	if _, err := io.ReadFull(conn, buf); err != nil {
		t.Fatalf("read back: %v", err)
	}
	return string(buf)
}

func TestRegistryEnsureForwardsTrafficToTheDialer(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	r := newTestRegistry(t, Options{})
	port := freePort(t)

	created, err := r.Ensure("pane-1", port, dialer)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if !created {
		t.Fatal("Ensure created = false, want true for a new forward")
	}

	if got := roundTrip(t, port, "hello"); got != "hello" {
		t.Fatalf("round trip = %q, want %q", got, "hello")
	}
	if got := dialer.ports(); len(got) != 1 || got[0] != port {
		t.Fatalf("dialer asked for ports %v, want [%d]", got, port)
	}
}

func TestRegistryEnsureIsIdempotentForTheSameSessionAndPort(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	r := newTestRegistry(t, Options{})
	port := freePort(t)

	if _, err := r.Ensure("pane-1", port, dialer); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	created, err := r.Ensure("pane-1", port, dialer)
	if err != nil {
		t.Fatalf("second Ensure: %v", err)
	}
	if created {
		t.Fatal("second Ensure created = true, want false for an existing forward")
	}
	if got := roundTrip(t, port, "still-up"); got != "still-up" {
		t.Fatalf("round trip = %q, want %q", got, "still-up")
	}
	if got := r.Ports("pane-1"); len(got) != 1 || got[0] != port {
		t.Fatalf("Ports = %v, want [%d]", got, port)
	}
}

func TestRegistryEnsureRejectsPortHeldByAnotherSession(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	r := newTestRegistry(t, Options{})
	port := freePort(t)

	if _, err := r.Ensure("pane-1", port, dialer); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	_, err := r.Ensure("pane-2", port, dialer)
	if !errors.Is(err, ErrPortUnavailable) {
		t.Fatalf("Ensure for other session err = %v, want ErrPortUnavailable", err)
	}
}

func TestRegistryEnsureReportsPortAlreadyBoundOnTheHost(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	r := newTestRegistry(t, Options{})

	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen blocker: %v", err)
	}
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port

	_, err = r.Ensure("pane-1", port, dialer)
	if !errors.Is(err, ErrPortUnavailable) {
		t.Fatalf("Ensure on a bound port err = %v, want ErrPortUnavailable", err)
	}
	if r.Ports("pane-1") != nil {
		t.Fatalf("failed Ensure left state behind: %v", r.Ports("pane-1"))
	}
}

func TestRegistryEnsureRejectsUnforwardablePorts(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	r := newTestRegistry(t, Options{})

	for _, port := range []int{0, -1, 80, 1023, 65536, 99999} {
		if _, err := r.Ensure("pane-1", port, dialer); err == nil {
			t.Fatalf("Ensure(port=%d) = nil, want error", port)
		}
	}
}

func TestRegistryEnsureRequiresSessionAndDialer(t *testing.T) {
	r := newTestRegistry(t, Options{})
	if _, err := r.Ensure("", freePort(t), &echoDialer{}); err == nil {
		t.Fatal("Ensure with empty session ID = nil, want error")
	}
	if _, err := r.Ensure("pane-1", freePort(t), nil); err == nil {
		t.Fatal("Ensure with nil dialer = nil, want error")
	}
}

func TestRegistryEnsureEnforcesPerSessionAndTotalLimits(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	r := newTestRegistry(t, Options{MaxPerSession: 1, MaxTotal: 2})

	if _, err := r.Ensure("pane-1", freePort(t), dialer); err != nil {
		t.Fatalf("first Ensure: %v", err)
	}
	if _, err := r.Ensure("pane-1", freePort(t), dialer); err == nil {
		t.Fatal("second forward for the same session = nil, want per-session limit error")
	}
	if _, err := r.Ensure("pane-2", freePort(t), dialer); err != nil {
		t.Fatalf("Ensure for second session: %v", err)
	}
	if _, err := r.Ensure("pane-3", freePort(t), dialer); err == nil {
		t.Fatal("third forward overall = nil, want total limit error")
	}
}

func TestRegistryCloseSessionStopsOnlyThatSessionsForwards(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	r := newTestRegistry(t, Options{})
	keptPort := freePort(t)
	closedPort := freePort(t)

	if _, err := r.Ensure("pane-1", closedPort, dialer); err != nil {
		t.Fatalf("Ensure pane-1: %v", err)
	}
	if _, err := r.Ensure("pane-2", keptPort, dialer); err != nil {
		t.Fatalf("Ensure pane-2: %v", err)
	}

	r.CloseSession("pane-1")

	if got := r.Ports("pane-1"); got != nil {
		t.Fatalf("Ports after CloseSession = %v, want nil", got)
	}
	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(closedPort)), time.Second); err == nil {
		t.Fatal("closed forward still accepts connections")
	}
	if got := roundTrip(t, keptPort, "kept"); got != "kept" {
		t.Fatalf("other session's forward broke: %q", got)
	}
}

func TestRegistryCloseStopsEveryForward(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	r := New(Options{})
	port := freePort(t)
	if _, err := r.Ensure("pane-1", port, dialer); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	r.Close()
	r.Close() // idempotent

	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second); err == nil {
		t.Fatal("forward still accepts connections after Close")
	}
	if _, err := r.Ensure("pane-1", freePort(t), dialer); err == nil {
		t.Fatal("Ensure after Close = nil, want error")
	}
}

func TestRegistryReapsIdleForwardsAndKeepsUsedOnes(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	clock := newFakeClock()
	r := newTestRegistry(t, Options{
		TTL:           10 * time.Minute,
		SweepInterval: -1, // no background sweeper; the test drives reaping
		Now:           clock.Now,
	})
	idlePort := freePort(t)
	usedPort := freePort(t)

	if _, err := r.Ensure("pane-1", idlePort, dialer); err != nil {
		t.Fatalf("Ensure idle: %v", err)
	}
	if _, err := r.Ensure("pane-1", usedPort, dialer); err != nil {
		t.Fatalf("Ensure used: %v", err)
	}

	// A connection through the used forward refreshes its deadline.
	clock.Advance(9 * time.Minute)
	if got := roundTrip(t, usedPort, "use"); got != "use" {
		t.Fatalf("round trip = %q", got)
	}

	clock.Advance(2 * time.Minute)
	r.reapExpired()

	if got := r.Ports("pane-1"); len(got) != 1 || got[0] != usedPort {
		t.Fatalf("Ports after reap = %v, want [%d]", got, usedPort)
	}
	if _, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(idlePort)), time.Second); err == nil {
		t.Fatal("idle forward still accepts connections after reaping")
	}
}

func TestRegistryEnsureRefreshesTheIdleDeadline(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	clock := newFakeClock()
	r := newTestRegistry(t, Options{
		TTL:           10 * time.Minute,
		SweepInterval: -1,
		Now:           clock.Now,
	})
	port := freePort(t)
	if _, err := r.Ensure("pane-1", port, dialer); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	clock.Advance(9 * time.Minute)
	if _, err := r.Ensure("pane-1", port, dialer); err != nil {
		t.Fatalf("refreshing Ensure: %v", err)
	}

	clock.Advance(2 * time.Minute)
	r.reapExpired()

	if got := r.Ports("pane-1"); len(got) != 1 {
		t.Fatalf("Ports after refresh + reap = %v, want the forward kept", got)
	}
}

func TestRegistryClosesLocalConnectionWhenTheRemoteDialFails(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t), err: errors.New("remote refused")}
	r := newTestRegistry(t, Options{})
	port := freePort(t)
	if _, err := r.Ensure("pane-1", port, dialer); err != nil {
		t.Fatalf("Ensure: %v", err)
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	if err != nil {
		t.Fatalf("dial forwarded port: %v", err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := io.ReadAll(conn); err != nil {
		t.Fatalf("read after failed remote dial: %v", err)
	}
}

func TestRegistryPortsIsSortedAndScopedToTheSession(t *testing.T) {
	dialer := &echoDialer{addr: startEchoServer(t)}
	r := newTestRegistry(t, Options{})
	ports := []int{freePort(t), freePort(t), freePort(t)}
	for _, p := range ports {
		if _, err := r.Ensure("pane-1", p, dialer); err != nil {
			t.Fatalf("Ensure(%d): %v", p, err)
		}
	}
	if _, err := r.Ensure("pane-2", freePort(t), dialer); err != nil {
		t.Fatalf("Ensure pane-2: %v", err)
	}

	got := r.Ports("pane-1")
	if len(got) != 3 {
		t.Fatalf("Ports = %v, want 3 entries", got)
	}
	for i := 1; i < len(got); i++ {
		if got[i-1] >= got[i] {
			t.Fatalf("Ports = %v, want ascending order", got)
		}
	}
}
