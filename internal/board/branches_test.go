package board

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// agmsg's output is a stream of JSON lines written by a program panemux does
// not control, on a host it may not own. Everything here is about what
// happens when that stream, or the files panemux keeps beside it, is not
// what was expected — and about not turning any of it into a fatal error,
// since the board is additive and must never take panemux down with it.

// captureBoardLog redirects the standard logger and returns what was written.
// It restores the previous writer rather than nil, since log.Printf against a
// nil writer panics rather than discarding.
func captureBoardLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// brokenParent returns a path under a regular file, so reading it fails with
// ENOTDIR — an error that is emphatically not "the file does not exist".
func brokenParent(t *testing.T, name string) string {
	t.Helper()
	notADir := filepath.Join(t.TempDir(), "notadir")
	require.NoError(t, os.WriteFile(notADir, []byte("not a directory\n"), 0600))
	return filepath.Join(notADir, name)
}

// ── Parsing agmsg's output ───────────────────────────────────────────────────

// Three kinds of line are skipped rather than parsed, and each is ordinary
// rather than exceptional: a trailing blank line from any writer that
// terminates its output, a line agmsg wrote that this version does not
// understand, and a row of some other type sharing the same stream.
//
// The rows are interleaved so that each skip is followed by a good row. That
// is what separates skipping from stopping: a `break` in place of any of the
// three `continue`s would also produce no bad rows, while silently dropping
// everything after the first one.
func TestParseAgmsgMessageRowsSkipsWhatItCannotUseAndKeepsGoing(t *testing.T) {
	data := []byte("" +
		`{"type":"message_sent","id":"1","team":"panemux","from":"a","to":"b","body":"first"}` + "\n" +
		"\n" +
		`{"type":"message_sent","id":"2","team":"panemux","from":"a","to":"b","body":"after a blank line"}` + "\n" +
		"this is not json at all\n" +
		`{"type":"message_sent","id":"3","team":"panemux","from":"a","to":"b","body":"after a bad line"}` + "\n" +
		`{"type":"agent_joined","id":"x","team":"panemux","from":"a","to":"b","body":"ignored"}` + "\n" +
		`{"type":"message_sent","id":"4","team":"panemux","from":"a","to":"b","body":"after another type"}` + "\n")

	rows := parseAgmsgMessageRows(data, "host-a")

	var bodies []string
	for _, r := range rows {
		bodies = append(bodies, r.Body)
	}
	assert.Equal(t, []string{"first", "after a blank line", "after a bad line", "after another type"}, bodies,
		"each skipped line must cost only itself, never the rows behind it")
}

// A version string that is not three numbers is not a version. Both halves
// matter: a non-numeric component and a negative one are equally not a
// release, and accepting either would let the mismatch warning compare
// nonsense and stay silent when it should not.
func TestParseAgmsgReleaseRejectsNonNumericAndNegativeComponents(t *testing.T) {
	for _, version := range []string{"1.x.3", "1.2.z", "1.-2.3", "-1.2.3"} {
		t.Run(version, func(t *testing.T) {
			_, ok := parseAgmsgRelease(version)
			assert.False(t, ok, "%q is not a release", version)
		})
	}

	got, ok := parseAgmsgRelease("1.2.3")
	require.True(t, ok, "the rejection above must not be rejecting everything")
	assert.Equal(t, agmsgRelease{major: 1, minor: 2, patch: 3}, got)
}

// ── The files panemux keeps beside agmsg ─────────────────────────────────────

// A missing file is the normal state before the first run and is not an
// error. Every other read failure is, and folding the two together would let
// an unreadable file look like a fresh install — silently discarding the
// panes already onboarded, or the relay's place in the message stream.
func TestStateFilesDistinguishMissingFromUnreadable(t *testing.T) {
	paneIDs, err := LoadBootstrapState(brokenParent(t, "board-bootstrap-state.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading bootstrap state file")
	assert.Nil(t, paneIDs, "an unreadable file must not look like an empty one")

	entries, err := LoadCursorFile(brokenParent(t, "board-relay-cursor.json"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "reading relay cursor file")
	assert.Nil(t, entries)
}

// The rename is the step that makes the write atomic, and the one that can
// still fail after everything before it succeeded. Renaming onto an existing
// directory fails, which stands in for any rename failure.
func TestAtomicWriteFileReportsAFailedRename(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "target-is-a-directory")
	require.NoError(t, os.Mkdir(target, 0750))

	err := atomicWriteFile(target, []byte("x"), 0600, "relay cursor file")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "replacing relay cursor file",
		"the label the caller passed must reach the message, so the log names which file failed")

	entries, readErr := os.ReadDir(dir)
	require.NoError(t, readErr)
	assert.Len(t, entries, 1, "the deferred cleanup must remove the temp file even on the failure path")
}

// ── Running agmsg's own scripts ──────────────────────────────────────────────

// The exec wrapper names the program it could not run, as its own prefix.
//
// The prefix, not a Contains: os/exec's own error is already
// "fork/exec <path>: ...", so asserting that the path appears somewhere
// would hold with the wrap deleted — verified, it does. Only the leading
// "exec <path>: " is contributed by this function, so only a prefix check
// distinguishes the wrap from the error it wraps.
func TestExecLocalCommandNamesTheProgramItCouldNotRun(t *testing.T) {
	const missing = "/nonexistent/agmsg/scripts/api.sh"

	out, err := execLocalCommand(context.Background(), missing, "since")

	require.Error(t, err)
	assert.True(t, strings.HasPrefix(err.Error(), "exec "+missing+": "),
		"want the wrap's own prefix, got %q", err.Error())
	assert.Nil(t, out)
}

func TestExecLocalCommandReturnsStdoutOnSuccess(t *testing.T) {
	out, err := execLocalCommand(context.Background(), "/bin/echo", "board output")

	require.NoError(t, err)
	assert.Equal(t, "board output\n", string(out))
}

// ── The relay's cursor state ─────────────────────────────────────────────────

// Cursors are persisted as a flat list across teams, so a panemux whose
// agent_board.team was changed will read back entries belonging to the old
// one. Adopting them would resume the new team's poll at a position derived
// from a different team's message stream.
func TestLoadCursorsIgnoresEntriesForAnotherTeam(t *testing.T) {
	r := newTestRelay(NewBoardCache(), nil, nil)

	r.LoadCursors([]CursorEntry{
		{Host: "host-a", Team: "panemux", Cursor: "10"},
		{Host: "host-b", Team: "some-other-team", Cursor: "99"},
	})

	assert.Equal(t, []CursorEntry{{Host: "host-a", Team: "panemux", Cursor: "10"}}, r.Cursors(),
		"an entry from another team must not become this team's cursor")
}

// Cursors are written to disk on every poll, so an unstable order would make
// the file churn on every tick with no change of meaning.
func TestCursorsAreSortedByHost(t *testing.T) {
	r := newTestRelay(NewBoardCache(), nil, nil)
	r.LoadCursors([]CursorEntry{
		{Host: "host-c", Team: "panemux", Cursor: "3"},
		{Host: "host-a", Team: "panemux", Cursor: "1"},
		{Host: "host-b", Team: "panemux", Cursor: "2"},
	})

	var hosts []string
	for _, e := range r.Cursors() {
		hosts = append(hosts, e.Host)
	}
	assert.Equal(t, []string{"host-a", "host-b", "host-c"}, hosts)
}

// ── Relaying to another host ─────────────────────────────────────────────────

// A pane mapped to a host the relay has no client for is a configuration the
// operator can produce simply by adding a pane on a host where agmsg is not
// installed. The message is still recorded locally — the dashboard shows it —
// and only the delivery is skipped, with a line naming both ends so the
// operator can see which pane is unreachable and why.
func TestRelaySkipsDeliveryToAHostWithNoClient(t *testing.T) {
	logs := captureBoardLog(t)
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "claude-a", To: "codex-b", Body: "please review"},
	}}
	cache := NewBoardCache()
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA},
		map[string]string{"claude-a": "host-a", "codex-b": "host-unreachable"})

	require.NoError(t, r.Poll(context.Background()),
		"an undeliverable message must not fail the whole poll")

	assert.Contains(t, logs.String(), `no client for host "host-unreachable"`)
	assert.Contains(t, logs.String(), `pane "codex-b"`)
	assert.Len(t, cache.MessagesSince(0), 1,
		"the message is still recorded locally; only the cross-host delivery is skipped")
}

// A Send that fails is logged and the poll continues. The alternative —
// returning the error — would abort the rest of the tick, so one unreachable
// host would stop messages flowing between all the others.
func TestRelayLogsAFailedSendAndKeepsPolling(t *testing.T) {
	logs := captureBoardLog(t)
	hostA := &fakeAgmsgClient{hostID: "host-a", sinceRows: []Row{
		{ID: "1", Team: "panemux", From: "claude-a", To: "codex-b", Body: "please review"},
	}}
	hostB := &fakeAgmsgClient{hostID: "host-b", sendErr: errors.New("ssh: connection refused")}
	cache := NewBoardCache()
	r := newTestRelay(cache, map[string]AgmsgClient{"host-a": hostA, "host-b": hostB},
		map[string]string{"claude-a": "host-a", "codex-b": "host-b"})

	require.NoError(t, r.Poll(context.Background()),
		"one host's delivery failure must not abort the tick for every other host")

	assert.Contains(t, logs.String(), `relaying to host "host-b" failed`)
	assert.Contains(t, logs.String(), "ssh: connection refused")
	assert.Len(t, cache.MessagesSince(0), 1)
}
