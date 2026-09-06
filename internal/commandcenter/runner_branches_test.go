package commandcenter

import (
	"context"
	"errors"
	"io"
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
	config  RunnerConfig
}

func newRunnerFixture(t *testing.T, stdout string) *runnerFixture {
	t.Helper()
	started := false
	return &runnerFixture{
		started: &started,
		config: RunnerConfig{
			ClaudeBin:   "claude",
			SessionPath: filepath.Join(t.TempDir(), "session.json"),
			HistoryPath: filepath.Join(t.TempDir(), "history.jsonl"),
			BuildMCPConfig: func() (string, func(), error) {
				return "/tmp/sample-mcp-config.json", func() {}, nil
			},
			NewCommand: func(_ context.Context, _, _ string, _ ...string) cmdRunner {
				started = true
				return &fakeCmd{stdout: io.NopCloser(strings.NewReader(stdout))}
			},
			Now: func() time.Time { return time.Unix(0, 0).UTC() },
		},
	}
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
		return "", nil, errors.New("creating command center work directory: no space left on device")
	}

	events := f.query(t)

	require.Len(t, errorEvents(events), 1)
	assert.Contains(t, errorEvents(events)[0], "creating command center work directory")
	assert.False(t, hasDone(events))
	assert.False(t, *f.started)
	assert.True(t, cleaned, "the mcp config's cleanup must run even when a later step fails")
}

// The pipe is opened after the command is constructed but before it is
// started, so this is the last failure that still leaves nothing running.
// It gets its own message because "creating stdout pipe" and "starting
// claude" are different operator problems: a pipe failure is a resource
// limit on this host, a start failure is usually a missing binary.
func TestQueryReportsAFailureToOpenTheSubprocessStdoutPipe(t *testing.T) {
	f := newRunnerFixture(t, "")
	f.config.NewCommand = func(_ context.Context, _, _ string, _ ...string) cmdRunner {
		return &noStdoutCmd{err: errors.New("too many open files")}
	}

	events := f.query(t)

	require.Len(t, errorEvents(events), 1)
	assert.Contains(t, errorEvents(events)[0], "creating stdout pipe")
	assert.Contains(t, errorEvents(events)[0], "too many open files")
	assert.False(t, hasDone(events))
}

// noStdoutCmd fails at StdoutPipe. fakeCmd cannot: its StdoutPipe never
// returns an error, so the arm above is unreachable through it.
type noStdoutCmd struct{ err error }

func (c *noStdoutCmd) StdoutPipe() (io.ReadCloser, error) { return nil, c.err }
func (c *noStdoutCmd) Start() error                       { return nil }
func (c *noStdoutCmd) Wait() error                        { return nil }

// ── Persistence failures after the turn itself succeeded ─────────────────────

// A history write is a record of what already happened, so losing it is
// worth reporting but must not turn a completed turn into a failed one — the
// operator has the answer on screen either way.
func TestQueryReportsAFailedHistoryWriteWithoutFailingTheTurn(t *testing.T) {
	f := newRunnerFixture(t, `{"type":"result","session_id":"s","result":"done"}`+"\n")
	f.config.HistoryPath = brokenParentPath(t, "history.jsonl")

	events := f.query(t)

	require.Len(t, errorEvents(events), 1)
	assert.Contains(t, errorEvents(events)[0], "persisting command center history")
	assert.True(t, hasDone(events), "the turn itself succeeded, so it must still report done")
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
func TestStreamOutputSkipsBlankLinesRatherThanFailingTheTurn(t *testing.T) {
	f := newRunnerFixture(t,
		`{"type":"system","subtype":"init","session_id":"s"}`+"\n"+
			"\n"+
			"   \n"+
			`{"type":"result","session_id":"s","result":"done"}`+"\n")

	events := f.query(t)

	assert.Empty(t, errorEvents(events), "a blank line is padding, not malformed output")
	assert.True(t, hasDone(events))
}
