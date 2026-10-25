// Package hostexec owns the shared, pane-independent execution connections.
package hostexec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"panemux/internal/homedir"
	"panemux/internal/host"
	"panemux/internal/session"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"
)

type Conn interface {
	// Run runs cmd, feeding it stdin, and returns its standard output. An
	// error that has an ExitStatus() method is the command failing on a
	// healthy connection; any other error means the connection itself can
	// no longer be trusted.
	Run(ctx context.Context, cmd string, stdin io.Reader) ([]byte, error)
	InspectGitContext(ctx context.Context, cwd string) (session.GitContext, error)
	// Ping reports whether the connection still answers.
	Ping(ctx context.Context) error
	Close() error
}

var ErrConnecting = errors.New("connecting")
var ErrUnknownHost = errors.New("unknown host")

const pingTimeout = 5 * time.Second
const localWaitDelay = time.Second

// Options selects hosts and adapters. Dial uses the pane SSH dialer, but opens its own connection.
type Options struct {
	Hosts      func() []string
	Dial       func(string) (Conn, error)
	RunLocal   func(context.Context, string) ([]byte, error)
	Now        func() time.Time
	RetryAfter time.Duration
}

// Pool owns one command connection per SSH key; each call uses a separate exec channel.
type Pool struct {
	hosts  map[string]*hostConn
	opts   Options
	mu     sync.Mutex
	closed bool
}
type hostConn struct {
	failedAt time.Time
	conn     Conn
	err      error
	dialing  chan struct{}
}

func New(opts Options) *Pool {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.RetryAfter <= 0 {
		opts.RetryAfter = time.Minute
	}
	return &Pool{opts: opts, hosts: map[string]*hostConn{}}
}
func RunLocal(ctx context.Context, script string, args ...string) ([]byte, error) {
	home, err := homedir.Dir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for task collection: %w", err)
	}
	cmd := exec.CommandContext(ctx, "sh", "-s", "--")
	cmd.Args = append(cmd.Args, args...)
	cmd.Stdin = strings.NewReader(script)
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	cmd.WaitDelay = localWaitDelay
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("run task collection: %w", err)
	}
	return out, nil
}

// WithoutDialWait starts missing SSH connections but leaves their completion to a later poll.
// Script execution on an established connection still obeys the supplied context.
func (s *Pool) WithoutDialWait() host.Executor { return noDialWait{s} }

type noDialWait struct{ pool *Pool }

func (e noDialWait) Run(ctx context.Context, target host.Host, script string, args ...string) ([]byte, error) {
	return e.pool.run(ctx, target, script, false, args...) //nolint:wrapcheck // transparent execution adapter
}

func (s *Pool) Run(ctx context.Context, target host.Host, script string, args ...string) ([]byte, error) {
	return s.run(ctx, target, script, true, args...)
}
func (s *Pool) run(ctx context.Context, target host.Host, script string, wait bool, args ...string) ([]byte, error) {
	name := string(target)
	if name == "" {
		if len(args) == 0 && s.opts.RunLocal != nil {
			return s.opts.RunLocal(ctx, script)
		}
		return RunLocal(ctx, script, args...)
	}
	conn, err := s.connect(ctx, name, wait)
	if err != nil {
		return nil, err
	}
	out, err := conn.Run(ctx, command(args), strings.NewReader(script))
	switch {
	case err == nil:
		return out, nil
	case isExitError(err):
		return nil, &RunError{Err: err}
	case ctx.Err() != nil:
		// Out of time is not the same as a broken connection: a host that
		// is slow to answer, or has a large ~/.claude, would otherwise be
		// redialed every collection and never produce a result. The
		// connection is kept as long as it still answers a keepalive.
		pingCtx, cancel := context.WithTimeout(context.Background(), pingTimeout)
		defer cancel()
		if pingErr := conn.Ping(pingCtx); pingErr != nil {
			s.drop(name, conn)
		}
		return nil, &RunError{Err: ctx.Err(), TimedOut: true}
	default:
		s.drop(name, conn)
		return nil, &RunError{Err: err}
	}
}

func (s *Pool) Current(host string) Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h := s.hosts[host]; h != nil {
		return h.conn
	}
	return nil
}

func (s *Pool) Reconnect(name string) error {
	if !s.known(name) {
		return fmt.Errorf("%w: %q", ErrUnknownHost, name)
	}

	s.mu.Lock()
	var stale Conn
	if h := s.hosts[name]; h != nil {
		stale = h.conn
		// A fresh entry rather than clearing this one: dial() only keeps
		// its result while the entry it was started for is still the host's.
		delete(s.hosts, name)
	}
	s.mu.Unlock()
	if stale != nil {
		_ = stale.Close()
	}
	return nil
}

func (s *Pool) Close() {
	s.mu.Lock()
	s.closed = true
	var conns []Conn
	for name, h := range s.hosts {
		if h.conn != nil {
			conns = append(conns, h.conn)
		}
		delete(s.hosts, name)
	}
	s.mu.Unlock()
	for _, conn := range conns {
		_ = conn.Close()
	}
}
func (s *Pool) known(name string) bool {
	for _, candidate := range s.hostNames() {
		if candidate == name {
			return true
		}
	}
	return false
}
func (s *Pool) Connect(ctx context.Context, name string) (Conn, error) {
	return s.connect(ctx, name, true)
}
func (s *Pool) connect(ctx context.Context, name string, wait bool) (Conn, error) {
	if !s.known(name) {
		return nil, fmt.Errorf("%w: %q", ErrUnknownHost, name)
	}
	conn, h, done, err := s.beginConnect(name)
	if err != nil || conn != nil {
		return conn, err
	}
	if !wait {
		return nil, ErrConnecting
	}

	select {
	case <-done:
	case <-ctx.Done():
		return nil, ErrConnecting
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if h.conn != nil {
		return h.conn, nil
	}
	if h.err != nil {
		return nil, h.err
	}
	// Closed or reconnected while this dial was in flight.
	return nil, ErrConnecting
}

// beginConnect keeps connection lookup and dial admission in one lock scope.
func (s *Pool) beginConnect(name string) (Conn, *hostConn, <-chan struct{}, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, nil, nil, errors.New("host execution is closed")
	}
	h := s.hosts[name]
	if h == nil {
		h = &hostConn{}
		s.hosts[name] = h
	}
	if h.conn != nil {
		return h.conn, h, nil, nil
	}
	if h.dialing == nil {
		if h.err != nil && s.opts.Now().Sub(h.failedAt) < s.opts.RetryAfter {
			return nil, h, nil, h.err
		}
		h.dialing = make(chan struct{})
		go s.dial(name, h, h.dialing)
	}
	return nil, h, h.dialing, nil
}

func (s *Pool) dial(name string, h *hostConn, done chan struct{}) {
	var conn Conn
	var err error
	if s.opts.Dial == nil {
		err = errors.New("no SSH dialer")
	} else {
		conn, err = s.opts.Dial(name)
	}

	s.mu.Lock()
	h.dialing = nil
	switch {
	case s.closed || s.hosts[name] != h:
		if conn != nil {
			defer conn.Close() //nolint:errcheck // nobody is left to use it
		}
	case err != nil:
		h.err = fmt.Errorf("connect to %s: %w", name, err)
		h.failedAt = s.opts.Now()
	default:
		h.conn = conn
		h.err = nil
	}
	s.mu.Unlock()
	close(done)
}

// drop discards conn after it failed, unless it was already replaced. The
// next collection dials again at once: a dropped connection (the host
// rebooted, the network moved) is not a reason to wait.
func (s *Pool) drop(name string, conn Conn) {
	s.mu.Lock()
	if h := s.hosts[name]; h != nil && h.conn == conn {
		h.conn = nil
	}
	s.mu.Unlock()
	_ = conn.Close()
}

func (s *Pool) Retain(names []string) {
	keep := map[string]bool{}
	for _, n := range names {
		keep[n] = true
	}
	s.mu.Lock()
	var stale []Conn
	for n, h := range s.hosts {
		if !keep[n] {
			if h.conn != nil {
				stale = append(stale, h.conn)
			}
			delete(s.hosts, n)
		}
	}
	s.mu.Unlock()
	for _, c := range stale {
		_ = c.Close()
	}
}
func (s *Pool) hostNames() []string {
	names := []string{""}
	if s.opts.Hosts != nil {
		names = append(names, s.opts.Hosts()...)
	}
	return names
}
func (s *Pool) Names() []host.Host {
	names := s.hostNames()
	sort.Strings(names)
	out := make([]host.Host, len(names))
	for i, n := range names {
		out[i] = host.Host(n)
	}
	return out
}
func command(args []string) string {
	var cmd strings.Builder
	cmd.WriteString("sh -s")
	if len(args) > 0 {
		cmd.WriteString(" --")
	}
	for _, arg := range args {
		cmd.WriteString(" '")
		cmd.WriteString(strings.ReplaceAll(arg, "'", `'\''`))
		cmd.WriteString("'")
	}
	return cmd.String()
}
func isExitError(err error) bool {
	var exit interface{ ExitStatus() int }
	return errors.As(err, &exit)
}

// RunError distinguishes an exec failure from connection setup errors.
type RunError struct {
	Err      error
	TimedOut bool
}

func (e *RunError) Error() string { return e.Err.Error() }
func (e *RunError) Unwrap() error { return e.Err }
