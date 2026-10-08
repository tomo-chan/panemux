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
	"runtime"
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

// The probes' effects, as variables so the package's tests can drive every
// branch: whether a capability exists is the machine's answer, not theirs.
var (
	openPTY = pty.Open
	output  = (*exec.Cmd).CombinedOutput
	goos    = runtime.GOOS
)

func probePTY() error {
	ptmx, tty, err := openPTY()
	if err != nil {
		return fmt.Errorf("opening a pty: %w", err)
	}
	_ = tty.Close()
	if err := ptmx.Close(); err != nil {
		return fmt.Errorf("closing the pty: %w", err)
	}
	return nil
}

func probeTmux() error {
	if _, err := exec.LookPath("tmux"); err != nil {
		return fmt.Errorf("finding tmux: %w", err)
	}
	dir, err := os.MkdirTemp("", "pmx-cap-")
	if err != nil {
		return fmt.Errorf("making the probe's socket directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	return probeTmuxIn(dir)
}

// probeTmuxIn starts a server on the socket "s" in dir, confirms it answers,
// and stops it. The socket is always named explicitly, so an inherited TMUX
// can never route the probe to the caller's own server; it is relative to the
// command's working directory so every argument stays a constant.
func probeTmuxIn(dir string) error {
	tmux := func(cmd *exec.Cmd) ([]byte, error) {
		cmd.Dir = dir
		return output(cmd)
	}
	defer func() { _, _ = tmux(exec.Command("tmux", "-S", "s", "kill-server")) }()
	out, err := tmux(exec.Command("tmux", "-S", "s", "-f", "/dev/null", "new-session", "-d", "-s", "probe", "sleep 5"))
	if err != nil {
		return fmt.Errorf("tmux new-session: %w: %s", err, out)
	}
	if out, err := tmux(exec.Command("tmux", "-S", "s", "has-session", "-t", "=probe")); err != nil {
		return fmt.Errorf("tmux has-session: %w: %s", err, out)
	}
	return nil
}

func probePS() error {
	if out, err := output(exec.Command("ps", "-A", "-o", "pid=")); err != nil {
		return fmt.Errorf("ps: %w: %s", err, out)
	}
	return nil
}

// probeDscl lists every user's shell: the same Directory Services query
// DetectLocalShell makes for one user, with no argument that varies.
func probeDscl() error {
	if goos != "darwin" {
		return nil
	}
	if out, err := output(exec.Command("/usr/bin/dscl", ".", "-list", "/Users", "UserShell")); err != nil {
		return fmt.Errorf("dscl: %w: %s", err, out)
	}
	return nil
}
