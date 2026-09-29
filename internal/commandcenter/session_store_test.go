package commandcenter

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadSessionFileMissingReturnsZeroValue(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	state, err := LoadSessionFile(path)

	require.NoError(t, err)
	assert.Equal(t, SessionState{}, state)
}

func TestSaveThenLoadSessionFileRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "command-center-session.json")

	require.NoError(t, SaveSessionFile(path, SessionState{SessionID: "sess-123"}))

	state, err := LoadSessionFile(path)
	require.NoError(t, err)
	assert.Equal(t, SessionState{SessionID: "sess-123"}, state)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestSaveSessionFileOverwritesPreviousSession(t *testing.T) {
	path := filepath.Join(t.TempDir(), "command-center-session.json")

	require.NoError(t, SaveSessionFile(path, SessionState{SessionID: "first"}))
	require.NoError(t, SaveSessionFile(path, SessionState{SessionID: "second"}))

	state, err := LoadSessionFile(path)
	require.NoError(t, err)
	assert.Equal(t, "second", state.SessionID)
}

func TestLoadSessionFileMalformedJSONReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "command-center-session.json")
	require.NoError(t, os.WriteFile(path, []byte("not json"), 0600))

	_, err := LoadSessionFile(path)

	assert.Error(t, err)
}

func TestDefaultSessionFilePath(t *testing.T) {
	path, err := DefaultSessionFilePath()

	require.NoError(t, err)
	assert.Contains(t, path, filepath.Join(".config", "panemux", "command-center-session.json"))
}
