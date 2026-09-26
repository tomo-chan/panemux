package tasks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/fileops"
	"panemux/internal/homedir"
)

func recordsPath(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "panemux", "tasks.json")
}

func TestRecordStore_MissingFileIsNoRecords(t *testing.T) {
	store := NewRecordStore(recordsPath(t))

	records, err := store.Records()
	require.NoError(t, err)
	assert.Empty(t, records)
}

func TestRecordStore_PutPersistsAcrossStores(t *testing.T) {
	path := recordsPath(t)
	store := NewRecordStore(path)

	saved, err := store.Put(Record{
		Host: "build-box", Agent: "claude", SessionID: "7c21e0a4", Done: true, Labels: []string{"payment", "sprint-42"},
	})
	require.NoError(t, err)
	assert.Equal(t, Record{
		Host: "build-box", Agent: "claude", SessionID: "7c21e0a4", Done: true, Labels: []string{"payment", "sprint-42"},
	}, saved)

	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm(), "the record file is private to the operator")

	reloaded, err := NewRecordStore(path).Records()
	require.NoError(t, err)
	assert.Equal(t, map[RecordKey]Record{
		{Host: "build-box", Agent: "claude", SessionID: "7c21e0a4"}: saved,
	}, reloaded)
}

// A record is keyed by host, agent and session ID together: the same session
// ID on two hosts, or under two agents, is two tasks.
func TestRecordStore_KeysAreHostAgentAndSession(t *testing.T) {
	store := NewRecordStore(recordsPath(t))
	for _, rec := range []Record{
		{Host: "", Agent: "claude", SessionID: "s1", Labels: []string{"here"}},
		{Host: "build-box", Agent: "claude", SessionID: "s1", Labels: []string{"remote"}},
		{Host: "", Agent: "codex", SessionID: "s1", Done: true},
	} {
		_, err := store.Put(rec)
		require.NoError(t, err)
	}

	records, err := store.Records()
	require.NoError(t, err)
	require.Len(t, records, 3)
	assert.Equal(t, []string{"here"}, records[RecordKey{Host: "", Agent: "claude", SessionID: "s1"}].Labels)
	assert.Equal(t, []string{"remote"}, records[RecordKey{Host: "build-box", Agent: "claude", SessionID: "s1"}].Labels)
	assert.True(t, records[RecordKey{Host: "", Agent: "codex", SessionID: "s1"}].Done)
}

func TestRecordStore_PutReplacesTheWholeRecord(t *testing.T) {
	store := NewRecordStore(recordsPath(t))
	_, err := store.Put(Record{Agent: "claude", SessionID: "s1", Done: true, Labels: []string{"a", "b"}})
	require.NoError(t, err)

	_, err = store.Put(Record{Agent: "claude", SessionID: "s1", Labels: []string{"b"}})
	require.NoError(t, err)

	records, err := store.Records()
	require.NoError(t, err)
	assert.Equal(t, Record{Agent: "claude", SessionID: "s1", Labels: []string{"b"}},
		records[RecordKey{Agent: "claude", SessionID: "s1"}])
}

// Clearing both done and every label removes the record, so the file only
// ever holds tasks that carry something.
func TestRecordStore_AnEmptyRecordIsRemoved(t *testing.T) {
	path := recordsPath(t)
	store := NewRecordStore(path)
	_, err := store.Put(Record{Agent: "claude", SessionID: "s1", Done: true})
	require.NoError(t, err)
	_, err = store.Put(Record{Agent: "claude", SessionID: "s2", Labels: []string{"keep"}})
	require.NoError(t, err)

	_, err = store.Put(Record{Agent: "claude", SessionID: "s1", Labels: []string{}})
	require.NoError(t, err)

	reloaded, err := NewRecordStore(path).Records()
	require.NoError(t, err)
	assert.Equal(t, map[RecordKey]Record{
		{Agent: "claude", SessionID: "s2"}: {Agent: "claude", SessionID: "s2", Labels: []string{"keep"}},
	}, reloaded)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(data), `"s1"`)
}

func TestRecordStore_FileFormat(t *testing.T) {
	path := recordsPath(t)
	store := NewRecordStore(path)
	// Put in an order that is none of host, agent or session order, with
	// records that tie on host, and on host and agent, so each key decides.
	for _, rec := range []Record{
		{Host: "b", Agent: "codex", SessionID: "s1", Done: true},
		{Host: "b", Agent: "claude", SessionID: "s3", Labels: []string{"x"}},
		{Host: "", Agent: "claude", SessionID: "s9", Done: true},
		{Host: "b", Agent: "claude", SessionID: "s2", Done: true},
		{Host: "a", Agent: "codex", SessionID: "s0", Done: true},
	} {
		_, err := store.Put(rec)
		require.NoError(t, err)
	}

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"version": 1,
		"records": [
			{"host": "", "agent": "claude", "session_id": "s9", "done": true},
			{"host": "a", "agent": "codex", "session_id": "s0", "done": true},
			{"host": "b", "agent": "claude", "session_id": "s2", "done": true},
			{"host": "b", "agent": "claude", "session_id": "s3", "labels": ["x"]},
			{"host": "b", "agent": "codex", "session_id": "s1", "done": true}
		]
	}`, string(data), "records are written in host, agent, session order")
}

func TestRecordStore_ReturnedRecordsAreCopies(t *testing.T) {
	store := NewRecordStore(recordsPath(t))
	_, err := store.Put(Record{Agent: "claude", SessionID: "s1", Labels: []string{"a"}})
	require.NoError(t, err)

	records, err := store.Records()
	require.NoError(t, err)
	records[RecordKey{Agent: "claude", SessionID: "s1"}].Labels[0] = "changed"

	again, err := store.Records()
	require.NoError(t, err)
	assert.Equal(t, []string{"a"}, again[RecordKey{Agent: "claude", SessionID: "s1"}].Labels)
}

// A file that cannot be understood is never overwritten: every write fails
// until the file is fixed, so records a newer panemux wrote, or a person
// edited, are not silently replaced by an empty set.
func TestRecordStore_UnreadableFileIsReportedAndNotOverwritten(t *testing.T) {
	cases := []struct {
		name    string
		content string
		wantErr string
	}{
		{name: "not JSON", content: "{", wantErr: "parsing task record file"},
		{name: "newer version", content: `{"version":2,"records":[]}`, wantErr: "unsupported task record file version 2"},
		{name: "no version", content: `{"records":[]}`, wantErr: "unsupported task record file version 0"},
		{
			name:    "invalid record",
			content: `{"version":1,"records":[{"agent":"claude","session_id":"bad id"}]}`,
			wantErr: "invalid session ID",
		},
		{
			name:    "invalid label",
			content: `{"version":1,"records":[{"agent":"claude","session_id":"s","labels":["a\u0007b"]}]}`,
			wantErr: "control character",
		},
		{
			name:    "duplicate record",
			content: `{"version":1,"records":[{"agent":"claude","session_id":"s"},{"agent":"claude","session_id":"s"}]}`,
			wantErr: "duplicate task record",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := recordsPath(t)
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
			require.NoError(t, os.WriteFile(path, []byte(tc.content), 0o600))
			store := NewRecordStore(path)

			_, err := store.Records()
			require.ErrorContains(t, err, tc.wantErr)

			_, err = store.Put(Record{Agent: "claude", SessionID: "s1", Done: true})
			require.ErrorContains(t, err, tc.wantErr)

			data, err := os.ReadFile(path)
			require.NoError(t, err)
			assert.Equal(t, tc.content, string(data))
		})
	}
}

// A load that failed is tried again on the next access, so fixing the file
// does not need a restart.
func TestRecordStore_ALoadFailureIsRetried(t *testing.T) {
	path := recordsPath(t)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte("{"), 0o600))
	store := NewRecordStore(path)
	_, err := store.Records()
	require.Error(t, err)

	fixed := `{"version":1,"records":[{"agent":"claude","session_id":"s","done":true}]}`
	require.NoError(t, os.WriteFile(path, []byte(fixed), 0o600))
	records, err := store.Records()
	require.NoError(t, err)
	assert.True(t, records[RecordKey{Agent: "claude", SessionID: "s"}].Done)
}

// Once loaded, the file is not read again: panemux is its only writer.
func TestRecordStore_LoadsOnce(t *testing.T) {
	path := recordsPath(t)
	store := NewRecordStore(path)
	_, err := store.Put(Record{Agent: "claude", SessionID: "s", Done: true})
	require.NoError(t, err)

	require.NoError(t, os.Remove(path))
	records, err := store.Records()
	require.NoError(t, err)
	assert.Len(t, records, 1)
}

func TestRecordStore_AFailedWriteChangesNothing(t *testing.T) {
	path := recordsPath(t)
	store := NewRecordStore(path)
	_, err := store.Put(Record{Agent: "claude", SessionID: "s1", Labels: []string{"before"}})
	require.NoError(t, err)

	fileops.SetOpsForTest(t, (&fileops.Spy{RenameErr: errors.New("disk full")}).Ops())
	_, err = store.Put(Record{Agent: "claude", SessionID: "s1", Labels: []string{"after"}})
	require.ErrorContains(t, err, "disk full")

	records, err := store.Records()
	require.NoError(t, err)
	assert.Equal(t, []string{"before"}, records[RecordKey{Agent: "claude", SessionID: "s1"}].Labels,
		"the record in memory still matches the file")
}

func TestRecordStore_PutValidates(t *testing.T) {
	cases := []struct {
		name    string
		wantErr string
		record  Record
	}{
		{name: "no session ID", record: Record{Agent: "claude"}, wantErr: "invalid session ID"},
		{name: "session ID with a slash", record: Record{Agent: "claude", SessionID: "../x"}, wantErr: "invalid session ID"},
		{name: "unknown agent", record: Record{Agent: "vim", SessionID: "s"}, wantErr: `unknown agent "vim"`},
		{name: "no agent", record: Record{SessionID: "s"}, wantErr: `unknown agent ""`},
		{
			name: "bad label", record: Record{Agent: "claude", SessionID: "s", Labels: []string{" "}},
			wantErr: "label is empty",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := recordsPath(t)
			store := NewRecordStore(path)

			_, err := store.Put(tc.record)
			require.ErrorIs(t, err, ErrInvalidRecord)
			assert.ErrorContains(t, err, tc.wantErr)
			_, statErr := os.Stat(path)
			assert.True(t, os.IsNotExist(statErr), "nothing is written")
		})
	}
}

func TestNormalizeLabels(t *testing.T) {
	longest := strings.Repeat("あ", MaxLabelLength)
	tooMany := make([]string, MaxLabels+1)
	for i := range tooMany {
		tooMany[i] = "l" + strings.Repeat("x", i)
	}
	cases := []struct {
		name    string
		wantErr string
		in      []string
		want    []string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "empty", in: []string{}, want: nil},
		{name: "kept in the order given", in: []string{"b", "a"}, want: []string{"b", "a"}},
		{name: "surrounding space trimmed", in: []string{"  payment "}, want: []string{"payment"}},
		{name: "duplicates dropped", in: []string{"a", "b", "a", " b"}, want: []string{"a", "b"}},
		{name: "case is significant", in: []string{"Docs", "docs"}, want: []string{"Docs", "docs"}},
		{name: "inner space and non-ASCII kept", in: []string{"sprint 42", "決済"}, want: []string{"sprint 42", "決済"}},
		{name: "longest allowed", in: []string{longest}, want: []string{longest}},
		{name: "blank", in: []string{"a", "   "}, wantErr: "label is empty"},
		{name: "too long", in: []string{strings.Repeat("x", MaxLabelLength+1)}, wantErr: "longer than 32 characters"},
		{name: "control character", in: []string{"a\tb"}, wantErr: "control character"},
		{name: "newline", in: []string{"a\nb"}, wantErr: "control character"},
		{name: "control character first", in: []string{"\x01a"}, wantErr: "control character"},
		{name: "invalid UTF-8", in: []string{"a\xffb"}, wantErr: "not valid UTF-8"},
		{name: "most allowed", in: tooMany[:MaxLabels], want: tooMany[:MaxLabels]},
		{name: "too many", in: tooMany, wantErr: "more than 20 labels"},
		{name: "duplicates do not count toward the limit", in: append(append([]string{}, tooMany[:MaxLabels]...), "l"),
			want: tooMany[:MaxLabels]},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormalizeLabels(tc.in)
			if tc.wantErr != "" {
				require.ErrorIs(t, err, ErrInvalidRecord)
				assert.ErrorContains(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDefaultRecordsPath(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)

	path, err := DefaultRecordsPath()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(home, ".config", "panemux", "tasks.json"), path)
}

func TestDefaultRecordsPath_NoHome(t *testing.T) {
	homedir.SetFailingForTest(t, errors.New("no home"))

	_, err := DefaultRecordsPath()
	require.ErrorContains(t, err, "no home")
}

// A store made with no path resolves ~/.config/panemux/tasks.json on first
// use, and reports a home directory it cannot find as a load failure.
func TestRecordStore_DefaultPath(t *testing.T) {
	t.Run("resolved on first use", func(t *testing.T) {
		home := t.TempDir()
		homedir.SetForTest(t, home)
		store := NewRecordStore("")

		_, err := store.Put(Record{Agent: "claude", SessionID: "s", Done: true})
		require.NoError(t, err)
		_, err = os.Stat(filepath.Join(home, ".config", "panemux", "tasks.json"))
		require.NoError(t, err)
	})
	t.Run("no home directory", func(t *testing.T) {
		homedir.SetFailingForTest(t, errors.New("no home"))
		store := NewRecordStore("")

		_, err := store.Records()
		require.ErrorContains(t, err, "no home")
	})
}

func TestRecordStore_ReadErrorOtherThanMissing(t *testing.T) {
	path := recordsPath(t)
	require.NoError(t, os.MkdirAll(path, 0o700), "a directory where the file should be")

	_, err := NewRecordStore(path).Records()
	require.ErrorContains(t, err, "reading task record file")
}
