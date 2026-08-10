package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
