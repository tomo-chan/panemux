package commandcenter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadHistoryMissingFileReturnsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.jsonl")

	entries, err := LoadHistory(path)

	require.NoError(t, err)
	assert.Empty(t, entries)
}

func TestAppendThenLoadHistoryRoundTrips(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "command-center-history.jsonl")
	at := time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC)

	err := AppendHistory(path, []HistoryEntry{
		{At: at, Raw: json.RawMessage(`{"type":"system","subtype":"init","session_id":"abc"}`)},
		{At: at.Add(time.Second), Raw: json.RawMessage(`{"type":"result","result":"done"}`)},
	})
	require.NoError(t, err)

	entries, err := LoadHistory(path)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Equal(t, at, entries[0].At.UTC())
	assert.JSONEq(t, `{"type":"system","subtype":"init","session_id":"abc"}`, string(entries[0].Raw))
	assert.JSONEq(t, `{"type":"result","result":"done"}`, string(entries[1].Raw))

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestAppendHistoryAppendsAcrossMultipleCalls(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")

	require.NoError(t, AppendHistory(path, []HistoryEntry{{Raw: json.RawMessage(`{"n":1}`)}}))
	require.NoError(t, AppendHistory(path, []HistoryEntry{{Raw: json.RawMessage(`{"n":2}`)}}))

	entries, err := LoadHistory(path)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.JSONEq(t, `{"n":1}`, string(entries[0].Raw))
	assert.JSONEq(t, `{"n":2}`, string(entries[1].Raw))
}

func TestAppendHistoryEmptyEntriesIsNoop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")

	require.NoError(t, AppendHistory(path, nil))

	_, err := os.Stat(path)
	assert.True(t, os.IsNotExist(err))
}

func TestLoadHistorySkipsTruncatedTrailingLine(t *testing.T) {
	path := filepath.Join(t.TempDir(), "history.jsonl")
	line := `{"at":"2026-08-10T12:00:00Z","raw":{"n":1}}` + "\n"
	truncated := `{"at":"2026-08-10T12:00:01Z","raw":{"n":2` // crash mid-write, no trailing newline
	require.NoError(t, os.WriteFile(path, []byte(line+truncated), 0600))

	entries, err := LoadHistory(path)

	require.NoError(t, err)
	require.Len(t, entries, 1)
	assert.JSONEq(t, `{"n":1}`, string(entries[0].Raw))
}

func TestLoadHistorySkipsMalformedMiddleLine(t *testing.T) {
	// A malformed line is skipped wherever it appears in the file, not just
	// at the end: history is best-effort, not authoritative state, so one
	// bad line (e.g. a truncated write later followed by more appends, at
	// which point it's no longer the trailing line — see
	// TestAppendHistoryAddsSeparatingNewlineAfterUnterminatedPriorWrite)
	// must not permanently break every read of the file, including the
	// good lines around it.
	path := filepath.Join(t.TempDir(), "history.jsonl")
	content := `{"at":"2026-08-10T12:00:00Z","raw":{"n":1}}` + "\n" +
		`not json at all` + "\n" +
		`{"at":"2026-08-10T12:00:02Z","raw":{"n":3}}` + "\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))

	entries, err := LoadHistory(path)

	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.JSONEq(t, `{"n":1}`, string(entries[0].Raw))
	assert.JSONEq(t, `{"n":3}`, string(entries[1].Raw))
}

func TestAppendHistoryAddsSeparatingNewlineAfterUnterminatedPriorWrite(t *testing.T) {
	// Simulates AppendHistory being interrupted mid-write (crash, power
	// loss) leaving a truncated final line with no trailing newline, then
	// called again normally after the process restarts. Without a
	// separating newline, the next entry's bytes would concatenate directly
	// onto the truncated line, producing one new, unrecoverable garbled
	// line instead of one isolated bad line plus one good one.
	path := filepath.Join(t.TempDir(), "history.jsonl")
	truncated := `{"at":"2026-08-10T12:00:00Z","raw":{"n":1` // no trailing newline
	require.NoError(t, os.WriteFile(path, []byte(truncated), 0600))

	require.NoError(t, AppendHistory(path, []HistoryEntry{{Raw: json.RawMessage(`{"n":2}`)}}))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, truncated+"\n"+`{"at":"0001-01-01T00:00:00Z","raw":{"n":2}}`+"\n", string(raw))

	entries, err := LoadHistory(path)
	require.NoError(t, err)
	require.Len(t, entries, 1, "only the new, well-formed entry should be readable back")
	assert.JSONEq(t, `{"n":2}`, string(entries[0].Raw))
}

func TestDefaultHistoryFilePath(t *testing.T) {
	path, err := DefaultHistoryFilePath()

	require.NoError(t, err)
	assert.Contains(t, path, filepath.Join(".config", "panemux", "command-center-history.jsonl"))
}
