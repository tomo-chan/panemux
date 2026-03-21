package session

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
