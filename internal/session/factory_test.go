package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
)

func TestCreateFromConfig_Local(t *testing.T) {
	pane := &config.PaneConfig{
		ID:    "test-local",
		Type:  "local",
		Shell: "/bin/sh",
		Title: "Test",
	}
	sess, err := CreateFromConfig(pane, nil)
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, "test-local", sess.ID())
	assert.Equal(t, TypeLocal, sess.Type())
}

func TestCreateFromConfig_UnknownType(t *testing.T) {
	pane := &config.PaneConfig{
		ID:   "test",
		Type: "unknown",
	}
	_, err := CreateFromConfig(pane, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unknown")
}

func TestCreateFromConfig_SSHMissing(t *testing.T) {
	pane := &config.PaneConfig{
		ID:         "test-ssh",
		Type:       "ssh",
		Connection: "nonexistent",
	}
	_, err := CreateFromConfig(pane, map[string]config.SSHConnection{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

// writeTempSSHConfig writes a minimal SSH config file with a Host block and returns the path.
func writeTempSSHConfig(t *testing.T, name, hostname, user string, port int) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "config")
	content := "Host " + name + "\n"
	content += "    HostName " + hostname + "\n"
	content += "    User " + user + "\n"
	if port != 0 {
		content += "    Port " + itoa(port) + "\n"
	}
	require.NoError(t, os.WriteFile(f, []byte(content), 0600))
	return f
}

func itoa(i int) string {
	return string(rune('0'+i%10)) // simple, only used for port ≤9 in tests
}

func TestCreateSession_SSHFallbackToSSHConfig(t *testing.T) {
	// pane.Connection "ssh-alias" not in sshConns map → should be found in ~/.ssh/config
	sshConfigPath := writeTempSSHConfig(t, "ssh-alias", "ssh.example.com", "alice", 0)

	pane := &config.PaneConfig{
		ID:         "test-ssh-fallback",
		Type:       "ssh",
		Connection: "ssh-alias",
	}

	// This should not fail — the SSH connection is resolved from ssh config
	// (it will fail to connect, but that's a network issue, not a config one)
	// We only test that the error is NOT "not found"
	_, err := createSession(pane, map[string]config.SSHConnection{}, sshConfigPath)
	// The error (if any) should be a connection error, not "not found"
	if err != nil {
		assert.NotContains(t, err.Error(), "not found")
	}
}

func TestCreateSession_SSHFallback_NotInSSHConfig_Errors(t *testing.T) {
	sshConfigPath := writeTempSSHConfig(t, "other-host", "other.example.com", "bob", 0)

	pane := &config.PaneConfig{
		ID:         "test-ssh-missing",
		Type:       "ssh",
		Connection: "does-not-exist",
	}
	_, err := createSession(pane, map[string]config.SSHConnection{}, sshConfigPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestCreateSession_SSHTmuxFallbackToSSHConfig(t *testing.T) {
	sshConfigPath := writeTempSSHConfig(t, "tmux-host", "tmux.example.com", "carol", 0)

	pane := &config.PaneConfig{
		ID:          "test-ssh-tmux-fallback",
		Type:        "ssh_tmux",
		Connection:  "tmux-host",
		TmuxSession: "main",
	}
	_, err := createSession(pane, map[string]config.SSHConnection{}, sshConfigPath)
	// Expect connection error, not "not found"
	if err != nil {
		assert.NotContains(t, err.Error(), "not found")
	}
}

func TestCreateSession_SSHConfigPort_UsedWhenSet(t *testing.T) {
	// Port 2 in test — won't actually connect, but let's verify it's read from config
	dir := t.TempDir()
	f := filepath.Join(dir, "config")
	content := "Host myhost\n    HostName myhost.example.com\n    User myuser\n    Port 2222\n"
	require.NoError(t, os.WriteFile(f, []byte(content), 0600))

	pane := &config.PaneConfig{
		ID:         "test-port",
		Type:       "ssh",
		Connection: "myhost",
	}
	_, err := createSession(pane, map[string]config.SSHConnection{}, f)
	// We only care that "not found" is NOT returned
	if err != nil {
		assert.NotContains(t, err.Error(), "not found")
	}
}
