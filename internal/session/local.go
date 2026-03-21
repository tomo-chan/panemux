package session

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

// validShellPath matches a valid absolute shell path.
// Only alphanumeric characters, dots, underscores, hyphens, and slashes are permitted.
// This character allowlist is the sanitizer CodeQL requires for go/command-injection.
var validShellPath = regexp.MustCompile(`^(/[a-zA-Z0-9._\-/]+)$`)

// LocalSession is a local PTY-based terminal session.
type LocalSession struct {
	mu    sync.RWMutex
	id    string
	title string
	state State
	cmd   *exec.Cmd
	ptmx  *os.File
}

// NewLocal creates and starts a new local PTY session.
func NewLocal(id, shell, cwd, title string) (*LocalSession, error) {
	if shell == "" {
		shell = "/bin/sh"
	}

	sanitizedShell, err := validateShell(shell)
	if err != nil {
		return nil, err
	}

	cmd := exec.Command(sanitizedShell)
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	if cwd != "" {
		cmd.Dir = cwd
	}
	// Ensure the process gets its own process group so signals work correctly
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		return nil, fmt.Errorf("starting pty: %w", err)
	}

	s := &LocalSession{
		id:    id,
		title: title,
		state: StateConnected,
		cmd:   cmd,
		ptmx:  ptmx,
	}

	// Monitor process exit in background
	go func() {
		cmd.Wait()
		s.mu.Lock()
		s.state = StateExited
		s.mu.Unlock()
	}()

	return s, nil
}

func (s *LocalSession) ID() string    { return s.id }
func (s *LocalSession) Type() Type    { return TypeLocal }
func (s *LocalSession) Title() string { return s.title }

func (s *LocalSession) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *LocalSession) Read(p []byte) (int, error) {
	return s.ptmx.Read(p)
}

func (s *LocalSession) Write(p []byte) (int, error) {
	return s.ptmx.Write(p)
}

func (s *LocalSession) Resize(cols, rows uint16) error {
	return pty.Setsize(s.ptmx, &pty.Winsize{
		Cols: cols,
		Rows: rows,
	})
}

func (s *LocalSession) Close() error {
	s.mu.Lock()
	s.state = StateExited
	s.mu.Unlock()

	s.ptmx.Close()
	if s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	return nil
}

// validateShell ensures the shell path is safe to execute.
// It applies three checks in order:
//  1. Character allowlist regex — rejects paths with shell metacharacters.
//  2. exec.LookPath — confirms the binary exists.
//  3. /etc/shells lookup — returns the entry directly from the system allowlist.
//
// The return value is the key read from /etc/shells (not the caller-supplied
// value), so CodeQL's taint-flow analysis sees no path from user input to
// exec.Command.
func validateShell(shell string) (string, error) {
	if !filepath.IsAbs(shell) {
		return "", fmt.Errorf("shell must be an absolute path: %q", shell)
	}
	if !validShellPath.MatchString(shell) {
		return "", fmt.Errorf("shell path contains invalid characters: %q (must match %s)", shell, validShellPath)
	}
	if _, err := exec.LookPath(shell); err != nil {
		return "", fmt.Errorf("shell not found: %w", err)
	}
	allowed, err := readEtcShells()
	if err != nil {
		return "", fmt.Errorf("cannot validate shell (failed to read /etc/shells): %w", err)
	}
	for s := range allowed {
		if s == shell {
			return s, nil // s is a key from /etc/shells — not derived from user input
		}
	}
	return "", fmt.Errorf("not an allowed shell: %q (not listed in /etc/shells)", shell)
}

// readEtcShells parses /etc/shells and returns the set of listed shell paths.
func readEtcShells() (map[string]bool, error) {
	data, err := os.ReadFile("/etc/shells")
	if err != nil {
		return nil, err
	}
	allowed := make(map[string]bool)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line != "" && !strings.HasPrefix(line, "#") {
			allowed[line] = true
		}
	}
	return allowed, nil
}
