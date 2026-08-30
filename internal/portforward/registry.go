package portforward

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"sort"
	"strconv"
	"sync"
	"time"
)

// ErrPortUnavailable reports that the loopback port cannot be bound on the
// panemux host: either another pane already forwards it, or an unrelated
// process on this machine holds it. The URL cannot be rewritten to a
// different port — an OAuth provider matches the registered redirect_uri
// exactly — so this is surfaced to the operator rather than worked around.
var ErrPortUnavailable = errors.New("loopback port unavailable")

// errRegistryClosed reports use of a registry after shutdown.
var errRegistryClosed = errors.New("port forward registry is closed")

const (
	// loopbackBindHost is the only address a forward is ever bound to. A
	// wildcard bind would expose the remote service to the whole network.
	loopbackBindHost = "127.0.0.1"

	defaultTTL           = 30 * time.Minute
	defaultMaxPerSession = 8
	defaultMaxTotal      = 32
	defaultSweepInterval = time.Minute
	remoteDialTimeout    = 10 * time.Second
)

// Dialer opens a connection to a loopback port on the host a pane's shell
// runs on. Implemented by SSH-backed sessions in internal/session.
type Dialer interface {
	DialLoopback(ctx context.Context, port int) (net.Conn, error)
}

// Options configures a Registry. The zero value selects the defaults.
type Options struct {
	// Now overrides the clock (tests).
	Now func() time.Time
	// Listen overrides the local listener factory (tests).
	Listen func(network, address string) (net.Listener, error)
	// TTL bounds how long an unused forward stays open.
	TTL time.Duration
	// SweepInterval sets the reaper's period; a negative value disables
	// the background reaper so a test can drive reaping itself.
	SweepInterval time.Duration
	MaxPerSession int
	MaxTotal      int
}

type forward struct {
	listener  net.Listener
	dialer    Dialer
	cancel    context.CancelFunc
	ctx       context.Context //nolint:containedctx // bounds the forward's own goroutines
	expiresAt time.Time
	sessionID string
	port      int
	// active counts the connections currently proxied through this
	// forward. Guarded by Registry.mu, like expiresAt.
	active int
}

// Registry owns every live loopback forward and their lifecycles.
type Registry struct {
	listenFn      func(network, address string) (net.Listener, error)
	nowFn         func() time.Time
	forwards      map[int]*forward
	done          chan struct{}
	ttl           time.Duration
	maxPerSession int
	maxTotal      int
	wg            sync.WaitGroup
	mu            sync.Mutex
	closed        bool
}

// New creates a Registry and starts its idle-forward reaper.
func New(opts Options) *Registry {
	r := &Registry{
		listenFn:      opts.Listen,
		nowFn:         opts.Now,
		forwards:      make(map[int]*forward),
		done:          make(chan struct{}),
		ttl:           opts.TTL,
		maxPerSession: opts.MaxPerSession,
		maxTotal:      opts.MaxTotal,
	}
	if r.listenFn == nil {
		r.listenFn = net.Listen
	}
	if r.nowFn == nil {
		r.nowFn = time.Now
	}
	if r.ttl <= 0 {
		r.ttl = defaultTTL
	}
	if r.maxPerSession <= 0 {
		r.maxPerSession = defaultMaxPerSession
	}
	if r.maxTotal <= 0 {
		r.maxTotal = defaultMaxTotal
	}
	if interval, run := resolveSweepInterval(opts.SweepInterval); run {
		r.wg.Add(1)
		go r.sweep(interval)
	}
	return r
}

// resolveSweepInterval reports the reaper's tick period for a configured
// Options.SweepInterval, and whether the background reaper runs at all: a
// negative value disables it so a test can drive reaping itself, zero selects
// defaultSweepInterval, and any positive value is used as given.
//
// Split out of New because the boundary between "disabled" and "defaulted"
// sits exactly at zero and nothing observable distinguishes the two from
// outside — a reaper that ticks once a minute cannot be waited on in a test,
// so New's own behavior at 0 was unverifiable in place. Same reason
// browserOpenArgv is split out of openChrome (see docs/security.md). Issue #190.
func resolveSweepInterval(configured time.Duration) (time.Duration, bool) {
	if configured < 0 {
		return 0, false
	}
	if configured == 0 {
		return defaultSweepInterval, true
	}
	return configured, true
}

// Ensure makes the pane-side loopback port reachable at the identical port on
// the panemux host, and reports whether a new forward was created. Calling it
// again for a live forward only refreshes its idle deadline.
func (r *Registry) Ensure(sessionID string, port int, dialer Dialer) (bool, error) {
	if sessionID == "" {
		return false, errors.New("session id is required")
	}
	if dialer == nil {
		return false, errors.New("session cannot forward loopback ports")
	}
	if !forwardablePort(port) {
		return false, fmt.Errorf(
			"port %d is outside the forwardable range %d-%d", port, minForwardablePort, maxPort,
		)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false, errRegistryClosed
	}
	if existing, ok := r.forwards[port]; ok {
		if existing.sessionID != sessionID {
			return false, fmt.Errorf(
				"%w: port %d is already forwarded for pane %s", ErrPortUnavailable, port, existing.sessionID,
			)
		}
		existing.expiresAt = r.nowFn().Add(r.ttl)
		return false, nil
	}
	if err := r.checkLimitsLocked(sessionID); err != nil {
		return false, err
	}

	f, err := r.startForwardLocked(sessionID, port, dialer)
	if err != nil {
		return false, err
	}
	r.forwards[port] = f
	return true, nil
}

func (r *Registry) checkLimitsLocked(sessionID string) error {
	if len(r.forwards) >= r.maxTotal {
		return fmt.Errorf("too many active port forwards (limit %d)", r.maxTotal)
	}
	count := 0
	for _, f := range r.forwards {
		if f.sessionID == sessionID {
			count++
		}
	}
	if count >= r.maxPerSession {
		return fmt.Errorf("pane %s already has %d port forwards (limit)", sessionID, count)
	}
	return nil
}

func (r *Registry) startForwardLocked(sessionID string, port int, dialer Dialer) (*forward, error) {
	addr := net.JoinHostPort(loopbackBindHost, strconv.Itoa(port))
	ln, err := r.listenFn("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot bind %s: %s", ErrPortUnavailable, addr, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &forward{
		listener:  ln,
		dialer:    dialer,
		cancel:    cancel,
		ctx:       ctx,
		sessionID: sessionID,
		expiresAt: r.nowFn().Add(r.ttl),
		port:      port,
	}
	r.wg.Add(1)
	go r.serve(f)
	return f, nil
}

func (r *Registry) serve(f *forward) {
	defer r.wg.Done()
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		r.beginConn(f)
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer r.endConn(f)
			r.proxy(f, conn)
		}()
	}
}

// proxy connects one accepted local connection to the pane host's loopback
// port and copies bytes in both directions until either side finishes.
func (r *Registry) proxy(f *forward, local net.Conn) {
	defer local.Close()

	ctx, cancel := context.WithTimeout(f.ctx, remoteDialTimeout)
	defer cancel()
	remote, err := f.dialer.DialLoopback(ctx, f.port)
	if err != nil {
		log.Printf("port forward %d: dialing pane loopback failed: %v", f.port, err)
		return
	}
	defer remote.Close()

	done := make(chan struct{}, 2)
	copyOnce := func(dst io.Writer, src io.Reader) {
		io.Copy(dst, src) //nolint:errcheck // a broken pipe just ends the forward
		done <- struct{}{}
	}
	go copyOnce(remote, local)
	go copyOnce(local, remote)

	select {
	case <-done:
	case <-f.ctx.Done():
	}
}

// beginConn records a connection entering the forward, which both refreshes
// the idle deadline and keeps the reaper away while the connection lives.
func (r *Registry) beginConn(f *forward) {
	r.mu.Lock()
	defer r.mu.Unlock()
	f.active++
	f.expiresAt = r.nowFn().Add(r.ttl)
}

// endConn records a connection leaving the forward and restarts its idle
// clock: the TTL measures time without traffic, not time since the last
// accept, so a long-lived connection must not be torn down mid-transfer.
func (r *Registry) endConn(f *forward) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// The guard is not decoration: without it an unpaired endConn drives
	// active negative, and reapExpired's own `f.active == 0` then never
	// matches again, so the forward outlives its TTL forever.
	// TestRegistryEndConnNeverDrivesTheCounterNegative pins that.
	if f.active > 0 {
		f.active--
	}
	f.expiresAt = r.nowFn().Add(r.ttl)
}

// Ports returns the forwarded ports for one pane, in ascending order.
func (r *Registry) Ports(sessionID string) []int {
	r.mu.Lock()
	defer r.mu.Unlock()
	var ports []int
	for port, f := range r.forwards {
		if f.sessionID == sessionID {
			ports = append(ports, port)
		}
	}
	sort.Ints(ports)
	return ports
}

// CloseSession tears down every forward belonging to one pane. Called when a
// pane is deleted or restarted: the listener it pointed at is gone.
func (r *Registry) CloseSession(sessionID string) {
	r.mu.Lock()
	var stale []*forward
	for port, f := range r.forwards {
		if f.sessionID == sessionID {
			stale = append(stale, f)
			delete(r.forwards, port)
		}
	}
	r.mu.Unlock()
	closeForwards(stale)
}

// Close tears down every forward and stops the reaper. It is idempotent.
func (r *Registry) Close() {
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return
	}
	r.closed = true
	stale := make([]*forward, 0, len(r.forwards))
	for port, f := range r.forwards {
		stale = append(stale, f)
		delete(r.forwards, port)
	}
	close(r.done)
	r.mu.Unlock()

	closeForwards(stale)
	r.wg.Wait()
}

func (r *Registry) sweep(interval time.Duration) {
	defer r.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.done:
			return
		case <-ticker.C:
			r.reapExpired()
		}
	}
}

// reapExpired closes forwards that have been idle for longer than the TTL.
func (r *Registry) reapExpired() {
	now := r.nowFn()
	r.mu.Lock()
	var stale []*forward
	for port, f := range r.forwards {
		if f.active == 0 && now.After(f.expiresAt) {
			stale = append(stale, f)
			delete(r.forwards, port)
		}
	}
	r.mu.Unlock()
	closeForwards(stale)
}

func closeForwards(forwards []*forward) {
	for _, f := range forwards {
		f.cancel()
		f.listener.Close()
	}
}
