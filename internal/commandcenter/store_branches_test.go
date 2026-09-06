package commandcenter

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The command center's two persisted files — the --resume session id and the
// captured conversation history — are written on a path nobody watches: a
// query finishes, the runner saves, and a failure there surfaces only in a
// log line. These tests cover the arms that decide what that failure says,
// because a wrong or swallowed one is invisible until continuity is already
// lost.
//
// The suite runs as root in CI, so permission bits cannot be used to make a
// write fail. Each fixture below breaks the shape of the path instead.

// regularFileAt puts a regular file where a directory is expected, so any
// attempt to descend through it fails with ENOTDIR.
func regularFileAt(t *testing.T, path string) string {
	t.Helper()
	require.NoError(t, os.WriteFile(path, []byte("a regular file, not a directory\n"), 0600))
	return path
}

// directoryAt puts a directory where a file is expected, so opening it for
// writing fails with EISDIR and renaming onto it fails with EEXIST.
func directoryAt(t *testing.T, path string) string {
	t.Helper()
	require.NoError(t, os.Mkdir(path, 0750))
	return path
}

func historyEntry(raw string) HistoryEntry {
	return HistoryEntry{At: time.Unix(0, 0).UTC(), Raw: json.RawMessage(raw)}
}

// ── AppendHistory ────────────────────────────────────────────────────────────

func TestAppendHistoryReportsAnUncreatableParentDirectory(t *testing.T) {
	notADir := regularFileAt(t, filepath.Join(t.TempDir(), "notadir"))
	path := filepath.Join(notADir, "nested", historyFileName)

	err := AppendHistory(path, []HistoryEntry{historyEntry(`{"a":1}`)})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating command center history directory",
		"the caller must be able to tell a directory failure from a write failure")
}

func TestAppendHistoryReportsAnUnopenableFile(t *testing.T) {
	path := directoryAt(t, filepath.Join(t.TempDir(), "history-is-a-directory"))

	err := AppendHistory(path, []HistoryEntry{historyEntry(`{"a":1}`)})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening command center history file")
}

// json.RawMessage validates on marshal rather than passing bytes through, so
// an entry holding something that is not JSON fails here rather than being
// written and only failing on the next LoadHistory.
//
// The good entry before the bad one is what makes the batching contract
// observable. AppendHistory buffers every entry and writes once at the end,
// so a failure partway must discard the whole batch rather than leave the
// entries before it on disk. With only the bad entry, the buffer is empty
// when the error fires and an implementation that flushed on the failure
// path would still write zero bytes — the assertion below would hold for
// exactly the behavior it exists to reject.
func TestAppendHistoryReportsAnUnencodableEntry(t *testing.T) {
	path := filepath.Join(t.TempDir(), historyFileName)

	err := AppendHistory(path, []HistoryEntry{historyEntry(`{"a":1}`), historyEntry("not json")})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "encoding command center history entry")

	_, statErr := os.Stat(path)
	assert.NoError(t, statErr,
		"the file is created by the open above, so this pins that the bad entry aborted before any write")
	data, readErr := os.ReadFile(path)
	require.NoError(t, readErr)
	assert.Empty(t, data, "the entry before the bad one must not survive a batch that failed to encode")
}

// /dev/full accepts an open and reports ENOSPC on write, which is the shape
// of a full disk — the failure this arm exists for.
func TestAppendHistoryReportsAFailedWrite(t *testing.T) {
	requireDevFull(t)

	err := AppendHistory("/dev/full", []HistoryEntry{historyEntry(`{"a":1}`)})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing command center history file")
	assert.Contains(t, err.Error(), "no space left on device")
}

// The mode check is not redundant with the stat. Existence alone does not
// make /dev/full the ENOSPC-on-write character device: bind-mount a regular
// file over it and the write *succeeds*, so this test fails with "An error is
// expected but got nil" rather than skipping — and running as uid 0 is what
// keeps that silent, since root bypasses the mode bits that would otherwise
// turn it into an open failure. A skip is a true statement about the
// environment; a red require.Error is a false statement about AppendHistory.
func requireDevFull(t *testing.T) {
	t.Helper()
	info, err := os.Stat("/dev/full")
	if err != nil {
		t.Skipf("/dev/full is not available on this platform: %v", err)
	}
	if info.Mode()&os.ModeCharDevice == 0 {
		t.Skipf("/dev/full is not a character device (%v)", info.Mode())
	}
}

// ── LoadHistory ──────────────────────────────────────────────────────────────

// A missing file is not an error (covered elsewhere); every other open
// failure is, and must not be quietly folded into the same empty result.
func TestLoadHistoryReportsAnOpenFailureThatIsNotAMissingFile(t *testing.T) {
	notADir := regularFileAt(t, filepath.Join(t.TempDir(), "notadir"))

	entries, err := LoadHistory(filepath.Join(notADir, historyFileName))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "opening command center history file")
	assert.Nil(t, entries, "a real failure must not look like an empty history")
}

// Blank lines are skipped rather than decoded into zero-value entries.
//
// Stated plainly, because this test does not meet the bar the other tests in
// this file do: LoadHistory's `if line == ""` arm is a fast path, and
// deleting it does not make this test fail. A blank line then reaches
// json.Unmarshal, which errors on empty input, and the malformed-line arm
// below skips it just the same. Verified, not assumed — the perturbation
// leaves the whole package green. The branch is equivalent in the mutation
// sense, not merely unreached, so no test can protect it.
//
// What this does pin is the behavior: a blank line never becomes a
// zero-value entry, whatever path skips it. That is worth having, since a
// history file can pick one up from an editor, a truncating filesystem, or a
// future writer — but note it is not AppendHistory that produces one. Its
// separating newline *terminates* a prior write that was cut off mid-line
// rather than adding an empty line after it, which
// TestAppendHistoryAddsSeparatingNewlineAfterUnterminatedPriorWrite already
// pins; an earlier revision of this comment claimed otherwise and was wrong.
func TestLoadHistorySkipsBlankLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), historyFileName)
	require.NoError(t, os.WriteFile(path, []byte(
		`{"at":"1970-01-01T00:00:00Z","raw":{"n":1}}`+"\n"+
			"\n"+
			`{"at":"1970-01-01T00:00:00Z","raw":{"n":2}}`+"\n"+
			"\n",
	), 0600))

	entries, err := LoadHistory(path)

	require.NoError(t, err)
	require.Len(t, entries, 2, "the blank lines must not become entries of their own")
	assert.JSONEq(t, `{"n":1}`, string(entries[0].Raw))
	assert.JSONEq(t, `{"n":2}`, string(entries[1].Raw))
}

// A read that fails partway is reported, unlike a line that fails to parse —
// LoadHistory tolerates the latter deliberately (see its doc comment) and
// must not extend that leniency to losing the rest of the file silently.
//
// The fixture is a good entry followed by a line past the 16 MB max token
// size LoadHistory sets on its scanner, which is what makes the nil check
// below mean anything: the good entry is parsed and appended before the read
// fails, so returning the accumulated entries alongside the error — handing
// callers a silently truncated history — is distinguishable from returning
// nil. An `os.Open`'d directory reaches the same arm far more cheaply, but
// its Scan fails on the first call, leaving the slice nil no matter what
// LoadHistory does with it. That was the earlier fixture here, and the nil
// check read as pinning a property it could not observe.
//
// The cost is ~150ms and a 16 MB file under t.TempDir(). It buys a real
// assertion, and the failure itself is not hypothetical: a history line is a
// raw stream-json line from `claude`, which is why that buffer limit is set
// at all.
func TestLoadHistoryReportsAReadFailureWithoutReturningPartialEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), historyFileName)
	writeHistoryWithAnOverlongLine(t, path)

	entries, err := LoadHistory(path)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading command center history file")
	assert.Nil(t, entries, "a failed read must not hand back the entries it managed to parse first")
}

// writeHistoryWithAnOverlongLine writes one well-formed entry followed by a
// line longer than LoadHistory's scanner will accept, so the read fails after
// the first entry has already been parsed.
func writeHistoryWithAnOverlongLine(t *testing.T, path string) {
	t.Helper()
	f, err := os.Create(path)
	require.NoError(t, err)
	defer f.Close() //nolint:errcheck

	_, err = f.WriteString(`{"at":"1970-01-01T00:00:00Z","raw":{"n":1}}` + "\n")
	require.NoError(t, err)

	const oneMiB = 1 << 20
	chunk := strings.Repeat("x", oneMiB)
	for written := 0; written <= maxHistoryLineBytes; written += oneMiB {
		_, err = f.WriteString(chunk)
		require.NoError(t, err)
	}
	_, err = f.WriteString("\n")
	require.NoError(t, err)
}

// maxHistoryLineBytes mirrors the max token size LoadHistory passes to
// scanner.Buffer. It is duplicated rather than exported: the test needs to
// exceed that limit, and a shared constant would make the production value
// changeable without this fixture noticing that it no longer does.
const maxHistoryLineBytes = 16 * 1024 * 1024

// ── LoadSessionFile ──────────────────────────────────────────────────────────

// The same distinction as LoadHistory's, and it matters more here: a missing
// session file means "no conversation yet" and starts a fresh one, so an
// unreadable file reported as missing would silently abandon the operator's
// running conversation instead of failing the query.
func TestLoadSessionFileReportsAReadFailureThatIsNotAMissingFile(t *testing.T) {
	notADir := regularFileAt(t, filepath.Join(t.TempDir(), "notadir"))

	state, err := LoadSessionFile(filepath.Join(notADir, sessionFileName))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading command center session file")
	assert.Equal(t, SessionState{}, state)
}

// ── atomicWriteFile ──────────────────────────────────────────────────────────

func TestAtomicWriteFileReportsAnUncreatableParentDirectory(t *testing.T) {
	notADir := regularFileAt(t, filepath.Join(t.TempDir(), "notadir"))

	err := atomicWriteFile(filepath.Join(notADir, "nested", "file.json"), []byte("x"), 0600, "test file")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating test file directory",
		"the label the caller passed must reach the message, so the log names which file failed")
}

// The rename is the step that makes the write atomic, and it is the one that
// can still fail after everything else succeeded. Renaming a file onto an
// existing directory fails, which stands in for any rename failure.
func TestAtomicWriteFileReportsAFailedRenameAndLeavesNoTempFile(t *testing.T) {
	dir := t.TempDir()
	path := directoryAt(t, filepath.Join(dir, "target-is-a-directory"))

	err := atomicWriteFile(path, []byte("x"), 0600, "test file")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "replacing test file")

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	require.Len(t, entries, 1, "the deferred cleanup must remove the temp file even on the failure path")
	assert.Equal(t, filepath.Base(path), entries[0].Name())
}

// ── Default paths ────────────────────────────────────────────────────────────

// Both default paths are resolved against the home directory, and both are
// called at startup. An empty HOME is what os.UserHomeDir reports on, and it
// is reachable in a stripped environment such as a systemd unit or a
// container with no passwd entry.
func TestDefaultPathsReportAnUnresolvableHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "")

	historyPath, historyErr := DefaultHistoryFilePath()
	require.Error(t, historyErr)
	assert.Contains(t, historyErr.Error(), "getting home directory")
	assert.Empty(t, historyPath, "no path may be returned alongside the error")

	sessionPath, sessionErr := DefaultSessionFilePath()
	require.Error(t, sessionErr)
	assert.Contains(t, sessionErr.Error(), "getting home directory")
	assert.Empty(t, sessionPath)
}
