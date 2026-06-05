package session

import (
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocal_Default(t *testing.T) {
	sess, err := NewLocal("test-id", "", "", "Test Title")
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, "test-id", sess.ID())
	assert.Equal(t, TypeLocal, sess.Type())
	assert.Equal(t, "Test Title", sess.Title())
	assert.Equal(t, StateConnected, sess.State())
}

func TestNewLocal_ExplicitShell(t *testing.T) {
	sess, err := NewLocal("test-id", "/bin/sh", "", "shell test")
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, StateConnected, sess.State())
}

func TestNewLocal_InvalidShell_Error(t *testing.T) {
	_, err := NewLocal("test-id", "/nonexistent/shell/xyz", "", "bad shell")
	assert.Error(t, err)
}

func TestNewLocal_State(t *testing.T) {
	sess, err := NewLocal("state-test", "/bin/sh", "", "state")
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, StateConnected, sess.State())
}

func TestNewLocal_Write_Read(t *testing.T) {
	sess, err := NewLocal("rw-test", "/bin/sh", "", "rw")
	require.NoError(t, err)
	defer sess.Close()

	_, err = sess.Write([]byte("echo hi\n"))
	require.NoError(t, err)

	type result struct {
		err error
		n   int
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 1024)
		n, err := sess.Read(buf)
		ch <- result{err: err, n: n}
	}()

	select {
	case r := <-ch:
		assert.NoError(t, r.err)
		assert.Greater(t, r.n, 0)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for session read")
	}
}

func TestNewLocal_Resize(t *testing.T) {
	sess, err := NewLocal("resize-test", "/bin/sh", "", "resize")
	require.NoError(t, err)
	defer sess.Close()

	err = sess.Resize(120, 40)
	assert.NoError(t, err)
}

func TestNewLocal_Close(t *testing.T) {
	sess, err := NewLocal("close-test", "/bin/sh", "", "close")
	require.NoError(t, err)

	err = sess.Close()
	assert.NoError(t, err)

	// Allow background goroutine to update state
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, StateExited, sess.State())
}

func TestNewLocal_RelativeShell_Error(t *testing.T) {
	_, err := NewLocal("test-id", "sh", "", "relative shell")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "absolute path")
}

func TestNewLocal_WithCwd(t *testing.T) {
	tmpDir := os.TempDir()
	sess, err := NewLocal("cwd-test", "/bin/sh", tmpDir, "cwd")
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, StateConnected, sess.State())
}

func TestLocalSessionGetCWD(t *testing.T) {
	tmpDir := os.TempDir()
	sess, err := NewLocal("cwd-live-test", "/bin/sh", tmpDir, "cwd-live")
	require.NoError(t, err)
	defer sess.Close()

	cwd, err := sess.GetCWD()
	require.NoError(t, err)
	assert.NotEmpty(t, cwd)
}

func TestNewTmuxLocal_InvalidSessionName_Error(t *testing.T) {
	_, err := NewTmuxLocal("tmux-id", "title", "bad;session")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tmux session name")
}

func TestProcessIDArg_RejectsNonPositivePID(t *testing.T) {
	_, err := processIDArg(0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pid")
}

func TestLocalDirectoryServiceUserPath_RejectsInvalidUsername(t *testing.T) {
	_, err := localDirectoryServiceUserPath("bad/user")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid username")
}

func TestParsePSOutput(t *testing.T) {
	processes, err := parsePSOutput([]byte("  PID  PPID COMMAND\n  101   10 /bin/zsh\n  202  101 codex exec\n"))
	require.NoError(t, err)
	require.Len(t, processes, 2)
	assert.Equal(t, 101, processes[0].PID)
	assert.Equal(t, 10, processes[0].PPID)
	assert.Equal(t, "/bin/zsh", processes[0].Command)
	assert.Equal(t, "codex exec", processes[1].Command)
}

func TestParsePSOutput_InvalidPID(t *testing.T) {
	_, err := parsePSOutput([]byte("PID PPID COMMAND\nabc 1 codex\n"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse pid")
}

func TestNewestInteractiveAgentDescendantPID(t *testing.T) {
	processes := []processInfo{
		{PID: 100, PPID: 1, Command: "/bin/zsh"},
		{PID: 110, PPID: 100, Command: "git status"},
		{PID: 120, PPID: 100, Command: "codex exec"},
		{PID: 130, PPID: 120, Command: "node helper"},
		{PID: 140, PPID: 100, Command: "claude"},
	}

	pid, ok := newestInteractiveAgentDescendantPID(processes, 100)
	require.True(t, ok)
	assert.Equal(t, 140, pid)
}

func TestNewestInteractiveAgentDescendantPID_NoAgent(t *testing.T) {
	processes := []processInfo{
		{PID: 100, PPID: 1, Command: "/bin/zsh"},
		{PID: 110, PPID: 100, Command: "git status"},
	}

	pid, ok := newestInteractiveAgentDescendantPID(processes, 100)
	assert.False(t, ok)
	assert.Zero(t, pid)
}

func TestNewestInteractiveAgentDescendantPID_IgnoresNonInteractiveAgents(t *testing.T) {
	processes := []processInfo{
		{PID: 100, PPID: 1, Command: "/bin/zsh"},
		{PID: 120, PPID: 100, Command: "codex exec"},
		{PID: 130, PPID: 100, Command: "claude -p"},
		{PID: 140, PPID: 100, Command: "/usr/local/bin/codex"},
	}

	pid, ok := newestInteractiveAgentDescendantPID(processes, 100)
	require.True(t, ok)
	assert.Equal(t, 140, pid)
}

func TestNewestInteractiveAgentDescendantPID_PrefersNewerChildOverInteractiveRoot(t *testing.T) {
	processes := []processInfo{
		{PID: 100, PPID: 1, Command: "codex resume --last"},
		{PID: 140, PPID: 100, Command: "claude"},
		{PID: 150, PPID: 100, Command: "codex exec"},
		{PID: 160, PPID: 100, Command: "codex"},
	}

	pid, ok := newestInteractiveAgentDescendantPID(processes, 100)
	require.True(t, ok)
	assert.Equal(t, 160, pid)
}

func TestDescendantPIDs(t *testing.T) {
	processes := []processInfo{
		{PID: 110, PPID: 100, Command: "/bin/zsh"},
		{PID: 120, PPID: 110, Command: "codex"},
		{PID: 130, PPID: 120, Command: "node helper"},
	}

	ids := descendantPIDs(processes, 110)
	sort.Ints(ids)
	assert.Equal(t, []int{110, 120, 130}, ids)
}

func TestIsInteractiveAgentCommand(t *testing.T) {
	assert.True(t, isInteractiveAgentCommand("codex"))
	assert.True(t, isInteractiveAgentCommand("/usr/local/bin/codex --model gpt-5"))
	assert.True(t, isInteractiveAgentCommand("claude"))
	assert.False(t, isInteractiveAgentCommand("codex exec"))
	assert.False(t, isInteractiveAgentCommand("claude -p"))
	assert.False(t, isInteractiveAgentCommand("claude --print"))
	assert.False(t, isInteractiveAgentCommand("python worker.py"))
}

func TestCodexSessionPath(t *testing.T) {
	path, ok := codexSessionPath([]string{
		"/tmp/other.log",
		"/Users/demo/.codex/sessions/2026/05/17/rollout.jsonl",
	})
	require.True(t, ok)
	assert.Equal(t, "/Users/demo/.codex/sessions/2026/05/17/rollout.jsonl", path)
}

func mustJSONLine(t *testing.T, value any) string {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)
	return string(data) + "\n"
}

func codexExecCommandRecord(t *testing.T, cmd, workdir string) string {
	t.Helper()

	type commandArguments struct {
		Cmd     string `json:"cmd"`
		Workdir string `json:"workdir"`
	}
	type responseItemPayload struct {
		Type      string `json:"type"`
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	}
	argsJSON, err := json.Marshal(commandArguments{Cmd: cmd, Workdir: workdir})
	require.NoError(t, err)

	return mustJSONLine(t, codexSessionRecord{
		Type: "response_item",
		Payload: mustRawJSON(t, responseItemPayload{
			Type:      "function_call",
			Name:      "exec_command",
			Arguments: string(argsJSON),
		}),
	})
}

func mustRawJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()

	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func TestReadCodexSessionCWD_PrefersTurnContext(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := "" +
		"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
		"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/tmp/worktree-a\"}}\n" +
		"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/tmp/worktree-b\"}}\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	cwd, err := readCodexSessionCWD(path)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-b", cwd)
}

func TestReadCodexSessionCWD_FallsBackToSessionMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := "{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	cwd, err := readCodexSessionCWD(path)
	require.NoError(t, err)
	assert.Equal(t, "/repo/main", cwd)
}

func TestReadCodexSessionCWD_PrefersLatestExecCommandWorkdir(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := "" +
		"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
		"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
		codexExecCommandRecord(t, "git status --short --branch", "/tmp/worktree-a") +
		codexExecCommandRecord(t, "go test ./...", "/tmp/worktree-b")
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	cwd, err := readCodexSessionCWD(path)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-b", cwd)
}

func TestReadCodexSessionCWD_PrefersExecCommandWorkdirEvenWhenTurnContextAppearsLater(t *testing.T) {
	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := "" +
		"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
		codexExecCommandRecord(t, "git status", "/tmp/worktree-from-command") +
		"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/repo/main\"}}\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	cwd, err := readCodexSessionCWD(path)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-from-command", cwd)
}

func TestGetActiveWorkdir_NoDescendantMatchReturnsEmpty(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalOpenFilePaths := openFilePathsForPIDFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		openFilePathsForPIDFn = originalOpenFilePaths
	})

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{{PID: 110, PPID: 100, Command: "git status"}}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		if pid == 100 {
			return "/repo/main", nil
		}
		return "", errors.New("should not be called")
	}

	cwd, err := sess.GetActiveWorkdir()
	require.NoError(t, err)
	assert.Empty(t, cwd)
}

func TestGetActiveWorkdir_ReturnsAgentDescendantCWD(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
	})

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 210, PPID: 110, Command: "codex exec"},
			{PID: 220, PPID: 110, Command: "codex"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		if pid == 100 {
			return "/repo/main", nil
		}
		assert.Equal(t, 220, pid)
		return "/tmp/panemux-pane-pr-link", nil
	}
	openFilePathsForPIDFn = func(pid int) ([]string, error) {
		return nil, nil
	}

	cwd, err := sess.GetActiveWorkdir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/panemux-pane-pr-link", cwd)
}

func TestGetActiveWorkdir_PrefersInteractiveAgentDescendantWorkdir(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalOpenFilePaths := openFilePathsForPIDFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		openFilePathsForPIDFn = originalOpenFilePaths
	})

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "codex resume --last"},
			{PID: 230, PPID: 220, Command: "node helper"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100:
			return "/repo/main", nil
		case 220:
			return "/repo/main", nil
		case 230:
			return "/tmp/panemux-pane-pr-link", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}
	openFilePathsForPIDFn = func(pid int) ([]string, error) {
		return nil, nil
	}

	cwd, err := sess.GetActiveWorkdir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/panemux-pane-pr-link", cwd)
}

func TestGetActiveWorkdir_PrefersCodexSessionCWD(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalOpenFilePaths := openFilePathsForPIDFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		openFilePathsForPIDFn = originalOpenFilePaths
	})

	sessionLog := filepath.Join(t.TempDir(), ".codex", "sessions", "2026", "05", "17", "session.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionLog), 0755))
	content := "" +
		"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
		"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/tmp/worktree-from-session\"}}\n"
	require.NoError(t, os.WriteFile(sessionLog, []byte(content), 0600))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "codex resume --last"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100:
			return "/repo/main", nil
		case 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}
	openFilePathsForPIDFn = func(pid int) ([]string, error) {
		assert.Equal(t, 220, pid)
		return []string{sessionLog}, nil
	}

	cwd, err := sess.GetActiveWorkdir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-from-session", cwd)
}

func TestGetActiveWorkdir_PrefersCodexExecCommandWorkdir(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalOpenFilePaths := openFilePathsForPIDFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		openFilePathsForPIDFn = originalOpenFilePaths
	})

	sessionLog := filepath.Join(t.TempDir(), ".codex", "sessions", "2026", "05", "17", "session.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionLog), 0755))
	content := "" +
		"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
		"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
		codexExecCommandRecord(t, "git status", "/tmp/worktree-from-command")
	require.NoError(t, os.WriteFile(sessionLog, []byte(content), 0600))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "codex resume --last"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100:
			return "/repo/main", nil
		case 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}
	openFilePathsForPIDFn = func(pid int) ([]string, error) {
		assert.Equal(t, 220, pid)
		return []string{sessionLog}, nil
	}

	cwd, err := sess.GetActiveWorkdir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-from-command", cwd)
}

func TestValidateShell_InEtcShells_OK(t *testing.T) {
	got, err := validateShell("/bin/sh")
	assert.NoError(t, err)
	assert.Equal(t, "/bin/sh", got)
}

func TestValidateShell_NotInEtcShells_Error(t *testing.T) {
	// Create a real executable that is not listed in /etc/shells.
	dir := t.TempDir()
	fakePath := dir + "/fakeshell"
	require.NoError(t, os.WriteFile(fakePath, []byte("#!/bin/sh\n"), 0755)) //nolint:gosec // G306: executable fixture

	_, err := validateShell(fakePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed shell")
}

func TestValidateShell_InvalidChars_Error(t *testing.T) {
	_, err := validateShell("/bin/sh; rm -rf /")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid characters")
}

func TestDetectLocalShell_ReturnsAbsolutePath(t *testing.T) {
	shell, err := DetectLocalShell()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(shell), "expected absolute shell path, got %q", shell)
}

func TestDetectLocalShellFrom_MatchesCurrentUID(t *testing.T) {
	currentUser, err := user.Current()
	require.NoError(t, err)

	// Build content with the current user's entry mapping to /usr/bin/bash.
	// Only prepend a separate root entry if we are NOT root, to avoid having
	// two lines with the same UID (which would cause the first one to win).
	var content string
	if currentUser.Uid != "0" {
		content = "root:x:0:0:root:/root:/bin/false\n"
	}
	content += currentUser.Username + ":x:" + currentUser.Uid + ":1000::/home/user:/usr/bin/bash\n"
	tmpFile := filepath.Join(t.TempDir(), "passwd")
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0600))

	shell, err := detectLocalShellFrom(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/bash", shell)
}

func TestDetectLocalShellFrom_UserNotFound_Error(t *testing.T) {
	content := "nobody:x:99999:99999::/nonexistent:/bin/false\n"
	tmpFile := filepath.Join(t.TempDir(), "passwd")
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0600))

	_, err := detectLocalShellFrom(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shell not found")
}

func TestDetectLocalShellDscl_ParsesOutput(t *testing.T) {
	runner := func(username string) ([]byte, error) {
		return []byte("UserShell: /bin/zsh\n"), nil
	}
	shell, err := detectLocalShellDscl("tomo", runner)
	require.NoError(t, err)
	assert.Equal(t, "/bin/zsh", shell)
}

func TestDetectLocalShellDscl_NoUserShellLine_Error(t *testing.T) {
	runner := func(username string) ([]byte, error) {
		return []byte("No such key: UserShell\n"), nil
	}
	_, err := detectLocalShellDscl("tomo", runner)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UserShell not found")
}

func TestTmuxLocalSessionGetActiveWorkdir_UsesPanePIDAndBaseCWD(t *testing.T) {
	prevTmuxOutput := tmuxLocalOutputFn
	prevListProcesses := listProcessesFn
	prevGetPIDCWD := getPIDCWDFn
	t.Cleanup(func() {
		tmuxLocalOutputFn = prevTmuxOutput
		listProcessesFn = prevListProcesses
		getPIDCWDFn = prevGetPIDCWD
	})

	tmuxLocalOutputFn = func(args ...string) ([]byte, error) {
		switch {
		case len(args) == 5 && args[4] == "#{pane_pid}":
			return []byte("220\n"), nil
		case len(args) == 5 && args[4] == "#{pane_current_path}":
			return []byte("/repo/main\n"), nil
		default:
			return nil, errors.New("unexpected tmux args")
		}
	}
	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 220, PPID: 1, Command: "zsh"},
			{PID: 230, PPID: 220, Command: "codex"},
			{PID: 240, PPID: 230, Command: "node helper"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 240:
			return "/tmp/worktree", nil
		case 230:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	sess := &TmuxLocalSession{tmuxSession: "demo"}
	cwd, err := sess.GetActiveWorkdir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree", cwd)
}

func TestTmuxLocalSessionGetActiveWorkdir_PrefersCodexExecCommandWorkdir(t *testing.T) {
	prevTmuxOutput := tmuxLocalOutputFn
	prevListProcesses := listProcessesFn
	prevGetPIDCWD := getPIDCWDFn
	prevOpenFilePaths := openFilePathsForPIDFn
	t.Cleanup(func() {
		tmuxLocalOutputFn = prevTmuxOutput
		listProcessesFn = prevListProcesses
		getPIDCWDFn = prevGetPIDCWD
		openFilePathsForPIDFn = prevOpenFilePaths
	})

	sessionLog := filepath.Join(t.TempDir(), ".codex", "sessions", "2026", "05", "17", "session.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionLog), 0755))
	content := "" +
		"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
		"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
		codexExecCommandRecord(t, "go test ./...", "/tmp/tmux-worktree-from-command")
	require.NoError(t, os.WriteFile(sessionLog, []byte(content), 0600))

	tmuxLocalOutputFn = func(args ...string) ([]byte, error) {
		switch {
		case len(args) == 5 && args[4] == "#{pane_pid}":
			return []byte("220\n"), nil
		case len(args) == 5 && args[4] == "#{pane_current_path}":
			return []byte("/repo/main\n"), nil
		default:
			return nil, errors.New("unexpected tmux args")
		}
	}
	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 220, PPID: 1, Command: "zsh"},
			{PID: 230, PPID: 220, Command: "codex"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 230:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}
	openFilePathsForPIDFn = func(pid int) ([]string, error) {
		assert.Equal(t, 230, pid)
		return []string{sessionLog}, nil
	}

	sess := &TmuxLocalSession{tmuxSession: "demo"}
	cwd, err := sess.GetActiveWorkdir()
	require.NoError(t, err)
	assert.Equal(t, "/tmp/tmux-worktree-from-command", cwd)
}

func TestTmuxLocalSessionGetActiveWorkdir_WhenPanePIDIsCodex_PrefersCodexExecCommandWorkdir(t *testing.T) {
	sessionLog := filepath.Join(t.TempDir(), ".codex", "sessions", "2026", "05", "17", "session.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionLog), 0755))
	require.NoError(t, os.WriteFile(
		sessionLog,
		[]byte(
			"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n"+
				"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/repo/main\"}}\n"+
				codexExecCommandRecord(t, "go test ./...", "/tmp/tmux-root-codex-worktree"),
		),
		0600,
	))

	originalTmuxLocalOutput := tmuxLocalOutputFn
	originalListProcesses := listProcessesFn
	originalOpenFilePaths := openFilePathsForPIDFn
	t.Cleanup(func() {
		tmuxLocalOutputFn = originalTmuxLocalOutput
		listProcessesFn = originalListProcesses
		openFilePathsForPIDFn = originalOpenFilePaths
	})

	tmuxLocalOutputFn = func(args ...string) ([]byte, error) {
		switch {
		case len(args) == 5 &&
			args[0] == "display-message" &&
			args[1] == "-p" &&
			args[2] == "-t" &&
			args[3] == "demo" &&
			args[4] == "#{pane_pid}":
			return []byte("220\n"), nil
		case len(args) == 5 &&
			args[0] == "display-message" &&
			args[1] == "-p" &&
			args[2] == "-t" &&
			args[3] == "demo" &&
			args[4] == "#{pane_current_path}":
			return []byte("/repo/main\n"), nil
		default:
			return nil, errors.New("unexpected tmux args")
		}
	}
	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 220, PPID: 1, Command: "codex resume --last"},
		}, nil
	}
	openFilePathsForPIDFn = func(pid int) ([]string, error) {
		require.Equal(t, 220, pid)
		return []string{sessionLog}, nil
	}

	cwd, err := tmuxLocalActiveWorkdir("demo")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/tmux-root-codex-worktree", cwd)
}
