// Package testcap lets a test require an operating-system capability the
// Claude Code sandbox denies — a pseudo-terminal, a tmux server (it listens on
// a Unix socket) or the ps command — and skip where it is missing.
//
// A missing capability skips the test only outside CI. In CI (the CI
// environment variable is set, as GitHub Actions sets it) the same probe
// failing fails the test: CI is where these tests are verified, so a runner
// that cannot open a pty must not quietly report every pane test as skipped.
// See issue #315 and DEVELOPMENT.md's "Claude Code sandbox".
package testcap

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"

	"github.com/creack/pty"
)

// TB is the subset of testing.TB the helpers need, declared here so the
// package's own tests can observe what it does without failing themselves.
type TB interface {
	Helper()
	Skipf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// RequirePTY skips (outside CI) or fails (in CI) t when this process cannot
// open a pseudo-terminal.
func RequirePTY(t TB) {
	t.Helper()
	require(t, "a pseudo-terminal", ptyOnce())
}

// RequireTmux skips (outside CI) or fails (in CI) t when tmux cannot start a
// server: either tmux is not installed, or it cannot create its socket.
func RequireTmux(t TB) {
	t.Helper()
	require(t, "a tmux server", tmuxOnce())
}

// RequirePS skips (outside CI) or fails (in CI) t when ps cannot list this
// process.
func RequirePS(t TB) {
	t.Helper()
	require(t, "the ps command", psOnce())
}

// RequireDscl skips (outside CI) or fails (in CI) t when, on macOS, dscl cannot
// read the current user's login shell. It never skips on another OS, where
// the shell comes from /etc/passwd.
func RequireDscl(t TB) {
	t.Helper()
	require(t, "dscl", dsclOnce())
}

// inCI is a variable so the package's tests can take either branch.
var inCI = func() bool { return os.Getenv("CI") != "" }

func require(t TB, what string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if inCI() {
		t.Fatalf("%s is unavailable in CI, where this test must run: %v", what, err)
		return
	}
	t.Skipf("%s is unavailable here (the Claude Code sandbox denies it; CI runs this test): %v", what, err)
}

var (
	ptyOnce  = sync.OnceValue(probePTY)
	tmuxOnce = sync.OnceValue(probeTmux)
	psOnce   = sync.OnceValue(probePS)
	dsclOnce = sync.OnceValue(probeDscl)
)

func probePTY() error {
	ptmx, tty, err := pty.Open()
	if err != nil {
		return err
	}
	_ = tty.Close()
	return ptmx.Close()
}

func probeTmux() error {
	bin, err := exec.LookPath("tmux")
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "pmx-cap-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(dir) }()
	return probeTmuxAt(bin, filepath.Join(dir, "s"))
}

// probeTmuxAt starts a server on socket, confirms it answers, and stops it.
// The socket is always named explicitly, so an inherited TMUX can never route
// the probe to the caller's own server.
func probeTmuxAt(bin, socket string) error {
	defer func() { _ = exec.Command(bin, "-S", socket, "kill-server").Run() }()                                                    // #nosec G204 -- bin is from LookPath("tmux"), socket is under a fresh temp dir
	out, err := exec.Command(bin, "-S", socket, "-f", "/dev/null", "new-session", "-d", "-s", "probe", "sleep 5").CombinedOutput() // #nosec G204 -- as above
	if err != nil {
		return fmt.Errorf("tmux new-session: %w: %s", err, out)
	}
	if out, err := exec.Command(bin, "-S", socket, "has-session", "-t", "=probe").CombinedOutput(); err != nil { // #nosec G204 -- as above
		return fmt.Errorf("tmux has-session: %w: %s", err, out)
	}
	return nil
}

func probePS() error {
	out, err := exec.Command("ps", "-p", strconv.Itoa(os.Getpid()), "-o", "pid=").CombinedOutput()
	if err != nil {
		return fmt.Errorf("ps: %w: %s", err, out)
	}
	return nil
}

func probeDscl() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	u, err := user.Current()
	if err != nil {
		return err
	}
	out, err := exec.Command("/usr/bin/dscl", ".", "-read", "/Users/"+u.Username, "UserShell").CombinedOutput() // #nosec G204 -- fixed binary; the name is the current user's own
	if err != nil {
		return fmt.Errorf("dscl: %w: %s", err, out)
	}
	return nil
}
