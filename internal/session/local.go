package session

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"github.com/creack/pty"
)

var listProcessesFn = listProcesses
var getPIDCWDFn = getPIDCWD
var openFilePathsForPIDFn = openFilePathsForPID

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
	return getPIDCWD(s.pid)
}

// GetActiveWorkdir returns the working directory of the newest active Codex or
// Claude descendant process, or an empty string when none is running.
func (s *LocalSession) GetActiveWorkdir() (string, error) {
	if s.pid == 0 {
		return "", errors.New("session has no PID")
	}

	processes, err := listProcessesFn()
	if err != nil {
		return "", err
	}

	baseCWD, err := getPIDCWDFn(s.pid)
	if err != nil {
		return "", err
	}

	pid, ok := newestInteractiveAgentDescendantPID(processes, s.pid)
	if !ok {
		return "", nil
	}

	return resolveInteractiveAgentWorkdir(processes, pid, baseCWD)
}

func getPIDCWD(pid int) (string, error) {
	switch runtime.GOOS {
	case "linux":
		return os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	case "darwin":
		// -a ANDs the -p and -d conditions; without -a they are OR'd, which
		// causes -d cwd to dump the cwd of every process on the system.
		out, err := exec.Command( //nolint:gosec // G204: lsof is trusted and pid is an internal process ID
			"lsof",
			"-a",
			"-p",
			strconv.Itoa(pid),
			"-d",
			"cwd",
			"-Fn",
		).Output()
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

type processInfo struct {
	Command string
	PID     int
	PPID    int
}

func listProcesses() ([]processInfo, error) {
	out, err := exec.Command("ps", "-Ao", "pid,ppid,command").Output()
	if err != nil {
		return nil, fmt.Errorf("ps: %w", err)
	}
	return parsePSOutput(out)
}

func parsePSOutput(out []byte) ([]processInfo, error) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 2 {
		return nil, errors.New("ps output missing process rows")
	}

	processes := make([]processInfo, 0, len(lines)-1)
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}

		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			return nil, fmt.Errorf("parse pid %q: %w", fields[0], err)
		}
		ppid, err := strconv.Atoi(fields[1])
		if err != nil {
			return nil, fmt.Errorf("parse ppid %q: %w", fields[1], err)
		}

		processes = append(processes, processInfo{
			PID:     pid,
			PPID:    ppid,
			Command: strings.Join(fields[2:], " "),
		})
	}

	return processes, nil
}

func newestInteractiveAgentDescendantPID(processes []processInfo, rootPID int) (int, bool) {
	children := childProcessMap(processes)
	stack := append([]processInfo(nil), children[rootPID]...)
	var matched int
	var ok bool

	for len(stack) > 0 {
		proc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		stack = append(stack, children[proc.PID]...)

		if isInteractiveAgentCommand(proc.Command) {
			if !ok || proc.PID > matched {
				matched = proc.PID
				ok = true
			}
		}
	}

	return matched, ok
}

func childProcessMap(processes []processInfo) map[int][]processInfo {
	children := make(map[int][]processInfo)
	for _, proc := range processes {
		children[proc.PPID] = append(children[proc.PPID], proc)
	}
	return children
}

func descendantPIDs(processes []processInfo, rootPID int) []int {
	children := childProcessMap(processes)
	stack := append([]processInfo(nil), children[rootPID]...)
	ids := []int{rootPID}

	for len(stack) > 0 {
		proc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		stack = append(stack, children[proc.PID]...)
		ids = append(ids, proc.PID)
	}

	return ids
}

func resolveInteractiveAgentWorkdir(processes []processInfo, agentPID int, baseCWD string) (string, error) {
	if sessionCWD, err := codexSessionCWD(processes, agentPID); err == nil && sessionCWD != "" {
		return sessionCWD, nil
	}

	candidatePIDs := descendantPIDs(processes, agentPID)
	sort.Slice(candidatePIDs, func(i, j int) bool {
		return candidatePIDs[i] > candidatePIDs[j]
	})

	var agentCWD string
	for _, pid := range candidatePIDs {
		cwd, err := getPIDCWDFn(pid)
		if err != nil || cwd == "" {
			continue
		}

		if pid == agentPID {
			agentCWD = cwd
		}
		if cwd != baseCWD {
			return cwd, nil
		}
	}

	if agentCWD != "" {
		return agentCWD, nil
	}
	return "", nil
}

func codexSessionCWD(processes []processInfo, agentPID int) (string, error) {
	proc, ok := processByPID(processes, agentPID)
	if !ok || !isCodexCommand(proc.Command) {
		return "", nil
	}

	paths, err := openFilePathsForPIDFn(agentPID)
	if err != nil {
		return "", err
	}

	sessionPath, ok := codexSessionPath(paths)
	if !ok {
		return "", nil
	}

	return readCodexSessionCWD(sessionPath)
}

func processByPID(processes []processInfo, pid int) (processInfo, bool) {
	for _, proc := range processes {
		if proc.PID == pid {
			return proc, true
		}
	}
	return processInfo{}, false
}

func isInteractiveAgentCommand(command string) bool {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return false
	}

	binary := strings.ToLower(filepath.Base(fields[0]))
	switch binary {
	case "codex":
		return !containsToken(fields[1:], "exec")
	case "claude":
		return !containsAnyToken(fields[1:], "-p", "--print")
	default:
		return false
	}
}

func isCodexCommand(command string) bool {
	fields := strings.Fields(command)
	return len(fields) > 0 && strings.ToLower(filepath.Base(fields[0])) == "codex"
}

func containsToken(tokens []string, target string) bool {
	for _, token := range tokens {
		if token == target {
			return true
		}
	}
	return false
}

func containsAnyToken(tokens []string, targets ...string) bool {
	for _, token := range tokens {
		for _, target := range targets {
			if token == target {
				return true
			}
		}
	}
	return false
}

func openFilePathsForPID(pid int) ([]string, error) {
	switch runtime.GOOS {
	case "linux":
		fdDir := "/proc/" + strconv.Itoa(pid) + "/fd"
		entries, err := os.ReadDir(fdDir)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", fdDir, err)
		}

		paths := make([]string, 0, len(entries))
		for _, entry := range entries {
			target, err := os.Readlink(filepath.Join(fdDir, entry.Name()))
			if err == nil {
				paths = append(paths, target)
			}
		}
		return paths, nil
	case "darwin":
		out, err := exec.Command( //nolint:gosec // G204: lsof is trusted and pid is an internal process ID
			"lsof",
			"-a",
			"-p",
			strconv.Itoa(pid),
			"-Fn",
		).Output()
		if err != nil {
			return nil, fmt.Errorf("lsof files: %w", err)
		}

		paths := make([]string, 0, 32)
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "n") {
				paths = append(paths, strings.TrimSpace(strings.TrimPrefix(line, "n")))
			}
		}
		return paths, nil
	default:
		return nil, fmt.Errorf("open file inspection not supported on %s", runtime.GOOS)
	}
}

func codexSessionPath(paths []string) (string, bool) {
	for _, path := range paths {
		if strings.Contains(path, "/.codex/sessions/") && strings.HasSuffix(path, ".jsonl") {
			return path, true
		}
	}
	return "", false
}

func readCodexSessionCWD(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open codex session log %q: %w", path, err)
	}
	defer file.Close()

	type payloadWithCWD struct {
		Cwd string `json:"cwd"`
	}
	type record struct {
		Type    string         `json:"type"`
		Payload payloadWithCWD `json:"payload"`
	}

	var latestTurnCWD string
	var sessionMetaCWD string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var rec record
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			continue
		}
		switch rec.Type {
		case "turn_context":
			if rec.Payload.Cwd != "" {
				latestTurnCWD = rec.Payload.Cwd
			}
		case "session_meta":
			if rec.Payload.Cwd != "" && sessionMetaCWD == "" {
				sessionMetaCWD = rec.Payload.Cwd
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("scan codex session log %q: %w", path, err)
	}
	if latestTurnCWD != "" {
		return latestTurnCWD, nil
	}
	return sessionMetaCWD, nil
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
		return exec.Command( //nolint:gosec // G204: username comes from os/user.Current for the local account
			"/usr/bin/dscl",
			".",
			"-read",
			"/Users/"+username,
			"UserShell",
		).Output()
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
