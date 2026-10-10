package tasks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"panemux/internal/fileops"
	"panemux/internal/homedir"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var storeNow = time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)

func sampleStoredSummary(host, agent, sessionID string) storedSummary {
	return storedSummary{
		Host:         host,
		Agent:        agent,
		SessionID:    sessionID,
		Text:         "Fixing a race.",
		Remaining:    []string{"Run make check"},
		SummarizedAt: storeNow.Add(-time.Hour),
		InputHash:    strings.Repeat("ab", 32),
		Summarizer:   summarizerVersion(agent),
		Log:          storedLog{ModTime: 990, Size: 100},
		LastSeen:     storeNow,
	}
}

func writeStoreFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

func TestDefaultSummaryStorePath(t *testing.T) {
	homedir.SetForTest(t, "/home/demo")
	path, err := DefaultSummaryStorePath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join("/home/demo", ".config", "panemux", "task-summaries.json"), path)

	homedir.SetFailingForTest(t, errors.New("no home"))
	_, err = DefaultSummaryStorePath()
	assert.ErrorContains(t, err, "no home")
	_, _, err = NewSummaryStore("").load(storeNow)
	assert.ErrorContains(t, err, "no home", "the store resolves the default path when it is first used")
}

func TestSummaryStore_SavesAndLoadsSummaries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-summaries.json")
	store := NewSummaryStore(path)
	entries, moved, err := store.load(storeNow)
	require.NoError(t, err)
	assert.Empty(t, entries, "a missing file is an empty store")
	assert.Empty(t, moved)

	codex := sampleStoredSummary("gpu-box", AgentCodex, "019a0000-0000-7000-8000-000000000001")
	codex.Log.File = "rollout-2026-10-10T00-00-00-019a0000-0000-7000-8000-000000000001.jsonl"
	saved := []storedSummary{sampleStoredSummary("", AgentClaude, "s10"), codex}
	require.NoError(t, store.save(1, saved))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
	var file map[string]any
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, &file))
	assert.EqualValues(t, 1, file["version"])

	loaded, moved, err := NewSummaryStore(path).load(storeNow)
	require.NoError(t, err)
	assert.Empty(t, moved)
	assert.ElementsMatch(t, saved, loaded)
	assert.NoError(t, store.Err())
}

// A save that arrives after a newer one was written is dropped, so two
// summaries finishing together cannot leave the older set on disk.
func TestSummaryStore_AnOlderSaveDoesNotReplaceANewerOne(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-summaries.json")
	store := NewSummaryStore(path)
	_, _, err := store.load(storeNow)
	require.NoError(t, err)
	newer := sampleStoredSummary("", AgentClaude, "newer")
	require.NoError(t, store.save(2, []storedSummary{newer}))
	require.NoError(t, store.save(1, []storedSummary{sampleStoredSummary("", AgentClaude, "older")}))

	loaded, _, err := NewSummaryStore(path).load(storeNow)
	require.NoError(t, err)
	assert.Equal(t, []storedSummary{newer}, loaded)
}

// A file this build cannot use — not JSON, another format version, or an
// entry it would not have written — is moved aside, never read or
// overwritten, and saving starts again from an empty store at once.
func TestSummaryStore_MovesAsideAFileItCannotUse(t *testing.T) {
	valid := sampleStoredSummary("", AgentClaude, "s10")
	entry := func(change func(*storedSummary)) string {
		e := valid
		change(&e)
		data, err := json.Marshal(summaryStoreFile{Version: 1, Summaries: []storedSummary{e}})
		require.NoError(t, err)
		return string(data)
	}
	duplicate, err := json.Marshal(summaryStoreFile{Version: 1, Summaries: []storedSummary{valid, valid}})
	require.NoError(t, err)
	cases := map[string]string{
		"not JSON":       "{broken",
		"newer version":  `{"version":2,"summaries":[]}`,
		"no version":     `{"summaries":[]}`,
		"unknown agent":  entry(func(e *storedSummary) { e.Agent = "gemini" }),
		"bad session ID": entry(func(e *storedSummary) { e.SessionID = "../x" }),
		"codex non-UUID": entry(func(e *storedSummary) { e.Agent = AgentCodex }),
		"bad input hash": entry(func(e *storedSummary) { e.InputHash = "zz" }),
		"text too long":  entry(func(e *storedSummary) { e.Text = strings.Repeat("a", maxSummaryTextBytes+1) }),
		"too many items": entry(func(e *storedSummary) { e.Remaining = make([]string, maxSummaryRemaining+1) }),
		"item too long": entry(func(e *storedSummary) {
			e.Remaining = []string{strings.Repeat("a", maxSummaryItemBytes+1)}
		}),
		"duplicate entry": string(duplicate),
	}
	for name, content := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "task-summaries.json")
			writeStoreFile(t, path, content)
			store := NewSummaryStore(path)

			entries, moved, err := store.load(storeNow)
			require.NoError(t, err)
			assert.Empty(t, entries)
			assert.Equal(t, path+".bad-20261010T120000Z", moved)
			kept, err := os.ReadFile(moved)
			require.NoError(t, err)
			assert.Equal(t, content, string(kept), "the file is moved aside unchanged")

			require.NoError(t, store.save(1, []storedSummary{valid}), "saving resumes at once")
			loaded, _, err := NewSummaryStore(path).load(storeNow)
			require.NoError(t, err)
			assert.Equal(t, []storedSummary{valid}, loaded)
		})
	}
}

// When the file cannot be moved aside, or cannot be read at all, nothing is
// written over it: every save fails until a load succeeds.
func TestSummaryStore_RefusesToSaveUntilLoaded(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "task-summaries.json")
	writeStoreFile(t, path, "{broken")
	require.NoError(t, os.Chmod(dir, 0o500))
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	store := NewSummaryStore(path)

	_, _, err := store.load(storeNow)
	require.Error(t, err)
	assert.ErrorContains(t, store.Err(), "task summary file")
	assert.ErrorContains(t, store.save(1, []storedSummary{sampleStoredSummary("", AgentClaude, "s10")}), "not loaded")

	require.NoError(t, os.Chmod(dir, 0o700))
	_, moved, err := store.load(storeNow)
	require.NoError(t, err)
	assert.NotEmpty(t, moved)
	assert.NoError(t, store.Err())
	assert.NoError(t, store.save(1, nil))
}

func TestSummaryStore_AFailedWriteIsReportedUntilOneSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-summaries.json")
	store := NewSummaryStore(path)
	_, _, err := store.load(storeNow)
	require.NoError(t, err)

	fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: errors.New("disk full")}).Ops())
	err = store.save(1, []storedSummary{sampleStoredSummary("", AgentClaude, "s10")})
	assert.ErrorContains(t, err, "disk full")
	assert.ErrorContains(t, store.Err(), "disk full")

	fileops.SetOpsForTest(t, (&fileops.Spy{}).Ops())
	require.NoError(t, store.save(2, nil))
	assert.NoError(t, store.Err())
}

// A symlink at the path is written through, as tasks.json's is.
func TestSummaryStore_WritesThroughASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "real.json")
	path := filepath.Join(dir, "task-summaries.json")
	require.NoError(t, os.Symlink(target, path))
	store := NewSummaryStore(path)
	_, _, err := store.load(storeNow)
	require.NoError(t, err)
	require.NoError(t, store.save(1, nil))

	info, err := os.Lstat(path)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the link survives")
	_, err = os.Stat(target)
	assert.NoError(t, err)
}
