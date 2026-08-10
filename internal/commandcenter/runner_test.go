package commandcenter

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeCmd struct {
	stdout   io.ReadCloser
	startErr error
	waitErr  error
}

func (f *fakeCmd) StdoutPipe() (io.ReadCloser, error) { return f.stdout, nil }
func (f *fakeCmd) Start() error                       { return f.startErr }
func (f *fakeCmd) Wait() error                        { return f.waitErr }

type capturedInvocation struct {
	Name string
	Args []string
}

func newTestRunner(t *testing.T, factory commandFactory) (*Runner, string, string) {
	t.Helper()
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	cleanupCalls := 0
	r := NewRunner(RunnerConfig{
		ClaudeBin:   "claude",
		SessionPath: sessionPath,
		HistoryPath: historyPath,
		BuildMCPConfig: func() (string, func(), error) {
			return "/tmp/fake-mcp-config.json", func() { cleanupCalls++ }, nil
		},
		AllowedTools: []string{"mcp__panemux-board__board_status"},
		NewCommand:   factory,
		Now:          func() time.Time { return time.Unix(0, 0).UTC() },
	})
	return r, sessionPath, historyPath
}

func drain(t *testing.T, events <-chan Event) []Event {
	t.Helper()
	var got []Event
	for ev := range events {
		got = append(got, ev)
	}
	return got
}

func TestRunnerFirstRunCapturesAndPersistsSessionID(t *testing.T) {
	output := `{"type":"system","subtype":"init","session_id":"sess-1"}` + "\n" +
		`{"type":"result","session_id":"sess-1","result":"done"}` + "\n"
	var captured capturedInvocation
	factory := func(_ context.Context, name string, args ...string) cmdRunner {
		captured = capturedInvocation{Name: name, Args: args}
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(output))}
	}
	r, sessionPath, historyPath := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "hello there")
	require.NoError(t, err)
	got := drain(t, events)

	require.Len(t, got, 3)
	assert.Equal(t, EventLine, got[0].Type)
	assert.Equal(t, EventLine, got[1].Type)
	assert.Equal(t, EventDone, got[2].Type)

	assert.Equal(t, "claude", captured.Name)
	assert.NotContains(t, captured.Args, "--resume")
	assert.Equal(t, "hello there", captured.Args[len(captured.Args)-1])

	state, err := LoadSessionFile(sessionPath)
	require.NoError(t, err)
	assert.Equal(t, "sess-1", state.SessionID)

	entries, err := LoadHistory(historyPath)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

// TestRunnerBuildArgsShapeIsSafeAgainstArgumentInjection locks in the exact
// argv shape verified live against the real `claude` CLI (v2.1.226):
//   - --allowedTools must use the "=" form as a single argv element
//     ("--allowedTools=a,b"), never two separate elements ("--allowedTools",
//     "a,b") — the flag is declared variadic (`<tools...>`), so a two-element
//     form lets it swallow the very next argv element (the prompt) as an
//     additional tool name, and a real query then fails with "Input must be
//     provided either through stdin or as a prompt argument".
//   - "--" must immediately precede the prompt — without it, a prompt
//     beginning with "-" (e.g. "--help") is parsed as a CLI flag instead of
//     being passed through as prompt text.
//
// Both failure modes were reproduced against the real CLI before this fix;
// this test only pins the argv shape so a regression is caught without
// needing the CLI installed.
func TestRunnerBuildArgsShapeIsSafeAgainstArgumentInjection(t *testing.T) {
	var captured capturedInvocation
	factory := func(_ context.Context, name string, args ...string) cmdRunner {
		captured = capturedInvocation{Name: name, Args: args}
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(""))}
	}
	r, _, _ := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "--help")
	require.NoError(t, err)
	drain(t, events)

	require.Contains(t, captured.Args, "--allowedTools=mcp__panemux-board__board_status")
	assert.NotContains(t, captured.Args, "--allowedTools")

	dashDashIdx := indexOf(captured.Args, "--")
	require.GreaterOrEqual(t, dashDashIdx, 0, "expected a literal -- before the prompt, got %v", captured.Args)
	require.Equal(t, len(captured.Args)-2, dashDashIdx, "-- must immediately precede the final (prompt) argument")
	assert.Equal(t, "--help", captured.Args[len(captured.Args)-1])
}

func TestRunnerResumeUsesPersistedSessionIDAndNeverOverwritesIt(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, SaveSessionFile(sessionPath, SessionState{SessionID: "existing-session"}))
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")

	output := `{"type":"result","session_id":"existing-session","result":"ok"}` + "\n"
	var captured capturedInvocation
	factory := func(_ context.Context, name string, args ...string) cmdRunner {
		captured = capturedInvocation{Name: name, Args: args}
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(output))}
	}
	r := NewRunner(RunnerConfig{
		ClaudeBin:   "claude",
		SessionPath: sessionPath,
		HistoryPath: historyPath,
		BuildMCPConfig: func() (string, func(), error) {
			return "/tmp/fake-mcp-config.json", func() {}, nil
		},
		AllowedTools: []string{"mcp__panemux-board__board_status"},
		NewCommand:   factory,
	})

	events, err := r.Query(context.Background(), "follow up")
	require.NoError(t, err)
	drain(t, events)

	require.Contains(t, captured.Args, "--resume")
	idx := indexOf(captured.Args, "--resume")
	require.GreaterOrEqual(t, idx, 0)
	assert.Equal(t, "existing-session", captured.Args[idx+1])

	state, err := LoadSessionFile(sessionPath)
	require.NoError(t, err)
	assert.Equal(t, "existing-session", state.SessionID)
}

// TestRunnerResumeFailureClearsStaleSessionID covers the case where claude
// no longer recognizes a persisted --resume id (e.g. the user cleared
// ~/.claude, or the session was garbage collected): without this, every
// future query would keep retrying the same dead id forever, since
// SaveSessionFile is only ever reached on a *first-run* success (see
// TestRunnerNonZeroExitEmitsErrorEventAndSkipsSessionPersist) — nothing
// else would ever clear a bad resume id.
func TestRunnerResumeFailureClearsStaleSessionID(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, SaveSessionFile(sessionPath, SessionState{SessionID: "dead-session"}))
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")

	factory := func(_ context.Context, _ string, _ ...string) cmdRunner {
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader("")), waitErr: errors.New("exit status 1")}
	}
	r := NewRunner(RunnerConfig{
		ClaudeBin:   "claude",
		SessionPath: sessionPath,
		HistoryPath: historyPath,
		BuildMCPConfig: func() (string, func(), error) {
			return "/tmp/fake-mcp-config.json", func() {}, nil
		},
		NewCommand: factory,
	})

	events, err := r.Query(context.Background(), "follow up")
	require.NoError(t, err)
	got := drain(t, events)

	require.Len(t, got, 1)
	assert.Equal(t, EventError, got[0].Type)

	state, err := LoadSessionFile(sessionPath)
	require.NoError(t, err)
	assert.Empty(t, state.SessionID, "a failed --resume must clear the stale session id, not repeat it forever")
}

func TestRunnerRejectsSecondQueryWhileBusy(t *testing.T) {
	pr, pw := io.Pipe()
	factory := func(_ context.Context, _ string, _ ...string) cmdRunner {
		return &fakeCmd{stdout: pr}
	}
	r, _, _ := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "first")
	require.NoError(t, err)

	_, err = r.Query(context.Background(), "second")
	assert.ErrorIs(t, err, ErrBusy)

	_, _ = pw.Write([]byte(`{"type":"result","session_id":"s"}` + "\n"))
	require.NoError(t, pw.Close())
	drain(t, events)

	// Busy is released once the first query's goroutine finishes.
	events2, err := r.Query(context.Background(), "third")
	require.NoError(t, err)
	drain(t, events2)
}

func TestRunnerMalformedStreamJSONEmitsErrorAndStops(t *testing.T) {
	output := `{"type":"system","subtype":"init","session_id":"sess-1"}` + "\n" +
		`not json` + "\n" +
		`{"type":"result","session_id":"sess-1"}` + "\n"
	factory := func(_ context.Context, _ string, _ ...string) cmdRunner {
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(output))}
	}
	r, sessionPath, historyPath := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "prompt")
	require.NoError(t, err)
	got := drain(t, events)

	require.Len(t, got, 2)
	assert.Equal(t, EventLine, got[0].Type)
	assert.Equal(t, EventError, got[1].Type)
	assert.Contains(t, got[1].Err, "malformed")

	// A failed query must never persist a session id.
	state, err := LoadSessionFile(sessionPath)
	require.NoError(t, err)
	assert.Empty(t, state.SessionID)

	// What was captured before the failure is still persisted to history.
	entries, err := LoadHistory(historyPath)
	require.NoError(t, err)
	assert.Len(t, entries, 1)
}

func TestRunnerNonZeroExitEmitsErrorEventAndSkipsSessionPersist(t *testing.T) {
	output := `{"type":"system","subtype":"init","session_id":"sess-1"}` + "\n"
	factory := func(_ context.Context, _ string, _ ...string) cmdRunner {
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(output)), waitErr: errors.New("exit status 1")}
	}
	r, sessionPath, historyPath := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "prompt")
	require.NoError(t, err)
	got := drain(t, events)

	require.Len(t, got, 2)
	assert.Equal(t, EventLine, got[0].Type)
	assert.Equal(t, EventError, got[1].Type)
	assert.Contains(t, got[1].Err, "exit status 1")

	state, err := LoadSessionFile(sessionPath)
	require.NoError(t, err)
	assert.Empty(t, state.SessionID, "a failed query must never persist a session id")

	entries, err := LoadHistory(historyPath)
	require.NoError(t, err)
	assert.Len(t, entries, 1, "output captured before the failing exit is still persisted")
}

func TestRunnerStartErrorEmitsErrorEventAndReleasesBusy(t *testing.T) {
	factory := func(_ context.Context, _ string, _ ...string) cmdRunner {
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader("")), startErr: errors.New("claude: command not found")}
	}
	r, _, _ := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "prompt")
	require.NoError(t, err)
	got := drain(t, events)

	require.Len(t, got, 1)
	assert.Equal(t, EventError, got[0].Type)
	assert.Contains(t, got[0].Err, "command not found")

	// Busy must be released even though this query failed to start.
	events2, err := r.Query(context.Background(), "second")
	require.NoError(t, err)
	drain(t, events2)
}

func TestRunnerBuildMCPConfigFailureEmitsErrorAndNeverStartsProcess(t *testing.T) {
	started := false
	factory := func(_ context.Context, _ string, _ ...string) cmdRunner {
		started = true
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(""))}
	}
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	r := NewRunner(RunnerConfig{
		ClaudeBin:   "claude",
		SessionPath: sessionPath,
		HistoryPath: historyPath,
		BuildMCPConfig: func() (string, func(), error) {
			return "", nil, errors.New("disk full")
		},
		NewCommand: factory,
	})

	events, err := r.Query(context.Background(), "prompt")
	require.NoError(t, err)
	got := drain(t, events)

	require.Len(t, got, 1)
	assert.Equal(t, EventError, got[0].Type)
	assert.Contains(t, got[0].Err, "disk full")
	assert.False(t, started, "must never spawn claude when mcp config setup fails")
}

func TestRunnerAlwaysCallsMCPConfigCleanup(t *testing.T) {
	cleanupCalled := false
	factory := func(_ context.Context, _ string, _ ...string) cmdRunner {
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader("")), waitErr: errors.New("boom")}
	}
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	r := NewRunner(RunnerConfig{
		ClaudeBin:   "claude",
		SessionPath: sessionPath,
		HistoryPath: historyPath,
		BuildMCPConfig: func() (string, func(), error) {
			return "/tmp/fake.json", func() { cleanupCalled = true }, nil
		},
		NewCommand: factory,
	})

	events, err := r.Query(context.Background(), "prompt")
	require.NoError(t, err)
	drain(t, events)

	assert.True(t, cleanupCalled)
}

func TestRunnerAppliesConfiguredQueryTimeoutToSubprocessContext(t *testing.T) {
	var capturedCtx context.Context
	factory := func(ctx context.Context, _ string, _ ...string) cmdRunner {
		capturedCtx = ctx
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(""))}
	}
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	r := NewRunner(RunnerConfig{
		SessionPath: sessionPath,
		HistoryPath: historyPath,
		BuildMCPConfig: func() (string, func(), error) {
			return "/tmp/fake.json", func() {}, nil
		},
		NewCommand:   factory,
		QueryTimeout: 30 * time.Second,
	})

	before := time.Now()
	events, err := r.Query(context.Background(), "prompt")
	require.NoError(t, err)
	drain(t, events)

	require.NotNil(t, capturedCtx, "the subprocess factory must have been called")
	deadline, ok := capturedCtx.Deadline()
	require.True(t, ok, "the subprocess context must carry a deadline, or an abandoned/hung "+
		"query could block the command center's busy flag forever")
	assert.WithinDuration(t, before.Add(30*time.Second), deadline, 2*time.Second)
}

func TestRunnerDefaultsQueryTimeoutWhenUnconfigured(t *testing.T) {
	r := NewRunner(RunnerConfig{})

	assert.Equal(t, defaultQueryTimeout, r.queryTimeout)
}

// errThenMoreReader emits `before` with nil errors, then `sentinel`, then
// (only if Read is called again — proving the caller kept draining) `after`
// followed by io.EOF. Used to test that streamOutput's scanner.Err() path
// keeps reading stdout to completion rather than abandoning it mid-stream.
type errThenMoreReader struct {
	before      []byte
	sentinel    error
	after       []byte
	pos         int
	stage       int
	afterServed bool
}

func (r *errThenMoreReader) Read(p []byte) (int, error) {
	switch r.stage {
	case 0:
		n := copy(p, r.before[r.pos:])
		r.pos += n
		if r.pos >= len(r.before) {
			r.stage = 1
		}
		return n, nil
	case 1:
		r.stage = 2
		return 0, r.sentinel
	default:
		r.afterServed = true
		if len(r.after) == 0 {
			return 0, io.EOF
		}
		n := copy(p, r.after)
		r.after = r.after[n:]
		return n, io.EOF
	}
}

func (r *errThenMoreReader) Close() error { return nil }

func TestRunnerStreamOutputDrainsRemainingOutputAfterScannerError(t *testing.T) {
	reader := &errThenMoreReader{
		before:   []byte(`{"type":"system","subtype":"init","session_id":"sess-1"}` + "\n"),
		sentinel: errors.New("boom"),
		after:    []byte("leftover buffered subprocess output"),
	}
	r := NewRunner(RunnerConfig{})
	events := make(chan Event, 8)

	entries, sessionID, failed := r.streamOutput(reader, events)
	close(events)

	assert.True(t, failed)
	assert.Equal(t, "sess-1", sessionID)
	require.Len(t, entries, 1)
	assert.True(t, reader.afterServed,
		"streamOutput must keep reading stdout after a scanner error (not just a malformed-JSON error), "+
			"or a subprocess still writing to the pipe could block cmd.Wait() forever")

	var got []Event
	for ev := range events {
		got = append(got, ev)
	}
	require.Len(t, got, 2)
	assert.Equal(t, EventLine, got[0].Type)
	assert.Equal(t, EventError, got[1].Type)
	assert.Contains(t, got[1].Err, "boom")
}

func indexOf(haystack []string, needle string) int {
	for i, v := range haystack {
		if v == needle {
			return i
		}
	}
	return -1
}
