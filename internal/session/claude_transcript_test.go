package session

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/homedir"
)

// TestClaudeProjectDirName_ObservedEncoding is the traceable record issue #119
// asked for: the mapping panemux relies on, written out as cases rather than
// left implicit in one regex-free line of string replacement.
//
// It is observed behavior of Claude Code's own storage layout, not a
// documented API, which is why resolveClaudeTranscriptPath below does not
// depend on it being right forever.
func TestClaudeProjectDirName_ObservedEncoding(t *testing.T) {
	for _, tc := range []struct {
		name string
		cwd  string
		want string
	}{
		{
			name: "each separator becomes a dash, including the leading one",
			cwd:  "/workspace/user/project",
			want: "-workspace-user-project",
		},
		{
			name: "a dot inside a path segment becomes a dash too",
			cwd:  "/Users/dev.user/development/panemux",
			want: "-Users-dev-user-development-panemux",
		},
		{
			name: "a dotfile-style directory is not special",
			cwd:  "/workspace/user/.config/project",
			want: "-workspace-user--config-project",
		},
		{
			name: "a trailing separator is cleaned away first",
			cwd:  "/workspace/user/project/",
			want: "-workspace-user-project",
		},
		{
			name: "a dotted version directory keeps one dash per dot",
			cwd:  "/opt/tool-1.2.3/work",
			want: "-opt-tool-1-2-3-work",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, claudeProjectDirName(tc.cwd))
		})
	}
}

// forgetClaudeProjectDirMismatches clears the once-per-session log guard so a
// test starts from a state where nothing has been reported yet.
func forgetClaudeProjectDirMismatches(t *testing.T) {
	t.Helper()
	reset := func() {
		claudeProjectDirMismatches.mu.Lock()
		defer claudeProjectDirMismatches.mu.Unlock()
		claudeProjectDirMismatches.seen = nil
	}
	reset()
	t.Cleanup(reset)
}

func writeTranscript(t *testing.T, path string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0750))
	require.NoError(t, os.WriteFile(path, []byte(`{"cwd":"/workspace/user/project"}`+"\n"), 0600))
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := log.Writer()
	log.SetOutput(&buf)
	t.Cleanup(func() { log.SetOutput(orig) })
	return &buf
}

func TestResolveClaudeTranscriptPath_UsesTheDerivedPathWhenItIsThere(t *testing.T) {
	forgetClaudeProjectDirMismatches(t)
	projects := t.TempDir()
	derived := filepath.Join(projects, "-workspace-user-project", "session-1.jsonl")
	writeTranscript(t, derived)
	logged := captureLog(t)

	got := resolveClaudeTranscriptPath(projects, "/workspace/user/project", "session-1")

	assert.Equal(t, derived, got)
	assert.Empty(t, logged.String(), "the fast path is the normal case and must stay quiet")
}

// TestResolveClaudeTranscriptPath_FallsBackWhenClaudeEncodedTheDirDifferently
// is the resilience half of issue #119: the encoding is observed behavior, so
// a future Claude release changing it must not silently cost the pane its
// worktree.
func TestResolveClaudeTranscriptPath_FallsBackWhenClaudeEncodedTheDirDifferently(t *testing.T) {
	forgetClaudeProjectDirMismatches(t)
	projects := t.TempDir()
	// An encoding panemux does not derive: dots kept as dots.
	actual := filepath.Join(projects, "-workspace-user-my.project", "session-1.jsonl")
	writeTranscript(t, actual)
	logged := captureLog(t)

	got := resolveClaudeTranscriptPath(projects, "/workspace/user/my.project", "session-1")

	assert.Equal(t, actual, got)
	assert.Contains(t, logged.String(), "session-1")
	assert.Contains(t, logged.String(), "-workspace-user-my-project", "the log must name what was derived")
	assert.Contains(t, logged.String(), "-workspace-user-my.project", "and what was found instead")
}

func TestResolveClaudeTranscriptPath_LogsOneMismatchPerSessionRatherThanPerLookup(t *testing.T) {
	forgetClaudeProjectDirMismatches(t)
	projects := t.TempDir()
	writeTranscript(t, filepath.Join(projects, "-workspace-user-my.project", "session-1.jsonl"))
	logged := captureLog(t)

	for range 5 {
		resolveClaudeTranscriptPath(projects, "/workspace/user/my.project", "session-1")
	}

	assert.Equal(t, 1, bytes.Count(logged.Bytes(), []byte("claude transcript")))
}

func TestResolveClaudeTranscriptPath_NoTranscriptAnywhere_ReturnsTheDerivedPath(t *testing.T) {
	forgetClaudeProjectDirMismatches(t)
	projects := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(projects, "-other-project"), 0750))
	logged := captureLog(t)

	got := resolveClaudeTranscriptPath(projects, "/workspace/user/project", "session-1")

	assert.Equal(t, filepath.Join(projects, "-workspace-user-project", "session-1.jsonl"), got)
	assert.Empty(t, logged.String(), "a session with no transcript at all is not a mismatch worth reporting")
}

func TestResolveClaudeTranscriptPath_MissingProjectsDirectory_ReturnsTheDerivedPath(t *testing.T) {
	forgetClaudeProjectDirMismatches(t)
	projects := filepath.Join(t.TempDir(), "never-created")

	got := resolveClaudeTranscriptPath(projects, "/workspace/user/project", "session-1")

	assert.Equal(t, filepath.Join(projects, "-workspace-user-project", "session-1.jsonl"), got)
}

// TestResolveClaudeTranscriptPath_ScanIgnoresNonDirectoriesAndOtherSessions
// pins what "bounded" means: one level down, one candidate per project
// directory, and only a file named for this session.
func TestResolveClaudeTranscriptPath_ScanIgnoresNonDirectoriesAndOtherSessions(t *testing.T) {
	forgetClaudeProjectDirMismatches(t)
	projects := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(projects, "session-1.jsonl"), []byte("{}"), 0600))
	writeTranscript(t, filepath.Join(projects, "-a-project", "session-2.jsonl"))
	writeTranscript(t, filepath.Join(projects, "-b-project", "nested", "session-1.jsonl"))
	wanted := filepath.Join(projects, "-c-project", "session-1.jsonl")
	writeTranscript(t, wanted)

	got := resolveClaudeTranscriptPath(projects, "/workspace/user/project", "session-1")

	assert.Equal(t, wanted, got)
}

// TestClaudeSessionCWDs_ReadsATranscriptFromAnUnexpectedProjectDir is the
// acceptance check end to end: the pane still learns its worktree when the
// derived directory name is wrong.
func TestClaudeSessionCWDs_ReadsATranscriptFromAnUnexpectedProjectDir(t *testing.T) {
	forgetClaudeProjectDirMismatches(t)
	home := t.TempDir()
	homedir.SetForTest(t, home)

	sessionMeta := filepath.Join(home, ".claude", "sessions", "220.json")
	require.NoError(t, os.MkdirAll(filepath.Dir(sessionMeta), 0750))
	require.NoError(t, os.WriteFile(sessionMeta, []byte(
		`{"pid":220,"sessionId":"session-123","cwd":"/workspace/user/my.project"}`,
	), 0600))

	// Stored under an encoding panemux does not derive.
	transcript := filepath.Join(
		home, ".claude", "projects", "-workspace-user-my.project", "session-123.jsonl",
	)
	require.NoError(t, os.MkdirAll(filepath.Dir(transcript), 0750))
	require.NoError(t, os.WriteFile(transcript, []byte(
		`{"type":"assistant","cwd":"/workspace/user/my.project/worktree",`+
			`"message":{"content":[{"type":"text","text":"ok"}]}}`+"\n",
	), 0600))

	cwds, err := claudeSessionCWDs(220)

	require.NoError(t, err)
	assert.Equal(t, []string{"/workspace/user/my.project/worktree"}, cwds)
}
