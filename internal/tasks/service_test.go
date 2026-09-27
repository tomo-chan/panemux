package tasks

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/homedir"
	"panemux/internal/session"
)

// minimalOutput is a complete collection with one busy claude session.
func minimalOutput(sessionID string) []byte {
	return joinLines(
		"::panemux-tasks v1",
		"::now 1000",
		"::section state",
		"::file 7.json",
		`{"pid":7,"sessionId":"`+sessionID+`","status":"busy","statusUpdatedAt":999000}`,
		"::section ps",
		"7 1 claude",
		"::section tmux",
		"::section cwd",
		"::section transcripts",
		"::end",
	)
}

type exitStatusError struct{ status int }

func (e exitStatusError) Error() string   { return "exit " + strconv.Itoa(e.status) }
func (e exitStatusError) ExitStatus() int { return e.status }

type fakeConn struct {
	runErr  error
	gitErr  error
	pingErr error
	output  []byte
	cmds    []string
	stdins  []string
	gitCWDs []string
	closed  int
	pings   int
	mu      sync.Mutex
	// hang makes Run block until its context ends, like a script that
	// outlasts the collection's time budget on a healthy connection.
	hang bool
}

func (c *fakeConn) Run(ctx context.Context, cmd string, stdin io.Reader) ([]byte, error) {
	if c.hang {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cmds = append(c.cmds, cmd)
	if stdin != nil {
		data, _ := io.ReadAll(stdin)
		c.stdins = append(c.stdins, string(data))
	}
	return c.output, c.runErr
}

func (c *fakeConn) InspectGitContext(_ context.Context, cwd string) (session.GitContext, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gitCWDs = append(c.gitCWDs, cwd)
	return session.GitContext{Branch: "main", Root: cwd}, c.gitErr
}

func (c *fakeConn) Ping(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pings++
	return c.pingErr
}

func (c *fakeConn) pingCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.pings
}

func (c *fakeConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed++
	return nil
}

func (c *fakeConn) closeCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

type fakeDialer struct {
	gate  chan struct{}
	conns []*fakeConn
	errs  []error
	calls int
	mu    sync.Mutex
}

func (d *fakeDialer) dial(string) (Conn, error) {
	if d.gate != nil {
		<-d.gate
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	i := d.calls
	d.calls++
	if i < len(d.errs) && d.errs[i] != nil {
		return nil, d.errs[i]
	}
	return d.conns[i], nil
}

func (d *fakeDialer) callCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.calls
}

type clock struct {
	now time.Time
	mu  sync.Mutex
}

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func localOutput(out []byte, err error) func(context.Context, string) ([]byte, error) {
	return func(context.Context, string) ([]byte, error) { return out, err }
}

func hostResult(t *testing.T, snap Snapshot, name string) HostResult {
	t.Helper()
	for _, h := range snap.Hosts {
		if h.Name == name {
			return h
		}
	}
	require.Failf(t, "host not found", "%q", name)
	return HostResult{}
}

func TestCollect_LocalHostOnly(t *testing.T) {
	clk := &clock{now: collectedAt}
	svc := New(Options{RunLocal: localOutput(minimalOutput("local-sess"), nil), Now: clk.Now})
	defer svc.Close()

	snap := svc.Collect(context.Background())
	require.Len(t, snap.Hosts, 1)
	assert.Equal(t, HostResult{Name: "", Status: HostOK, CollectedAt: &collectedAt}, snap.Hosts[0])
	require.Len(t, snap.Tasks, 1)
	assert.Equal(t, "local:claude:local-sess", snap.Tasks[0].ID)
	assert.Equal(t, collectedAt.Add(-time.Second), *snap.Tasks[0].StatusSince)
}

func TestCollect_HostsAreSortedAfterLocalAndFailuresStayPerHost(t *testing.T) {
	good := &fakeConn{output: minimalOutput("remote-sess")}
	dialer := &fakeDialer{conns: []*fakeConn{good}}
	svc := New(Options{
		Hosts:    func() []string { return []string{"zeta", "", "alpha"} },
		Dial:     func(name string) (Conn, error) { return dialByName(name, dialer) },
		RunLocal: localOutput(nil, errors.New("sh: not found")),
		Now:      (&clock{now: collectedAt}).Now,
	})
	defer svc.Close()

	snap := svc.Collect(context.Background())
	names := []string{}
	for _, h := range snap.Hosts {
		names = append(names, h.Name)
	}
	assert.Equal(t, []string{"", "alpha", "zeta"}, names, "the empty name from config is not a second local host")

	local := hostResult(t, snap, "")
	assert.Equal(t, HostError, local.Status)
	assert.Contains(t, local.Error, "sh: not found")
	assert.Nil(t, local.CollectedAt)

	assert.Equal(t, HostOK, hostResult(t, snap, "alpha").Status)
	zeta := hostResult(t, snap, "zeta")
	assert.Equal(t, HostError, zeta.Status)
	assert.Contains(t, zeta.Error, "no route")

	require.Len(t, snap.Tasks, 1)
	assert.Equal(t, "ssh:alpha:claude:remote-sess", snap.Tasks[0].ID)
	assert.Equal(t, "alpha", snap.Tasks[0].Host)
}

func dialByName(name string, dialer *fakeDialer) (Conn, error) {
	if name == "zeta" {
		return nil, errors.New("no route to host")
	}
	return dialer.dial(name)
}

func TestCollect_ReusesOneConnectionAndSendsTheScriptOnStdin(t *testing.T) {
	conn := &fakeConn{output: minimalOutput("s")}
	dialer := &fakeDialer{conns: []*fakeConn{conn}}
	svc := New(Options{
		Hosts:    func() []string { return []string{"build-box"} },
		Dial:     dialer.dial,
		RunLocal: localOutput(minimalOutput("l"), nil),
	})
	defer svc.Close()

	svc.Collect(context.Background())
	svc.Collect(context.Background())

	assert.Equal(t, 1, dialer.callCount())
	assert.Equal(t, []string{"sh -s", "sh -s"}, conn.cmds)
	assert.Equal(t, []string{collectScript, collectScript}, conn.stdins)
	assert.Zero(t, conn.closeCount())
}

func TestCollect_ATransportFailureDropsTheConnectionAndRedialsAtOnce(t *testing.T) {
	broken := &fakeConn{runErr: errors.New("EOF")}
	fresh := &fakeConn{output: minimalOutput("s")}
	dialer := &fakeDialer{conns: []*fakeConn{broken, fresh}}
	svc := New(Options{
		Hosts:    func() []string { return []string{"build-box"} },
		Dial:     dialer.dial,
		RunLocal: localOutput(minimalOutput("l"), nil),
	})
	defer svc.Close()

	first := svc.Collect(context.Background())
	assert.Equal(t, HostError, hostResult(t, first, "build-box").Status)
	assert.Contains(t, hostResult(t, first, "build-box").Error, "EOF")
	assert.Equal(t, 1, broken.closeCount())

	second := svc.Collect(context.Background())
	assert.Equal(t, HostOK, hostResult(t, second, "build-box").Status)
	assert.Equal(t, 2, dialer.callCount())
}

// A script that outlasts the time budget on a connection that still answers
// keeps the connection: dropping it would redial every collection and never
// produce a result.
func TestCollect_ATimedOutScriptOnAHealthyConnectionKeepsIt(t *testing.T) {
	conn := &fakeConn{hang: true}
	dialer := &fakeDialer{conns: []*fakeConn{conn}}
	svc := New(Options{
		Hosts:       func() []string { return []string{"slow"} },
		Dial:        dialer.dial,
		RunLocal:    localOutput(minimalOutput("l"), nil),
		HostTimeout: 30 * time.Millisecond,
	})
	defer svc.Close()

	first := hostResult(t, svc.Collect(context.Background()), "slow")
	assert.Equal(t, HostError, first.Status)
	assert.Equal(t, "task collection on slow did not finish within 30ms", first.Error)
	svc.Collect(context.Background())

	assert.Equal(t, 1, dialer.callCount())
	assert.Zero(t, conn.closeCount())
	assert.Equal(t, 2, conn.pingCount())
}

func TestCollect_ATimedOutScriptOnADeadConnectionDropsIt(t *testing.T) {
	dead := &fakeConn{hang: true, pingErr: errors.New("EOF")}
	fresh := &fakeConn{output: minimalOutput("s")}
	dialer := &fakeDialer{conns: []*fakeConn{dead, fresh}}
	svc := New(Options{
		Hosts:       func() []string { return []string{"slow"} },
		Dial:        dialer.dial,
		RunLocal:    localOutput(minimalOutput("l"), nil),
		HostTimeout: 30 * time.Millisecond,
	})
	defer svc.Close()

	first := hostResult(t, svc.Collect(context.Background()), "slow")
	assert.Equal(t, HostError, first.Status)
	assert.Equal(t, 1, dead.closeCount())
	assert.Equal(t, HostOK, hostResult(t, svc.Collect(context.Background()), "slow").Status)
	assert.Equal(t, 2, dialer.callCount())
}

func TestCollect_ACommandFailureKeepsTheConnection(t *testing.T) {
	conn := &fakeConn{runErr: exitStatusError{status: 127}}
	dialer := &fakeDialer{conns: []*fakeConn{conn}}
	svc := New(Options{
		Hosts:    func() []string { return []string{"build-box"} },
		Dial:     dialer.dial,
		RunLocal: localOutput(minimalOutput("l"), nil),
	})
	defer svc.Close()

	snap := svc.Collect(context.Background())
	assert.Equal(t, HostError, hostResult(t, snap, "build-box").Status)
	svc.Collect(context.Background())
	assert.Equal(t, 1, dialer.callCount())
	assert.Zero(t, conn.closeCount())
}

func TestCollect_UnparseableOutputIsAHostError(t *testing.T) {
	conn := &fakeConn{output: []byte("::panemux-tasks v1\n::now 1\n")}
	svc := New(Options{
		Hosts:    func() []string { return []string{"build-box"} },
		Dial:     (&fakeDialer{conns: []*fakeConn{conn}}).dial,
		RunLocal: localOutput(minimalOutput("l"), nil),
	})
	defer svc.Close()

	snap := svc.Collect(context.Background())
	host := hostResult(t, snap, "build-box")
	assert.Equal(t, HostError, host.Status)
	assert.Contains(t, host.Error, "ended early")
}

func TestCollect_AFailedDialWaitsBeforeRetryingUnlessReconnected(t *testing.T) {
	conn := &fakeConn{output: minimalOutput("s")}
	dialer := &fakeDialer{
		conns: []*fakeConn{nil, nil, conn},
		errs:  []error{errors.New("i/o timeout"), errors.New("still down"), nil},
	}
	clk := &clock{now: collectedAt}
	svc := New(Options{
		Hosts:      func() []string { return []string{"gpu-box"} },
		Dial:       dialer.dial,
		RunLocal:   localOutput(minimalOutput("l"), nil),
		Now:        clk.Now,
		RetryAfter: 10 * time.Second,
	})
	defer svc.Close()

	first := hostResult(t, svc.Collect(context.Background()), "gpu-box")
	assert.Equal(t, HostError, first.Status)
	assert.Equal(t, "connect to gpu-box: i/o timeout", first.Error)

	clk.advance(9 * time.Second)
	second := hostResult(t, svc.Collect(context.Background()), "gpu-box")
	assert.Equal(t, first, second, "the remembered failure is reported without dialing")
	assert.Equal(t, 1, dialer.callCount())

	clk.advance(time.Second) // exactly RetryAfter since the failure
	third := hostResult(t, svc.Collect(context.Background()), "gpu-box")
	assert.Equal(t, "connect to gpu-box: still down", third.Error)
	assert.Equal(t, 2, dialer.callCount())

	require.NoError(t, svc.Reconnect("gpu-box"))
	fourth := hostResult(t, svc.Collect(context.Background()), "gpu-box")
	assert.Equal(t, HostOK, fourth.Status)
	assert.Equal(t, 3, dialer.callCount())
}

func TestCollect_RetryAfterDefaultsToAMinute(t *testing.T) {
	dialer := &fakeDialer{errs: []error{errors.New("down"), errors.New("down")}, conns: []*fakeConn{nil, nil}}
	clk := &clock{now: collectedAt}
	svc := New(Options{
		Hosts:    func() []string { return []string{"gpu-box"} },
		Dial:     dialer.dial,
		RunLocal: localOutput(minimalOutput("l"), nil),
		Now:      clk.Now,
	})
	defer svc.Close()

	svc.Collect(context.Background())
	clk.advance(59 * time.Second)
	svc.Collect(context.Background())
	assert.Equal(t, 1, dialer.callCount())
	clk.advance(time.Second)
	svc.Collect(context.Background())
	assert.Equal(t, 2, dialer.callCount())
}

func TestCollect_ASlowDialReportsConnectingAndServesTheNextCollection(t *testing.T) {
	conn := &fakeConn{output: minimalOutput("s")}
	dialer := &fakeDialer{conns: []*fakeConn{conn}, gate: make(chan struct{})}
	svc := New(Options{
		Hosts:       func() []string { return []string{"slow-box"} },
		Dial:        dialer.dial,
		RunLocal:    localOutput(minimalOutput("l"), nil),
		HostTimeout: 20 * time.Millisecond,
	})
	defer svc.Close()

	first := hostResult(t, svc.Collect(context.Background()), "slow-box")
	assert.Equal(t, HostResult{Name: "slow-box", Status: HostConnecting}, first)

	close(dialer.gate)
	require.Eventually(t, func() bool {
		return hostResult(t, svc.Collect(context.Background()), "slow-box").Status == HostOK
	}, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, 1, dialer.callCount(), "the dial that was in flight is the one reused")
}

func TestCollect_AHostRemovedFromTheConfigIsDisconnected(t *testing.T) {
	conn := &fakeConn{output: minimalOutput("s")}
	hosts := []string{"build-box"}
	var mu sync.Mutex
	svc := New(Options{
		Hosts: func() []string {
			mu.Lock()
			defer mu.Unlock()
			return hosts
		},
		Dial:     (&fakeDialer{conns: []*fakeConn{conn}}).dial,
		RunLocal: localOutput(minimalOutput("l"), nil),
	})
	defer svc.Close()

	svc.Collect(context.Background())
	mu.Lock()
	hosts = nil
	mu.Unlock()
	snap := svc.Collect(context.Background())

	assert.Len(t, snap.Hosts, 1)
	assert.Equal(t, 1, conn.closeCount())
}

func TestReconnect(t *testing.T) {
	conn := &fakeConn{output: minimalOutput("s")}
	svc := New(Options{
		Hosts:    func() []string { return []string{"build-box"} },
		Dial:     (&fakeDialer{conns: []*fakeConn{conn, conn}}).dial,
		RunLocal: localOutput(minimalOutput("l"), nil),
	})
	defer svc.Close()

	err := svc.Reconnect("nope")
	require.ErrorIs(t, err, ErrUnknownHost)
	require.NoError(t, svc.Reconnect("build-box"), "a host never connected can still be reconnected")

	svc.Collect(context.Background())
	require.NoError(t, svc.Reconnect("build-box"))
	assert.Equal(t, 1, conn.closeCount())
}

func TestClose_ClosesOpenAndInFlightConnections(t *testing.T) {
	open := &fakeConn{output: minimalOutput("s")}
	late := &fakeConn{output: minimalOutput("t")}
	lateDialer := &fakeDialer{conns: []*fakeConn{late}, gate: make(chan struct{})}
	svc := New(Options{
		Hosts: func() []string { return []string{"fast", "late"} },
		Dial: func(name string) (Conn, error) {
			if name == "fast" {
				return open, nil
			}
			return lateDialer.dial(name)
		},
		RunLocal:    localOutput(minimalOutput("l"), nil),
		HostTimeout: 20 * time.Millisecond,
	})

	snap := svc.Collect(context.Background())
	assert.Equal(t, HostConnecting, hostResult(t, snap, "late").Status)

	svc.Close()
	assert.Equal(t, 1, open.closeCount())

	close(lateDialer.gate)
	require.Eventually(t, func() bool { return late.closeCount() == 1 }, 2*time.Second, 5*time.Millisecond)

	after := svc.Collect(context.Background())
	assert.Equal(t, HostError, hostResult(t, after, "fast").Status)
	assert.Contains(t, hostResult(t, after, "fast").Error, "closed")
}

// A collection waiting on a dial when the service closes gives up on that
// host rather than handing back a connection nobody will close.
func TestCollect_ADialThatFinishesAfterCloseReportsConnecting(t *testing.T) {
	late := &fakeConn{output: minimalOutput("s")}
	dialer := &fakeDialer{conns: []*fakeConn{late}, gate: make(chan struct{})}
	svc := New(Options{
		Hosts:       func() []string { return []string{"late"} },
		Dial:        dialer.dial,
		RunLocal:    localOutput(minimalOutput("l"), nil),
		HostTimeout: 5 * time.Second,
	})

	done := make(chan Snapshot)
	go func() { done <- svc.Collect(context.Background()) }()
	require.Eventually(t, func() bool {
		svc.mu.Lock()
		defer svc.mu.Unlock()
		h := svc.hosts["late"]
		return h != nil && h.dialing != nil
	}, 2*time.Second, 5*time.Millisecond)

	svc.Close()
	close(dialer.gate)

	snap := <-done
	assert.Equal(t, HostConnecting, hostResult(t, snap, "late").Status)
	require.Eventually(t, func() bool { return late.closeCount() == 1 }, 2*time.Second, 5*time.Millisecond)
}

func TestInspectGitContext_UsesTheOpenConnectionOnly(t *testing.T) {
	conn := &fakeConn{output: minimalOutput("s")}
	svc := New(Options{
		Hosts:    func() []string { return []string{"build-box"} },
		Dial:     (&fakeDialer{conns: []*fakeConn{conn}}).dial,
		RunLocal: localOutput(minimalOutput("l"), nil),
	})
	defer svc.Close()

	_, err := svc.InspectGitContext(context.Background(), "build-box", "/remote/home/demo/app")
	require.Error(t, err, "no connection yet, and InspectGitContext does not dial")

	svc.Collect(context.Background())
	gitCtx, err := svc.InspectGitContext(context.Background(), "build-box", "/remote/home/demo/app")
	require.NoError(t, err)
	assert.Equal(t, "main", gitCtx.Branch)
	assert.Equal(t, []string{"/remote/home/demo/app"}, conn.gitCWDs)

	conn.gitErr = errors.New("not a git repository")
	_, err = svc.InspectGitContext(context.Background(), "build-box", "/remote/home/demo/app")
	assert.ErrorContains(t, err, "not a git repository")
}

// The script itself, run by the real local runner against a home directory
// the test controls. `ps` and `tmux` report whatever this machine has, so the
// assertions stay on what the test put there.
func TestRunLocal_CollectScriptRunsUnderShAndParses(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)

	sessions := filepath.Join(home, ".claude", "sessions")
	require.NoError(t, os.MkdirAll(sessions, 0o700))
	selfPID := os.Getpid()
	require.NoError(t, os.WriteFile(filepath.Join(sessions, strconv.Itoa(selfPID)+".json"),
		[]byte(`{"pid":`+strconv.Itoa(selfPID)+`,"sessionId":"abc","status":"idle"}`), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(sessions, "ignored.key"), []byte("x"), 0o600))

	project := filepath.Join(home, ".claude", "projects", "-workspace-user-project")
	require.NoError(t, os.MkdirAll(filepath.Join(project, "stopped-one", "subagents"), 0o700))
	require.NoError(t, os.WriteFile(filepath.Join(project, "stopped-one.jsonl"),
		[]byte(`{"type":"user","cwd":"/workspace/user/project","message":"x"}`+"\n"+`{"cwd":"/second"}`+"\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(project, "no-cwd.jsonl"), []byte("{}\n"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(project, "stopped-one", "subagents", "agent-1.jsonl"),
		[]byte(`{"cwd":"/sub"}`+"\n"), 0o600))
	old := filepath.Join(project, "old.jsonl")
	require.NoError(t, os.WriteFile(old, []byte(`{"cwd":"/old"}`+"\n"), 0o600))
	longAgo := time.Now().Add(-10 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(old, longAgo, longAgo))

	out, err := runLocal(context.Background(), collectScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out)
	require.NoError(t, err)

	assert.InDelta(t, time.Now().Unix(), raw.Now, 5)
	require.Len(t, raw.StateFiles, 1)
	assert.Equal(t, strconv.Itoa(selfPID)+".json", raw.StateFiles[0].Name)

	var sawSelf bool
	for _, p := range raw.Processes {
		sawSelf = sawSelf || p.PID == selfPID
	}
	assert.True(t, sawSelf, "ps lists this test process")

	byID := map[string]transcript{}
	for _, tr := range raw.Transcripts {
		byID[tr.SessionID] = tr
	}
	assert.Len(t, byID, 2, "subagent logs and logs older than 7 days are not listed: %+v", raw.Transcripts)
	assert.Equal(t, "/workspace/user/project", byID["stopped-one"].CWD, "the first cwd in the log")
	assert.Empty(t, byID["no-cwd"].CWD)
	assert.InDelta(t, time.Now().Unix(), byID["stopped-one"].ModTime, 60)
}

// The cwd probe reports a claude process's working directory, and ps lists
// this user's own processes. `claude` here is a symlink to sleep.
func TestRunLocal_ReportsTheWorkingDirectoryOfAClaudeProcess(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	sleepPath, err := exec.LookPath("sleep")
	require.NoError(t, err)
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "claude")
	require.NoError(t, os.Symlink(sleepPath, fake))
	workDir := t.TempDir()

	cmd := exec.Command(fake, "30")
	cmd.Dir = workDir
	require.NoError(t, cmd.Start())
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})

	out, err := runLocal(context.Background(), collectScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out)
	require.NoError(t, err)

	var listed bool
	for _, p := range raw.Processes {
		listed = listed || p.PID == cmd.Process.Pid
	}
	assert.True(t, listed, "ps lists the fake claude process")
	if runtime.GOOS == "linux" {
		resolved, err := filepath.EvalSymlinks(workDir)
		require.NoError(t, err)
		assert.Equal(t, resolved, raw.ProcessCWDs[cmd.Process.Pid])
	}
}

// The env probe reports the PANEMUX_PANE_ID a claude or codex process was
// started with, read from its environment and not its command line. Linux
// reads /proc/<pid>/environ. macOS reads `ps -E` instead, which is covered by
// TestRunLocal_ReadsThePaneIDFromPsEOnMacOS: these fake agents are the OS's
// own sleep, and what macOS shows of such a binary's environment when it is
// started through a symlink was not checked (issue #263).
//
//efficacy:exempt only the non-Linux skip changed; the Linux reading it pins predates this branch
func TestRunLocal_ReportsThePaneIDInAnAgentsEnvironment(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc/<pid>/environ, which only Linux has")
	}
	homedir.SetForTest(t, t.TempDir())
	sleepPath, err := exec.LookPath("sleep")
	require.NoError(t, err)
	binDir := t.TempDir()
	for _, name := range []string{"claude", "codex"} {
		require.NoError(t, os.Symlink(sleepPath, filepath.Join(binDir, name)))
	}

	start := func(name string, env []string, args ...string) int {
		cmd := exec.Command(filepath.Join(binDir, name), args...) //nolint:gosec // G204: fixture binary under test
		cmd.Env = env
		require.NoError(t, cmd.Start())
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		})
		return cmd.Process.Pid
	}
	withPane := start("claude", []string{"PATH=/usr/bin:/bin", "PANEMUX_PANE_ID=pane-1790346631000-a1b2c"}, "30")
	codex := start("codex", []string{"PANEMUX_PANE_ID=pane-codex"}, "30")
	unsafe := start("claude", []string{"PANEMUX_PANE_ID=my pane"}, "30")
	onlyInArgs := start("claude", []string{"PATH=/usr/bin:/bin"}, "30", "PANEMUX_PANE_ID=pane-argv")
	// Another variable's value can hold a line that looks like the variable
	// itself; only the entry named PANEMUX_PANE_ID counts, wherever it is.
	inAValue := start("claude",
		[]string{"NOTES=line1\nPANEMUX_PANE_ID=pane-other", "PANEMUX_PANE_ID=pane-real"}, "30")
	onlyInAValue := start("claude", []string{"NOTES=line1\nPANEMUX_PANE_ID=pane-other"}, "30")
	withoutPane := start("claude", []string{"PATH=/usr/bin:/bin"}, "30")

	out, err := runLocal(context.Background(), collectScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out)
	require.NoError(t, err)

	assert.Equal(t, "pane-1790346631000-a1b2c", raw.PaneIDs[withPane])
	assert.Equal(t, "pane-codex", raw.PaneIDs[codex])
	assert.Equal(t, "pane-real", raw.PaneIDs[inAValue], "a line inside another variable's value is not the variable")
	for name, pid := range map[string]int{
		"unsafe": unsafe, "only in args": onlyInArgs, "without": withoutPane, "only in a value": onlyInAValue,
	} {
		_, ok := raw.PaneIDs[pid]
		assert.False(t, ok, "%s: %v", name, raw.PaneIDs)
	}
}

// fakeMacOSProcess is one process as the macOS `ps` in
// installFakeMacOSTools reports it: Args is `ps -p <pid> -o command=` and
// Full is `ps -E -p <pid> -o command=`, which on macOS is the arguments, a
// space, and the environment's NAME=value entries joined by spaces (issue
// #263, checked on macOS 26.3.1).
type fakeMacOSProcess struct {
	Args string
	Full string
	PID  int
}

// installFakeMacOSTools puts a `uname` that says Darwin and a `ps` that
// answers the four forms collectScript uses on macOS first on PATH, so the
// script's macOS branch runs on any host. Paths in the fixtures are
// placeholders.
func installFakeMacOSTools(t *testing.T, procs []fakeMacOSProcess) {
	t.Helper()
	bin := t.TempDir()
	data := t.TempDir()
	var list strings.Builder
	for _, p := range procs {
		pid := strconv.Itoa(p.PID)
		listed := p.Args
		if listed == "" {
			listed = "claude" // gone by the time its arguments are read
		}
		list.WriteString(pid + " 1 " + listed + "\n")
		require.NoError(t, os.WriteFile(filepath.Join(data, pid+".args"), []byte(p.Args+"\n"), 0o600))
		require.NoError(t, os.WriteFile(filepath.Join(data, pid+".full"), []byte(p.Full+"\n"), 0o600))
	}
	require.NoError(t, os.WriteFile(filepath.Join(data, "list"), []byte(list.String()), 0o600))

	fakePS := `#!/bin/sh
d='` + data + `'
case "$*" in
"-U "*" -o pid=,ppid=,command=") cat "$d/list" ;;
"-U "*" -o pid=,command=") awk '{ $2 = ""; print }' "$d/list" ;;
"-p "*" -o command=") cat "$d/$2.args" 2>/dev/null ;;
"-E -p "*" -o command=") cat "$d/$3.full" 2>/dev/null ;;
*) exit 1 ;;
esac
`
	writeExecutable(t, filepath.Join(bin, "ps"), fakePS)
	writeExecutable(t, filepath.Join(bin, "uname"), "#!/bin/sh\necho Darwin\n")
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func writeExecutable(t *testing.T, path, script string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(script), 0o700)) //nolint:gosec // G306: it has to be executable
}

// On macOS the env probe reads `ps -E` (issue #263). Its output does not
// delimit anything, so the probe removes the arguments `ps -o command=`
// reports from the front and takes the value only when exactly one
// " PANEMUX_PANE_ID=" is left. The fixtures follow the output recorded on
// macOS 26.3.1 in issue #263: claude, codex's node wrapper and native binary
// show their environment; an Apple-signed binary shows none; a value can
// hide in an argument or in another variable's value.
func TestRunLocal_ReadsThePaneIDFromPsEOnMacOS(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	const env = "HOME=/Users/demo SHELL=/bin/zsh"
	codexWrapper := "node /Users/demo/.nvm/versions/node/v24.13.0/bin/codex -m gpt-5.5"
	codexNative := "/Users/demo/.nvm/versions/node/v24.13.0/lib/node_modules/@openai/codex/bin/codex -m gpt-5.5"
	installFakeMacOSTools(t, []fakeMacOSProcess{
		{PID: 101, Args: "claude", Full: "claude TERM=xterm-256color PANEMUX_PANE_ID=test-1 " + env},
		{PID: 102, Args: codexWrapper, Full: codexWrapper + " PANEMUX_PANE_ID=pane-1790346631000-a1b2c " + env},
		{PID: 103, Args: codexNative, Full: codexNative + " " + env + " PANEMUX_PANE_ID=pane-codex"},
		// An argument is not the environment.
		{PID: 104, Args: "node /Users/demo/claude/cli.js PANEMUX_PANE_ID=spoofed",
			Full: "node /Users/demo/claude/cli.js PANEMUX_PANE_ID=spoofed " + env},
		{PID: 105, Args: "node /Users/demo/claude/cli.js PANEMUX_PANE_ID=spoofed",
			Full: "node /Users/demo/claude/cli.js PANEMUX_PANE_ID=spoofed " + env + " PANEMUX_PANE_ID=pane-real"},
		// Another variable's value can hold the same text; two are ambiguous.
		{PID: 106, Args: "/tmp/scratch/claude 304",
			Full: "/tmp/scratch/claude 304 HOME=/tmp AAA=x PANEMUX_PANE_ID=evil PANEMUX_PANE_ID=test-1"},
		// An Apple-signed binary: the kernel returns no environment.
		{PID: 107, Args: "/bin/zsh -c claude", Full: "/bin/zsh -c claude"},
		// The arguments are not the front of the output (the process changed
		// between the two calls, or the output is not what was checked).
		{PID: 108, Args: "claude", Full: "codex PANEMUX_PANE_ID=pane-x " + env},
		{PID: 109, Args: "claude", Full: "claude-x PANEMUX_PANE_ID=pane-x " + env},
		// No arguments (the process is gone): the whole output would count
		// as its environment.
		{PID: 110, Args: "", Full: " PANEMUX_PANE_ID=pane-x " + env},
		// Values a pane is never given.
		{PID: 111, Args: "claude", Full: "claude PANEMUX_PANE_ID=pane/x " + env},
		{PID: 112, Args: "claude", Full: "claude PANEMUX_PANE_ID= " + env},
		{PID: 113, Args: "claude", Full: "claude PANEMUX_PANE_ID=" + strings.Repeat("a", 129) + " " + env},
		// A newline in the value must not start a row of its own.
		{PID: 114, Args: "claude", Full: "claude PANEMUX_PANE_ID=pane-a\n999 pane-evil " + env},
		{PID: 115, Args: "claude", Full: "claude " + env},
	})

	out, err := runLocal(context.Background(), collectScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out)
	require.NoError(t, err)

	assert.Equal(t, map[int]string{
		101: "test-1",
		102: "pane-1790346631000-a1b2c",
		103: "pane-codex",
		105: "pane-real",
	}, raw.PaneIDs)
}

// The `ps -E` reading is for macOS only: on another host whose /proc has no
// environ for the process, nothing is reported even when `ps -E` would
// print one.
//
//efficacy:exempt a guard: before this branch no host read ps -E, so it also held then
func TestRunLocal_ReadsPsEOnlyOnMacOS(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	installFakeMacOSTools(t, []fakeMacOSProcess{
		{PID: 999999999, Args: "claude", Full: "claude PANEMUX_PANE_ID=test-1"},
	})
	writeExecutable(t, filepath.Join(filepath.SplitList(os.Getenv("PATH"))[0], "uname"), "#!/bin/sh\necho Linux\n")

	out, err := runLocal(context.Background(), collectScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out)
	require.NoError(t, err)

	assert.Empty(t, raw.PaneIDs)
}

func TestRunLocal_EmptyHomeStillCompletes(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	out, err := runLocal(context.Background(), collectScript)
	require.NoError(t, err)
	raw, err := parseCollectOutput(out)
	require.NoError(t, err)
	assert.Empty(t, raw.StateFiles)
	assert.Empty(t, raw.Transcripts)
}

func TestRunLocal_Errors(t *testing.T) {
	t.Run("home directory", func(t *testing.T) {
		homedir.SetFailingForTest(t, errors.New("no home"))
		_, err := runLocal(context.Background(), collectScript)
		assert.ErrorContains(t, err, "no home")
	})
	t.Run("script failure", func(t *testing.T) {
		homedir.SetForTest(t, t.TempDir())
		_, err := runLocal(context.Background(), "exit 3")
		assert.ErrorContains(t, err, "run task collection")
	})
}

// A probe the script starts in the background holds the script's stdout
// open. When the collection's context ends, runLocal returns at once rather
// than waiting for that probe to close stdout, and the probe is killed with
// the script instead of being left running.
func TestRunLocal_ReturnsWhenTheContextEndsWhileAChildHoldsStdout(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	pidFile := filepath.Join(t.TempDir(), "child.pid")

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err := runLocal(ctx, "sleep 30 &\necho $! > '"+pidFile+"'\nwait\n")
	elapsed := time.Since(started)

	require.ErrorContains(t, err, "run task collection")
	assert.Less(t, elapsed, 5*time.Second, "returned without waiting for the child's stdout")

	raw, err := os.ReadFile(pidFile)
	require.NoError(t, err)
	childPID, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	require.NoError(t, err)
	assert.Eventually(t, func() bool { return !processRunning(childPID) }, 5*time.Second, 20*time.Millisecond,
		"the background child is killed with the script")
}

// processRunning reports whether pid is a process that has not exited. An
// exited child nobody has reaped yet still answers signal 0, so on Linux its
// state in /proc is read as well.
func processRunning(pid int) bool {
	if err := syscall.Kill(pid, 0); err != nil {
		return false
	}
	if runtime.GOOS != "linux" {
		return true
	}
	stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
	if err != nil {
		return false
	}
	fields := strings.Fields(string(stat[strings.LastIndexByte(string(stat), ')')+1:]))
	return len(fields) > 0 && fields[0] != "Z"
}
