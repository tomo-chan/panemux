package commandcenter

import (
	"context"
	"encoding/json"
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

// TestRunnerFirstRunMintsAndPersistsItsOwnSessionID pins the fix for a real
// isolation failure, reproduced against the real CLI (v2.1.233): a plain
// `claude -p` with no --resume does not mint a fresh conversation, it
// reports the *ambient* session id of the Claude Code session the
// environment already belongs to. The Runner used to persist that reported
// id and --resume it, silently attaching the command center to a
// conversation it does not own — one holding full tool permissions, while
// the command center is deliberately launched with three board tools. The
// subprocess's reported id must therefore never be adopted; panemux mints
// its own and pins it with --session-id.
func TestRunnerFirstRunMintsAndPersistsItsOwnSessionID(t *testing.T) {
	// The stream reports an id that is NOT the one panemux minted — exactly
	// the ambient-session case this guards against.
	output := `{"type":"system","subtype":"init","session_id":"ambient-session-of-the-operator"}` + "\n" +
		`{"type":"result","session_id":"ambient-session-of-the-operator","result":"done"}` + "\n"
	var captured capturedInvocation
	factory := func(_ context.Context, _, name string, args ...string) cmdRunner {
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

	idIdx := indexOf(captured.Args, "--session-id")
	require.GreaterOrEqual(t, idIdx, 0, "a first run must pin its own session id, got %v", captured.Args)
	minted := captured.Args[idIdx+1]
	assert.Regexp(t, uuidV4, minted, "--session-id requires a v4 UUID")

	state, err := LoadSessionFile(sessionPath)
	require.NoError(t, err)
	assert.Equal(t, minted, state.SessionID, "the persisted id must be the one panemux passed")
	assert.NotEqual(t, "ambient-session-of-the-operator", state.SessionID,
		"the id the subprocess reported must never be adopted")

	entries, err := LoadHistory(historyPath)
	require.NoError(t, err)
	// The prompt leads the turn, followed by the subprocess's own lines.
	require.Len(t, entries, 3)
	assert.Contains(t, string(entries[0].Raw), promptHistoryType)
}

// TestRunnerIsolatesTheSubprocessFromOperatorConfig pins the flags that keep
// the command center from inheriting ambient context. Each was verified
// against the real CLI: --setting-sources ” suppresses user/project/local
// settings *and* CLAUDE.md discovery, which is why panemux's own
// instructions travel via --append-system-prompt instead of a file.
func TestRunnerIsolatesTheSubprocessFromOperatorConfig(t *testing.T) {
	var captured capturedInvocation
	var capturedDir string
	factory := func(_ context.Context, dir, name string, args ...string) cmdRunner {
		captured = capturedInvocation{Name: name, Args: args}
		capturedDir = dir
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(""))}
	}
	r, _, _ := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "status")
	require.NoError(t, err)
	drain(t, events)

	assert.Contains(t, captured.Args, "--strict-mcp-config",
		"only the board MCP server this query configured may load")

	srcIdx := indexOf(captured.Args, "--setting-sources")
	require.GreaterOrEqual(t, srcIdx, 0, "settings inheritance must be switched off explicitly")
	assert.Empty(t, captured.Args[srcIdx+1], "no user, project or local settings — operator hooks must not fire here")

	promptIdx := indexOf(captured.Args, "--append-system-prompt")
	require.GreaterOrEqual(t, promptIdx, 0, "panemux's own instructions must reach the subprocess")
	assert.Equal(t, DefaultSystemPrompt, captured.Args[promptIdx+1])

	assert.NotEmpty(t, capturedDir, "the subprocess needs its own working directory")
	assert.NotEqual(t, ".", capturedDir)
}

// queryWithContextDir runs one query against a Runner configured with the
// given operator context directory and returns the argv it produced.
func queryWithContextDir(t *testing.T, contextDir string) capturedInvocation {
	t.Helper()
	var captured capturedInvocation
	r := NewRunner(RunnerConfig{
		ClaudeBin:   "claude",
		SessionPath: filepath.Join(t.TempDir(), "session.json"),
		HistoryPath: filepath.Join(t.TempDir(), "history.jsonl"),
		ContextDir:  contextDir,
		BuildMCPConfig: func() (string, func(), error) {
			return "/tmp/fake-mcp-config.json", func() {}, nil
		},
		NewCommand: func(_ context.Context, _, name string, args ...string) cmdRunner {
			captured = capturedInvocation{Name: name, Args: args}
			return &fakeCmd{stdout: io.NopCloser(strings.NewReader(""))}
		},
	})
	events, err := r.Query(context.Background(), "status")
	require.NoError(t, err)
	drain(t, events)
	return captured
}

// TestRunnerSendsOnlyPanemuxOwnSettings pins that the subprocess receives
// exactly one settings document — panemux's own narrowing literal — and
// that no operator settings file can be routed into --settings. A settings
// value can nullify --allowedTools outright (see
// TestSubprocessSettingsOnlyNarrows), so this is a security boundary, not a
// configuration preference.
func TestRunnerSendsOnlyPanemuxOwnSettings(t *testing.T) {
	contextDir := t.TempDir()
	// An operator settings file must be ignored even when one is sitting
	// right there in the context directory.
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "settings.json"),
		[]byte(`{"permissions":{"defaultMode":"acceptEdits"}}`), 0o600))

	captured := queryWithContextDir(t, contextDir)

	var settingsValues []string
	for i, arg := range captured.Args {
		if arg == "--settings" && i+1 < len(captured.Args) {
			settingsValues = append(settingsValues, captured.Args[i+1])
		}
	}
	require.Len(t, settingsValues, 1, "exactly one --settings, got %v", captured.Args)
	assert.Equal(t, SubprocessSettings, settingsValues[0])
	assert.NotContains(t, settingsValues[0], "acceptEdits")
	assert.NotContains(t, strings.Join(captured.Args, " "), filepath.Join(contextDir, "settings.json"),
		"an operator settings file must never reach argv")
}

func TestRunnerAppendsOperatorInstructionsToTheSystemPrompt(t *testing.T) {
	contextDir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(contextDir, "CLAUDE.md"),
		[]byte("Never broadcast during a release freeze."), 0o600))

	captured := queryWithContextDir(t, contextDir)

	prompt := captured.Args[indexOf(captured.Args, "--append-system-prompt")+1]
	assert.Contains(t, prompt, "Never broadcast during a release freeze.")
	assert.Contains(t, prompt, "Always call board_status before answering",
		"operator text extends panemux's own rules, it does not replace them")
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
	factory := func(_ context.Context, _, name string, args ...string) cmdRunner {
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
	factory := func(_ context.Context, _, name string, args ...string) cmdRunner {
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

	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
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

// TestRunnerMalformedPersistedSessionIDFallsBackToFirstRun covers a
// persisted session id that doesn't look like anything claude itself would
// ever emit — in particular one shaped like a CLI flag. --resume's value is
// optional in the claude CLI's own parser, so passing such a value straight
// through as the argv element after "--resume" risks the same class of
// argument-injection buildArgs's own "--" marker defends the prompt
// against, except --resume's value sits before that marker and has no
// marker of its own to rely on. The Runner must instead treat this the same
// as no persisted id at all: query without --resume, and persist whatever
// fresh session id this successful first-run query captures.
func TestRunnerMalformedPersistedSessionIDFallsBackToFirstRun(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, SaveSessionFile(sessionPath, SessionState{SessionID: "--dangerously-skip-permissions"}))
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")

	output := `{"type":"system","subtype":"init","session_id":"new-session-1"}` + "\n" +
		`{"type":"result","session_id":"new-session-1","result":"done"}` + "\n"
	var captured capturedInvocation
	factory := func(_ context.Context, _, name string, args ...string) cmdRunner {
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
		NewCommand: factory,
	})

	events, err := r.Query(context.Background(), "hello there")
	require.NoError(t, err)
	got := drain(t, events)

	require.NotEmpty(t, got)
	assert.Equal(t, EventDone, got[len(got)-1].Type)
	assert.NotContains(t, captured.Args, "--resume",
		"a flag-shaped persisted session id must never reach --resume's argv slot")

	require.Contains(t, captured.Args, "--session-id",
		"a fallback first run must still pin its own session id")
	minted := captured.Args[indexOf(captured.Args, "--session-id")+1]

	state, err := LoadSessionFile(sessionPath)
	require.NoError(t, err)
	assert.Equal(t, minted, state.SessionID,
		"the fallback first run must persist the id panemux minted")
	assert.NotEqual(t, "new-session-1", state.SessionID,
		"the id the subprocess reported must never be adopted")
}

// sleepingFakeCmd behaves like fakeCmd, but Wait() sleeps first — used to
// force a query's own context deadline to have already passed by the time
// Wait() returns, so ctx.Err() reliably reports context.DeadlineExceeded
// rather than depending on scheduler timing.
type sleepingFakeCmd struct {
	stdout          io.ReadCloser
	waitErr         error
	sleepBeforeWait time.Duration
}

func (f *sleepingFakeCmd) StdoutPipe() (io.ReadCloser, error) { return f.stdout, nil }
func (f *sleepingFakeCmd) Start() error                       { return nil }
func (f *sleepingFakeCmd) Wait() error {
	time.Sleep(f.sleepBeforeWait)
	return f.waitErr
}

func TestRunnerTimeoutDoesNotClearSessionIdAndReportsTimeoutMessage(t *testing.T) {
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	require.NoError(t, SaveSessionFile(sessionPath, SessionState{SessionID: "existing-session"}))
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")

	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
		return &sleepingFakeCmd{
			stdout:          io.NopCloser(strings.NewReader("")),
			waitErr:         errors.New("signal: killed"),
			sleepBeforeWait: 50 * time.Millisecond,
		}
	}
	r := NewRunner(RunnerConfig{
		SessionPath: sessionPath,
		HistoryPath: historyPath,
		BuildMCPConfig: func() (string, func(), error) {
			return "/tmp/fake.json", func() {}, nil
		},
		NewCommand:   factory,
		QueryTimeout: 10 * time.Millisecond, // well under sleepBeforeWait, so the deadline has already passed
	})

	events, err := r.Query(context.Background(), "prompt")
	require.NoError(t, err)
	got := drain(t, events)

	require.Len(t, got, 1)
	assert.Equal(t, EventError, got[0].Type)
	assert.Contains(t, got[0].Err, "timed out")

	state, err := LoadSessionFile(sessionPath)
	require.NoError(t, err)
	assert.Equal(t, "existing-session", state.SessionID,
		"a query-timeout kill must never be mistaken for a genuine --resume rejection")
}

// ctxAwareFakeCmd's Wait() blocks until ctx is canceled, then returns
// ctx.Err() — mirroring exec.CommandContext's real behavior, where
// canceling the context kills the process and is what unblocks a real
// Wait() call. Used to prove a query context is actually canceled
// immediately on a malformed line, not merely eventually via the deferred
// cancel() that only fires after run() itself returns — which can't happen
// until Wait() returns, an actual deadlock without the fix.
type ctxAwareFakeCmd struct {
	stdout io.ReadCloser
	// Deliberately stored: this fake needs it inside Wait(), mirroring
	// exec.Cmd's own real ctx-awareness.
	ctx context.Context //nolint:containedctx
}

func (f *ctxAwareFakeCmd) StdoutPipe() (io.ReadCloser, error) { return f.stdout, nil }
func (f *ctxAwareFakeCmd) Start() error                       { return nil }
func (f *ctxAwareFakeCmd) Wait() error {
	<-f.ctx.Done()
	return f.ctx.Err() //nolint:wrapcheck // test fake mirrors exec.Cmd's own unwrapped ctx.Err() propagation
}

func TestRunnerMalformedStreamJSONCancelsQueryContextImmediately(t *testing.T) {
	// A malformed line already tells the client the query failed (see
	// TestRunnerMalformedStreamJSONEmitsErrorAndStops); the subprocess must
	// not be left running for up to the full query timeout just to exit on
	// its own afterward, holding the busy flag the whole time.
	output := `not json` + "\n"
	factory := func(ctx context.Context, _, _ string, _ ...string) cmdRunner {
		return &ctxAwareFakeCmd{stdout: io.NopCloser(strings.NewReader(output)), ctx: ctx}
	}
	sessionPath := filepath.Join(t.TempDir(), "session.json")
	historyPath := filepath.Join(t.TempDir(), "history.jsonl")
	r := NewRunner(RunnerConfig{
		SessionPath: sessionPath,
		HistoryPath: historyPath,
		BuildMCPConfig: func() (string, func(), error) {
			return "/tmp/fake.json", func() {}, nil
		},
		NewCommand: factory,
		// Deliberately long: if the fix doesn't call cancel() before
		// cmd.Wait(), this test deadlocks (Wait() can't return until
		// canceled; run() can't call its deferred cancel() until Wait()
		// returns) rather than merely running slowly, so the 2s bound below
		// catches a real regression, not just a slow one.
		QueryTimeout: time.Minute,
	})

	events, err := r.Query(context.Background(), "prompt")
	require.NoError(t, err)

	done := make(chan struct{})
	go func() {
		drain(t, events)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("query did not complete promptly after a malformed line — " +
			"the subprocess context was not canceled immediately")
	}
}

func TestRunnerRejectsSecondQueryWhileBusy(t *testing.T) {
	pr, pw := io.Pipe()
	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
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
	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
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

	// What was captured before the failure is still persisted to history,
	// behind the prompt that leads the turn.
	entries, err := LoadHistory(historyPath)
	require.NoError(t, err)
	require.Len(t, entries, 2)
	assert.Contains(t, string(entries[0].Raw), promptHistoryType)
	assert.Contains(t, string(entries[1].Raw), "sess-1")
}

func TestRunnerNonZeroExitEmitsErrorEventAndSkipsSessionPersist(t *testing.T) {
	output := `{"type":"system","subtype":"init","session_id":"sess-1"}` + "\n"
	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
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
	// The prompt leads the turn, and the line captured before the failing
	// exit follows it — a failed turn is still part of the record.
	require.Len(t, entries, 2)
	assert.Contains(t, string(entries[0].Raw), promptHistoryType)
	assert.Contains(t, string(entries[1].Raw), "sess-1", "output captured before the failing exit is still persisted")
}

func TestRunnerStartErrorEmitsErrorEventAndReleasesBusy(t *testing.T) {
	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
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
	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
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
	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
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
	factory := func(ctx context.Context, _, _ string, _ ...string) cmdRunner {
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

// TestRunnerDeniesActingToolsByName pins that the subprocess is launched
// with an explicit denial list, not only an allowlist. Verified against the
// real CLI: --allowedTools alone blocks Bash, but a settings document
// carrying {"permissions":{"defaultMode":"acceptEdits"}} defeats it and Bash
// runs; adding --disallowedTools blocks it again. The allowlist is a policy
// another policy can override; this denial is not.
func TestRunnerDeniesActingToolsByName(t *testing.T) {
	var captured capturedInvocation
	factory := func(_ context.Context, _, name string, args ...string) cmdRunner {
		captured = capturedInvocation{Name: name, Args: args}
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(""))}
	}
	r, _, _ := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "status")
	require.NoError(t, err)
	drain(t, events)

	var denyArg string
	for _, arg := range captured.Args {
		if strings.HasPrefix(arg, "--disallowedTools=") {
			denyArg = arg
		}
	}
	require.NotEmpty(t, denyArg, "expected a --disallowedTools argument, got %v", captured.Args)
	// The "=" form, for the same reason --allowedTools uses it: the flag is
	// variadic and a two-element form would swallow the next argv element.
	assert.NotContains(t, captured.Args, "--disallowedTools")
	assert.Contains(t, denyArg, "Bash")
	assert.Contains(t, denyArg, "Agent")
}

// TestRunnerDisablesSlashCommands pins the third argv-level restriction.
// Slash commands are outside both --allowedTools and --disallowedTools: a
// prompt of "/context" against the real CLI returned the command's own
// output, so anyone able to type into the palette could reach the CLI's
// whole slash-command registry (/config, /model, /mcp, /doctor, ...).
// --disable-slash-commands turns that into "/context isn't available in
// this environment." while leaving the board MCP tools untouched.
func TestRunnerDisablesSlashCommands(t *testing.T) {
	var captured capturedInvocation
	factory := func(_ context.Context, _, name string, args ...string) cmdRunner {
		captured = capturedInvocation{Name: name, Args: args}
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(""))}
	}
	r, _, _ := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "/context")
	require.NoError(t, err)
	drain(t, events)

	assert.Contains(t, captured.Args, "--disable-slash-commands")
	// The prompt still travels as prompt text, after the end-of-options marker.
	assert.Equal(t, "/context", captured.Args[len(captured.Args)-1])
}

// TestRunnerRecordsThePromptInHistory covers a gap found by reading a real
// history file: the stream carries no record of the operator's own prompt.
// A real run produced stream_event, system, assistant and result frames and
// nothing else, so the history panel could only ever show answers with no
// question attached to them. panemux owns this file's format, so it records
// the prompt itself, under a type the CLI never emits.
func TestRunnerRecordsThePromptInHistory(t *testing.T) {
	output := `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}` + "\n"
	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader(output))}
	}
	r, _, historyPath := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "which panes are blocked?")
	require.NoError(t, err)
	drain(t, events)

	entries, err := LoadHistory(historyPath)
	require.NoError(t, err)
	require.NotEmpty(t, entries)

	var first map[string]any
	require.NoError(t, json.Unmarshal(entries[0].Raw, &first))
	assert.Equal(t, "panemux_prompt", first["type"], "the prompt must be the first entry of its turn")
	assert.Equal(t, "which panes are blocked?", first["text"])

	// The subprocess's own lines still follow, unmodified.
	require.Len(t, entries, 2)
	assert.JSONEq(t, strings.TrimSpace(output), string(entries[1].Raw))
}

func TestRunnerRecordsThePromptEvenWhenTheQueryFails(t *testing.T) {
	factory := func(_ context.Context, _, _ string, _ ...string) cmdRunner {
		return &fakeCmd{stdout: io.NopCloser(strings.NewReader("")), waitErr: errors.New("exit status 1")}
	}
	r, _, historyPath := newTestRunner(t, factory)

	events, err := r.Query(context.Background(), "a question that failed")
	require.NoError(t, err)
	drain(t, events)

	entries, err := LoadHistory(historyPath)
	require.NoError(t, err)
	require.Len(t, entries, 1, "a failed turn still belongs in the record")
	assert.Contains(t, string(entries[0].Raw), "a question that failed")
}
