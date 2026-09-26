package tasks

import (
	"context"
	"errors"
	"io"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// summaryHost is a fake panemux host for summary tests: its collection lists
// the given state files and logs, and the fetch script returns log.
type summaryHost struct {
	fetchErr error
	collect  []byte
	log      []byte
	fetches  []string
	mu       sync.Mutex
}

func (h *summaryHost) run(_ context.Context, script string) ([]byte, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if script == collectScript {
		return h.collect, nil
	}
	h.fetches = append(h.fetches, script)
	return h.log, h.fetchErr
}

func (h *summaryHost) set(collect, log []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.collect, h.log = collect, log
}

func (h *summaryHost) fetchCount() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.fetches)
}

// hostCollection lists one live claude session per status given (pids from
// 10, session IDs "s<pid>") and a stopped session "stopped", each with a log
// of the given size.
func hostCollection(logSize int, statuses ...string) []byte {
	ls := []string{"::panemux-tasks v1", "::now 1000", "::section state"}
	var ps, transcripts []string
	for i, status := range statuses {
		pid := strconv.Itoa(10 + i)
		ls = append(ls, "::file "+pid+".json",
			`{"pid":`+pid+`,"sessionId":"s`+pid+`","status":"`+status+`","statusUpdatedAt":999000}`)
		ps = append(ps, pid+" 1 claude")
		transcripts = append(transcripts, "990\ts"+pid+".jsonl\t\t"+strconv.Itoa(logSize))
	}
	transcripts = append(transcripts, "900\tstopped.jsonl\t\t"+strconv.Itoa(logSize))
	ls = append(ls, "::section ps")
	ls = append(ls, ps...)
	ls = append(ls, "::section tmux", "::section cwd", "::section transcripts")
	ls = append(ls, transcripts...)
	ls = append(ls, "::end")
	return joinLines(ls...)
}

func conversationLog(texts ...string) []byte {
	var body []string
	for i, text := range texts {
		if i%2 == 0 {
			body = append(body, userLine(text))
		} else {
			body = append(body, assistantLine(text))
		}
	}
	joined := strings.Join(body, "\n") + "\n"
	return transcriptOutput(len(joined), joined)
}

type fakeSummarizer struct {
	gate     chan struct{}
	err      error
	result   Summary
	excerpts []string
	running  int
	peak     int
	mu       sync.Mutex
}

func (f *fakeSummarizer) summarize(ctx context.Context, excerpt string) (Summary, error) {
	f.mu.Lock()
	f.excerpts = append(f.excerpts, excerpt)
	f.running++
	if f.running > f.peak {
		f.peak = f.running
	}
	gate := f.gate
	f.mu.Unlock()
	defer func() {
		f.mu.Lock()
		f.running--
		f.mu.Unlock()
	}()
	if gate != nil {
		select {
		case <-gate:
		case <-ctx.Done():
			return Summary{}, ctx.Err()
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.result, f.err
}

func (f *fakeSummarizer) calls() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.excerpts)
}

func (f *fakeSummarizer) set(result Summary, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.result, f.err = result, err
}

func newSummaryService(t *testing.T, host *summaryHost, summarizer *fakeSummarizer) *Service {
	t.Helper()
	svc := New(Options{
		RunLocal:  host.run,
		Summarize: summarizer.summarize,
		Now:       func() time.Time { return collectedAt },
	})
	t.Cleanup(svc.Close)
	return svc
}

// collectAndSummarize runs one dashboard poll: a collection, then the
// summaries for it, waiting for any summary it started.
func collectAndSummarize(svc *Service) (Snapshot, map[string]*SummaryView) {
	snap := svc.Collect(context.Background())
	svc.Summaries(snap.Tasks)
	svc.waitSummaries()
	return snap, svc.Summaries(snap.Tasks)
}

// A running task that is not working — waiting for input, idle, or in an
// unknown state — is summarized without being asked; the answer is kept.
func TestSummaries_RunningTasksThatAreNotBusyAreSummarized(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "waiting", "idle", "busy"), conversationLog("Fix the flaky test", "Found the race"))
	summarizer := &fakeSummarizer{
		result: Summary{Text: "Fixing a race.", Remaining: []string{"Run make check", "Update docs"}},
	}
	svc := newSummaryService(t, host, summarizer)

	_, views := collectAndSummarize(svc)

	assert.Equal(t, 2, summarizer.calls(), "the waiting and the idle task, not the busy one")
	assert.Equal(t, &SummaryView{
		State:        SummaryReady,
		Text:         "Fixing a race.",
		Remaining:    []string{"Run make check", "Update docs"},
		SummarizedAt: &collectedAt,
	}, views["local:claude:s10"])
	assert.NotNil(t, views["local:claude:s11"])
	assert.Nil(t, views["local:claude:s12"], "a busy task is not summarized")
	assert.Nil(t, views["local:claude:stopped"], "a stopped task is summarized only when asked")
	assert.Contains(t, summarizer.excerpts[0], "[user] Fix the flaky test")
}

func TestSummaries_AnswerWithNothingRemainingIsADoneCandidate(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("Fix it", "Fixed and merged"))
	summarizer := &fakeSummarizer{result: Summary{Text: "Merged.", Remaining: []string{}}}
	svc := newSummaryService(t, host, summarizer)

	_, views := collectAndSummarize(svc)
	require.NotNil(t, views["local:claude:s10"])
	assert.True(t, views["local:claude:s10"].DoneCandidate)
	assert.Empty(t, views["local:claude:s10"].Remaining)
}

// A summary is reused while the log keeps its modification time and size;
// the 10-second poll does not summarize again.
func TestSummaries_AreReusedUntilTheLogChanges(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{result: Summary{Text: "first", Remaining: []string{"x"}}}
	svc := newSummaryService(t, host, summarizer)

	collectAndSummarize(svc)
	collectAndSummarize(svc)
	assert.Equal(t, 1, summarizer.calls())
	assert.Equal(t, 1, host.fetchCount())

	host.set(hostCollection(200, "idle"), conversationLog("a", "b"))
	summarizer.set(Summary{Text: "second", Remaining: []string{"y"}}, nil)
	_, views := collectAndSummarize(svc)
	assert.Equal(t, 2, summarizer.calls(), "a log that grew is summarized again")
	assert.Equal(t, "second", views["local:claude:s10"].Text)
	assert.False(t, views["local:claude:s10"].Outdated)
}

// While a task works its log changes all the time. Its last summary is kept
// on screen, marked outdated, and it is not summarized again until it stops
// working.
func TestSummaries_ABusyTaskKeepsItsLastSummaryMarkedOutdated(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{result: Summary{Text: "old", Remaining: []string{}}}
	svc := newSummaryService(t, host, summarizer)
	collectAndSummarize(svc)

	host.set(hostCollection(300, "busy"), conversationLog("a", "b"))
	_, views := collectAndSummarize(svc)
	assert.Equal(t, 1, summarizer.calls())
	view := views["local:claude:s10"]
	require.NotNil(t, view)
	assert.Equal(t, SummaryReady, view.State)
	assert.Equal(t, "old", view.Text)
	assert.True(t, view.Outdated)
	assert.False(t, view.DoneCandidate, "an outdated answer is not a done candidate")
}

// While a summary runs, the task reports pending, with the previous answer
// if there is one.
func TestSummaries_PendingWhileRunning(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{result: Summary{Text: "old", Remaining: []string{"x"}}}
	svc := newSummaryService(t, host, summarizer)
	collectAndSummarize(svc)

	summarizer.gate = make(chan struct{})
	host.set(hostCollection(200, "idle"), conversationLog("a", "b"))
	snap := svc.Collect(context.Background())
	views := svc.Summaries(snap.Tasks)
	view := views["local:claude:s10"]
	require.NotNil(t, view)
	assert.Equal(t, SummaryPending, view.State)
	assert.Equal(t, "old", view.Text)
	assert.True(t, view.Outdated)

	views = svc.Summaries(snap.Tasks)
	assert.Equal(t, SummaryPending, views["local:claude:s10"].State, "a second poll does not start a second run")
	close(summarizer.gate)
	svc.waitSummaries()
	assert.Equal(t, 2, summarizer.calls())
}

// A failed summary is reported and not retried by the poll for the same
// log; asking for it again retries.
func TestSummaries_AFailureIsNotRetriedUntilAskedOrTheLogChanges(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{err: errors.New("claude exited with status 1")}
	svc := newSummaryService(t, host, summarizer)

	_, views := collectAndSummarize(svc)
	assert.Equal(t, &SummaryView{State: SummaryFailed, Error: "claude exited with status 1"}, views["local:claude:s10"])
	collectAndSummarize(svc)
	assert.Equal(t, 1, summarizer.calls())

	summarizer.set(Summary{Text: "ok", Remaining: []string{"x"}}, nil)
	view, err := svc.RequestSummary("", "s10")
	require.NoError(t, err)
	assert.Equal(t, SummaryPending, view.State)
	svc.waitSummaries()
	_, views = collectAndSummarize(svc)
	assert.Equal(t, 2, summarizer.calls())
	assert.Equal(t, SummaryReady, views["local:claude:s10"].State)
}

// A log in which no message is understood is not sent to claude.
func TestSummaries_AnUnreadableLogIsNotSummarized(t *testing.T) {
	host := &summaryHost{}
	body := `{"kind":"a format this reading does not know"}` + "\n"
	host.set(hostCollection(100, "idle"), transcriptOutput(len(body), body))
	summarizer := &fakeSummarizer{}
	svc := newSummaryService(t, host, summarizer)

	_, views := collectAndSummarize(svc)
	assert.Zero(t, summarizer.calls())
	assert.Equal(t, &SummaryView{State: SummaryUnreadable}, views["local:claude:s10"])
}

func TestSummaries_AMissingOrBrokenLogIsAFailure(t *testing.T) {
	for name, log := range map[string][]byte{
		"missing": []byte("::panemux-transcript none\n"),
		"broken":  []byte("::panemux-transcript v1 99\nshort"),
	} {
		t.Run(name, func(t *testing.T) {
			host := &summaryHost{}
			host.set(hostCollection(100, "idle"), log)
			summarizer := &fakeSummarizer{}
			svc := newSummaryService(t, host, summarizer)

			_, views := collectAndSummarize(svc)
			assert.Zero(t, summarizer.calls())
			require.NotNil(t, views["local:claude:s10"])
			assert.Equal(t, SummaryFailed, views["local:claude:s10"].State)
			assert.NotEmpty(t, views["local:claude:s10"].Error)
		})
	}
}

// A stopped task is summarized when the dashboard asks — when it is
// selected — and only for a session the host lists.
func TestRequestSummary(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "busy"), conversationLog("a"))
	summarizer := &fakeSummarizer{result: Summary{Text: "stopped work", Remaining: []string{"x"}}}
	svc := newSummaryService(t, host, summarizer)
	svc.Collect(context.Background())

	_, err := svc.RequestSummary("", "not-listed")
	require.ErrorIs(t, err, ErrNoSummaryTask)
	_, err = svc.RequestSummary("gpu-box", "stopped")
	require.ErrorIs(t, err, ErrNoSummaryTask)
	_, err = svc.RequestSummary("", "bad id!")
	require.ErrorIs(t, err, ErrInvalidSummary)

	view, err := svc.RequestSummary("", "stopped")
	require.NoError(t, err)
	assert.Equal(t, SummaryPending, view.State)
	svc.waitSummaries()
	view, err = svc.RequestSummary("", "stopped")
	require.NoError(t, err)
	assert.Equal(t, SummaryReady, view.State, "asking again for an unchanged log reuses the answer")
	assert.Equal(t, 1, summarizer.calls())

	// A busy task can be asked for too: asking is the operator's choice.
	_, err = svc.RequestSummary("", "s10")
	require.NoError(t, err)
	svc.waitSummaries()
	assert.Equal(t, 2, summarizer.calls())
}

// A session the collection listed without a log (older than the collected
// ones) cannot be summarized.
func TestRequestSummary_NeedsACollectedLog(t *testing.T) {
	host := &summaryHost{}
	host.set(joinLines(
		"::panemux-tasks v1", "::now 1000", "::section state",
		"::file 10.json", `{"pid":10,"sessionId":"s10","status":"idle"}`,
		"::section ps", "10 1 claude", "::section tmux", "::section cwd", "::section transcripts", "::end",
	), conversationLog("a"))
	summarizer := &fakeSummarizer{}
	svc := newSummaryService(t, host, summarizer)
	snap := svc.Collect(context.Background())

	assert.Empty(t, svc.Summaries(snap.Tasks), "no log, no automatic summary")
	_, err := svc.RequestSummary("", "s10")
	assert.ErrorIs(t, err, ErrNoSummaryTask)
}

// At most summaryConcurrency summaries run at once; the rest wait, pending.
func TestSummaries_RunAtMostTwoAtOnce(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle", "idle", "idle", "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{gate: make(chan struct{}), result: Summary{Text: "s", Remaining: []string{}}}
	svc := newSummaryService(t, host, summarizer)

	snap := svc.Collect(context.Background())
	views := svc.Summaries(snap.Tasks)
	for _, view := range views {
		assert.Equal(t, SummaryPending, view.State)
	}
	require.Eventually(t, func() bool { return summarizer.calls() == summaryConcurrency },
		5*time.Second, 10*time.Millisecond)
	close(summarizer.gate)
	svc.waitSummaries()
	assert.Equal(t, 4, summarizer.calls())
	assert.Equal(t, summaryConcurrency, summarizer.peak)
}

// An SSH host's log is read over its collection connection with the literal
// command `sh -s` and the fetch script on stdin.
func TestSummaries_ReadARemoteLogOverTheHostsConnection(t *testing.T) {
	conn := &scriptedConn{outputs: map[bool][]byte{
		true:  hostCollection(100, "idle"),
		false: conversationLog("remote work"),
	}}
	summarizer := &fakeSummarizer{result: Summary{Text: "remote", Remaining: []string{"x"}}}
	svc := New(Options{
		Hosts:     func() []string { return []string{"gpu-box"} },
		Dial:      func(string) (Conn, error) { return conn, nil },
		RunLocal:  localOutput(minimalOutput("local"), nil),
		Summarize: summarizer.summarize,
	})
	t.Cleanup(svc.Close)

	_, views := collectAndSummarize(svc)
	require.NotNil(t, views["ssh:gpu-box:claude:s10"])
	assert.Equal(t, "remote", views["ssh:gpu-box:claude:s10"].Text)
	cmds, stdins := conn.calls()
	require.Len(t, cmds, 2)
	assert.Equal(t, []string{"sh -s", "sh -s"}, cmds)
	assert.Contains(t, stdins[1], "sid='s10'")
	assert.Contains(t, summarizer.excerpts[0], "remote work")
}

// scriptedConn answers the collection script with one output and any other
// script with another.
type scriptedConn struct {
	outputs map[bool][]byte
	fakeConn
}

func (c *scriptedConn) Run(ctx context.Context, cmd string, stdin io.Reader) ([]byte, error) {
	data, _ := io.ReadAll(stdin)
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cmds = append(c.cmds, cmd)
	c.stdins = append(c.stdins, string(data))
	return c.outputs[string(data) == collectScript], nil
}

func (c *scriptedConn) calls() ([]string, []string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.cmds...), append([]string(nil), c.stdins...)
}

// A task that leaves a host's list takes its summary with it; a host that
// failed to collect keeps what it had.
func TestSummaries_AreForgottenWithTheirTask(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{result: Summary{Text: "s", Remaining: []string{"x"}}}
	svc := newSummaryService(t, host, summarizer)
	collectAndSummarize(svc)

	host.set(joinLines("broken"), nil)
	svc.Collect(context.Background())
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	collectAndSummarize(svc)
	assert.Equal(t, 1, summarizer.calls(), "a failed collection does not forget summaries")

	host.set(joinLines("::panemux-tasks v1", "::now 1000", "::end"), nil)
	svc.Collect(context.Background())
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	collectAndSummarize(svc)
	assert.Equal(t, 2, summarizer.calls(), "the task left the list, so its summary was dropped")
}

// Closing the service stops the summaries in flight.
func TestSummaries_CloseStopsRunningSummaries(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{gate: make(chan struct{})}
	svc := New(Options{RunLocal: host.run, Summarize: summarizer.summarize})
	snap := svc.Collect(context.Background())
	svc.Summaries(snap.Tasks)
	require.Eventually(t, func() bool { return summarizer.calls() == 1 }, 5*time.Second, 10*time.Millisecond)

	svc.Close()
	done := make(chan struct{})
	go func() { svc.waitSummaries(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a running summary outlived Close")
	}
	_, err := svc.RequestSummary("", "s10")
	assert.Error(t, err, "no summary starts after Close")
}

// A summary that runs out of time is a failure, not a hang.
func TestSummaries_TimeOut(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{gate: make(chan struct{})}
	svc := New(Options{RunLocal: host.run, Summarize: summarizer.summarize, SummaryTimeout: 50 * time.Millisecond})
	t.Cleanup(svc.Close)

	_, views := collectAndSummarize(svc)
	require.NotNil(t, views["local:claude:s10"])
	assert.Equal(t, SummaryFailed, views["local:claude:s10"].State)
}

// A log the host could not be asked for is a failure too.
func TestSummaries_ALogThatCannotBeReadIsAFailure(t *testing.T) {
	host := &summaryHost{fetchErr: errors.New("run conversation log read: exit status 1")}
	host.set(hostCollection(100, "idle"), nil)
	summarizer := &fakeSummarizer{}
	svc := newSummaryService(t, host, summarizer)

	_, views := collectAndSummarize(svc)
	assert.Zero(t, summarizer.calls())
	assert.Equal(t, &SummaryView{State: SummaryFailed, Error: "run conversation log read: exit status 1"},
		views["local:claude:s10"])
}

// A summary that finishes after its task left the list is dropped.
func TestSummaries_ASummaryForATaskThatLeftIsDropped(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{gate: make(chan struct{}), result: Summary{Text: "late", Remaining: []string{}}}
	svc := newSummaryService(t, host, summarizer)
	snap := svc.Collect(context.Background())
	svc.Summaries(snap.Tasks)
	require.Eventually(t, func() bool { return summarizer.calls() == 1 }, 5*time.Second, 10*time.Millisecond)

	host.set(joinLines("::panemux-tasks v1", "::now 1000", "::end"), nil)
	svc.Collect(context.Background())
	close(summarizer.gate)
	svc.waitSummaries()

	assert.Empty(t, svc.Summaries(asBusy(snap.Tasks)), "the late answer was not kept")
}

// Removing a host from ssh_connections drops its summaries.
func TestSummaries_AHostRemovedFromTheConfigLosesItsSummaries(t *testing.T) {
	conn := &scriptedConn{outputs: map[bool][]byte{
		true:  hostCollection(100, "idle"),
		false: conversationLog("remote work"),
	}}
	var mu sync.Mutex
	hosts := []string{"gpu-box"}
	summarizer := &fakeSummarizer{result: Summary{Text: "remote", Remaining: []string{"x"}}}
	svc := New(Options{
		Hosts: func() []string {
			mu.Lock()
			defer mu.Unlock()
			return hosts
		},
		Dial:      func(string) (Conn, error) { return conn, nil },
		RunLocal:  localOutput(minimalOutput("local"), nil),
		Summarize: summarizer.summarize,
	})
	t.Cleanup(svc.Close)
	snap, views := collectAndSummarize(svc)
	require.NotNil(t, views["ssh:gpu-box:claude:s10"])

	mu.Lock()
	hosts = nil
	mu.Unlock()
	svc.Collect(context.Background())
	_, err := svc.RequestSummary("gpu-box", "s10")
	assert.ErrorIs(t, err, ErrNoSummaryTask)
	mu.Lock()
	hosts = []string{"gpu-box"}
	mu.Unlock()
	assert.Nil(t, svc.Summaries(asBusy(snap.Tasks))["ssh:gpu-box:claude:s10"], "its summary was dropped")
}

// Nothing is summarized once the service is closed.
func TestSummaries_NothingStartsAfterClose(t *testing.T) {
	host := &summaryHost{}
	host.set(hostCollection(100, "idle"), conversationLog("a"))
	summarizer := &fakeSummarizer{}
	svc := New(Options{RunLocal: host.run, Summarize: summarizer.summarize})
	snap := svc.Collect(context.Background())
	svc.Close()

	assert.Empty(t, svc.Summaries(snap.Tasks))
	svc.waitSummaries()
	assert.Zero(t, summarizer.calls())
}

// asBusy is tasks with every state set to busy, so that asking for their
// summaries starts none.
func asBusy(tasks []Task) []Task {
	busy := append([]Task(nil), tasks...)
	for i := range busy {
		busy[i].State = StateBusy
	}
	return busy
}
