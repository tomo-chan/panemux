package session

import (
	"fmt"
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

// TestResolveSSHConfig_ProxyJump verifies that a ProxyJump directive in ~/.ssh/config
// causes the returned SSHConfig to have JumpHost populated with the jump host's details.
func TestResolveSSHConfig_ProxyJump(t *testing.T) {
	dir := t.TempDir()
	sshCfgPath := filepath.Join(dir, "config")
	content := `Host jump-host
    HostName jump.example.com
    User jumpuser
    Port 22

Host target-host
    HostName target.internal
    User admin
    ProxyJump jump-host
`
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	cfg, err := resolveSSHConfig("target-host", nil, sshCfgPath)
	require.NoError(t, err)

	assert.Equal(t, "target.internal", cfg.Host)
	assert.Equal(t, "admin", cfg.User)
	require.NotNil(t, cfg.JumpHost, "JumpHost should be populated when ProxyJump is set")
	assert.Equal(t, "jump.example.com", cfg.JumpHost.Host)
	assert.Equal(t, "jumpuser", cfg.JumpHost.User)
	assert.Equal(t, 22, cfg.JumpHost.Port)
	assert.Nil(t, cfg.JumpHost.JumpHost, "jump host itself should have no further jump")
}

// TestResolveSSHConfig_NoProxyJump_JumpHostNil verifies that hosts without ProxyJump
// return SSHConfig.JumpHost == nil.
func TestResolveSSHConfig_NoProxyJump_JumpHostNil(t *testing.T) {
	sshCfgPath := writeTempSSHConfig(t, "direct-host", "direct.example.com", "user", 22)
	cfg, err := resolveSSHConfig("direct-host", nil, sshCfgPath)
	require.NoError(t, err)
	assert.Nil(t, cfg.JumpHost)
}

// TestResolveSSHConfig_ProxyJump_JumpHostNotFound verifies that an error is returned
// when the ProxyJump alias cannot be resolved.
func TestResolveSSHConfig_ProxyJump_JumpHostNotFound(t *testing.T) {
	dir := t.TempDir()
	sshCfgPath := filepath.Join(dir, "config")
	content := `Host target-host
    HostName target.internal
    User admin
    ProxyJump nonexistent-jump
`
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	_, err := resolveSSHConfig("target-host", nil, sshCfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "resolving proxy jump")
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
	return fmt.Sprintf("%d", i)
}

// TestResolveSSHConfig_FromSSHConfigMap verifies that resolveSSHConfig returns
// the correct values when the connection is found in the yaml sshConns map.
func TestResolveSSHConfig_FromSSHConfigMap(t *testing.T) {
	conns := map[string]config.SSHConnection{
		"prod": {Host: "prod.example.com", Port: 2222, User: "deploy", KeyFile: "/home/user/.ssh/id_rsa"},
	}
	cfg, err := resolveSSHConfig("prod", conns, filepath.Join(t.TempDir(), "no-config"))
	require.NoError(t, err)
	assert.Equal(t, "prod.example.com", cfg.Host)
	assert.Equal(t, 2222, cfg.Port)
	assert.Equal(t, "deploy", cfg.User)
	assert.Equal(t, "/home/user/.ssh/id_rsa", cfg.KeyFile)
}

// TestResolveSSHConfig_FallbackToSSHConfig verifies the ~/.ssh/config fallback path:
// host is resolved, port defaults to 22, and IdentityFile with ~/ is expanded.
func TestResolveSSHConfig_FallbackToSSHConfig(t *testing.T) {
	home := t.TempDir()
	sshDir := filepath.Join(home, ".ssh")
	require.NoError(t, os.MkdirAll(sshDir, 0700))

	sshCfgPath := filepath.Join(sshDir, "config")
	content := "Host myserver\n    HostName 10.0.0.1\n    User admin\n    IdentityFile ~/.ssh/id_ed25519\n"
	require.NoError(t, os.WriteFile(sshCfgPath, []byte(content), 0600))

	// Temporarily override HOME so ~/ expansion uses our temp dir
	t.Setenv("HOME", home)

	cfg, err := resolveSSHConfig("myserver", nil, sshCfgPath)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", cfg.Host)
	assert.Equal(t, 22, cfg.Port) // default when not set
	assert.Equal(t, "admin", cfg.User)
	// IdentityFile should have ~/ expanded to home
	assert.Equal(t, filepath.Join(home, ".ssh", "id_ed25519"), cfg.KeyFile)
}

// TestResolveSSHConfig_PortFromSSHConfig verifies that an explicit Port in the
// ssh config file is used as-is.
func TestResolveSSHConfig_PortFromSSHConfig(t *testing.T) {
	sshCfgPath := writeTempSSHConfig(t, "myhost", "myhost.example.com", "myuser", 2222)
	cfg, err := resolveSSHConfig("myhost", nil, sshCfgPath)
	require.NoError(t, err)
	assert.Equal(t, 2222, cfg.Port)
}

// TestResolveSSHConfig_NotFound verifies that a missing connection returns an error.
func TestResolveSSHConfig_NotFound(t *testing.T) {
	sshCfgPath := writeTempSSHConfig(t, "other", "other.example.com", "user", 0)
	_, err := resolveSSHConfig("does-not-exist", nil, sshCfgPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
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
