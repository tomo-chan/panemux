package session

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// generateTestKeyFile creates a real ed25519 private key file at the given path
// and returns the path. Used by tests that need a valid SSH key without
// requiring a real SSH server.
func generateTestKeyFile(t *testing.T, path string) {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	block, err := gossh.MarshalPrivateKey(priv, "")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, pem.EncodeToMemory(block), 0600))
}

// TestBuildAuthMethods_WithKeyFile verifies that a valid key file produces an auth method.
func TestBuildAuthMethods_WithKeyFile(t *testing.T) {
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	generateTestKeyFile(t, keyPath)

	cfg := SSHConfig{KeyFile: keyPath}
	methods, err := buildAuthMethods(cfg)
	require.NoError(t, err)
	assert.Len(t, methods, 1)
}

// TestBuildAuthMethods_NoKeyNoPassword_NoDefaultKeys_Error verifies that when
// neither KeyFile nor Password is set and no default keys exist, an error is returned.
// This is the case that caused the 500 on Restart Session when ~/.ssh/config
// entries don't specify IdentityFile.
func TestBuildAuthMethods_NoKeyNoPassword_NoDefaultKeys_Error(t *testing.T) {
	// Override HOME to a temp dir with no .ssh keys
	t.Setenv("HOME", t.TempDir())

	cfg := SSHConfig{}
	_, err := buildAuthMethods(cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no auth methods")
}

// TestBuildAuthMethods_NoKeyNoPassword_DefaultKeyFound verifies that when no
// explicit KeyFile is set but a default key exists at ~/.ssh/id_ed25519,
// it is used automatically (mirrors OpenSSH behavior).
// This tests the fix for the case where ~/.ssh/config entries omit IdentityFile.
func TestBuildAuthMethods_NoKeyNoPassword_DefaultKeyFound(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))

	generateTestKeyFile(t, filepath.Join(sshDir, "id_ed25519"))
	t.Setenv("HOME", home)

	cfg := SSHConfig{}
	methods, err := buildAuthMethods(cfg)
	require.NoError(t, err)
	assert.Len(t, methods, 1)
}

// TestValidRemotePath_* cover the regex guard that prevents command injection
// via the working directory field (CodeQL go/command-injection pattern).

func TestValidRemotePath_AbsoluteOK(t *testing.T) {
	for _, p := range []string{
		"/home/user/projects",
		"/tmp",
		"/var/log/my app", // space is allowed
		"/データ/プロジェクト",     // unicode allowed
		"/home/user_name/file-name.txt",
	} {
		assert.True(t, validRemotePath.MatchString(p), "expected %q to be valid", p)
	}
}

func TestValidRemotePath_Rejected(t *testing.T) {
	for _, p := range []string{
		"relative/path",              // not absolute
		"",                           // empty
		"/tmp/$(evil)",               // command substitution $()
		"/tmp/`evil`",                // backtick substitution
		"/tmp/'; rm -rf /; echo '",   // single-quote injection
		"/tmp/\"; rm -rf /; echo \"", // double-quote injection
		"/tmp/a;b",                   // semicolon
		"/tmp/a|b",                   // pipe
		"/tmp/a&b",                   // background
		"/tmp/a\x00b",                // null byte
		"/tmp/a\nb",                  // newline
	} {
		assert.False(t, validRemotePath.MatchString(p), "expected %q to be rejected", p)
	}
}

func TestShellQuotePath_Simple(t *testing.T) {
	assert.Equal(t, "'/home/user/projects'", shellQuotePath("/home/user/projects"))
}

func TestShellQuotePath_WithSpaces(t *testing.T) {
	assert.Equal(t, "'/home/user/my project'", shellQuotePath("/home/user/my project"))
}

func TestShellQuotePath_WithSingleQuote(t *testing.T) {
	// /home/user/it's → '/home/user/it'\''s'
	assert.Equal(t, `'/home/user/it'\''s'`, shellQuotePath("/home/user/it's"))
}

func TestShellQuotePath_Empty(t *testing.T) {
	assert.Equal(t, "''", shellQuotePath(""))
}

func TestSubstituteProxyCommand_Substitutions(t *testing.T) {
	cmd := "gcloud compute start-iap-tunnel %h %p --listen-on-stdin"
	got := substituteProxyCommand(cmd, "myhost.example.com", 22)
	assert.Equal(t, "gcloud compute start-iap-tunnel myhost.example.com 22 --listen-on-stdin", got)
}

func TestSubstituteProxyCommand_PercentEscape(t *testing.T) {
	got := substituteProxyCommand("echo %%h is not %h", "host", 22)
	assert.Equal(t, "echo %h is not host", got)
}

func TestSubstituteProxyCommand_NoTokens(t *testing.T) {
	cmd := "nc -q0 bastion 22"
	got := substituteProxyCommand(cmd, "unused", 0)
	assert.Equal(t, cmd, got)
}

func TestBuildHostKeyCallback_NonexistentFile_Error(t *testing.T) {
	_, _, err := buildHostKeyCallback("/nonexistent/path/known_hosts")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "known_hosts")
}

func TestBuildHostKeyCallback_ValidFile_NoError(t *testing.T) {
	dir := t.TempDir()
	knownHostsPath := filepath.Join(dir, "known_hosts")
	require.NoError(t, os.WriteFile(knownHostsPath, []byte(""), 0600))

	_, resolvedPath, err := buildHostKeyCallback(knownHostsPath)
	assert.NoError(t, err)
	assert.Equal(t, knownHostsPath, resolvedPath)
}

func generateTestPublicKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	pub, err := gossh.NewPublicKey(priv.Public())
	require.NoError(t, err)
	return pub
}

func generateTestECDSAPublicKey(t *testing.T) gossh.PublicKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	pub, err := gossh.NewPublicKey(priv.Public())
	require.NoError(t, err)
	return pub
}

func TestKnownHostsAlgorithms(t *testing.T) {
	pub1 := generateTestPublicKey(t)
	pub2 := generateTestECDSAPublicKey(t)
	hashedHost := knownhosts.HashHostname("hashed.example.com")

	tests := []struct {
		name    string
		addr    string
		content string
		want    []string
	}{
		{
			name:    "plaintext host",
			addr:    "plain.example.com:22",
			content: knownhosts.Line([]string{"plain.example.com"}, pub1) + "\n",
			want:    []string{pub1.Type()},
		},
		{
			name:    "bracketed host and port",
			addr:    "port.example.com:2200",
			content: knownhosts.Line([]string{"[port.example.com]:2200"}, pub1) + "\n",
			want:    []string{pub1.Type()},
		},
		{
			name: "multiple matching algorithms preserve file order and dedupe",
			addr: "multi.example.com:22",
			content: knownhosts.Line([]string{"multi.example.com"}, pub1) + "\n" +
				knownhosts.Line([]string{"multi.example.com"}, pub1) + "\n" +
				knownhosts.Line([]string{"multi.example.com"}, pub2) + "\n",
			want: []string{pub1.Type(), pub2.Type()},
		},
		{
			name:    "hashed host",
			addr:    "hashed.example.com:22",
			content: knownhosts.Line([]string{hashedHost}, pub1) + "\n",
			want:    []string{pub1.Type()},
		},
		{
			name: "ignores unmatched and malformed entries",
			addr: "target.example.com:22",
			content: "# comment\n" +
				"@cert-authority *.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZm\n" +
				"!target.example.com ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZm\n" +
				"|1|bad|entry ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZmZm\n" +
				knownhosts.Line([]string{"other.example.com"}, pub1) + "\n",
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			knownHostsPath := filepath.Join(dir, "known_hosts")
			require.NoError(t, os.WriteFile(knownHostsPath, []byte(tt.content), 0600))
			assert.Equal(t, tt.want, knownHostsAlgorithms(knownHostsPath, tt.addr))
		})
	}
}

func TestKnownHostsAlgorithms_FileNotFound_ReturnsEmpty(t *testing.T) {
	assert.Empty(t, knownHostsAlgorithms("/nonexistent/known_hosts", "myhost:22"))
}

// TestSSHGetCWDCmd_* verify that the CWD-detection shell command embeds the
// correct techniques so reviewers can confirm the logic without running a
// live SSH server.

func TestSSHGetCWDCmd_UsesPgrep(t *testing.T) {
	assert.Contains(t, sshGetCWDCmd, "pgrep -P $PPID -o")
}

func TestSSHGetCWDCmd_ReadsProc(t *testing.T) {
	assert.Contains(t, sshGetCWDCmd, "readlink /proc/$PID/cwd")
}

func TestSSHGetCWDCmd_ReadsLsof(t *testing.T) {
	assert.Contains(t, sshGetCWDCmd, "lsof -a -p $PID -d cwd -Fn")
}

func TestSSHGetCWDCmd_FallsBackToPwd(t *testing.T) {
	assert.Contains(t, sshGetCWDCmd, "|| pwd")
}

func TestRemoteDirectoryListCommand_DoesNotUseShellGlobs(t *testing.T) {
	cmd := remoteDirectoryListCommand("/home/user/project", false)
	assert.NotContains(t, cmd, "./*")
	assert.Contains(t, cmd, "find . -mindepth 1 -maxdepth 1 -type d -print")
	assert.NotContains(t, cmd, "do;")
}

func TestRemoteDirectoryListCommand_FiltersHiddenDirectoriesWithoutGlobs(t *testing.T) {
	cmd := remoteDirectoryListCommand("/home/user/project", false)
	assert.Contains(t, cmd, `[ "${name#*.}" != "$name" ]`)
}

func TestParseRemoteDirectoryListOutput_EmptyDirectory(t *testing.T) {
	resolvedPath, entries, err := parseRemoteDirectoryListOutput([]byte("/home/user/project\n"))
	require.NoError(t, err)
	assert.Equal(t, "/home/user/project", resolvedPath)
	assert.Empty(t, entries)
}

// TestSSHConfig_ShellField verifies the Shell field exists on SSHConfig.
func TestSSHConfig_ShellField(t *testing.T) {
	cfg := SSHConfig{
		Host:  "example.com",
		Shell: "/usr/bin/zsh",
	}
	assert.Equal(t, "/usr/bin/zsh", cfg.Shell)
}

func TestSSHSessionConnectionName(t *testing.T) {
	// SSHSession.connectionName is set from SSHConfig.ConnectionName.
	// We test the getter directly via an unexported struct field since
	// NewSSH requires a live server — construct the struct manually.
	s := &SSHSession{connectionName: "my-server"}
	assert.Equal(t, "my-server", s.ConnectionName())
}

func TestTmuxSSHSessionConnectionName(t *testing.T) {
	s := &TmuxSSHSession{connectionName: "remote-box"}
	assert.Equal(t, "remote-box", s.ConnectionName())
}

func TestClassifySSHWaitError_ExitedOnNil(t *testing.T) {
	assert.Equal(t, StateExited, classifySSHWaitError(nil))
}

func TestClassifySSHWaitError_ExitedOnExitError(t *testing.T) {
	err := &gossh.ExitError{Waitmsg: gossh.Waitmsg{}}
	assert.Equal(t, StateExited, classifySSHWaitError(err))
}

func TestClassifySSHWaitError_ExitedOnExitMissingError(t *testing.T) {
	err := &gossh.ExitMissingError{}
	assert.Equal(t, StateExited, classifySSHWaitError(err))
}

func TestClassifySSHWaitError_DisconnectedOnTransportError(t *testing.T) {
	assert.Equal(t, StateDisconnected, classifySSHWaitError(io.EOF))
}

func TestNewTmuxSSH_InvalidSessionName_Error(t *testing.T) {
	cfg := SSHConfig{
		Host:     "127.0.0.1",
		Port:     22,
		User:     "user",
		Password: "pass",
	}
	_, err := NewTmuxSSH("id", "title", "foo;bar$(evil)", cfg)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid tmux session name")
}

type fakeSSHRunner struct {
	outputs map[string][]byte
	errs    map[string]error
}

func (f *fakeSSHRunner) Output(cmd string) ([]byte, error) {
	if err := f.errs[cmd]; err != nil {
		return nil, err
	}
	if out, ok := f.outputs[cmd]; ok {
		return out, nil
	}
	return nil, errors.New("unexpected command: " + cmd)
}

func (f *fakeSSHRunner) Close() error { return nil }

func TestActiveRemoteWorkdir_IgnoresNonInteractiveAgents(t *testing.T) {
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd: []byte(" 100 1 sh\n 120 100 codex exec\n 130 100 claude -p\n"),
		},
	}

	cwd, err := activeRemoteWorkdir(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Empty(t, cwd)
}

func TestActiveRemoteWorkdir_PrefersRemoteCodexSessionCWD(t *testing.T) {
	sessionPath := "/home/user/.codex/sessions/2026/05/17/session.jsonl"
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:                       []byte(" 100 1 sh\n 220 100 codex resume --last\n"),
			fmt.Sprintf(sshOpenFilesCmdTemplate, 220): []byte(sessionPath + "\n"),
			"cat " + shellQuotePath(sessionPath): []byte(
				"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
					"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/tmp/remote-worktree\"}}\n",
			),
		},
	}

	cwd, err := activeRemoteWorkdir(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-worktree", cwd)
}

func TestActiveRemoteWorkdir_PrefersRemoteCodexExecCommandWorkdir(t *testing.T) {
	sessionPath := "/home/user/.codex/sessions/2026/05/17/session.jsonl"
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:                       []byte(" 100 1 sh\n 220 100 codex resume --last\n"),
			fmt.Sprintf(sshOpenFilesCmdTemplate, 220): []byte(sessionPath + "\n"),
			"cat " + shellQuotePath(sessionPath): []byte(
				"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
					"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
					codexExecCommandRecord(t, "git status", "/tmp/remote-worktree-from-command"),
			),
		},
	}

	cwd, err := activeRemoteWorkdir(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-worktree-from-command", cwd)
}

func TestActiveRemoteWorkdir_FallsBackToRemoteDescendantCWD(t *testing.T) {
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:                       []byte(" 100 1 sh\n 220 100 codex\n 230 220 node helper\n"),
			fmt.Sprintf(sshOpenFilesCmdTemplate, 220): []byte(""),
			fmt.Sprintf(sshPIDCWDCmdTemplate, 230):    []byte("/tmp/remote-worktree\n"),
			fmt.Sprintf(sshPIDCWDCmdTemplate, 220):    []byte("/repo/main\n"),
		},
	}

	cwd, err := activeRemoteWorkdir(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-worktree", cwd)
}

func TestActiveRemoteWorkdir_PrefersRemoteClaudeTranscriptWorktree(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectPath := "~/.claude/projects/-repo-main/session-123.jsonl"
	worktreeFile := "/tmp/remote-claude-worktree/AGENTS.md"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:    []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath:   []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			"cat " + shellQuotePath(projectPath): []byte(
				"{\"type\":\"file-history-snapshot\",\"snapshot\":{\"trackedFileBackups\":{\"" +
					worktreeFile +
					"\":{}}}}\n",
			),
		},
	}

	cwd, err := activeRemoteWorkdir(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-claude-worktree", cwd)
}

func TestActiveRemoteWorkdir_PrefersRemoteClaudeBashCDWorktree(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectPath := "~/.claude/projects/-repo-main/session-123.jsonl"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:    []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath:   []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			"cat " + shellQuotePath(projectPath): []byte(
				"{\"type\":\"assistant\",\"message\":{\"content\":[" +
					"{\"type\":\"tool_use\",\"name\":\"Bash\",\"input\":{\"command\":\"cd /tmp/remote-bash-worktree && git status\"}}" +
					"]}}\n",
			),
		},
	}

	cwd, err := activeRemoteWorkdir(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-bash-worktree", cwd)
}

func TestRemoteShellPID_ParsesShellProcess(t *testing.T) {
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshShellPIDCmd: []byte("220\n"),
		},
	}

	pid, err := remoteShellPID(runner)
	require.NoError(t, err)
	assert.Equal(t, 220, pid)
}

func TestActiveRemoteWorkdirFromSessionFactory_UsesSeparateRunners(t *testing.T) {
	outputs := map[string][]byte{
		sshShellPIDCmd:      []byte("220\n"),
		sshListProcessesCmd: []byte(" 220 1 zsh\n 230 220 codex resume --last\n"),
		fmt.Sprintf(sshOpenFilesCmdTemplate, 230): []byte(""),
		fmt.Sprintf(sshPIDCWDCmdTemplate, 230):    []byte("/tmp/remote-worktree\n"),
	}
	index := 0
	factory := func() (sshSessionRunner, error) {
		index++
		return &fakeSSHRunner{outputs: outputs}, nil
	}

	cwd, err := activeRemoteWorkdirFromSessionFactory(
		factory,
		"test active remote",
		"/repo/main",
	)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-worktree", cwd)
	// Separate runners are used for: shell PID, process list, open-files probe,
	// and PID cwd fallback.
	assert.Equal(t, 4, index)
}

func TestActiveRemoteWorkdir_RootPIDScopesRemoteAgents(t *testing.T) {
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:                       []byte(" 100 1 sh\n 200 1 codex\n 220 100 codex\n 230 220 node helper\n"),
			fmt.Sprintf(sshOpenFilesCmdTemplate, 220): []byte(""),
			fmt.Sprintf(sshPIDCWDCmdTemplate, 230):    []byte("/tmp/remote-worktree\n"),
			fmt.Sprintf(sshPIDCWDCmdTemplate, 220):    []byte("/repo/main\n"),
		},
	}

	cwd, err := activeRemoteWorkdir(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-worktree", cwd)
}

func TestTmuxSSHActiveWorkdir_UsesPanePIDAndBaseCWD(t *testing.T) {
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			"tmux display-message -p -t 'demo' '#{pane_pid}\t#{pane_current_path}'": []byte("220\t/repo/main\n"),
			sshListProcessesCmd:                       []byte(" 220 1 zsh\n 230 220 codex\n 240 230 node helper\n"),
			fmt.Sprintf(sshOpenFilesCmdTemplate, 230): []byte(""),
			fmt.Sprintf(sshPIDCWDCmdTemplate, 240):    []byte("/tmp/remote-worktree\n"),
			fmt.Sprintf(sshPIDCWDCmdTemplate, 230):    []byte("/repo/main\n"),
		},
	}

	cwd, err := tmuxSSHActiveWorkdir(runner, "test ssh_tmux", "demo")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-worktree", cwd)
}

func TestTmuxSSHActiveWorkdir_PrefersCodexExecCommandWorkdir(t *testing.T) {
	sessionPath := "/home/user/.codex/sessions/2026/05/17/session.jsonl"
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			"tmux display-message -p -t 'demo' '#{pane_pid}\t#{pane_current_path}'": []byte("220\t/repo/main\n"),
			sshListProcessesCmd:                       []byte(" 220 1 zsh\n 230 220 codex resume --last\n"),
			fmt.Sprintf(sshOpenFilesCmdTemplate, 230): []byte(sessionPath + "\n"),
			"cat " + shellQuotePath(sessionPath): []byte(
				"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
					"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
					codexExecCommandRecord(t, "go test ./...", "/tmp/remote-tmux-worktree-from-command"),
			),
		},
	}

	cwd, err := tmuxSSHActiveWorkdir(runner, "test ssh_tmux", "demo")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-tmux-worktree-from-command", cwd)
}

func TestTmuxSSHActiveWorkdir_PrefersClaudeTranscriptWorktree(t *testing.T) {
	sessionPath := "~/.claude/sessions/230.json"
	projectPath := "~/.claude/projects/-repo-main/session-123.jsonl"
	worktreeFile := "/tmp/remote-tmux-claude-worktree/AGENTS.md"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			"tmux display-message -p -t 'demo' '#{pane_pid}\t#{pane_current_path}'": []byte("220\t/repo/main\n"),
			sshListProcessesCmd:  []byte(" 220 1 zsh\n 230 220 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":230,"sessionId":"session-123","cwd":"/repo/main"}`),
			"cat " + shellQuotePath(projectPath): []byte(
				"{\"type\":\"file-history-snapshot\",\"snapshot\":{\"trackedFileBackups\":{\"" +
					worktreeFile +
					"\":{}}}}\n",
			),
		},
	}

	cwd, err := tmuxSSHActiveWorkdir(runner, "test ssh_tmux", "demo")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-tmux-claude-worktree", cwd)
}

func TestTmuxSSHActiveWorkdir_WhenPanePIDIsCodex_PrefersCodexExecCommandWorkdir(t *testing.T) {
	sessionPath := "/home/user/.codex/sessions/2026/05/17/session.jsonl"
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			"tmux display-message -p -t 'demo' '#{pane_pid}\t#{pane_current_path}'": []byte("230\t/repo/main\n"),
			sshListProcessesCmd:                       []byte(" 230 1 codex resume --last\n"),
			fmt.Sprintf(sshOpenFilesCmdTemplate, 230): []byte(sessionPath + "\n"),
			"cat " + shellQuotePath(sessionPath): []byte(
				"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
					"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
					codexExecCommandRecord(t, "go test ./...", "/tmp/remote-root-codex-worktree"),
			),
		},
	}

	cwd, err := tmuxSSHActiveWorkdir(runner, "test ssh_tmux", "demo")
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-root-codex-worktree", cwd)
}

func TestTmuxSSHActiveWorkdirFromSessionFactory_UsesSeparateRunners(t *testing.T) {
	outputs := map[string][]byte{
		"tmux display-message -p -t 'demo' '#{pane_pid}\t#{pane_current_path}'": []byte("220\t/repo/main\n"),
		sshListProcessesCmd:                       []byte(" 220 1 zsh\n 230 220 codex resume --last\n"),
		fmt.Sprintf(sshOpenFilesCmdTemplate, 230): []byte(""),
		fmt.Sprintf(sshPIDCWDCmdTemplate, 230):    []byte("/tmp/remote-worktree\n"),
	}
	index := 0
	factory := func() (sshSessionRunner, error) {
		index++
		return &fakeSSHRunner{outputs: outputs}, nil
	}

	cwd, err := tmuxSSHActiveWorkdirFromSessionFactory(
		factory,
		"test ssh_tmux",
		"demo",
	)
	require.NoError(t, err)
	assert.Equal(t, "/tmp/remote-worktree", cwd)
	// Separate runners are used for: tmux pane info, process list, open-files
	// probe, and PID cwd fallback.
	assert.Equal(t, 4, index)
}

func TestRemoteGitContext_ReturnsBranchAndRepo(t *testing.T) {
	const cwd = "/home/user/panemux"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			fmt.Sprintf(sshGitContextCmdTemplate, shellQuotePath(cwd)): []byte(
				"/home/user/panemux\n" +
					"/home/user/panemux/.git\n" +
					"main\n" +
					"git@github.com:example/panemux.git\n",
			),
		},
	}

	ctx, err := remoteGitContext(runner, cwd)
	require.NoError(t, err)
	assert.Equal(t, "main", ctx.Branch)
	assert.Equal(t, "/home/user/panemux/.git", ctx.CommonDir)
	assert.Equal(t, "git@github.com:example/panemux.git", ctx.OriginURL)
	assert.Equal(t, "panemux", ctx.Repo)
	assert.Equal(t, cwd, ctx.Root)
}

func TestRemoteGitContext_RejectsRelativePath(t *testing.T) {
	_, err := remoteGitContext(&fakeSSHRunner{}, "panemux")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "working directory")
}
