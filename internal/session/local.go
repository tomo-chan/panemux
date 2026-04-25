package session

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
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
	cmd   *exec.Cmd
	ptmx  *os.File
	id    string
	title string
	state State
	mu    sync.RWMutex
	pid   int
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
		pid:   cmd.Process.Pid,
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

// GetCWD returns the current working directory of the shell process.
// On Linux it reads /proc/<pid>/cwd; on macOS it runs lsof.
func (s *LocalSession) GetCWD() (string, error) {
	if s.pid == 0 {
		return "", errors.New("session has no PID")
	}
	switch runtime.GOOS {
	case "linux":
		return os.Readlink("/proc/" + strconv.Itoa(s.pid) + "/cwd")
	case "darwin":
		// -a ANDs the -p and -d conditions; without -a they are OR'd, which
		// causes -d cwd to dump the cwd of every process on the system.
		out, err := exec.Command("lsof", "-a", "-p", strconv.Itoa(s.pid), "-d", "cwd", "-Fn").Output()
		if err != nil {
			return "", fmt.Errorf("lsof: %w", err)
		}
		// Output format (one entry per line):
		//   p<pid>
		//   fcwd
		//   n<path>
		// Find the n-line that immediately follows fcwd to be safe.
		lines := strings.Split(string(out), "\n")
		for i, line := range lines {
			if strings.TrimSpace(line) == "fcwd" && i+1 < len(lines) {
				p := strings.TrimPrefix(lines[i+1], "n")
				return strings.TrimSpace(p), nil
			}
		}
		return "", errors.New("cwd not found in lsof output")
	default:
		return "", fmt.Errorf("GetCWD not supported on %s", runtime.GOOS)
	}
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

// DetectLocalShell returns the login shell for the current user.
// It first checks /etc/passwd (Linux). On macOS, where regular users are not
// listed in /etc/passwd, it falls back to querying Directory Services via dscl.
func DetectLocalShell() (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("getting current user: %w", err)
	}
	shell, err := detectLocalShellFrom("/etc/passwd")
	if err == nil {
		return shell, nil
	}
	// /etc/passwd lookup failed (expected on macOS) — try dscl.
	return detectLocalShellDscl(currentUser.Username, func(username string) ([]byte, error) {
		return exec.Command("/usr/bin/dscl", ".", "-read", "/Users/"+username, "UserShell").Output()
	})
}

// detectLocalShellDscl queries macOS Directory Services for the user's login shell.
// The runner parameter exists for testability.
func detectLocalShellDscl(username string, runner func(string) ([]byte, error)) (string, error) {
	out, err := runner(username)
	if err != nil {
		return "", fmt.Errorf("dscl: %w", err)
	}
	// Output format: "UserShell: /bin/zsh\n"
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "UserShell:") {
			shell := strings.TrimSpace(strings.TrimPrefix(line, "UserShell:"))
			if shell != "" {
				return shell, nil
			}
		}
	}
	return "", fmt.Errorf("UserShell not found in dscl output for user %q", username)
}

// detectLocalShellFrom is the testable version that accepts a custom passwd file path.
func detectLocalShellFrom(passwdPath string) (string, error) {
	currentUser, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("getting current user: %w", err)
	}
	data, err := os.ReadFile(passwdPath)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", passwdPath, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, ":")
		if len(parts) < 7 {
			continue
		}
		// Match by UID (more reliable than username)
		if parts[2] == currentUser.Uid {
			shell := strings.TrimSpace(parts[6])
			if shell != "" {
				return shell, nil
			}
		}
	}
	return "", fmt.Errorf("shell not found for user %q (uid %s) in %s", currentUser.Username, currentUser.Uid, passwdPath)
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
