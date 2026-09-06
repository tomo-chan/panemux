package commandcenter

import (
	"encoding/json"
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

// TestSubprocessSettingsOnlyNarrows is the regression guard for the
// escalation that rules out merging the operator's own settings: with
// --allowedTools scoped to a single board tool, a settings document
// carrying {"permissions":{"defaultMode":"acceptEdits"}} let the real CLI
// run Bash with no permission denial recorded at all (reproduced twice,
// v2.1.233). Whatever else this document grows, it must never carry a key
// that can widen the subprocess's reach.
func TestSubprocessSettingsOnlyNarrows(t *testing.T) {
	var doc map[string]any
	require.NoError(t, json.Unmarshal([]byte(SubprocessSettings), &doc), "must be valid JSON for --settings")

	widening := []string{
		"permissions", "hooks", "apiKeyHelper", "statusLine",
		"env", "enabledPlugins", "additionalDirectories", "mcpServers",
	}
	for _, key := range widening {
		assert.NotContains(t, doc, key,
			"%q can widen what the subprocess may do and must never be sent", key)
	}

	sandbox, ok := doc["sandbox"].(map[string]any)
	require.True(t, ok, "sandbox must be present: nothing else can enable it once --setting-sources is empty")
	assert.Equal(t, true, sandbox["enabled"])
}

func TestDefaultContextDirIsUnderPanemuxConfig(t *testing.T) {
	t.Setenv("HOME", "/workspace/user/home")
	dir, err := DefaultContextDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/workspace/user/home", ".config", "panemux", "command-center"), dir)
}

// The wrap is what tells an operator which step failed: run() emits this
// error with errorEvent("%v", err) and adds no context of its own, so if the
// wrap is dropped here a full disk surfaces as a bare "no space left on
// device" with nothing naming the work directory.
func TestNewWorkDirFailureNamesTheStepThatFailed(t *testing.T) {
	// MkdirTemp with an empty dir argument resolves through TMPDIR, so
	// pointing it at a path under a regular file makes it fail with ENOTDIR.
	notADir := filepath.Join(t.TempDir(), "notadir")
	require.NoError(t, os.WriteFile(notADir, []byte("not a directory\n"), 0600))
	t.Setenv("TMPDIR", filepath.Join(notADir, "tmp"))

	dir, cleanup, err := NewWorkDir()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating command center work directory")
	assert.Contains(t, err.Error(), "not a directory")
	assert.Empty(t, dir)
	require.NotNil(t, cleanup, "the caller defers this unconditionally, so it must never be nil")
	cleanup()
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
