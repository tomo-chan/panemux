package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
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
		"text too long": entry(func(e *storedSummary) {
			e.Text = strings.Repeat("a", maxSummaryTextBytes+len(truncationMark)+1)
		}),
		"empty text":     entry(func(e *storedSummary) { e.Text = "" }),
		"blank text":     entry(func(e *storedSummary) { e.Text = " \n\t" }),
		"too many items": entry(func(e *storedSummary) { e.Remaining = slices.Repeat([]string{"x"}, maxSummaryRemaining+1) }),
		"item too long": entry(func(e *storedSummary) {
			e.Remaining = []string{strings.Repeat("a", maxSummaryItemBytes+len(truncationMark)+1)}
		}),
		"blank item":      entry(func(e *storedSummary) { e.Remaining = []string{"ok", "  "} }),
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

// A summary cut at its limit carries the cut's mark past the limit, and the
// file keeps it: loading it again neither moves the file aside nor drops it.
func TestSummaryStore_KeepsASummaryCutAtItsLimit(t *testing.T) {
	out, err := json.Marshal(map[string]any{"structured_output": map[string]any{
		"summary":   strings.Repeat("a", maxSummaryTextBytes+50),
		"remaining": slices.Repeat([]string{strings.Repeat("b", maxSummaryItemBytes+50)}, maxSummaryRemaining),
	}})
	require.NoError(t, err)
	summary, err := parseSummaryOutput(out)
	require.NoError(t, err)
	require.Len(t, summary.Text, maxSummaryTextBytes+len(truncationMark))
	entry := sampleStoredSummary("", AgentClaude, "s10")
	entry.Text, entry.Remaining = summary.Text, summary.Remaining

	path := filepath.Join(t.TempDir(), "task-summaries.json")
	store := NewSummaryStore(path)
	_, _, err = store.load(storeNow)
	require.NoError(t, err)
	require.NoError(t, store.save(1, []storedSummary{entry}))
	loaded, moved, err := NewSummaryStore(path).load(storeNow)
	require.NoError(t, err)
	assert.Empty(t, moved)
	assert.Equal(t, []storedSummary{entry}, loaded)
}

// Two files moved aside in the same second both survive.
func TestSummaryStore_MovingAsideNeverReplacesAnEarlierBadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-summaries.json")
	var moved []string
	for i, content := range []string{"{first", "{second", "{third"} {
		writeStoreFile(t, path, content)
		_, m, err := NewSummaryStore(path).load(storeNow)
		require.NoError(t, err, "move %d", i)
		moved = append(moved, m)
	}
	assert.Equal(t, []string{
		path + ".bad-20261010T120000Z", path + ".bad-20261010T120000Z-1", path + ".bad-20261010T120000Z-2",
	}, moved)
	for i, content := range []string{"{first", "{second", "{third"} {
		kept, err := os.ReadFile(moved[i])
		require.NoError(t, err)
		assert.Equal(t, content, string(kept))
	}
}

// A file takes the last of the names it can be moved aside to in one second;
// once every one is taken, the next file is left where it is and nothing is
// written over it.
func TestSummaryStore_GivesUpMovingAsideWhenEveryNameIsTaken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-summaries.json")
	base := path + ".bad-20261010T120000Z"
	writeStoreFile(t, base, "")
	for n := 1; n < summaryStoreBadLimit-1; n++ {
		writeStoreFile(t, fmt.Sprintf("%s-%d", base, n), "")
	}
	writeStoreFile(t, path, "{broken")
	_, moved, err := NewSummaryStore(path).load(storeNow)
	require.NoError(t, err)
	assert.Equal(t, fmt.Sprintf("%s-%d", base, summaryStoreBadLimit-1), moved)

	writeStoreFile(t, path, "{broken")
	store := NewSummaryStore(path)
	_, _, err = store.load(storeNow)
	assert.ErrorContains(t, err, "moving aside task summary file")
	kept, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Equal(t, "{broken", string(kept))
	assert.ErrorContains(t, store.save(1, nil), "not loaded")
}

// A save that fails does not let an older generation, arriving after it,
// write a set that lacks the newer summaries and report saving as healthy.
func TestSummaryStore_AnOlderSaveAfterAFailedNewerOneIsDropped(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-summaries.json")
	store := NewSummaryStore(path)
	_, _, err := store.load(storeNow)
	require.NoError(t, err)

	fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: errors.New("disk full")}).Ops())
	require.Error(t, store.save(2, []storedSummary{sampleStoredSummary("", AgentClaude, "newer")}))
	fileops.SetOpsForTest(t, (&fileops.Spy{}).Ops())
	require.NoError(t, store.save(1, []storedSummary{sampleStoredSummary("", AgentClaude, "older")}))

	assert.ErrorContains(t, store.Err(), "disk full", "the newest summaries are still unsaved")
	_, err = os.Stat(path)
	assert.ErrorIs(t, err, os.ErrNotExist, "the older set is not written")

	newest := []storedSummary{sampleStoredSummary("", AgentClaude, "newest")}
	require.NoError(t, store.save(3, newest))
	assert.NoError(t, store.Err())
	loaded, _, err := NewSummaryStore(path).load(storeNow)
	require.NoError(t, err)
	assert.Equal(t, newest, loaded)
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

func TestSummaryStore_AFailedDirectoryCreationIsReported(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "config")
	store := NewSummaryStore(filepath.Join(parent, "task-summaries.json"))
	_, _, err := store.load(storeNow)
	require.NoError(t, err, "a missing directory is an empty store")
	writeStoreFile(t, parent, "")

	err = store.save(1, nil)
	assert.ErrorContains(t, err, "creating task summary directory")
	assert.Equal(t, err, store.Err())
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

// Each agent's summarizer version is made from the instruction that agent's
// summarizer is given.
func TestSummarizerVersion_FollowsTheAgentsInstruction(t *testing.T) {
	version := func(agent, instruction string) string {
		sum := sha256.Sum256(fmt.Appendf(nil, "%d\x00%s\x00%s\x00%s",
			summaryPipelineVersion, agent, instruction, summarySchema))
		return hex.EncodeToString(sum[:])
	}
	assert.Equal(t, version(AgentClaude, summaryInstruction), summarizerVersion(AgentClaude))
	assert.Equal(t, version(AgentCodex, codexSummaryInstruction), summarizerVersion(AgentCodex))
}
