package commandcenter

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// uuidV4 is the shape --session-id requires. Anything else is rejected by
// the CLI outright, so this is a contract, not a style preference.
var uuidV4 = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestNewSessionIDIsAUniqueV4UUID(t *testing.T) {
	seen := make(map[string]struct{}, 64)
	for range 64 {
		id, err := NewSessionID()
		require.NoError(t, err)
		assert.Regexp(t, uuidV4, id)
		_, dup := seen[id]
		require.False(t, dup, "minted a duplicate session id: %s", id)
		seen[id] = struct{}{}
	}
}

func TestNewSessionIDPassesTheRunnersOwnArgvGuard(t *testing.T) {
	// A minted id goes straight into argv as --resume's value on later
	// queries, so it must satisfy the same allowlist a persisted id does.
	id, err := NewSessionID()
	require.NoError(t, err)
	assert.True(t, validSessionID.MatchString(id), "minted id %q must satisfy validSessionID", id)
}

func TestSystemPromptWithoutOperatorFile(t *testing.T) {
	assert.Equal(t, DefaultSystemPrompt, SystemPrompt(t.TempDir()))
	assert.Equal(t, DefaultSystemPrompt, SystemPrompt(""))
	assert.Equal(t, DefaultSystemPrompt, SystemPrompt(filepath.Join(t.TempDir(), "does-not-exist")))
}

func TestSystemPromptInstructsRecheckingBoardStatus(t *testing.T) {
	// Regression guard for the observed failure this rule exists for: a
	// second question answered from conversation context without calling
	// board_status again.
	assert.Contains(t, DefaultSystemPrompt, "Always call board_status before answering")
	assert.Contains(t, DefaultSystemPrompt, "board_broadcast")
}

func TestSystemPromptAppendsOperatorInstructions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("  Prefer pane-a for releases.  "), 0o600))

	got := SystemPrompt(dir)

	assert.True(t, strings.HasPrefix(got, DefaultSystemPrompt), "operator text must extend the default, not replace it")
	assert.Contains(t, got, "Prefer pane-a for releases.")
	assert.NotContains(t, got, "  Prefer", "surrounding whitespace should be trimmed")
}

func TestSystemPromptIgnoresEmptyOperatorInstructions(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte("   \n\t\n"), 0o600))

	assert.Equal(t, DefaultSystemPrompt, SystemPrompt(dir))
}

func TestOperatorSettingsPath(t *testing.T) {
	dir := t.TempDir()
	assert.Empty(t, OperatorSettingsPath(dir), "absent file yields no --settings flag")
	assert.Empty(t, OperatorSettingsPath(""))

	path := filepath.Join(dir, "settings.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"model":"claude-sonnet-5"}`), 0o600))
	assert.Equal(t, path, OperatorSettingsPath(dir))

	// A directory of that name is not a settings file.
	other := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(other, "settings.json"), 0o700))
	assert.Empty(t, OperatorSettingsPath(other))
}

func TestDefaultContextDirIsUnderPanemuxConfig(t *testing.T) {
	t.Setenv("HOME", "/workspace/user/home")
	dir, err := DefaultContextDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/workspace/user/home", ".config", "panemux", "command-center"), dir)
}

func TestNewWorkDirIsEmptyAndRemovable(t *testing.T) {
	dir, cleanup, err := NewWorkDir()
	require.NoError(t, err)

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	assert.Empty(t, entries, "the subprocess must not inherit any file through its working directory")

	cleanup()
	_, err = os.Stat(dir)
	assert.True(t, os.IsNotExist(err), "cleanup must remove the work directory")
}
