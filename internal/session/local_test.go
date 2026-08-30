package session

import (
	"encoding/json"
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeFileInfo struct {
	name        string
	size        int64
	modTimeUnix int64
}

func (f fakeFileInfo) Name() string       { return f.name }
func (f fakeFileInfo) Size() int64        { return f.size }
func (f fakeFileInfo) Mode() os.FileMode  { return 0600 }
func (f fakeFileInfo) ModTime() time.Time { return time.Unix(f.modTimeUnix, 0) }
func (f fakeFileInfo) IsDir() bool        { return false }
func (f fakeFileInfo) Sys() any           { return nil }

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
	_, err := NewTmuxLocal("tmux-id", "title", "bad;session", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tmux session name")
}

func TestTmuxLocalArgs_EmptyCwd_NoDashC(t *testing.T) {
	args := tmuxLocalArgs("mysession", "")
	assert.Equal(t, []string{tmuxNewSessionSubcommand, "-A", "-s", "mysession"}, args)
}

func TestTmuxLocalArgs_WithCwd_AppendsDashC(t *testing.T) {
	args := tmuxLocalArgs("mysession", "/workspace/user/project")
	assert.Equal(t, []string{tmuxNewSessionSubcommand, "-A", "-s", "mysession", "-c", "/workspace/user/project"}, args)
}

func TestProcessIDArg_RejectsNonPositivePID(t *testing.T) {
	_, err := processIDArg(0)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid pid")
}

// TestProcessIDArg_PIDBoundary walks both sides of the boundary. The test
// above only pins 0; without the accepting cases nothing said the guard stops
// at exactly the right place. Issue #190.
// Nothing under this test changed on this branch, so the red-check could never
// see it go red: it pins behavior that was already correct and merely
// unasserted. See docs/quality-gateway.md's "Clearing the boundary-value class".
//
//efficacy:exempt pins pre-existing behavior; no implementation under it changed
func TestProcessIDArg_PIDBoundary(t *testing.T) {
	tests := []struct {
		name    string
		want    string
		pid     int
		wantErr bool
	}{
		{name: "negative", pid: -1, wantErr: true},
		{name: "zero", pid: 0, wantErr: true},
		{name: "the lowest usable pid", pid: 1, want: "1"},
		{name: "an ordinary pid", pid: 4242, want: "4242"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arg, err := processIDArg(tt.pid)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid pid")
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, arg)
		})
	}
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

func TestDetectAgmsgAgentType(t *testing.T) {
	tests := []struct {
		command  string
		wantType string
		wantOK   bool
	}{
		{"claude", "claude-code", true},
		{"/usr/local/bin/claude --resume", "claude-code", true},
		{"claude-code", "claude-code", true},
		{"claude-code-nightly", "claude-code", true}, // matches the claude-* wildcard
		{"claude -p", "", false},
		{"claude --print", "", false},
		{"codex", "codex", true},
		{"/usr/local/bin/codex --model gpt-5", "codex", true},
		{"codex-nightly", "codex", true}, // matches the codex-* wildcard
		{"codex exec", "", false},
		{"cursor-agent", "cursor", true},
		{"gemini", "gemini", true},
		{"grok", "grok-build", true},
		{"opencode", "opencode", true},
		{"python worker.py", "", false},
		{"", "", false},
		// Types agmsg itself does not process-detect (detect=explicit in its
		// own type.conf) must never match here either.
		{"agy", "", false}, // antigravity's cli
		{"copilot", "", false},
		{"hermes", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			gotType, gotOK := detectAgmsgAgentType(tc.command)
			assert.Equal(t, tc.wantOK, gotOK)
			assert.Equal(t, tc.wantType, gotType)
		})
	}
}

func TestNewestKnownAgentTypeDescendantPID_FindsClaudeAmongOthers(t *testing.T) {
	processes := []processInfo{
		{PID: 100, PPID: 1, Command: "/bin/zsh"},
		{PID: 120, PPID: 100, Command: "codex exec"}, // headless, excluded
		{PID: 140, PPID: 100, Command: "claude"},
	}

	pid, agmsgType, ok := newestKnownAgentTypeDescendantPID(processes, 100)
	require.True(t, ok)
	assert.Equal(t, 140, pid)
	assert.Equal(t, "claude-code", agmsgType)
}

func TestNewestKnownAgentTypeDescendantPID_FindsGemini(t *testing.T) {
	processes := []processInfo{
		{PID: 100, PPID: 1, Command: "/bin/zsh"},
		{PID: 150, PPID: 100, Command: "gemini"},
	}

	pid, agmsgType, ok := newestKnownAgentTypeDescendantPID(processes, 100)
	require.True(t, ok)
	assert.Equal(t, 150, pid)
	assert.Equal(t, "gemini", agmsgType)
}

func TestNewestKnownAgentTypeDescendantPID_PrefersNewestAcrossTypes(t *testing.T) {
	processes := []processInfo{
		{PID: 100, PPID: 1, Command: "/bin/zsh"},
		{PID: 140, PPID: 100, Command: "claude"},
		{PID: 160, PPID: 100, Command: "opencode"},
	}

	pid, agmsgType, ok := newestKnownAgentTypeDescendantPID(processes, 100)
	require.True(t, ok)
	assert.Equal(t, 160, pid)
	assert.Equal(t, "opencode", agmsgType)
}

func TestNewestKnownAgentTypeDescendantPID_NoneFound(t *testing.T) {
	processes := []processInfo{
		{PID: 100, PPID: 1, Command: "/bin/zsh"},
		{PID: 110, PPID: 100, Command: "git status"},
	}

	pid, agmsgType, ok := newestKnownAgentTypeDescendantPID(processes, 100)
	assert.False(t, ok)
	assert.Zero(t, pid)
	assert.Empty(t, agmsgType)
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

func TestParseCodexSessionCWD_LargeExecCommandRecord(t *testing.T) {
	data := []byte(
		codexExecCommandRecord(
			t,
			"printf '%s' '"+strings.Repeat("x", 70_000)+"'",
			"/tmp/worktree-from-large-command",
		),
	)

	cwd, err := parseCodexSessionCWD(data)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-from-large-command", cwd)
}

func TestParseClaudeProjectCWD_PrefersLatestTrackedFileBackup(t *testing.T) {
	worktreeDir := filepath.Join(t.TempDir(), "panemux-worktree")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))
	targetFile := filepath.Join(worktreeDir, "AGENTS.md")
	require.NoError(t, os.WriteFile(targetFile, []byte("test"), 0600))

	prefix := "{\"type\":\"file-history-snapshot\",\"snapshot\":{\"trackedFileBackups\":"
	data := []byte(
		prefix +
			"{\"/Users/demo/.claude/plans/demo.md\":{},\"" +
			targetFile +
			"\":{}}}}\n",
	)

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, worktreeDir, cwd)
}

func TestParseClaudeProjectCWD_PrefersLatestTopLevelCWD(t *testing.T) {
	data := []byte("" +
		"{\"type\":\"user\",\"cwd\":\"/repo/main\"}\n" +
		"{\"type\":\"assistant\",\"cwd\":\"/tmp/worktree-a\"," +
		"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"hi\"}]}}\n" +
		"{\"type\":\"attachment\",\"cwd\":\"/tmp/worktree-b\"}\n" +
		"{\"type\":\"system\",\"subtype\":\"turn_duration\",\"cwd\":\"/tmp/worktree-c\"}\n")

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-c", cwd)
}

func TestParseClaudeProjectCWD_PrefersTopLevelCWDOverToolPaths(t *testing.T) {
	worktreeDir := filepath.Join(t.TempDir(), "panemux-worktree")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))
	targetFile := filepath.Join(worktreeDir, "AGENTS.md")
	require.NoError(t, os.WriteFile(targetFile, []byte("test"), 0600))

	data := []byte("" +
		"{\"type\":\"assistant\",\"cwd\":\"/repo/main\",\"message\":{\"content\":[" +
		"{\"type\":\"tool_use\",\"name\":\"Read\",\"input\":{\"file_path\":\"" + targetFile + "\"}}" +
		"]}}\n")

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, "/repo/main", cwd)
}

func TestParseClaudeProjectCWD_FallsBackWhenTopLevelCWDMissing(t *testing.T) {
	worktreeDir := filepath.Join(t.TempDir(), "panemux-worktree")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))

	data := []byte(
		"{\"type\":\"assistant\",\"message\":{\"content\":[" +
			"{\"type\":\"tool_use\",\"name\":\"Bash\",\"input\":{\"command\":\"cd " +
			worktreeDir +
			" && git status\"}}]}}\n",
	)

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, worktreeDir, cwd)
}

func TestParseClaudeProjectCWD_PrefersBashCDWorktree(t *testing.T) {
	worktreeDir := filepath.Join(t.TempDir(), "panemux-worktree")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))

	prefix := "{\"type\":\"assistant\",\"message\":{\"content\":["
	data := []byte(
		prefix +
			"{\"type\":\"tool_use\",\"name\":\"Bash\",\"input\":{\"command\":\"cd " +
			worktreeDir +
			" && git status\"}}]}}\n",
	)

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, worktreeDir, cwd)
}

// TestParseClaudeProjectCWD_PrefersBashCDWorktreeOverTopLevelCWD encodes a
// bug reproduced against a real Claude Code transcript: the top-level `cwd`
// field on every record always reflects the CLI's own launch directory, not
// wherever a `Bash` tool call's `cd X && ...` actually executed, because the
// interactive Claude process's own OS-level cwd never changes for its
// lifetime — only the individual tool call records the real execution
// directory. If `cwd` is naively preferred whenever present (as it always
// is in real transcripts), the resolver can never detect a sibling-worktree
// divergence reached via a plain Bash `cd`, mirroring the same class of
// problem already solved for Codex (see claudeBashCDPattern and
// docs/architecture.md's Codex `workdir` precedence).
func TestParseClaudeProjectCWD_PrefersBashCDWorktreeOverTopLevelCWD(t *testing.T) {
	worktreeDir := filepath.Join(t.TempDir(), "panemux-worktree")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))

	data := []byte(
		"{\"type\":\"assistant\",\"cwd\":\"/repo/main\",\"message\":{\"content\":[" +
			"{\"type\":\"tool_use\",\"name\":\"Bash\",\"input\":{\"command\":\"cd " +
			worktreeDir +
			" && git status\"}}]}}\n" +
			"{\"type\":\"assistant\",\"cwd\":\"/repo/main\",\"message\":{\"content\":[" +
			"{\"type\":\"text\",\"text\":\"done\"}]}}\n",
	)

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, worktreeDir, cwd)
}

// TestParseClaudeProjectCWD_BashCDPersistsOverLaterWeakerSignals documents
// an intentional asymmetry: a Bash `cd` target is a durable "the agent has
// moved its base of operations here" signal, so once seen it stays
// authoritative for the rest of the transcript — later top-level `cwd`
// records and later file-touch paths cannot displace it, since neither is
// reliable enough evidence that the agent moved back. Only a later Bash
// `cd` target can replace it. See docs/architecture.md's "Pane Git/PR
// resolution" section.
func TestParseClaudeProjectCWD_BashCDPersistsOverLaterWeakerSignals(t *testing.T) {
	worktreeDir := filepath.Join(t.TempDir(), "panemux-worktree")
	require.NoError(t, os.MkdirAll(worktreeDir, 0755))
	unrelatedFile := filepath.Join(t.TempDir(), "unrelated", "notes.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(unrelatedFile), 0755))

	data := []byte(
		"{\"type\":\"assistant\",\"cwd\":\"/repo/main\",\"message\":{\"content\":[" +
			"{\"type\":\"tool_use\",\"name\":\"Bash\",\"input\":{\"command\":\"cd " +
			worktreeDir +
			" && git status\"}}]}}\n" +
			// A later record's top-level cwd is still pinned to the launch
			// directory, and a later file-touch reads some unrelated file —
			// neither should displace the earlier Bash cd target.
			"{\"type\":\"assistant\",\"cwd\":\"/repo/main\",\"message\":{\"content\":[" +
			"{\"type\":\"tool_use\",\"name\":\"Read\",\"input\":{\"file_path\":\"" +
			unrelatedFile +
			"\"}}]}}\n",
	)

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, worktreeDir, cwd)
}

func TestParseClaudeProjectCWD_LargeTopLevelCWDRecord(t *testing.T) {
	data := []byte(
		"{\"type\":\"assistant\",\"cwd\":\"/tmp/worktree-from-large-record\",\"message\":{\"content\":[" +
			"{\"type\":\"text\",\"text\":\"" + strings.Repeat("x", 70_000) + "\"}" +
			"]}}\n",
	)

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-from-large-record", cwd)
}

func TestParseClaudeProjectCWD_LargeTopLevelCWDRecord_WithoutTrailingNewline(t *testing.T) {
	data := []byte(
		"{\"type\":\"assistant\",\"cwd\":\"/tmp/worktree-without-trailing-newline\",\"message\":{\"content\":[" +
			"{\"type\":\"text\",\"text\":\"" + strings.Repeat("x", 70_000) + "\"}" +
			"]}}",
	)

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-without-trailing-newline", cwd)
}

func TestLatestClaudeTrackedPath_IsDeterministic(t *testing.T) {
	backups := map[string]json.RawMessage{
		"/tmp/repo-main/AGENTS.md":       {},
		"/tmp/repo-worktree/README.md":   {},
		"/Users/demo/.claude/plans/x.md": {},
	}

	first := latestClaudeTrackedPath(backups)
	for range 20 {
		assert.Equal(t, first, latestClaudeTrackedPath(backups))
	}
	assert.Equal(t, "/tmp/repo-worktree/README.md", first)
}

func TestParseClaudeProjectCWD_TrackedBackupTieBreakUsesAlphabeticalLastPath(t *testing.T) {
	dir := t.TempDir()
	mainFile := filepath.Join(dir, "repo-main", "AGENTS.md")
	worktreeFile := filepath.Join(dir, "repo-worktree", "README.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(mainFile), 0755))
	require.NoError(t, os.MkdirAll(filepath.Dir(worktreeFile), 0755))
	require.NoError(t, os.WriteFile(mainFile, []byte("main"), 0600))
	require.NoError(t, os.WriteFile(worktreeFile, []byte("worktree"), 0600))

	data := []byte(
		"{\"type\":\"file-history-snapshot\",\"snapshot\":{\"trackedFileBackups\":{" +
			"\"" + mainFile + "\":{}," +
			"\"" + worktreeFile + "\":{}" +
			"}}}\n",
	)

	cwd, err := parseClaudeProjectCWD(data)
	require.NoError(t, err)
	assert.Equal(t, filepath.Dir(worktreeFile), cwd)
}

func TestLocalSession_DetectInteractiveAgentType_NoPID_Error(t *testing.T) {
	sess := &LocalSession{pid: 0}
	_, _, err := sess.DetectInteractiveAgentType()
	assert.Error(t, err)
}

func TestLocalSession_DetectInteractiveAgentType_Present(t *testing.T) {
	tests := []struct {
		command  string
		wantType string
	}{
		{"claude", "claude-code"},
		{"gemini", "gemini"},
		{"opencode", "opencode"},
	}
	for _, tc := range tests {
		t.Run(tc.command, func(t *testing.T) {
			sess := &LocalSession{pid: 100}

			original := listProcessesFn
			t.Cleanup(func() { listProcessesFn = original })
			listProcessesFn = func() ([]processInfo, error) {
				return []processInfo{
					{PID: 100, PPID: 1, Command: "/bin/zsh"},
					{PID: 110, PPID: 100, Command: tc.command},
				}, nil
			}

			agmsgType, ok, err := sess.DetectInteractiveAgentType()
			require.NoError(t, err)
			require.True(t, ok)
			assert.Equal(t, tc.wantType, agmsgType)
		})
	}
}

func TestLocalSession_DetectInteractiveAgentType_NoKnownAgent_False(t *testing.T) {
	sess := &LocalSession{pid: 100}

	original := listProcessesFn
	t.Cleanup(func() { listProcessesFn = original })
	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 100, PPID: 1, Command: "/bin/zsh"},
			{PID: 110, PPID: 100, Command: "git status"},
		}, nil
	}

	agmsgType, ok, err := sess.DetectInteractiveAgentType()
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, agmsgType)
}

func TestLocalSession_DetectInteractiveAgentType_ListProcessesError_Propagated(t *testing.T) {
	sess := &LocalSession{pid: 100}
	wantErr := errors.New("ps failed")

	original := listProcessesFn
	t.Cleanup(func() { listProcessesFn = original })
	listProcessesFn = func() ([]processInfo, error) { return nil, wantErr }

	_, _, err := sess.DetectInteractiveAgentType()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
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

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Empty(t, cwds)
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

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/panemux-pane-pr-link"}, cwds)
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

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/panemux-pane-pr-link"}, cwds)
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

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/worktree-from-session"}, cwds)
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

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/worktree-from-command"}, cwds)
}

func TestReadCodexSessionCWD_UsesCachedValueWhenFingerprintUnchanged(t *testing.T) {
	originalReadFile := readFileFn
	originalStatFile := statFileFn
	t.Cleanup(func() {
		readFileFn = originalReadFile
		statFileFn = originalStatFile
		resetAgentLogCache()
	})

	path := filepath.Join(t.TempDir(), "session.jsonl")
	content := []byte("{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/tmp/worktree-a\"}}\n")
	var readCount int
	readFileFn = func(name string) ([]byte, error) {
		readCount++
		assert.Equal(t, path, name)
		return content, nil
	}
	statFileFn = func(name string) (os.FileInfo, error) {
		assert.Equal(t, path, name)
		return fakeFileInfo{name: filepath.Base(name), size: int64(len(content)), modTimeUnix: 100}, nil
	}

	cwd, err := readCodexSessionCWD(path)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-a", cwd)

	cwd, err = readCodexSessionCWD(path)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-a", cwd)
	assert.Equal(t, 1, readCount)
}

func TestReadClaudeProjectCWD_ReReadsWhenFingerprintChanges(t *testing.T) {
	originalReadFile := readFileFn
	originalStatFile := statFileFn
	t.Cleanup(func() {
		readFileFn = originalReadFile
		statFileFn = originalStatFile
		resetAgentLogCache()
	})

	path := filepath.Join(t.TempDir(), "session.jsonl")
	contents := [][]byte{
		[]byte(
			"{\"type\":\"assistant\",\"cwd\":\"/tmp/worktree-a\"," +
				"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
		),
		[]byte(
			"{\"type\":\"assistant\",\"cwd\":\"/tmp/worktree-b\"," +
				"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
		),
	}
	readCount := 0
	statCall := 0
	readFileFn = func(name string) ([]byte, error) {
		assert.Equal(t, path, name)
		data := contents[readCount]
		readCount++
		return data, nil
	}
	statFileFn = func(name string) (os.FileInfo, error) {
		assert.Equal(t, path, name)
		info := fakeFileInfo{
			name:        filepath.Base(name),
			size:        int64(len(contents[min(statCall, len(contents)-1)])),
			modTimeUnix: int64(100 + statCall),
		}
		statCall++
		return info, nil
	}

	cwd, err := readClaudeProjectCWD(path)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-a", cwd)

	cwd, err = readClaudeProjectCWD(path)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/worktree-b", cwd)
	assert.Equal(t, 2, readCount)
}

func TestGetActiveWorkdir_PrefersClaudeTranscriptWorktree(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	sessionMetaPath := filepath.Join(homeDir, ".claude", "sessions", "220.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionMetaPath), 0755))
	require.NoError(t, os.WriteFile(sessionMetaPath, []byte(
		`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`,
	), 0600))

	transcriptPath := filepath.Join(homeDir, ".claude", "projects", "-repo-main", "session-123.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(transcriptPath), 0755))
	require.NoError(t, os.WriteFile(transcriptPath, []byte(
		"{\"type\":\"assistant\",\"cwd\":\"/tmp/panemux-worktree\","+
			"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
	), 0600))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
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

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/panemux-worktree"}, cwds)
}

func TestClaudeProjectDirName_NormalizesDots(t *testing.T) {
	assert.Equal(t, "-repo-main", claudeProjectDirName("/repo/main"))
	assert.Equal(
		t,
		"-Users-tomo-chan-development-panemux",
		claudeProjectDirName("/Users/tomo.chan/development/panemux"),
	)
}

func TestGetActiveWorkdir_PrefersClaudeTranscriptWorktree_WithDotInCWD(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	sessionMetaPath := filepath.Join(homeDir, ".claude", "sessions", "220.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionMetaPath), 0755))
	require.NoError(t, os.WriteFile(sessionMetaPath, []byte(
		`{"pid":220,"sessionId":"session-123","cwd":"/Users/tomo.chan/development/panemux"}`,
	), 0600))

	transcriptPath := filepath.Join(
		homeDir,
		".claude",
		"projects",
		"-Users-tomo-chan-development-panemux",
		"session-123.jsonl",
	)
	require.NoError(t, os.MkdirAll(filepath.Dir(transcriptPath), 0755))
	require.NoError(t, os.WriteFile(transcriptPath, []byte(
		"{\"type\":\"assistant\",\"cwd\":\"/tmp/panemux-worktree\","+
			"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
	), 0600))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100, 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/panemux-worktree"}, cwds)
}

func TestGetActiveWorkdir_ClaudeSessionScanSkipsUnreadableMetadata(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	originalReadFile := readFileFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
		readFileFn = originalReadFile
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	badSessionPath := filepath.Join(homeDir, ".claude", "sessions", "100.json")
	goodSessionPath := filepath.Join(homeDir, ".claude", "sessions", "220.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(badSessionPath), 0755))
	require.NoError(t, os.WriteFile(goodSessionPath, []byte(
		`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`,
	), 0600))

	transcriptPath := filepath.Join(homeDir, ".claude", "projects", "-repo-main", "session-123.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(transcriptPath), 0755))
	worktreeFile := filepath.Join(t.TempDir(), "panemux-worktree", "AGENTS.md")
	require.NoError(t, os.MkdirAll(filepath.Dir(worktreeFile), 0755))
	require.NoError(t, os.WriteFile(worktreeFile, []byte("test"), 0600))
	require.NoError(t, os.WriteFile(transcriptPath, []byte(
		"{\"type\":\"file-history-snapshot\",\"snapshot\":{\"trackedFileBackups\":{\""+
			worktreeFile+
			"\":{}}}}\n",
	), 0600))

	readFileFn = func(path string) ([]byte, error) {
		if path == badSessionPath {
			return nil, errors.New("transient read failure")
		}
		return os.ReadFile(path)
	}

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100, 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{filepath.Dir(worktreeFile)}, cwds)
}

func TestGetActiveWorkdir_ClaudeSessionScanRejectsInvalidSessionID(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	sessionMetaPath := filepath.Join(homeDir, ".claude", "sessions", "220.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionMetaPath), 0755))
	require.NoError(t, os.WriteFile(sessionMetaPath, []byte(
		`{"pid":220,"sessionId":"../../etc/shadow","cwd":"/repo/main"}`,
	), 0600))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100, 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main"}, cwds)
}

func setUpClaudeSessionTranscripts(t *testing.T, homeDir string) (sessionMetaPath, transcriptPath string) {
	t.Helper()

	sessionMetaPath = filepath.Join(homeDir, ".claude", "sessions", "220.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionMetaPath), 0755))
	require.NoError(t, os.WriteFile(sessionMetaPath, []byte(
		`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`,
	), 0600))

	transcriptPath = filepath.Join(homeDir, ".claude", "projects", "-repo-main", "session-123.jsonl")
	require.NoError(t, os.MkdirAll(filepath.Dir(transcriptPath), 0755))
	return sessionMetaPath, transcriptPath
}

func claudeAssistantCWDRecord(cwd string) string {
	return "{\"type\":\"assistant\",\"cwd\":\"" + cwd + "\"," +
		"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n"
}

func TestGetActiveWorkdirs_EmptySubagentsDirectory(t *testing.T) {
	// Distinct from TestGetActiveWorkdirs_NoSubagentsDirectory_BackwardCompatible:
	// here the "subagents" directory exists but contains no transcript files,
	// rather than not existing at all.
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	_, transcriptPath := setUpClaudeSessionTranscripts(t, homeDir)
	require.NoError(t, os.WriteFile(
		transcriptPath,
		[]byte(claudeAssistantCWDRecord("/tmp/panemux-worktree")),
		0600,
	))
	subagentsDir := filepath.Join(filepath.Dir(transcriptPath), "session-123", "subagents")
	require.NoError(t, os.MkdirAll(subagentsDir, 0755))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100, 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/panemux-worktree"}, cwds)
}

func TestGetActiveWorkdirs_IncludesSubagentTranscriptWorktree(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	// Parent transcript never leaves the base repo; only a subagent transcript
	// actually visited a sibling worktree, mirroring the real-world case where
	// a delegated Task subagent does the worktree-relative work.
	_, transcriptPath := setUpClaudeSessionTranscripts(t, homeDir)
	require.NoError(t, os.WriteFile(
		transcriptPath,
		[]byte(claudeAssistantCWDRecord("/repo/main")),
		0600,
	))

	subagentsDir := filepath.Join(filepath.Dir(transcriptPath), "session-123", "subagents")
	require.NoError(t, os.MkdirAll(subagentsDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(subagentsDir, "agent-a1.jsonl"),
		[]byte(claudeAssistantCWDRecord("/repo/worktree-a")),
		0600,
	))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100, 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main", "/repo/worktree-a"}, cwds)
}

func TestGetActiveWorkdirs_MultipleSubagentTranscripts_AllDistinctWorktreesReturned(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	_, transcriptPath := setUpClaudeSessionTranscripts(t, homeDir)
	require.NoError(t, os.WriteFile(
		transcriptPath,
		[]byte(claudeAssistantCWDRecord("/repo/main")),
		0600,
	))

	subagentsDir := filepath.Join(filepath.Dir(transcriptPath), "session-123", "subagents")
	require.NoError(t, os.MkdirAll(subagentsDir, 0755))
	require.NoError(t, os.WriteFile(
		filepath.Join(subagentsDir, "agent-a1.jsonl"),
		[]byte(claudeAssistantCWDRecord("/repo/worktree-a")),
		0600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(subagentsDir, "agent-a2.jsonl"),
		[]byte(claudeAssistantCWDRecord("/repo/worktree-b")),
		0600,
	))
	// A subagent that stayed in the same worktree as the parent should not
	// produce a duplicate entry.
	require.NoError(t, os.WriteFile(
		filepath.Join(subagentsDir, "agent-a3.jsonl"),
		[]byte(claudeAssistantCWDRecord("/repo/main")),
		0600,
	))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100, 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main", "/repo/worktree-a", "/repo/worktree-b"}, cwds)
}

func TestGetActiveWorkdirs_StaleSubagentTranscriptStillIncluded(t *testing.T) {
	// Confirms there is no recency/time-window filter: a subagent transcript
	// with an old modification time is still returned as a candidate.
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	_, transcriptPath := setUpClaudeSessionTranscripts(t, homeDir)
	require.NoError(t, os.WriteFile(
		transcriptPath,
		[]byte(claudeAssistantCWDRecord("/repo/main")),
		0600,
	))

	subagentsDir := filepath.Join(filepath.Dir(transcriptPath), "session-123", "subagents")
	require.NoError(t, os.MkdirAll(subagentsDir, 0755))
	oldAgentPath := filepath.Join(subagentsDir, "agent-old.jsonl")
	require.NoError(t, os.WriteFile(
		oldAgentPath,
		[]byte(claudeAssistantCWDRecord("/repo/worktree-old")),
		0600,
	))
	oldTime := time.Now().Add(-90 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(oldAgentPath, oldTime, oldTime))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100, 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main", "/repo/worktree-old"}, cwds)
}

func TestGetActiveWorkdirs_NoSubagentsDirectory_BackwardCompatible(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	_, transcriptPath := setUpClaudeSessionTranscripts(t, homeDir)
	require.NoError(t, os.WriteFile(
		transcriptPath,
		[]byte(claudeAssistantCWDRecord("/tmp/panemux-worktree")),
		0600,
	))
	// No subagents directory is created at all (older session layout).

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100, 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/panemux-worktree"}, cwds)
}

func TestGetActiveWorkdirs_CorruptSubagentTranscriptIgnored(t *testing.T) {
	sess := &LocalSession{pid: 100}

	originalListProcesses := listProcessesFn
	originalGetPIDCWD := getPIDCWDFn
	originalUserHomeDir := userHomeDirFn
	t.Cleanup(func() {
		listProcessesFn = originalListProcesses
		getPIDCWDFn = originalGetPIDCWD
		userHomeDirFn = originalUserHomeDir
	})

	homeDir := t.TempDir()
	userHomeDirFn = func() (string, error) { return homeDir, nil }

	_, transcriptPath := setUpClaudeSessionTranscripts(t, homeDir)
	require.NoError(t, os.WriteFile(
		transcriptPath,
		[]byte(claudeAssistantCWDRecord("/repo/main")),
		0600,
	))

	subagentsDir := filepath.Join(filepath.Dir(transcriptPath), "session-123", "subagents")
	require.NoError(t, os.MkdirAll(subagentsDir, 0755))
	// Empty/corrupt subagent transcripts should be skipped without affecting
	// resolution of the other, well-formed subagent transcript.
	require.NoError(t, os.WriteFile(filepath.Join(subagentsDir, "agent-empty.jsonl"), []byte(""), 0600))
	require.NoError(t, os.WriteFile(
		filepath.Join(subagentsDir, "agent-corrupt.jsonl"),
		[]byte("not json at all\n"),
		0600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(subagentsDir, "agent-good.jsonl"),
		[]byte(claudeAssistantCWDRecord("/repo/worktree-good")),
		0600,
	))

	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 110, PPID: 100, Command: "/bin/zsh"},
			{PID: 220, PPID: 110, Command: "claude"},
		}, nil
	}
	getPIDCWDFn = func(pid int) (string, error) {
		switch pid {
		case 100, 220:
			return "/repo/main", nil
		default:
			return "", errors.New("unexpected pid")
		}
	}

	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main", "/repo/worktree-good"}, cwds)
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

func TestTmuxLocalSession_DetectInteractiveAgentType_ClaudePresent(t *testing.T) {
	prevTmuxOutput := tmuxLocalOutputFn
	prevListProcesses := listProcessesFn
	t.Cleanup(func() {
		tmuxLocalOutputFn = prevTmuxOutput
		listProcessesFn = prevListProcesses
	})

	tmuxLocalOutputFn = func(args ...string) ([]byte, error) {
		if len(args) == 5 && args[4] == "#{pane_pid}" {
			return []byte("220\n"), nil
		}
		return nil, errors.New("unexpected tmux args")
	}
	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{
			{PID: 220, PPID: 1, Command: "/bin/zsh"},
			{PID: 230, PPID: 220, Command: "claude"},
		}, nil
	}

	sess := &TmuxLocalSession{tmuxSession: "demo"}
	agmsgType, ok, err := sess.DetectInteractiveAgentType()
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "claude-code", agmsgType)
}

func TestTmuxLocalSession_DetectInteractiveAgentType_NoKnownAgent_False(t *testing.T) {
	prevTmuxOutput := tmuxLocalOutputFn
	prevListProcesses := listProcessesFn
	t.Cleanup(func() {
		tmuxLocalOutputFn = prevTmuxOutput
		listProcessesFn = prevListProcesses
	})

	tmuxLocalOutputFn = func(args ...string) ([]byte, error) {
		if len(args) == 5 && args[4] == "#{pane_pid}" {
			return []byte("220\n"), nil
		}
		return nil, errors.New("unexpected tmux args")
	}
	listProcessesFn = func() ([]processInfo, error) {
		return []processInfo{{PID: 220, PPID: 1, Command: "/bin/zsh"}}, nil
	}

	sess := &TmuxLocalSession{tmuxSession: "demo"}
	agmsgType, ok, err := sess.DetectInteractiveAgentType()
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Empty(t, agmsgType)
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
	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/worktree"}, cwds)
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
	cwds, err := sess.GetActiveWorkdirs()
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/tmux-worktree-from-command"}, cwds)
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

	cwds, err := tmuxLocalActiveWorkdirs("demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/tmux-root-codex-worktree"}, cwds)
}
