package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"iter"
	"log"
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

	"panemux/internal/agent"
	"panemux/internal/homedir"
)

var listProcessesFn = listProcesses
var getPIDCWDFn = getPIDCWD
var openFilePathsForPIDFn = openFilePathsForPID
var readFileFn = os.ReadFile

// validShellPath matches a valid absolute shell path.
// Only alphanumeric characters, dots, underscores, hyphens, and slashes are permitted.
// This character allowlist is the sanitizer CodeQL requires for go/command-injection.
var validShellPath = regexp.MustCompile(`^(/[a-zA-Z0-9._\-/]+)$`)
var claudeBashCDPattern = regexp.MustCompile(`(?:^|&&)\s*cd\s+((?:"[^"]+"|'[^']+'|[^&|;]+))\s*&&`)
var validClaudeSessionID = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

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
	// A fresh local PTY is outside the parent's tmux transport, even if
	// panemux itself inherited the opt-in from a remote tmux session.
	cmd.Env = paneIDEnv(append(os.Environ(), "TERM=xterm-256color", "PANEMUX_SHIM_TMUX="), id)
	if browserShimEnabled.Load() {
		// Best effort: a pane must still start when the shim cannot be
		// installed, just without browser-open interception.
		shimEnv, shimErr := browserShimEnvForLocalSession()
		if shimErr != nil {
			log.Printf("browser-open shim unavailable for session %s: %v", id, shimErr)
		} else {
			cmd.Env = append(cmd.Env, shimEnv...)
		}
	}
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

// Read reads the shell's output. Once the pty reports the terminal gone, the
// session is exited: the read loop ends every subscription on that error, and
// the WebSocket handler sends the state it then reads, which must not still
// be "connected" because the process monitor has yet to record the exit.
func (s *LocalSession) Read(p []byte) (int, error) {
	n, err := s.ptmx.Read(p)
	if err != nil {
		s.mu.Lock()
		s.state = StateExited
		s.mu.Unlock()
	}
	return n, err
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

// BoardHostID identifies this session as running on panemux's own host.
func (s *LocalSession) BoardHostID() string { return boardHostIDLocal }

// GetCWD returns the current working directory of the shell process.
// On Linux it reads /proc/<pid>/cwd; on macOS it runs lsof.
func (s *LocalSession) GetCWD() (string, error) {
	if s.pid == 0 {
		return "", errors.New("session has no PID")
	}
	return getPIDCWD(s.pid)
}

// GetActiveWorkdirs returns every distinct working directory currently in
// play for the newest active Codex or Claude descendant process, including
// worktrees only visited by a delegated Claude Task subagent. Returns an
// empty slice if no such process exists.
func (s *LocalSession) GetActiveWorkdirs() ([]string, error) {
	if s.pid == 0 {
		return nil, errors.New("session has no PID")
	}

	processes, err := listProcessesFn()
	if err != nil {
		return nil, err
	}

	baseCWD, err := getPIDCWDFn(s.pid)
	if err != nil {
		return nil, err
	}

	pid, ok := newestInteractiveAgentDescendantPID(processes, s.pid)
	if !ok {
		return nil, nil
	}

	return resolveInteractiveAgentWorkdirs(processes, pid, baseCWD)
}

// DetectInteractiveAgent reports the agent ID of any live agent
// process currently running as a descendant of this pane's shell, among
// the registered agents with a Board rule. It skips the transcript/workdir resolution
// GetActiveWorkdirs does, so it's cheap enough to poll frequently — see
// AgentTypeDetector.
func (s *LocalSession) DetectInteractiveAgent() (agent.ID, bool, error) {
	if s.pid == 0 {
		return "", false, errors.New("session has no PID")
	}

	processes, err := listProcessesFn()
	if err != nil {
		return "", false, err
	}

	_, agentID, ok := newestKnownAgentTypeDescendantPID(processes, s.pid)
	return agentID, ok, nil
}

func getPIDCWD(pid int) (string, error) {
	switch runtime.GOOS {
	case "linux":
		return os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
	case "darwin":
		pidArg, err := processIDArg(pid)
		if err != nil {
			return "", err
		}
		// -a ANDs the -p and -d conditions; without -a they are OR'd, which
		// causes -d cwd to dump the cwd of every process on the system.
		out, err := exec.Command(
			"lsof",
			"-a",
			"-p",
			pidArg,
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

var validProcessIDArg = regexp.MustCompile(`^[1-9][0-9]*$`)
var validLocalUsername = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

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
	return newestMatchingDescendantPID(processes, rootPID, isInteractiveAgentCommand)
}

// detectBoardAgent reports the agent ID of command, if a
// registered agent's Board rule — which mirrors the board process rules
// detect_proc — matches it.
func detectBoardAgent(command string) (agent.ID, bool) {
	d, ok := agent.MatchBoardProcess(command)
	return d.ID, ok
}

// newestKnownAgentTypeDescendantPID is AgentTypeDetector's primitive: like
// newestInteractiveAgentDescendantPID, but matches any
// registered Board rule (not just claude/codex) and reports which type
// matched, since Agent Board's bootstrap instruction differs by type.
func newestKnownAgentTypeDescendantPID(processes []processInfo, rootPID int) (pid int, agentID agent.ID, ok bool) {
	children := childProcessMap(processes)
	stack := append([]processInfo(nil), children[rootPID]...)

	if proc, found := processByPID(processes, rootPID); found {
		if t, matched := detectBoardAgent(proc.Command); matched {
			pid, agentID, ok = rootPID, t, true
		}
	}

	for len(stack) > 0 {
		proc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		stack = append(stack, children[proc.PID]...)

		if t, matched := detectBoardAgent(proc.Command); matched {
			// `>=` here is killable, but only by input the OS cannot
			// produce: two matching processes sharing a PID, where the
			// agentID reported would differ (the PID would not). A test
			// for it would have to fabricate that snapshot and then pin
			// whichever type the traversal happens to reach first — an
			// accident of stack order, not designed behavior. Issue #190.
			//mutation:exempt[CONDITIONALS_BOUNDARY] unreachable — killable only by a fabricated snapshot with a duplicate PID
			if !ok || proc.PID > pid {
				pid, agentID, ok = proc.PID, t, true
			}
		}
	}

	return pid, agentID, ok
}

func newestMatchingDescendantPID(processes []processInfo, rootPID int, match func(string) bool) (int, bool) {
	children := childProcessMap(processes)
	stack := append([]processInfo(nil), children[rootPID]...)
	matched := 0
	ok := false

	if proc, found := processByPID(processes, rootPID); found && match(proc.Command) {
		matched = rootPID
		ok = true
	}

	for len(stack) > 0 {
		proc := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		stack = append(stack, children[proc.PID]...)

		if match(proc.Command) {
			// Unlike newestKnownAgentTypeDescendantPID above, this one is
			// equivalent outright rather than merely unreachable: the guard's
			// only writes are `matched = proc.PID` and `ok = true`, so on an
			// equal PID `>=` assigns the value already there. There is no
			// second return value for it to change. Issue #190.
			//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — on an equal PID the guard reassigns the same value
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

// resolveInteractiveAgentWorkdirs returns every distinct workdir signaled by
// the interactive agent's own session data (e.g. a Claude session's parent
// transcript plus any Task subagent transcripts), falling back to the single
// descendant-process cwd scan when no session-derived candidates are found.
func resolveInteractiveAgentWorkdirs(processes []processInfo, agentPID int, baseCWD string) ([]string, error) {
	if cwds, err := interactiveAgentSessionCWDs(processes, agentPID); err == nil && len(cwds) > 0 {
		return cwds, nil
	}

	cwd, err := descendantCWDFallback(processes, agentPID, baseCWD)
	if err != nil || cwd == "" {
		return nil, err
	}
	return []string{cwd}, nil
}

func descendantCWDFallback(processes []processInfo, agentPID int, baseCWD string) (string, error) {
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

func interactiveAgentSessionCWDs(processes []processInfo, agentPID int) ([]string, error) {
	proc, ok := processByPID(processes, agentPID)
	if !ok {
		return nil, nil
	}
	d, ok := agent.MatchInteractiveSession(proc.Command)
	if !ok {
		return nil, nil
	}
	return resolveSessionWorkdirs(localWorkdirHost{}, d.ID, agentPID)
}

// localWorkdirHost reads the panemux host's own files.
type localWorkdirHost struct{}

func (localWorkdirHost) hostKey() string { return "" }

// claudeStateFiles are every file under ~/.claude/sessions: Claude Code names
// them <pid>.json, but the pid inside is what is matched.
func (localWorkdirHost) claudeStateFiles(int) ([]claudeStateFile, error) {
	homeDir, err := homedir.Dir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for claude session: %w", err)
	}
	sessionsDir := filepath.Join(homeDir, ".claude", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read claude sessions dir %q: %w", sessionsDir, err)
	}

	var files []claudeStateFile
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := readFileFn(filepath.Join(sessionsDir, entry.Name()))
		if err != nil {
			continue
		}
		files = append(files, claudeStateFile{name: entry.Name(), data: data})
	}
	return files, nil
}

// claudeProjectDirs is the one directory resolveClaudeTranscriptPath picks:
// the derived one, or the one a scan found the transcript in.
func (localWorkdirHost) claudeProjectDirs(sessionID, cwd string) iter.Seq[string] {
	return func(yield func(string) bool) {
		homeDir, err := homedir.Dir()
		if err != nil {
			return
		}
		projects := filepath.Join(homeDir, ".claude", "projects")
		yield(filepath.Dir(resolveClaudeTranscriptPath(projects, cwd, sessionID)))
	}
}

// claudeSubagentLogs lists dir's transcripts; a session older than the
// "subagents" directory has none.
func (localWorkdirHost) claudeSubagentLogs(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		entries = nil
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".jsonl") {
			continue
		}
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	return names, nil
}

func (localWorkdirHost) claudeLogCWD(path string) (string, error) { return readClaudeProjectCWD(path) }

func (localWorkdirHost) agentOpenFiles(pid int) ([]string, error) { return openFilePathsForPIDFn(pid) }

func (localWorkdirHost) codexLogCWD(path string) (string, error) { return readCodexSessionCWD(path) }

func processByPID(processes []processInfo, pid int) (processInfo, bool) {
	for _, proc := range processes {
		if proc.PID == pid {
			return proc, true
		}
	}
	return processInfo{}, false
}

// isInteractiveAgentCommand reports whether command is the session of an
// agent that declares workdir, by the Task rule the task board uses.
func isInteractiveAgentCommand(command string) bool {
	_, ok := agent.MatchInteractiveSession(command)
	return ok
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
		pidArg, err := processIDArg(pid)
		if err != nil {
			return nil, err
		}
		out, err := exec.Command(
			"lsof",
			"-a",
			"-p",
			pidArg,
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

func processIDArg(pid int) (string, error) {
	// The `<= 0` boundary cannot be pinned by a test: validProcessIDArg below
	// is `^[1-9][0-9]*$`, so a pid of 0 that got past this guard is rejected
	// by the regex with the identical error. TestProcessIDArg_PIDBoundary
	// covers both sides of the boundary; the mutant on it is equivalent, not
	// unkilled. Issue #190.
	//mutation:exempt[CONDITIONALS_BOUNDARY] equivalent — validProcessIDArg rejects "0" with the same error
	if pid <= 0 {
		return "", fmt.Errorf("invalid pid: %d", pid)
	}
	pidArg := strconv.Itoa(pid)
	if !validProcessIDArg.MatchString(pidArg) {
		return "", fmt.Errorf("invalid pid: %d", pid)
	}
	return pidArg, nil
}

func readCodexSessionCWD(path string) (string, error) {
	fingerprint, err := localFileFingerprint(path)
	if err != nil {
		return "", fmt.Errorf("stat codex session log %q: %w", path, err)
	}

	return cachedAgentLogCWD("local:codex:"+path, fingerprint, func() (string, error) {
		data, err := readFileFn(path)
		if err != nil {
			return "", fmt.Errorf("read codex session log %q: %w", path, err)
		}
		return parseCodexSessionCWD(data)
	})
}

type codexSessionRecord struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

func parseCodexSessionCWD(data []byte) (string, error) {
	var latestTurnCWD string
	var sessionMetaCWD string
	var latestExecWorkdir string
	forEachJSONLRecord(data, func(line []byte) {
		var rec codexSessionRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			return
		}
		execWorkdir, turnCWD, metaCWD := codexSessionRecordCWD(rec)
		if execWorkdir != "" {
			latestExecWorkdir = execWorkdir
		}
		if turnCWD != "" {
			latestTurnCWD = turnCWD
		}
		if metaCWD != "" && sessionMetaCWD == "" {
			sessionMetaCWD = metaCWD
		}
	})
	// Codex may keep both process cwd and turn/session metadata pinned to the
	// original pane directory while tool calls run inside a sibling worktree.
	// The latest exec_command workdir is therefore the strongest signal.
	if latestExecWorkdir != "" {
		return latestExecWorkdir, nil
	}
	if latestTurnCWD != "" {
		return latestTurnCWD, nil
	}
	return sessionMetaCWD, nil
}

func codexSessionRecordCWD(rec codexSessionRecord) (execWorkdir, turnCWD, metaCWD string) {
	switch rec.Type {
	case "turn_context":
		return "", parsePayloadCWD(rec.Payload), ""
	case "session_meta":
		return "", "", parsePayloadCWD(rec.Payload)
	case "response_item":
		return parseExecCommandWorkdir(rec.Payload), "", ""
	default:
		return "", "", ""
	}
}

func parsePayloadCWD(raw json.RawMessage) string {
	type payloadWithCWD struct {
		Cwd string `json:"cwd"`
	}

	var payload payloadWithCWD
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return payload.Cwd
}

func parseExecCommandWorkdir(raw json.RawMessage) string {
	type responseItemPayload struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	type commandArguments struct {
		Workdir string `json:"workdir"`
	}

	var payload responseItemPayload
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	if payload.Type != "function_call" || payload.Name != "exec_command" || payload.Arguments == "" {
		return ""
	}

	var args commandArguments
	if err := json.Unmarshal([]byte(payload.Arguments), &args); err != nil {
		return ""
	}
	return args.Workdir
}

// claudeProjectDirName derives the ~/.claude/projects subdirectory Claude Code
// stores a session's transcript in: every path separator and every dot becomes
// a dash.
//
// This is observed behavior of Claude Code's own storage layout, not a
// documented interface, and it is re-validated only by someone looking at a
// real installation. TestClaudeProjectDirName_ObservedEncoding writes the
// mapping out case by case so what panemux believes is reviewable, and
// resolveClaudeTranscriptPath does not assume it stays true (issue #119).
func claudeProjectDirName(cwd string) string {
	name := strings.ReplaceAll(filepath.Clean(cwd), string(os.PathSeparator), "-")
	return strings.ReplaceAll(name, ".", "-")
}

// claudeProjectDirMismatches remembers which sessions have already had a
// derived-directory miss reported. The workdir lookup runs on every git-info
// refresh, so without this a single mismatch would be logged every few
// seconds for as long as the pane lives.
var claudeProjectDirMismatches struct {
	seen map[string]bool
	mu   sync.Mutex
}

// resolveClaudeTranscriptPath returns the transcript file for sessionID,
// preferring the path claudeProjectDirName derives and falling back to a scan
// of projectsDir when that file is not there.
//
// The fast path stays the normal case: one Stat, no directory listing. The
// fallback is bounded to one level — a single listing of projectsDir and at
// most one Stat per project directory, first match wins in name order — so a
// Claude release that changes the encoding costs a pane its worktree only if
// the transcript is not under projectsDir at all, rather than immediately.
//
// A miss is logged once per session so the change is visible in a log rather
// than only as a pane that quietly stopped reporting its worktree. When
// nothing is found anywhere the derived path is returned unchanged, which
// keeps "no transcript yet" silent and behaving exactly as before.
//
// sessionID must already have been checked against validClaudeSessionID: it is
// joined into a path here, and the callers in this package validate it before
// they get this far.
func resolveClaudeTranscriptPath(projectsDir, cwd, sessionID string) string {
	derived := filepath.Join(projectsDir, claudeProjectDirName(cwd), sessionID+".jsonl")
	if isRegularFile(derived) {
		return derived
	}

	found := scanClaudeProjectsForSession(projectsDir, sessionID)
	if found == "" {
		return derived
	}

	logClaudeProjectDirMismatchOnce(sessionID, derived, found)
	return found
}

func isRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// scanClaudeProjectsForSession looks for <sessionID>.jsonl directly inside each
// project directory, in the order ReadDir returns them (sorted by name).
func scanClaudeProjectsForSession(projectsDir, sessionID string) string {
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		return ""
	}

	name := sessionID + ".jsonl"
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(projectsDir, entry.Name(), name)
		if isRegularFile(candidate) {
			return candidate
		}
	}
	return ""
}

func logClaudeProjectDirMismatchOnce(sessionID, derived, found string) {
	claudeProjectDirMismatches.mu.Lock()
	defer claudeProjectDirMismatches.mu.Unlock()

	if claudeProjectDirMismatches.seen[sessionID] {
		return
	}
	if claudeProjectDirMismatches.seen == nil {
		claudeProjectDirMismatches.seen = make(map[string]bool)
	}
	claudeProjectDirMismatches.seen[sessionID] = true

	log.Printf(
		"Warning: claude transcript for session %s was not at the derived path %q; "+
			"using %q instead. Claude Code's project directory encoding may have changed.",
		sessionID, derived, found,
	)
}

func readClaudeProjectCWD(path string) (string, error) {
	fingerprint, err := localFileFingerprint(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("stat claude transcript %q: %w", path, err)
	}

	return cachedAgentLogCWD("local:claude:"+path, fingerprint, func() (string, error) {
		data, err := readFileFn(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return "", nil
			}
			return "", fmt.Errorf("read claude transcript %q: %w", path, err)
		}
		return parseClaudeProjectCWD(data)
	})
}

// parseClaudeProjectCWD resolves the effective working directory for a
// Claude transcript, in priority order: the latest Bash `cd X && ...`
// target, then the latest top-level `cwd` field, then the latest
// non-auxiliary file-touch path (Read/Edit/Write/etc, or file-history
// snapshot). See docs/architecture.md's "Pane Git/PR resolution" section
// for the full rationale (top-level `cwd` is pinned at process launch and
// never tracks a Bash `cd`, mirroring Codex's `workdir` precedence).
//
// A Bash-cd target, once seen, remains authoritative for the rest of the
// transcript until a *later* Bash-cd target replaces it — a top-level `cwd`
// or file-touch appearing afterward cannot displace it. This is intentional:
// an explicit `cd` is a durable "the agent has moved its base of operations
// here" signal, and neither of the weaker signals below it in this
// function's priority order is reliable enough evidence that the agent
// moved back to justify overriding it.
func parseClaudeProjectCWD(data []byte) (string, error) {
	var latestCWD string
	var latestBashCDPath string
	var latestFileTouchPath claudePathCandidate
	forEachJSONLRecord(data, func(line []byte) {
		cwd, candidate := claudeRecordPath(line)
		if cwd != "" {
			latestCWD = cwd
		}
		if candidate.Path == "" {
			return
		}
		if candidate.IsDir {
			latestBashCDPath = candidate.Path
		} else {
			latestFileTouchPath = candidate
		}
	})
	if latestBashCDPath != "" {
		return latestBashCDPath, nil
	}
	if latestCWD != "" {
		return latestCWD, nil
	}
	if latestFileTouchPath.Path == "" {
		return "", nil
	}
	return filepath.Dir(latestFileTouchPath.Path), nil
}

// forEachJSONLRecord iterates over already-buffered JSONL data without using
// bufio.Scanner, so it cannot surface Scanner's token-size limit.
// It also preserves a final unterminated record.
func forEachJSONLRecord(data []byte, fn func([]byte)) {
	for len(data) > 0 {
		line := data
		if idx := bytes.IndexByte(data, '\n'); idx >= 0 {
			line = data[:idx]
			data = data[idx+1:]
		} else {
			data = nil
		}

		line = bytes.TrimSuffix(line, []byte{'\r'})
		if len(line) == 0 {
			continue
		}
		fn(line)
	}
}

type claudeProjectRecord struct {
	Type     string `json:"type"`
	CWD      string `json:"cwd"`
	Snapshot struct {
		TrackedFileBackups map[string]json.RawMessage `json:"trackedFileBackups"`
	} `json:"snapshot"`
	Message struct {
		Content []json.RawMessage `json:"content"`
	} `json:"message"`
}

type claudePathCandidate struct {
	Path  string
	IsDir bool
}

func claudeRecordPath(line []byte) (string, claudePathCandidate) {
	var rec claudeProjectRecord
	if err := json.Unmarshal(line, &rec); err != nil {
		return "", claudePathCandidate{}
	}

	switch rec.Type {
	case "file-history-snapshot":
		return rec.CWD, claudePathCandidate{Path: latestClaudeTrackedPath(rec.Snapshot.TrackedFileBackups)}
	case "assistant":
		return rec.CWD, latestClaudeToolPath(rec.Message.Content)
	default:
		return rec.CWD, claudePathCandidate{}
	}
}

func latestClaudeTrackedPath(backups map[string]json.RawMessage) string {
	paths := make([]string, 0, len(backups))
	for path := range backups {
		if isClaudeAuxiliaryPath(path) {
			continue
		}
		paths = append(paths, path)
	}
	sort.Strings(paths)
	if len(paths) == 0 {
		return ""
	}
	return paths[len(paths)-1]
}

func latestClaudeToolPath(content []json.RawMessage) claudePathCandidate {
	latest := claudePathCandidate{}
	for _, item := range content {
		if path := claudeToolPath(item); path.Path != "" {
			latest = path
		}
	}
	return latest
}

func claudeToolPath(raw json.RawMessage) claudePathCandidate {
	type toolUseContent struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Input json.RawMessage `json:"input"`
	}
	type fileInput struct {
		FilePath string `json:"file_path"`
	}
	type bashInput struct {
		Command string `json:"command"`
	}

	var item toolUseContent
	if err := json.Unmarshal(raw, &item); err != nil || item.Type != "tool_use" {
		return claudePathCandidate{}
	}

	switch item.Name {
	case "Read", "Edit", "Write", "MultiEdit", "NotebookEdit":
		var input fileInput
		if err := json.Unmarshal(item.Input, &input); err != nil {
			return claudePathCandidate{}
		}
		if input.FilePath == "" || isClaudeAuxiliaryPath(input.FilePath) {
			return claudePathCandidate{}
		}
		return claudePathCandidate{Path: input.FilePath}
	case "Bash":
		var input bashInput
		if err := json.Unmarshal(item.Input, &input); err != nil {
			return claudePathCandidate{}
		}
		path := claudeBashCommandDir(input.Command)
		if path == "" {
			return claudePathCandidate{}
		}
		return claudePathCandidate{Path: path, IsDir: true}
	default:
		return claudePathCandidate{}
	}
}

func isClaudeAuxiliaryPath(path string) bool {
	return strings.Contains(path, "/.claude/")
}

func claudeBashCommandDir(command string) string {
	matches := claudeBashCDPattern.FindStringSubmatch(command)
	if len(matches) < 2 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(matches[1]), `"'`)
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
		userPath, err := localDirectoryServiceUserPath(username)
		if err != nil {
			return nil, err
		}
		return exec.Command(
			"/usr/bin/dscl",
			".",
			"-read",
			userPath,
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

func localDirectoryServiceUserPath(username string) (string, error) {
	if !validLocalUsername.MatchString(username) {
		return "", fmt.Errorf("invalid username for dscl lookup: %q", username)
	}
	return "/Users/" + username, nil
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
