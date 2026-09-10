package commandcenter

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Every failure in a command-center turn reaches the operator as an
// EventError on the WS stream and nowhere else — there is no HTTP status to
// carry it and no caller to return it to. These tests cover the arms that
// decide what that event says, and, just as importantly, whether the turn
// stops there or carries on.
//
// The distinction is the reason most of these are worth pinning: a setup
// failure must abort before the subprocess is launched, a failed history
// write must not fail an otherwise-good query, and a failed session-id write
// must abort rather than report success, because reporting done would leave
// the next turn silently starting a new conversation.

// runnerFixture is the seam-by-seam Runner these tests need. newTestRunner
// covers the happy path; this one exists to fail exactly one seam at a time.
type runnerFixture struct {
	started *bool
	// waitErr is what the fake subprocess's Wait returns. Assign it before
	// calling query to drive the turn down its failure arm; the zero value
	// is a subprocess that exited cleanly.
	waitErr error
	config  RunnerConfig
}

func newRunnerFixture(t *testing.T, stdout string) *runnerFixture {
	t.Helper()
	started := false
	f := &runnerFixture{started: &started}
	f.config = RunnerConfig{
		ClaudeBin:   "claude",
		SessionPath: filepath.Join(t.TempDir(), "session.json"),
		HistoryPath: filepath.Join(t.TempDir(), "history.jsonl"),
		BuildMCPConfig: func() (string, func(), error) {
			return "/tmp/sample-mcp-config.json", func() {}, nil
		},
		NewCommand: func(_ context.Context, _, _ string, _ ...string) cmdRunner {
			started = true
			return &fakeCmd{stdout: io.NopCloser(strings.NewReader(stdout)), waitErr: f.waitErr}
		},
		Now: func() time.Time { return time.Unix(0, 0).UTC() },
	}
	return f
}

// query runs one turn to completion and returns every event it emitted.
func (f *runnerFixture) query(t *testing.T) []Event {
	t.Helper()
	events, err := NewRunner(f.config).Query(context.Background(), "how is the board")
	require.NoError(t, err)
	return drain(t, events)
}

// errorEvents returns just the EventError messages, so a test can assert
// both what was reported and that nothing else was.
func errorEvents(events []Event) []string {
	var msgs []string
	for _, ev := range events {
		if ev.Type == EventError {
			msgs = append(msgs, ev.Err)
		}
	}
	return msgs
}

func hasDone(events []Event) bool {
	for _, ev := range events {
		if ev.Type == EventDone {
			return true
		}
	}
	return false
}

// terminalEvent returns the turn's single terminal event. It asserts there is
// exactly one, which is the property #214 was filed about: an EventError and
// an EventDone both claim to be the last frame, so a turn that emits both has
// no terminal frame at all.
func terminalEvent(t *testing.T, events []Event) Event {
	t.Helper()
	var terminal []Event
	for _, ev := range events {
		if ev.Type == EventError || ev.Type == EventDone {
			terminal = append(terminal, ev)
		}
	}
	require.Len(t, terminal, 1, "a turn emits exactly one terminal event, got %v", terminal)
	return terminal[0]
}

// captureRunnerLog redirects the standard logger and returns what was
// written, so a test can assert that a failure the WS stream deliberately
// does not carry still reaches the operator's terminal. It restores the
// previous writer rather than nil, since log.Printf against a nil writer
// panics rather than discarding.
func captureRunnerLog(t *testing.T) *bytes.Buffer {
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

// brokenParentPath returns a path whose parent is a regular file, so both
// reading it and creating it fail with ENOTDIR.
func brokenParentPath(t *testing.T, name string) string {
	t.Helper()
	notADir := filepath.Join(t.TempDir(), "notadir")
	require.NoError(t, os.WriteFile(notADir, []byte("not a directory\n"), 0600))
	return filepath.Join(notADir, name)
}

// ── Setup failures, which must abort before the subprocess is launched ────────

func TestQueryReportsAnUnreadableSessionFile(t *testing.T) {
	f := newRunnerFixture(t, "")
	f.config.SessionPath = brokenParentPath(t, "session.json")

	events := f.query(t)

	require.Len(t, errorEvents(events), 1)
	assert.Contains(t, errorEvents(events)[0], "loading command center session state")
	assert.False(t, hasDone(events), "a turn that never ran must not report done")
	assert.False(t, *f.started, "the subprocess must not be launched after a setup failure")
}

func TestQueryReportsAFailureToMintASessionID(t *testing.T) {
	f := newRunnerFixture(t, "")
	f.config.NewSessionID = func() (string, error) { return "", errors.New("no entropy available") }

	events := f.query(t)

	require.Len(t, errorEvents(events), 1)
	assert.Contains(t, errorEvents(events)[0], "no entropy available")
	assert.False(t, hasDone(events))
	assert.False(t, *f.started)
}

// The work directory is created after the MCP config, so this also pins that
// the config's cleanup still runs when a later step fails — otherwise the
// token-bearing temp file that BuildMCPConfig writes (docs/security.md) would
// outlive the query that needed it.
func TestQueryReportsAFailureToCreateTheWorkDirAndStillCleansUpTheMCPConfig(t *testing.T) {
	f := newRunnerFixture(t, "")
	cleaned := false
	f.config.BuildMCPConfig = func() (string, func(), error) {
		return "/tmp/sample-mcp-config.json", func() { cleaned = true }, nil
	}
	f.config.NewWorkDir = func() (string, func(), error) {
		return "", nil, errors.New("no space left on device")
	}

	events := f.query(t)

	require.Len(t, errorEvents(events), 1)
	// A bare error, deliberately: run() emits this arm as errorEvent("%v", err)
	// and adds no prefix of its own, so asserting the "creating command center
	// work directory" context here would only round-trip this fixture's own
	// literal. That context comes from NewWorkDir's own wrap, and is asserted
	// where it lives — TestNewWorkDirFailureNamesTheStepThatFailed.
	assert.Contains(t, errorEvents(events)[0], "no space left on device")
	assert.False(t, hasDone(events))
	assert.False(t, *f.started)
	assert.True(t, cleaned, "the mcp config's cleanup must run even when a later step fails")
}

// The mirror of the case above: nothing pinned that run() ever invokes the
// cleanup NewWorkDir hands back. Without it every command-center query leaks
// a panemux-command-center-* directory into /tmp for the life of the host —
// one per palette query, never reclaimed.
func TestQueryCleansUpTheWorkDirWhenTheTurnFails(t *testing.T) {
	f := newRunnerFixture(t, "")
	cleaned := false
	f.config.NewWorkDir = func() (string, func(), error) {
		return t.TempDir(), func() { cleaned = true }, nil
	}
	f.config.NewCommand = func(_ context.Context, _, _ string, _ ...string) cmdRunner {
		return &noStdoutCmd{err: errors.New("too many open files")}
	}

	f.query(t)

	assert.True(t, cleaned, "the work directory must be removed however the turn ends")
}

// The pipe is opened after the command is constructed but before it is
// started, so this is the last failure that still leaves nothing running.
// It gets its own message because "creating stdout pipe" and "starting
// claude" are different operator problems: a pipe failure is a resource
// limit on this host, a start failure is usually a missing binary.
func TestQueryReportsAFailureToOpenTheSubprocessStdoutPipe(t *testing.T) {
	f := newRunnerFixture(t, "")
	cmd := &noStdoutCmd{err: errors.New("too many open files")}
	f.config.NewCommand = func(_ context.Context, _, _ string, _ ...string) cmdRunner { return cmd }

	events := f.query(t)

	require.Len(t, errorEvents(events), 1)
	assert.Contains(t, errorEvents(events)[0], "creating stdout pipe")
	assert.Contains(t, errorEvents(events)[0], "too many open files")
	assert.False(t, hasDone(events))
	assert.False(t, cmd.started,
		"the pipe is opened before Start, and it must stay that way: a subprocess started and then "+
			"abandoned on the pipe error is never Wait()ed, leaving a zombie claude per failed query, "+
			"while the deferred work-dir cleanup deletes the directory out from under it")
}

// noStdoutCmd fails at StdoutPipe and records whether Start was reached.
// fakeCmd can do neither: its StdoutPipe never returns an error, and nothing
// it records survives the factory this test replaces.
type noStdoutCmd struct {
	err     error
	started bool
}

func (c *noStdoutCmd) StdoutPipe() (io.ReadCloser, error) { return nil, c.err }
func (c *noStdoutCmd) Start() error                       { c.started = true; return nil }
func (c *noStdoutCmd) Wait() error                        { return nil }

// ── Persistence failures after the turn itself succeeded ─────────────────────

// A history write is a record of what already happened, so losing it must not
// turn a completed turn into a failed one — the operator has the answer on
// screen either way. Continuing rather than returning is right.
//
// How it is *reported* is what #214 fixed. It used to emit an EventError and
// then carry on to EventDone, so a turn had two frames each documented as
// "always the last frame for that query" (docs/behavior.md), EventDone's own
// doc comment promising it "is never sent after an EventError" was false, and
// the dashboard rendered the turn as complete *and* errored. The failure is
// now a warning riding the one terminal frame the turn does emit.
func TestQueryReportsAFailedHistoryWriteAsAWarningOnTheDoneFrame(t *testing.T) {
	f := newRunnerFixture(t, `{"type":"result","session_id":"s","result":"done"}`+"\n")
	f.config.HistoryPath = brokenParentPath(t, "history.jsonl")

	events := f.query(t)

	assert.Empty(t, errorEvents(events), "a lost record is not a failed turn")
	done := terminalEvent(t, events)
	assert.Equal(t, EventDone, done.Type)
	require.Len(t, done.Warnings, 1)
	assert.Contains(t, done.Warnings[0], "persisting command center history")
}

// The complement, without which the test above is satisfied by a Runner that
// warns on every turn: an ordinary turn carries no warnings at all, so the
// dashboard has nothing to render and the `warnings` key never reaches the
// wire.
func TestQueryReportsNoWarningsWhenTheHistoryWriteSucceeds(t *testing.T) {
	f := newRunnerFixture(t, `{"type":"result","session_id":"s","result":"done"}`+"\n")

	events := f.query(t)

	done := terminalEvent(t, events)
	assert.Equal(t, EventDone, done.Type)
	assert.Empty(t, done.Warnings)
}

// A turn that failed for its own reasons still emits exactly one terminal
// event, and it is the error — the subprocess failure is the actionable one,
// and a warning about the record of a turn that did not produce an answer
// would only compete with it. The history failure is logged instead, so it is
// not lost entirely.
func TestQueryKeepsOneTerminalEventWhenBothTheTurnAndTheHistoryWriteFail(t *testing.T) {
	f := newRunnerFixture(t, `{"type":"result","session_id":"s","result":"done"}`+"\n")
	f.config.HistoryPath = brokenParentPath(t, "history.jsonl")
	f.waitErr = errors.New("exit status 1")

	logged := captureRunnerLog(t)

	events := f.query(t)

	failure := terminalEvent(t, events)
	assert.Equal(t, EventError, failure.Type)
	assert.Contains(t, failure.Err, "claude exited with error")
	assert.Empty(t, failure.Warnings)
	assert.Contains(t, logged.String(), "persisting command center history")
}

// The session id is the opposite case: it is what the *next* turn resumes
// from, so a turn that could not persist it has not really succeeded.
// Reporting done would leave the next query silently starting a new
// conversation with no sign anything was lost.
//
// The fixture is a dangling symlink rather than a regular file, because this
// arm needs the load to succeed and only the save to fail: reading through a
// dangling symlink is ErrNotExist, which LoadSessionFile treats as "no
// session yet", while MkdirAll on one fails with EEXIST.
func TestQueryReportsAFailureToPersistTheSessionID(t *testing.T) {
	f := newRunnerFixture(t, `{"type":"result","session_id":"s","result":"done"}`+"\n")
	dir := t.TempDir()
	dangling := filepath.Join(dir, "dangling")
	require.NoError(t, os.Symlink(filepath.Join(dir, "no-such-target"), dangling))
	f.config.SessionPath = filepath.Join(dangling, "session.json")

	events := f.query(t)

	msgs := errorEvents(events)
	require.Len(t, msgs, 1)
	assert.Contains(t, msgs[0], "persisting command center session id")
	assert.False(t, hasDone(events),
		"the next turn would resume nothing, so this turn must not report done")
}

// ── The stream ───────────────────────────────────────────────────────────────

// A blank line between stream-json objects is skipped rather than parsed.
// Unlike LoadHistory's equivalent fast path, this one is load-bearing:
// without it the blank line reaches json.Unmarshal, which fails, and
// streamOutput treats a parse failure as a malformed stream — aborting the
// turn and reporting an error for output that was merely padded.
//
// Asserting the absence of an error and the presence of done is not enough on
// its own, and this test used to stop there. Turning the `continue` into a
// `break` also produces no error and still reaches done, while silently
// dropping every frame after the first blank line — including the result the
// operator is waiting for. So the line *after* the blank ones is what has to
// be asserted: skipping and stopping are only distinguishable there.
func TestStreamOutputSkipsBlankLinesAndKeepsForwardingWhatFollows(t *testing.T) {
	f := newRunnerFixture(t,
		`{"type":"system","subtype":"init","session_id":"s"}`+"\n"+
			"\n"+
			"   \n"+
			`{"type":"result","session_id":"s","result":"done"}`+"\n")

	events := f.query(t)

	assert.Empty(t, errorEvents(events), "a blank line is padding, not malformed output")
	assert.True(t, hasDone(events))

	var lines []string
	for _, ev := range events {
		if ev.Type == EventLine {
			lines = append(lines, string(ev.Raw))
		}
	}
	require.Len(t, lines, 2, "the blank lines produce no frame of their own, and neither one ends the stream")
	assert.Contains(t, lines[1], `"type":"result"`,
		"the frame after the blank lines is the answer, and it must still reach the operator")
}
