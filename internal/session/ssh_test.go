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
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

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
		return f.outputs[cmd], err
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

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Empty(t, cwds)
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

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-worktree"}, cwds)
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

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-worktree-from-command"}, cwds)
}

func TestActiveRemoteWorkdir_SkipsRemoteCodexLogReadWhenFingerprintUnchanged(t *testing.T) {
	resetAgentLogCache()
	t.Cleanup(resetAgentLogCache)

	sessionPath := "/home/user/.codex/sessions/2026/05/17/session.jsonl"
	shellPath := shellQuotePath(sessionPath)
	fingerprintCmd := remoteFileFingerprintCmd(shellPath)
	catCmd := "cat " + shellQuotePath(sessionPath)
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:                       []byte(" 100 1 sh\n 220 100 codex resume --last\n"),
			fmt.Sprintf(sshOpenFilesCmdTemplate, 220): []byte(sessionPath + "\n"),
			fingerprintCmd:                            []byte("42 1700000000\n"),
			catCmd: []byte(
				"{\"type\":\"session_meta\",\"payload\":{\"cwd\":\"/repo/main\"}}\n" +
					"{\"type\":\"turn_context\",\"payload\":{\"cwd\":\"/tmp/remote-worktree\"}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-worktree"}, cwds)

	delete(runner.outputs, catCmd)
	cwds, err = activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-worktree"}, cwds)
}

func TestActiveRemoteWorkdir_ReReadsRemoteClaudeTranscriptWhenFingerprintChanges(t *testing.T) {
	resetAgentLogCache()
	t.Cleanup(resetAgentLogCache)

	projectShellPath := "~/.claude/projects/'-repo-main/session-123.jsonl'"
	projectCmd := "cat " + projectShellPath
	fingerprintCmd := remoteFileFingerprintCmd(projectShellPath)
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:               []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat ~/.claude/sessions/220.json": []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			fingerprintCmd:                    []byte("100 1700000000\n"),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/tmp/remote-claude-worktree-a\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-claude-worktree-a"}, cwds)

	runner.outputs[fingerprintCmd] = []byte("101 1700000001\n")
	runner.outputs[projectCmd] = []byte(
		"{\"type\":\"assistant\",\"cwd\":\"/tmp/remote-claude-worktree-b\"," +
			"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
	)
	cwds, err = activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-claude-worktree-b"}, cwds)
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

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-worktree"}, cwds)
}

func TestActiveRemoteWorkdir_PrefersRemoteClaudeTranscriptWorktree(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectCmd := "cat ~/.claude/projects/'-repo-main/session-123.jsonl'"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:  []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/tmp/remote-claude-worktree\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-claude-worktree"}, cwds)
}

func TestActiveRemoteWorkdir_PrefersRemoteClaudeTranscriptWorktree_WithDotInCWD(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectCmd := "cat ~/.claude/projects/'-home-tomo-chan-repo-main/session-123.jsonl'"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:  []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":220,"sessionId":"session-123","cwd":"/home/tomo.chan/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/tmp/remote-claude-worktree\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-claude-worktree"}, cwds)
}

// TestActiveRemoteWorkdir_PrefersRemoteClaudeTopLevelCWDOverToolPaths
// asserts that top-level `cwd` still wins over a *file-touch* tool path
// (Read/Edit/Write/etc), which remains a weaker signal than `cwd`: touching
// a single unrelated file elsewhere does not by itself mean the agent moved
// its active work there. This is distinct from a Bash `cd` target, which is
// a stronger, explicit directory-change signal — see
// TestActiveRemoteWorkdir_PrefersRemoteClaudeBashCDWorktreeOverTopLevelCWD.
func TestActiveRemoteWorkdir_PrefersRemoteClaudeTopLevelCWDOverToolPaths(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectCmd := "cat ~/.claude/projects/'-repo-main/session-123.jsonl'"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:  []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/main\",\"message\":{\"content\":[" +
					"{\"type\":\"tool_use\",\"name\":\"Read\"," +
					"\"input\":{\"file_path\":\"/tmp/remote-unrelated-file.txt\"}}" +
					"]}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main"}, cwds)
}

// TestActiveRemoteWorkdir_PrefersRemoteClaudeBashCDWorktreeOverTopLevelCWD
// encodes the bug reproduced against a real Claude Code transcript: the
// top-level `cwd` field is set once from the interactive process's own
// OS-level working directory and never changes for its lifetime, so it
// cannot by itself signal that a Bash tool call `cd`'d into a sibling
// worktree. See parseClaudeProjectCWD (local.go) for the full reasoning;
// this is the same resolver used for both local and remote transcripts.
func TestActiveRemoteWorkdir_PrefersRemoteClaudeBashCDWorktreeOverTopLevelCWD(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectCmd := "cat ~/.claude/projects/'-repo-main/session-123.jsonl'"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:  []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/main\",\"message\":{\"content\":[" +
					"{\"type\":\"tool_use\",\"name\":\"Bash\"," +
					"\"input\":{\"command\":\"cd /tmp/remote-bash-worktree && git status\"}}" +
					"]}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-bash-worktree"}, cwds)
}

func TestActiveRemoteWorkdir_PrefersRemoteClaudeBashCDWorktree(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectCmd := "cat ~/.claude/projects/'-repo-main/session-123.jsonl'"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:  []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"message\":{\"content\":[" +
					"{\"type\":\"tool_use\",\"name\":\"Bash\"," +
					"\"input\":{\"command\":\"cd /tmp/remote-bash-worktree && git status\"}}" +
					"]}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-bash-worktree"}, cwds)
}

func TestActiveRemoteWorkdir_RejectsRemoteClaudeInvalidSessionID(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:                    []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath:                   []byte(`{"pid":220,"sessionId":"../../etc/shadow","cwd":"/repo/main"}`),
			fmt.Sprintf(sshPIDCWDCmdTemplate, 220): []byte("/repo/main\n"),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main"}, cwds)
}

func TestActiveRemoteWorkdir_IncludesSubagentTranscriptWorktree(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectCmd := "cat ~/.claude/projects/'-repo-main/session-123.jsonl'"
	subagentsListCmd := "ls -1 ~/.claude/projects/'-repo-main/session-123/subagents' 2>/dev/null || true"
	subagentCmd := "cat ~/.claude/projects/'-repo-main/session-123/subagents/agent-a1.jsonl'"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:  []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/main\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
			subagentsListCmd: []byte("agent-a1.jsonl\n"),
			subagentCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/worktree-a\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main", "/repo/worktree-a"}, cwds)
}

func TestActiveRemoteWorkdir_MultipleSubagentTranscripts_AllDistinctWorktreesReturned(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectCmd := "cat ~/.claude/projects/'-repo-main/session-123.jsonl'"
	subagentsListCmd := "ls -1 ~/.claude/projects/'-repo-main/session-123/subagents' 2>/dev/null || true"
	subagentACmd := "cat ~/.claude/projects/'-repo-main/session-123/subagents/agent-a1.jsonl'"
	subagentBCmd := "cat ~/.claude/projects/'-repo-main/session-123/subagents/agent-a2.jsonl'"
	subagentCCmd := "cat ~/.claude/projects/'-repo-main/session-123/subagents/agent-a3.jsonl'"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:  []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/main\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
			subagentsListCmd: []byte("agent-a1.jsonl\nagent-a2.jsonl\nagent-a3.jsonl\n"),
			subagentACmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/worktree-a\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
			subagentBCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/worktree-b\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
			// A subagent that stayed in the same worktree as the parent should
			// not produce a duplicate entry.
			subagentCCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/main\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main", "/repo/worktree-a", "/repo/worktree-b"}, cwds)
}

func TestActiveRemoteWorkdir_NoSubagentsDirectory_BackwardCompatible(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectCmd := "cat ~/.claude/projects/'-repo-main/session-123.jsonl'"
	subagentsListCmd := "ls -1 ~/.claude/projects/'-repo-main/session-123/subagents' 2>/dev/null || true"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:  []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/tmp/remote-claude-worktree\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
			// The remote "|| true" makes a missing subagents directory look like
			// an empty, successful listing rather than a command error.
			subagentsListCmd: []byte(""),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-claude-worktree"}, cwds)
}

func TestActiveRemoteWorkdir_CorruptSubagentTranscriptIgnored(t *testing.T) {
	sessionPath := "~/.claude/sessions/220.json"
	projectCmd := "cat ~/.claude/projects/'-repo-main/session-123.jsonl'"
	subagentsListCmd := "ls -1 ~/.claude/projects/'-repo-main/session-123/subagents' 2>/dev/null || true"
	corruptCmd := "cat ~/.claude/projects/'-repo-main/session-123/subagents/agent-corrupt.jsonl'"
	goodCmd := "cat ~/.claude/projects/'-repo-main/session-123/subagents/agent-good.jsonl'"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			sshListProcessesCmd:  []byte(" 100 1 sh\n 220 100 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":220,"sessionId":"session-123","cwd":"/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/main\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
			subagentsListCmd: []byte("agent-corrupt.jsonl\nagent-good.jsonl\n"),
			corruptCmd:       []byte("not json at all\n"),
			goodCmd: []byte(
				"{\"type\":\"assistant\",\"cwd\":\"/repo/worktree-good\"," +
					"\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			),
		},
	}

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/repo/main", "/repo/worktree-good"}, cwds)
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

	cwds, err := activeRemoteWorkdirsFromSessionFactory(
		factory,
		"test active remote",
		"/repo/main",
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-worktree"}, cwds)
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

	cwds, err := activeRemoteWorkdirs(runner, "test active remote", "/repo/main", 100)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-worktree"}, cwds)
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

	cwds, err := tmuxSSHActiveWorkdirs(runner, "test ssh_tmux", "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-worktree"}, cwds)
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

	cwds, err := tmuxSSHActiveWorkdirs(runner, "test ssh_tmux", "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-tmux-worktree-from-command"}, cwds)
}

func TestTmuxSSHActiveWorkdir_PrefersClaudeTranscriptWorktree(t *testing.T) {
	sessionPath := "~/.claude/sessions/230.json"
	projectCmd := "cat ~/.claude/projects/'-repo-main/session-123.jsonl'"
	worktreeFile := "/tmp/remote-tmux-claude-worktree/AGENTS.md"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			"tmux display-message -p -t 'demo' '#{pane_pid}\t#{pane_current_path}'": []byte("220\t/repo/main\n"),
			sshListProcessesCmd:  []byte(" 220 1 zsh\n 230 220 claude\n"),
			"cat " + sessionPath: []byte(`{"pid":230,"sessionId":"session-123","cwd":"/repo/main"}`),
			projectCmd: []byte(
				"{\"type\":\"file-history-snapshot\",\"snapshot\":{\"trackedFileBackups\":{\"" +
					worktreeFile +
					"\":{}}}}\n",
			),
		},
	}

	cwds, err := tmuxSSHActiveWorkdirs(runner, "test ssh_tmux", "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-tmux-claude-worktree"}, cwds)
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

	cwds, err := tmuxSSHActiveWorkdirs(runner, "test ssh_tmux", "demo")
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-root-codex-worktree"}, cwds)
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

	cwds, err := tmuxSSHActiveWorkdirsFromSessionFactory(
		factory,
		"test ssh_tmux",
		"demo",
	)
	require.NoError(t, err)
	assert.Equal(t, []string{"/tmp/remote-worktree"}, cwds)
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
	var gitErr *GitContextError
	require.ErrorAs(t, err, &gitErr)
	assert.Equal(t, GitContextCauseInvalidCWD, gitErr.Cause)
	assert.Equal(t, "validate remote working directory", gitErr.Operation)
}

func TestRemoteGitContext_NotGitRepoReturnsDiagnosticError(t *testing.T) {
	const cwd = "/home/user"

	runner := &fakeSSHRunner{
		errs: map[string]error{
			fmt.Sprintf(sshGitContextCmdTemplate, shellQuotePath(cwd)): errors.New("Process exited with status 128"),
		},
		outputs: map[string][]byte{
			fmt.Sprintf(sshGitContextCmdTemplate, shellQuotePath(cwd)): []byte(
				"__PANEMUX_GIT_CONTEXT_ERROR__\n" +
					"show-toplevel\n" +
					"fatal: not a git repository (or any of the parent directories): .git\n",
			),
		},
	}

	_, err := remoteGitContext(runner, cwd)
	require.Error(t, err)
	var gitErr *GitContextError
	require.ErrorAs(t, err, &gitErr)
	assert.Equal(t, GitContextCauseNotGitRepo, gitErr.Cause)
	assert.Equal(t, "git rev-parse --show-toplevel", gitErr.Operation)
	assert.Equal(t, "fatal: not a git repository (or any of the parent directories): .git", gitErr.Stderr)
}

func TestRemoteGitContext_IncompleteResponseReturnsDiagnosticError(t *testing.T) {
	const cwd = "/home/user/panemux"

	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			fmt.Sprintf(sshGitContextCmdTemplate, shellQuotePath(cwd)): []byte("/home/user/panemux\n"),
		},
	}

	_, err := remoteGitContext(runner, cwd)
	require.Error(t, err)
	var gitErr *GitContextError
	require.ErrorAs(t, err, &gitErr)
	assert.Equal(t, GitContextCauseIncomplete, gitErr.Cause)
	assert.Equal(t, "/home/user/panemux", gitErr.Stderr)
}

// fakeClock is a mutable clock so tests can simulate a dial attempt itself
// consuming wall-clock time (as a slow/hanging attempt would), which a plain
// injected nowFn sequence cannot express since the retry loop reads nowFn
// independently of when dialTransportFn returns.
type fakeClock struct {
	t  time.Time
	mu sync.Mutex
}

func newFakeClock() *fakeClock { return &fakeClock{t: time.Now()} }

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// stubDialTransport tracks how many times it was called and pops canned
// results off the given queue, useful for exercising dialTransportWithRetry
// without any real network I/O.
func stubDialTransport(
	t *testing.T, results ...error,
) (func(SSHConfig, string, int, time.Time) (net.Conn, *gossh.Client, error), *int) {
	t.Helper()
	calls := 0
	fn := func(SSHConfig, string, int, time.Time) (net.Conn, *gossh.Client, error) {
		calls++
		idx := calls - 1
		require.Less(t, idx, len(results), "unexpected extra dial attempt")
		if results[idx] != nil {
			return nil, nil, results[idx]
		}
		return &net.TCPConn{}, nil, nil
	}
	return fn, &calls
}

func withStubbedSleep(t *testing.T) *[]time.Duration {
	t.Helper()
	var slept []time.Duration
	origSleep := sleepFn
	sleepFn = func(d time.Duration) { slept = append(slept, d) }
	t.Cleanup(func() { sleepFn = origSleep })
	return &slept
}

func TestDialWithRetry_SucceedsFirstTry(t *testing.T) {
	slept := withStubbedSleep(t)
	origDial := dialTransportFn
	fn, calls := stubDialTransport(t, nil)
	dialTransportFn = fn
	t.Cleanup(func() { dialTransportFn = origDial })

	_, _, err := dialTransportWithRetry(SSHConfig{}, "host:22", 22, nowFn().Add(dialRetryBudget))
	require.NoError(t, err)
	assert.Equal(t, 1, *calls)
	assert.Empty(t, *slept)
}

func TestDialWithRetry_SucceedsAfterTransientFailures(t *testing.T) {
	slept := withStubbedSleep(t)
	origDial := dialTransportFn
	fn, calls := stubDialTransport(t,
		errors.New("dial tcp: lookup host: no address associated with hostname"),
		errors.New("dial tcp: lookup host: no address associated with hostname"),
		nil,
	)
	dialTransportFn = fn
	t.Cleanup(func() { dialTransportFn = origDial })

	_, _, err := dialTransportWithRetry(SSHConfig{}, "host:22", 22, nowFn().Add(dialRetryBudget))
	require.NoError(t, err)
	assert.Equal(t, 3, *calls)
	assert.Len(t, *slept, 2)
}

func TestDialWithRetry_ExhaustsAllRetries(t *testing.T) {
	withStubbedSleep(t)
	origDial := dialTransportFn
	wantErr := errors.New("dial tcp: lookup host: no address associated with hostname")
	fn, calls := stubDialTransport(t, errors.New("attempt 1"), errors.New("attempt 2"), wantErr)
	dialTransportFn = fn
	t.Cleanup(func() { dialTransportFn = origDial })

	_, _, err := dialTransportWithRetry(SSHConfig{}, "host:22", 22, nowFn().Add(dialRetryBudget))
	require.Error(t, err)
	assert.Equal(t, dialRetryMaxAttempts, *calls)
	assert.Equal(t, wantErr, err)
}

func TestDialWithRetry_StopsRetryingPastElapsedBudget(t *testing.T) {
	withStubbedSleep(t)
	origDial := dialTransportFn
	fn, calls := stubDialTransport(t, errors.New("attempt 1"), errors.New("attempt 2"), nil)
	dialTransportFn = fn
	t.Cleanup(func() { dialTransportFn = origDial })

	clock := newFakeClock()
	origNow := nowFn
	nowFn = clock.now
	t.Cleanup(func() { nowFn = origNow })

	deadline := clock.now().Add(dialRetryBudget)
	// Advance past the deadline before the retry loop even starts its first
	// pre-attempt check.
	clock.advance(dialRetryBudget + time.Second)

	_, _, err := dialTransportWithRetry(SSHConfig{}, "host:22", 22, deadline)
	require.Error(t, err)
	assert.Equal(t, 0, *calls, "no attempt should be made once the deadline has already passed")
}

// TestDialWithRetry_HangingFirstAttemptConsumesWholeBudget is the regression
// test for a reviewer-flagged gap: the retry loop only checked elapsed time
// *between* attempts, so a target that fails slowly (hangs near its own
// per-attempt timeout) rather than quickly (e.g. an instant DNS error) could
// still cost up to dialRetryMaxAttempts full timeouts before the budget
// check caught up. Each attempt must now be dialed with a timeout clamped to
// the *remaining* budget, so one hanging attempt that consumes the whole
// budget leaves no room for further attempts.
func TestDialWithRetry_HangingFirstAttemptConsumesWholeBudget(t *testing.T) {
	withStubbedSleep(t)
	clock := newFakeClock()
	origNow := nowFn
	nowFn = clock.now
	t.Cleanup(func() { nowFn = origNow })

	origDial := dialTransportFn
	calls := 0
	var gotTimeouts []time.Duration
	dialTransportFn = func(_ SSHConfig, _ string, _ int, deadline time.Time) (net.Conn, *gossh.Client, error) {
		calls++
		gotTimeouts = append(gotTimeouts, deadline.Sub(clock.now()))
		// Simulate the attempt itself hanging for its entire allotted budget
		// before finally failing, rather than failing instantly.
		clock.advance(dialRetryBudget)
		return nil, nil, errors.New("i/o timeout")
	}
	t.Cleanup(func() { dialTransportFn = origDial })

	deadline := clock.now().Add(dialRetryBudget)
	_, _, err := dialTransportWithRetry(SSHConfig{}, "host:22", 22, deadline)
	require.Error(t, err)
	assert.Equal(t, 1, calls, "a hanging attempt that consumes the whole budget must not be retried")
	require.Len(t, gotTimeouts, 1)
	assert.InDelta(t, dialRetryBudget, gotTimeouts[0], float64(50*time.Millisecond))
}

// TestDialWithRetry_ShrinksTimeoutForSubsequentAttempts verifies each retry's
// own dial timeout is clamped to whatever budget remains, not a fresh
// dialRetryBudget every time — otherwise attempts that each take just under
// the full budget could cumulatively run far longer than dialRetryBudget.
func TestDialWithRetry_ShrinksTimeoutForSubsequentAttempts(t *testing.T) {
	withStubbedSleep(t)
	clock := newFakeClock()
	origNow := nowFn
	nowFn = clock.now
	t.Cleanup(func() { nowFn = origNow })

	origDial := dialTransportFn
	var gotTimeouts []time.Duration
	dialTransportFn = func(_ SSHConfig, _ string, _ int, deadline time.Time) (net.Conn, *gossh.Client, error) {
		gotTimeouts = append(gotTimeouts, deadline.Sub(clock.now()))
		// Each attempt takes most, but not all, of what's currently left.
		clock.advance(dialRetryBudget / 2)
		return nil, nil, errors.New("attempt failed")
	}
	t.Cleanup(func() { dialTransportFn = origDial })

	deadline := clock.now().Add(dialRetryBudget)
	_, _, err := dialTransportWithRetry(SSHConfig{}, "host:22", 22, deadline)
	require.Error(t, err)
	require.Len(t, gotTimeouts, 2, "budget should be exhausted after two half-budget attempts")
	assert.InDelta(t, dialRetryBudget, gotTimeouts[0], float64(50*time.Millisecond))
	assert.InDelta(t, dialRetryBudget/2, gotTimeouts[1], float64(50*time.Millisecond))
}

// TestDialSSHClient_HandshakeFailure_NotRetried verifies that a TCP connection
// that succeeds but fails the SSH handshake is not retried by the transport
// dial loop: only auth/handshake failures should fail fast, since retrying
// them could look like automated credential-guessing.
func TestDialSSHClient_HandshakeFailure_NotRetried(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	var accepted int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, acceptErr := ln.Accept()
			if acceptErr != nil {
				return
			}
			accepted++
			conn.Close()
		}
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	require.NoError(t, err)
	var port int
	_, err = fmt.Sscanf(portStr, "%d", &port)
	require.NoError(t, err)

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(knownHosts, nil, 0600))

	cfg := SSHConfig{
		Host:           host,
		Port:           port,
		User:           "test",
		Password:       "wrong",
		KnownHostsFile: knownHosts,
	}

	start := time.Now()
	_, _, err = dialSSHClient(cfg)
	elapsed := time.Since(start)
	require.Error(t, err)

	ln.Close()
	<-done

	// The transport itself connects fine every time; only the handshake
	// fails, and handshake failures happen outside the dial-retry loop.
	assert.Equal(t, int32(1), accepted)
	assert.Less(t, elapsed, dialRetryInitialBackoff, "handshake failure should fail fast without dial-retry backoff")
}

// TestDialTransport_JumpHostSharesRetryDeadlineWithOuterCall is the
// regression test for a reviewer-flagged gap: dialThroughJump used to call
// dialSSHClient (the public entry point), which computed its own fresh
// dialRetryBudget window. That meant a ProxyJump chain could retry the jump
// hop's own transport dial independently of the outer target dial's retry
// loop, multiplying the worst-case wall-clock time for a hanging/unreachable
// jump host. The jump hop must now reuse the exact same deadline as the
// outer call.
func TestDialTransport_JumpHostSharesRetryDeadlineWithOuterCall(t *testing.T) {
	withStubbedSleep(t)
	origDial := dialTransportFn
	var gotDeadlines []time.Time
	dialTransportFn = func(_ SSHConfig, _ string, _ int, deadline time.Time) (net.Conn, *gossh.Client, error) {
		gotDeadlines = append(gotDeadlines, deadline)
		return nil, nil, errors.New("jump dial failed")
	}
	t.Cleanup(func() { dialTransportFn = origDial })

	knownHosts := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(knownHosts, nil, 0600))

	jumpCfg := SSHConfig{Host: "jump.invalid", Port: 22, User: "jump", Password: "x", KnownHostsFile: knownHosts}
	cfg := SSHConfig{Host: "target.invalid", Port: 22, JumpHost: &jumpCfg}

	deadline := nowFn().Add(dialRetryBudget)
	_, _, err := dialTransport(cfg, "target.invalid:22", 22, deadline)
	require.Error(t, err)
	require.NotEmpty(t, gotDeadlines)
	for _, d := range gotDeadlines {
		assert.True(t, d.Equal(deadline), "jump host dial must reuse the same deadline as the outer call, not a fresh budget")
	}
}
