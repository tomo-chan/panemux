package session

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
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
// it is used automatically (mirrors OpenSSH behaviour).
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

func TestBuildHostKeyCallback_NonexistentFile_Error(t *testing.T) {
	_, err := buildHostKeyCallback("/nonexistent/path/known_hosts")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "known_hosts")
}

func TestBuildHostKeyCallback_ValidFile_NoError(t *testing.T) {
	dir := t.TempDir()
	knownHostsPath := filepath.Join(dir, "known_hosts")
	require.NoError(t, os.WriteFile(knownHostsPath, []byte(""), 0600))

	_, err := buildHostKeyCallback(knownHostsPath)
	assert.NoError(t, err)
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
