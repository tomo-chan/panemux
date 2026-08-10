package board

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSaveBootstrapState_ThenLoad_RoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bootstrap.json")
	paneIDs := []string{"pane-a", "pane-b"}

	require.NoError(t, SaveBootstrapState(path, paneIDs))

	loaded, err := LoadBootstrapState(path)
	require.NoError(t, err)
	assert.Equal(t, paneIDs, loaded)
}

func TestSaveBootstrapState_SetsFileMode0600(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bootstrap.json")
	require.NoError(t, SaveBootstrapState(path, []string{"pane-a"}))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestSaveBootstrapState_CreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "dir", "bootstrap.json")
	require.NoError(t, SaveBootstrapState(path, []string{"pane-a"}))

	_, err := os.Stat(path)
	require.NoError(t, err)
}

func TestLoadBootstrapState_MissingFile_ReturnsNilNotError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.json")

	paneIDs, err := LoadBootstrapState(path)
	require.NoError(t, err)
	assert.Nil(t, paneIDs)
}

func TestLoadBootstrapState_MalformedJSON_Error(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bootstrap.json")
	require.NoError(t, os.WriteFile(path, []byte("not valid json"), 0600))

	_, err := LoadBootstrapState(path)
	assert.Error(t, err)
}

func TestLoadBootstrapState_EmptyArray_ReturnsEmptySlice(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bootstrap.json")
	require.NoError(t, os.WriteFile(path, []byte("[]"), 0600))

	paneIDs, err := LoadBootstrapState(path)
	require.NoError(t, err)
	assert.Empty(t, paneIDs)
}

func TestDefaultBootstrapStateFilePath_ContainsExpectedSuffix(t *testing.T) {
	path, err := DefaultBootstrapStateFilePath()
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, filepath.Join(".config", "panemux", "board-bootstrap-state.json")))
}
