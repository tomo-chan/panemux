// Package tasks collects the agent sessions running on each host panemux
// knows — the panemux host itself and every ssh_connections entry — for the
// task dashboard (issue #252).
//
// Collection does not depend on panes. It keeps one SSH connection per host,
// reused across collections and independent of any pane's own connection,
// and runs one fixed script over it (collectScript) that reads only what the
// agents write for themselves (~/.claude/sessions, ~/.claude/projects) and
// what `ps` and `tmux` report. Nothing collects in the background: a
// collection runs only when the dashboard asks for one.
package tasks

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"panemux/internal/homedir"
	"panemux/internal/session"
)

// Conn is one open connection to a remote host, as the service needs it.
// *session.CommandConn is the production implementation.
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

// HostStatus is whether a host's tasks could be collected.
type HostStatus string

// Host statuses. Connecting means the first connection is still being set
// up and has not failed; the host's tasks appear on a later collection.
const (
	HostOK         HostStatus = "ok"
	HostError      HostStatus = "error"
	HostConnecting HostStatus = "connecting"
)

// HostResult is one host's outcome in a collection.
type HostResult struct {
	CollectedAt *time.Time `json:"collected_at,omitempty"`
	// Name is the ssh_connections key, or "" for the panemux host.
	Name   string     `json:"name"`
	Status HostStatus `json:"status"`
	Error  string     `json:"error,omitempty"`
}

// Snapshot is one collection across every host.
type Snapshot struct {
	Hosts []HostResult `json:"hosts"`
	Tasks []Task       `json:"tasks"`
}

// Options configures a Service. Only Hosts and Dial are required.
type Options struct {
	// Hosts returns the ssh_connections keys to collect from, read on every
	// collection so a connection added or removed in the config is picked up.
	Hosts func() []string
	// Dial opens a connection to the named host.
	Dial func(name string) (Conn, error)
	// RunLocal runs the collection script on the panemux host.
	RunLocal func(ctx context.Context, script string) ([]byte, error)
	Now      func() time.Time
	// HostTimeout bounds one host's collection, including waiting for its
	// connection to come up. A connection still coming up when it expires
	// keeps dialing and serves the next collection.
	HostTimeout time.Duration
	// RetryAfter is how long a host whose connection failed is left alone
	// before an ordinary collection dials it again. Reconnect skips the wait.
	RetryAfter time.Duration
}

const (
	defaultHostTimeout = 15 * time.Second
	defaultRetryAfter  = time.Minute
	// pingTimeout bounds the keepalive that decides whether a connection
	// whose collection ran out of time is still worth keeping.
	pingTimeout = 5 * time.Second
	// localWaitDelay bounds how long the local collection waits for its
	// output pipe to close after its context ended and its process group
	// was killed.
	localWaitDelay = time.Second
)

// errConnecting reports a host whose connection is still being set up.
var errConnecting = errors.New("connecting")

// ErrUnknownHost is returned for a host name that is not an ssh_connections key.
var ErrUnknownHost = errors.New("unknown host")

// Service collects tasks and owns the per-host connections.
type Service struct {
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

// New returns a Service. Close it to release its connections.
func New(opts Options) *Service {
	if opts.RunLocal == nil {
		opts.RunLocal = runLocal
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.HostTimeout <= 0 {
		opts.HostTimeout = defaultHostTimeout
	}
	if opts.RetryAfter <= 0 {
		opts.RetryAfter = defaultRetryAfter
	}
	return &Service{opts: opts, hosts: map[string]*hostConn{}}
}

// runLocal runs the collection script with `sh -s`. The command is a
// literal and the script, itself a constant, arrives on stdin. HOME is set
// from internal/homedir so the script reads the same home directory the rest
// of panemux resolves.
//
// The script runs in a process group of its own, and the whole group is
// killed when ctx ends: a probe the script started (lsof, a subshell) holds
// its stdout open, and killing sh alone would leave Output waiting for that
// probe. WaitDelay bounds the wait for anything that escaped the group.
func runLocal(ctx context.Context, script string) ([]byte, error) {
	home, err := homedir.Dir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for task collection: %w", err)
	}
	cmd := exec.CommandContext(ctx, "sh", "-s")
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

// Collect collects every host concurrently. A host that fails is reported in
// its HostResult and does not hold up or hide the others.
func (s *Service) Collect(ctx context.Context) Snapshot {
	names := s.hostNames()
	s.forgetHostsExcept(names)

	results := make([]HostResult, len(names)+1)
	taskLists := make([][]Task, len(names)+1)
	var wg sync.WaitGroup
	collect := func(i int, name string) {
		defer wg.Done()
		results[i], taskLists[i] = s.collectHost(ctx, name)
	}
	wg.Add(len(names) + 1)
	go collect(0, "")
	for i, name := range names {
		go collect(i+1, name)
	}
	wg.Wait()

	snapshot := Snapshot{Hosts: results, Tasks: []Task{}}
	for _, list := range taskLists {
		snapshot.Tasks = append(snapshot.Tasks, list...)
	}
	return snapshot
}

func (s *Service) hostNames() []string {
	var names []string
	if s.opts.Hosts != nil {
		for _, name := range s.opts.Hosts() {
			if name != "" {
				names = append(names, name)
			}
		}
	}
	sort.Strings(names)
	return names
}

func (s *Service) collectHost(ctx context.Context, name string) (HostResult, []Task) {
	ctx, cancel := context.WithTimeout(ctx, s.opts.HostTimeout)
	defer cancel()

	result := HostResult{Name: name}
	out, err := s.runScript(ctx, name)
	if err != nil {
		result.Status = HostError
		if errors.Is(err, errConnecting) {
			result.Status = HostConnecting
		} else {
			result.Error = err.Error()
		}
		return result, nil
	}

	collectedAt := s.opts.Now()
	raw, err := parseCollectOutput(out)
	if err != nil {
		result.Status = HostError
		result.Error = err.Error()
		return result, nil
	}
	result.Status = HostOK
	result.CollectedAt = &collectedAt
	return result, buildTasks(name, raw, collectedAt)
}

func (s *Service) runScript(ctx context.Context, name string) ([]byte, error) {
	if name == "" {
		return s.opts.RunLocal(ctx, collectScript)
	}
	conn, err := s.conn(ctx, name)
	if err != nil {
		return nil, err
	}
	out, err := conn.Run(ctx, "sh -s", strings.NewReader(collectScript))
	switch {
	case err == nil:
		return out, nil
	case isExitError(err):
		return nil, fmt.Errorf("run task collection on %s: %w", name, err)
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
		return nil, fmt.Errorf("task collection on %s did not finish within %s", name, s.opts.HostTimeout)
	default:
		s.drop(name, conn)
		return nil, fmt.Errorf("run task collection on %s: %w", name, err)
	}
}

// InspectGitContext resolves Git metadata for cwd on a remote host over the
// host's collection connection. It never dials: a host with no connection
// open reports an error, and the next collection sets one up.
func (s *Service) InspectGitContext(ctx context.Context, host, cwd string) (session.GitContext, error) {
	conn := s.openConn(host)
	if conn == nil {
		return session.GitContext{}, fmt.Errorf("no connection to %s", host)
	}
	gitCtx, err := conn.InspectGitContext(ctx, cwd)
	if err != nil {
		return session.GitContext{}, fmt.Errorf("inspect git context on %s: %w", host, err)
	}
	return gitCtx, nil
}

// openConn is the host's open connection, or nil. It never dials.
func (s *Service) openConn(host string) Conn {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h := s.hosts[host]; h != nil {
		return h.conn
	}
	return nil
}

// Reconnect discards a host's connection and any remembered failure, so the
// next collection dials it straight away.
func (s *Service) Reconnect(name string) error {
	known := false
	for _, candidate := range s.hostNames() {
		if candidate == name {
			known = true
			break
		}
	}
	if !known {
		return fmt.Errorf("%w: %q", ErrUnknownHost, name)
	}

	s.mu.Lock()
	h := s.hosts[name]
	var stale Conn
	if h != nil {
		stale = h.conn
		h.conn = nil
		h.err = nil
	}
	s.mu.Unlock()
	if stale != nil {
		_ = stale.Close()
	}
	return nil
}

// Close closes every connection. A dial still in flight closes its own
// connection when it finishes.
func (s *Service) Close() {
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

// conn returns the host's open connection, dialing one if there is none.
// It waits for the dial only as long as ctx allows.
func (s *Service) conn(ctx context.Context, name string) (Conn, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, errors.New("task collection is closed")
	}
	h := s.hosts[name]
	if h == nil {
		h = &hostConn{}
		s.hosts[name] = h
	}
	if h.conn != nil {
		conn := h.conn
		s.mu.Unlock()
		return conn, nil
	}
	if h.dialing == nil {
		if h.err != nil && s.opts.Now().Sub(h.failedAt) < s.opts.RetryAfter {
			err := h.err
			s.mu.Unlock()
			return nil, err
		}
		h.dialing = make(chan struct{})
		go s.dial(name, h, h.dialing)
	}
	done := h.dialing
	s.mu.Unlock()

	select {
	case <-done:
	case <-ctx.Done():
		return nil, errConnecting
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
	return nil, errConnecting
}

func (s *Service) dial(name string, h *hostConn, done chan struct{}) {
	conn, err := s.opts.Dial(name)

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
func (s *Service) drop(name string, conn Conn) {
	s.mu.Lock()
	if h := s.hosts[name]; h != nil && h.conn == conn {
		h.conn = nil
	}
	s.mu.Unlock()
	_ = conn.Close()
}

// forgetHostsExcept closes connections to hosts no longer configured.
func (s *Service) forgetHostsExcept(names []string) {
	keep := make(map[string]bool, len(names))
	for _, name := range names {
		keep[name] = true
	}
	s.mu.Lock()
	var stale []Conn
	for name, h := range s.hosts {
		if keep[name] {
			continue
		}
		if h.conn != nil {
			stale = append(stale, h.conn)
		}
		delete(s.hosts, name)
	}
	s.mu.Unlock()
	for _, conn := range stale {
		_ = conn.Close()
	}
}

func isExitError(err error) bool {
	var exit interface{ ExitStatus() int }
	return errors.As(err, &exit)
}

var _ Conn = (*session.CommandConn)(nil)
