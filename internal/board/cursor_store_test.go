package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/homedir"
)

func TestSaveCursorFile_ThenLoad_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	entries := []CursorEntry{
		{Host: "local", Team: "panemux", Cursor: "5"},
		{Host: "build-host", Team: "panemux", Cursor: "12"},
	}

	require.NoError(t, SaveCursorFile(path, entries))

	loaded, err := LoadCursorFile(path)
	require.NoError(t, err)
	assert.Equal(t, entries, loaded)
}

func TestSaveCursorFile_SetsFileMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	require.NoError(t, SaveCursorFile(path, []CursorEntry{{Host: "local", Team: "t", Cursor: "1"}}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestSaveCursorFile_CreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "cursor.json")
	require.NoError(t, SaveCursorFile(path, []CursorEntry{{Host: "local", Team: "t", Cursor: "1"}}))

	_, err := os.Stat(path)
	require.NoError(t, err)
}

func TestLoadCursorFile_MissingFile_ReturnsNilNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	entries, err := LoadCursorFile(path)
	require.NoError(t, err)
	assert.Nil(t, entries)
}

func TestLoadCursorFile_MalformedJSON_Error(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	require.NoError(t, os.WriteFile(path, []byte("not valid json"), 0600))

	_, err := LoadCursorFile(path)
	assert.Error(t, err)
}

func TestLoadCursorFile_EmptyArray_ReturnsEmptySlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cursor.json")
	require.NoError(t, os.WriteFile(path, []byte("[]"), 0600))

	entries, err := LoadCursorFile(path)
	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestDefaultCursorFilePath_ContainsExpectedSuffix(t *testing.T) {
	path, err := DefaultCursorFilePath()
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, filepath.Join(".config", "panemux", "board-relay-cursor.json")))
}

// Both default paths are resolved at startup and again on every relay save, so
// a home directory that cannot be resolved — a stripped environment such as a
// systemd unit or a container with no passwd entry — has to name the step that
// failed rather than return a path relative to the working directory. board.go
// turns each into its own log line, and those lines are all an operator gets.
func TestDefaultFilePaths_UnresolvableHomeDirectory_Error(t *testing.T) {
	homedir.SetFailingForTest(t, errNoHomeDir)

	cursorPath, cursorErr := DefaultCursorFilePath()
	require.ErrorIs(t, cursorErr, errNoHomeDir)
	assert.Empty(t, cursorPath, "no path may be returned alongside the error")

	statePath, stateErr := DefaultBootstrapStateFilePath()
	require.ErrorIs(t, stateErr, errNoHomeDir)
	assert.Empty(t, statePath)
}

func TestDefaultFilePaths_AreUnderTheResolvedHomeDirectory(t *testing.T) {
	homedir.SetForTest(t, "/workspace/user/home")

	cursorPath, err := DefaultCursorFilePath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/workspace/user/home", ".config", "panemux", cursorFileName), cursorPath)

	statePath, err := DefaultBootstrapStateFilePath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/workspace/user/home", ".config", "panemux", bootstrapStateFileName), statePath)
}
