package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"panemux/internal/fileops"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Persisted summaries (issue #352).

// summaryClock is a settable Now for the services of one test.
type summaryClock struct {
	now time.Time
	mu  sync.Mutex
}

func (c *summaryClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *summaryClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type persistedSummaryFixture struct {
	host       *summaryHost
	summarizer *fakeSummarizer
	clock      *summaryClock
	path       string
	logs       []string
	mu         sync.Mutex
}

func newPersistedSummaryFixture(t *testing.T) *persistedSummaryFixture {
	t.Helper()
	return &persistedSummaryFixture{
		host:       &summaryHost{},
		summarizer: &fakeSummarizer{result: Summary{Text: "Fixing a race.", Remaining: []string{"Run make check"}}},
		clock:      &summaryClock{now: collectedAt},
		path:       filepath.Join(t.TempDir(), "task-summaries.json"),
	}
}

// start is one run of panemux: a new Service over the same file.
func (f *persistedSummaryFixture) start(t *testing.T) *Service {
	t.Helper()
	svc := New(Options{
		RunLocal:     f.host.run,
		Summarize:    f.summarizer.summarize,
		Now:          f.clock.Now,
		SummaryStore: NewSummaryStore(f.path),
		Logf: func(format string, args ...any) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.logs = append(f.logs, fmt.Sprintf(format, args...))
		},
	})
	// Close stops the summaries without waiting for them; one that saves
	// after it must still finish before the temporary directory is removed.
	t.Cleanup(func() {
		svc.Close()
		svc.waitSummaries()
	})
	return svc
}

func (f *persistedSummaryFixture) logged() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return strings.Join(f.logs, "\n")
}

func (f *persistedSummaryFixture) stored(t *testing.T) []storedSummary {
	t.Helper()
	data, err := os.ReadFile(f.path)
	require.NoError(t, err)
	var file summaryStoreFile
	require.NoError(t, json.Unmarshal(data, &file))
	return file.Summaries
}

func (f *persistedSummaryFixture) writeStored(t *testing.T, entries ...storedSummary) {
	t.Helper()
	data, err := json.Marshal(summaryStoreFile{Version: summaryStoreFileVersion, Summaries: entries})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f.path, data, 0o600))
}

// logWithLines is a conversation log whose lines are given as they are.
func logWithLines(lines ...string) []byte {
	body := strings.Join(lines, "\n") + "\n"
	return transcriptOutput(len(body), body)
}

func toolResultLine(text string) string {
	return `{"type":"user","message":{"role":"user","content":[{"type":"tool_result","content":` +
		fmt.Sprintf("%q", text) + `}]}}`
}

// After a restart the summary comes back — text, remaining work and when it
// was made — without reading the log or asking the agent again.
func TestPersistedSummaries_ARestartRestoresThemWithoutSummarizing(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), conversationLog("Fix the flaky test", "Found the race"))
	first := f.start(t)
	_, views := collectAndSummarize(first)
	require.Equal(t, SummaryReady, views["local:claude:s10"].State)
	first.Close()
	stored := f.stored(t)
	require.Len(t, stored, 1)
	assert.NotContains(t, string(must(os.ReadFile(f.path))), "Fix the flaky test", "the conversation is never stored")

	f.clock.advance(time.Hour)
	second := f.start(t)
	_, views = collectAndSummarize(second)
	assert.Equal(t, &SummaryView{
		State:        SummaryReady,
		Text:         "Fixing a race.",
		Remaining:    []string{"Run make check"},
		SummarizedAt: &collectedAt,
	}, views["local:claude:s10"])
	assert.Equal(t, 1, f.summarizer.calls(), "restoring does not summarize")
	assert.Equal(t, 1, f.host.fetchCount(), "nor read the log")
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

// A log that changed only where the excerpt does not look — a tool result
// appended — is read again, but the agent is not asked: the excerpt's hash
// is the one the summary was made from. The summary is current again, and
// stays current over a restart.
func TestPersistedSummaries_ALogChangeOutsideTheExcerptReusesTheSummary(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), logWithLines(userLine("Fix it"), assistantLine("Looking")))
	svc := f.start(t)
	collectAndSummarize(svc)

	f.host.set(hostCollection(300, "idle"),
		logWithLines(userLine("Fix it"), assistantLine("Looking"), toolResultLine("go test output")))
	_, views := collectAndSummarize(svc)
	assert.Equal(t, 1, f.summarizer.calls(), "the same excerpt is not summarized again")
	assert.Equal(t, 2, f.host.fetchCount(), "the changed log was read")
	assert.False(t, views["local:claude:s10"].Outdated)
	assert.True(t, views["local:claude:s10"].DoneCandidate == false)
	assert.Equal(t, &collectedAt, views["local:claude:s10"].SummarizedAt, "the answer is the one made before")

	svc.Close()
	again := f.start(t)
	_, views = collectAndSummarize(again)
	assert.False(t, views["local:claude:s10"].Outdated)
	assert.Equal(t, 2, f.host.fetchCount(), "the restored summary is current for the log it was last checked against")
	assert.Equal(t, 1, f.summarizer.calls())
}

// A conversation that moved on is summarized again, and that answer is the
// one stored.
func TestPersistedSummaries_AChangedConversationIsSummarizedAgain(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)
	collectAndSummarize(svc)
	firstHash := f.stored(t)[0].InputHash

	f.host.set(hostCollection(200, "idle"), conversationLog("a", "b"))
	f.summarizer.set(Summary{Text: "second", Remaining: []string{}}, nil)
	_, views := collectAndSummarize(svc)
	assert.Equal(t, 2, f.summarizer.calls())
	assert.Equal(t, "second", views["local:claude:s10"].Text)
	stored := f.stored(t)
	require.Len(t, stored, 1)
	assert.Equal(t, "second", stored[0].Text)
	assert.NotEqual(t, firstHash, stored[0].InputHash)
	assert.Equal(t, storedLog{ModTime: 990, Size: 200}, stored[0].Log)
}

// Asking again for a stopped task whose log changed, but not its excerpt,
// reads the log and keeps the answer: nothing new would be said.
func TestPersistedSummaries_AskingAgainWithTheSameExcerptReusesTheSummary(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100), logWithLines(userLine("Fix it")))
	svc := f.start(t)
	svc.Collect(context.Background())
	_, err := svc.RequestSummary("", "stopped")
	require.NoError(t, err)
	svc.waitSummaries()
	require.Equal(t, 1, f.summarizer.calls())

	f.host.set(hostCollection(200), logWithLines(userLine("Fix it"), toolResultLine("done")))
	svc.Collect(context.Background())
	_, err = svc.RequestSummary("", "stopped")
	require.NoError(t, err)
	svc.waitSummaries()
	snap := svc.Collect(context.Background())
	view := svc.Summaries(snap.Tasks)["local:claude:stopped"]
	assert.Equal(t, 1, f.summarizer.calls())
	assert.Equal(t, SummaryReady, view.State)
	assert.False(t, view.Outdated)
}

// A summary made by another version of the summarizer is shown, never as
// current, and is made again where a summary would be: here, a task idle on
// the very log it was made from.
func TestPersistedSummaries_AnotherSummarizerVersionIsOutdated(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	old := storedSummary{
		Host: "", Agent: AgentClaude, SessionID: "s10", Text: "old", Remaining: []string{},
		SummarizedAt: collectedAt.Add(-time.Hour), LastSeen: collectedAt,
		InputHash: strings.Repeat("0", 64), Summarizer: "an earlier summarizer",
		Log: storedLog{ModTime: 990, Size: 100},
	}
	busy := old
	busy.SessionID = "s11"
	f.writeStored(t, old, busy)
	f.host.set(hostCollection(100, "idle", "busy"), conversationLog("a"))
	svc := f.start(t)

	snap := svc.Collect(context.Background())
	views := svc.Summaries(snap.Tasks)
	assert.True(t, views["local:claude:s11"].Outdated, "a busy task keeps it, outdated")
	assert.False(t, views["local:claude:s11"].DoneCandidate)
	svc.waitSummaries()
	views = svc.Summaries(snap.Tasks)
	assert.Equal(t, 1, f.summarizer.calls(), "the idle task is summarized again")
	assert.Equal(t, "Fixing a race.", views["local:claude:s10"].Text)
	assert.False(t, views["local:claude:s10"].Outdated)
	assert.True(t, views["local:claude:s11"].Outdated)
}

// Summaries are kept apart by host, agent and session: a summary stored for
// one never shows on another.
func TestPersistedSummaries_AreKeptApartByHostAgentAndSession(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	other := func(host, agent, sessionID string) storedSummary {
		e := sampleStoredSummary(host, agent, sessionID)
		e.Text = "someone else's"
		e.LastSeen = collectedAt
		return e
	}
	f.writeStored(t,
		other("gpu-box", AgentClaude, "s10"),
		other("", AgentCodex, "019a0000-0000-7000-8000-000000000001"),
		other("", AgentClaude, "s99"),
	)
	f.host.set(hostCollection(100, "busy"), conversationLog("a"))
	svc := f.start(t)
	snap := svc.Collect(context.Background())
	assert.Nil(t, svc.Summaries(snap.Tasks)["local:claude:s10"])

	collectAndSummarize(svc)
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	collectAndSummarize(svc)
	keys := map[summaryKey]string{}
	for _, e := range f.stored(t) {
		keys[e.key()] = e.Text
	}
	assert.Equal(t, map[summaryKey]string{
		{host: "", agent: AgentClaude, sessionID: "s10"}:                                 "Fixing a race.",
		{host: "", agent: AgentCodex, sessionID: "019a0000-0000-7000-8000-000000000001"}: "someone else's",
		{host: "", agent: AgentClaude, sessionID: "s99"}:                                 "someone else's",
	}, keys, "gpu-box is not configured, so its summary went; the others were left alone")
}

// A save that fails keeps the summary on screen, logs why and says so, and
// the next save that succeeds clears it.
func TestPersistedSummaries_AFailedSaveKeepsTheSummary(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)
	fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: errors.New("disk full")}).Ops())

	_, views := collectAndSummarize(svc)
	assert.Equal(t, "Fixing a race.", views["local:claude:s10"].Text)
	assert.Contains(t, f.logged(), "disk full")
	assert.ErrorContains(t, svc.SummaryStoreError(), "disk full")

	fileops.SetOpsForTest(t, (&fileops.Spy{}).Ops())
	f.host.set(hostCollection(200, "idle"), conversationLog("a", "b"))
	collectAndSummarize(svc)
	assert.NoError(t, svc.SummaryStoreError())
	assert.Len(t, f.stored(t), 1)
}

// A file that cannot be used is moved aside and logged; summaries go on and
// are saved again at once.
func TestPersistedSummaries_ABrokenFileIsMovedAside(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	require.NoError(t, os.WriteFile(f.path, []byte(`{"version":99}`), 0o600))
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)

	collectAndSummarize(svc)
	assert.Contains(t, f.logged(), f.path+".bad-")
	assert.NoError(t, svc.SummaryStoreError())
	assert.Len(t, f.stored(t), 1)
	assert.FileExists(t, f.path+".bad-"+collectedAt.UTC().Format(summaryStoreBadTime))
}

// A file that cannot be read is not written over. Summaries work from memory
// and the error is reported until the file can be read.
func TestPersistedSummaries_AnUnreadableFileIsNotOverwritten(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	require.NoError(t, os.Mkdir(f.path, 0o700))
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)

	_, views := collectAndSummarize(svc)
	assert.Equal(t, "Fixing a race.", views["local:claude:s10"].Text)
	assert.Error(t, svc.SummaryStoreError())
	collectAndSummarize(svc)
	assert.Equal(t, 1, strings.Count(f.logged(), "task summary file"), "the same error is logged once")

	require.NoError(t, os.Remove(f.path))
	collectAndSummarize(svc)
	assert.NoError(t, svc.SummaryStoreError())
	assert.Len(t, f.stored(t), 1, "the summary made meanwhile is saved")
}

// A restart while a summary runs restores the summary before it, and nothing
// of the run: no pending, no error.
func TestPersistedSummaries_ARestartWhileSummarizingRestoresTheLastAnswer(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	first := f.start(t)
	collectAndSummarize(first)

	f.summarizer.gate = make(chan struct{})
	f.host.set(hostCollection(200, "idle"), conversationLog("a", "b"))
	snap := first.Collect(context.Background())
	views := first.Summaries(snap.Tasks)
	require.Equal(t, SummaryPending, views["local:claude:s10"].State)
	first.Close()

	f.summarizer.gate = nil
	f.host.set(hostCollection(200, "busy"), conversationLog("a", "b"))
	second := f.start(t)
	snap = second.Collect(context.Background())
	view := second.Summaries(snap.Tasks)["local:claude:s10"]
	assert.Equal(t, SummaryReady, view.State)
	assert.Empty(t, view.Error)
	assert.True(t, view.Outdated)
	assert.Equal(t, "Fixing a race.", view.Text)
}

// A task that leaves the list keeps its summary for 30 days after it was
// last listed, and while summaries are in use at all the file never holds
// more than 1000.
func TestPersistedSummaries_AreKeptThirtyDaysAfterLastListed(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)
	collectAndSummarize(svc)

	f.host.set(joinLines("::panemux-tasks v1", "::now 1000", "::end"), nil)
	f.clock.advance(29 * 24 * time.Hour)
	collectAndSummarize(svc)
	assert.Len(t, f.stored(t), 1, "kept within 30 days")
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	_, views := collectAndSummarize(svc)
	assert.Equal(t, "Fixing a race.", views["local:claude:s10"].Text, "a task listed again has its summary")
	assert.Equal(t, 1, f.summarizer.calls())

	f.host.set(joinLines("::panemux-tasks v1", "::now 1000", "::end"), nil)
	f.clock.advance(30*24*time.Hour + time.Second)
	collectAndSummarize(svc)
	assert.Empty(t, f.stored(t), "dropped 30 days after it was last listed")
}

func TestPersistedSummaries_AreCappedOldestFirst(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	entries := make([]storedSummary, 0, maxStoredSummaries+1)
	for i := range maxStoredSummaries + 1 {
		e := sampleStoredSummary("", AgentClaude, fmt.Sprintf("old%04d", i))
		e.LastSeen = collectedAt.Add(-time.Duration(i) * time.Minute)
		entries = append(entries, e)
	}
	f.writeStored(t, entries...)
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)
	collectAndSummarize(svc)

	stored := f.stored(t)
	assert.Len(t, stored, maxStoredSummaries)
	keys := map[string]bool{}
	for _, e := range stored {
		keys[e.SessionID] = true
	}
	assert.True(t, keys["s10"], "the new summary is kept")
	assert.False(t, keys[fmt.Sprintf("old%04d", maxStoredSummaries)], "the least recently listed went first")
	assert.False(t, keys[fmt.Sprintf("old%04d", maxStoredSummaries-1)])
	assert.True(t, keys["old0000"])
}

// When the dashboard last listed a task is saved at most once a day per
// summary: a poll every 10 seconds does not rewrite the file.
func TestPersistedSummaries_LastListedIsSavedDaily(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)
	collectAndSummarize(svc)
	stamp := func() time.Time { return f.stored(t)[0].LastSeen }
	require.Equal(t, collectedAt, stamp())

	f.clock.advance(23 * time.Hour)
	collectAndSummarize(svc)
	assert.Equal(t, collectedAt, stamp())
	f.clock.advance(time.Hour)
	collectAndSummarize(svc)
	assert.Equal(t, collectedAt.Add(24*time.Hour), stamp())
}

// A host removed from ssh_connections takes its stored summaries with it.
func TestPersistedSummaries_AHostRemovedFromTheConfigIsForgotten(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	remote := sampleStoredSummary("gpu-box", AgentClaude, "s10")
	remote.LastSeen = collectedAt
	local := sampleStoredSummary("", AgentClaude, "s10")
	local.LastSeen = collectedAt
	f.writeStored(t, remote, local)
	var mu sync.Mutex
	hosts := []string{"gpu-box"}
	conn := &scriptedConn{outputs: map[bool][]byte{true: hostCollection(100, "busy"), false: conversationLog("a")}}
	svc := New(Options{
		Hosts: func() []string {
			mu.Lock()
			defer mu.Unlock()
			return hosts
		},
		Dial:         func(string) (Conn, error) { return conn, nil },
		RunLocal:     localOutput(minimalOutput("local"), nil),
		Summarize:    f.summarizer.summarize,
		Now:          f.clock.Now,
		SummaryStore: NewSummaryStore(f.path),
	})
	t.Cleanup(svc.Close)
	_, views := collectAndSummarize(svc)
	require.NotNil(t, views["ssh:gpu-box:claude:s10"])

	mu.Lock()
	hosts = nil
	mu.Unlock()
	svc.Collect(context.Background())
	stored := f.stored(t)
	require.Len(t, stored, 1)
	assert.Empty(t, stored[0].Host)
}

// With summaries off nothing reads or writes the file: Summaries and
// RequestSummary are what use it, and the dashboard calls neither then.
func TestPersistedSummaries_CollectingAloneLeavesTheFileAlone(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)
	svc.Collect(context.Background())
	svc.Collect(context.Background())
	assert.NoFileExists(t, f.path)
	assert.NoError(t, svc.SummaryStoreError())
}

// Codex summaries are stored and reused the same way, under their own agent.
func TestPersistedSummaries_CodexIsStoredAndReused(t *testing.T) {
	path := filepath.Join(t.TempDir(), "task-summaries.json")
	codex := &fakeSummarizer{result: Summary{Text: "Codex"}}
	start := func() *Service {
		s := New(Options{
			RunLocal:       func(context.Context, string) ([]byte, error) { return codexTranscript("Codex request"), nil },
			SummarizeCodex: codex.summarize,
			Summarize:      (&fakeSummarizer{}).summarize,
			SummaryStore:   NewSummaryStore(path),
		})
		t.Cleanup(s.Close)
		return s
	}
	list := []Task{{ID: "codex", Agent: AgentCodex, SessionID: summarySessionID, State: StateIdle,
		Log: &LogVersion{File: codexRolloutName, ModTime: 1, Size: 2}}}
	first := start()
	first.rememberSummaryTasks("", list)
	first.Summaries(list)
	first.waitSummaries()
	first.Close()

	second := start()
	second.rememberSummaryTasks("", list)
	assert.Equal(t, "Codex", second.Summaries(list)["codex"].Text)
	list[0].Log = &LogVersion{File: codexRolloutName, ModTime: 2, Size: 3}
	second.Summaries(list)
	second.waitSummaries()
	view := second.Summaries(list)["codex"]
	assert.False(t, view.Outdated)
	assert.Equal(t, 1, codex.calls(), "the same Codex excerpt is not summarized again")
}

const codexRolloutName = "rollout-2026-10-08T01-00-00-" + summarySessionID + ".jsonl"

// A summary made while the file could not be read stays when the file comes
// back holding an older answer for the same task: the one in memory is newer.
func TestPersistedSummaries_ASummaryMadeMeanwhileWinsOverTheFile(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	require.NoError(t, os.Mkdir(f.path, 0o700))
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)
	_, views := collectAndSummarize(svc)
	require.Equal(t, "Fixing a race.", views["local:claude:s10"].Text)

	require.NoError(t, os.Remove(f.path))
	stale := sampleStoredSummary("", AgentClaude, "s10")
	stale.Text = "An older answer."
	f.writeStored(t, stale)
	_, views = collectAndSummarize(svc)
	assert.Equal(t, "Fixing a race.", views["local:claude:s10"].Text)
	stored := f.stored(t)
	require.Len(t, stored, 1)
	assert.Equal(t, "Fixing a race.", stored[0].Text)
}

// A summary that finishes after its host left the config is dropped, not
// saved: the task it belonged to is forgotten.
func TestPersistedSummaries_ASummaryFinishingAfterItsHostLeftIsDropped(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.summarizer.gate = make(chan struct{})
	var mu sync.Mutex
	hosts := []string{"gpu-box"}
	conn := &scriptedConn{outputs: map[bool][]byte{true: hostCollection(100, "idle"), false: conversationLog("a")}}
	svc := New(Options{
		Hosts: func() []string {
			mu.Lock()
			defer mu.Unlock()
			return hosts
		},
		Dial:         func(string) (Conn, error) { return conn, nil },
		RunLocal:     localOutput(minimalOutput("local"), nil),
		Summarize:    f.summarizer.summarize,
		Now:          f.clock.Now,
		SummaryStore: NewSummaryStore(f.path),
	})
	t.Cleanup(svc.Close)
	snap := svc.Collect(context.Background())
	views := svc.Summaries(snap.Tasks)
	require.Equal(t, SummaryPending, views["ssh:gpu-box:claude:s10"].State)

	mu.Lock()
	hosts = nil
	mu.Unlock()
	svc.Collect(context.Background())
	close(f.summarizer.gate)
	svc.waitSummaries()

	assert.Equal(t, 1, f.summarizer.calls())
	_, err := os.Stat(f.path)
	assert.ErrorIs(t, err, os.ErrNotExist, "nothing is saved for the forgotten task")
}

// Exactly as many summaries as are kept is not over the cap: nothing is
// dropped, so the file is not written again.
func TestPersistedSummaries_AtTheCapNothingIsDropped(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	entries := make([]storedSummary, 0, maxStoredSummaries)
	for i := range maxStoredSummaries {
		e := sampleStoredSummary("", AgentClaude, fmt.Sprintf("old%04d", i))
		e.LastSeen = collectedAt
		entries = append(entries, e)
	}
	data, err := json.MarshalIndent(summaryStoreFile{Version: summaryStoreFileVersion, Summaries: entries}, "", "  ")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(f.path, data, 0o600))
	f.host.set(hostCollection(100, "busy"), conversationLog("a"))
	svc := f.start(t)
	collectAndSummarize(svc)

	assert.Equal(t, string(data), string(must(os.ReadFile(f.path))), "the file is left as it was")
}

// A summary made while the file could not be read is saved once the file
// can be, even when its task is no longer listed by then.
func TestPersistedSummaries_ASummaryMadeMeanwhileIsSavedAfterItsTaskLeft(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	require.NoError(t, os.Mkdir(f.path, 0o700))
	f.host.set(hostCollection(100, "idle"), conversationLog("a"))
	svc := f.start(t)
	collectAndSummarize(svc)
	require.Error(t, svc.SummaryStoreError())

	require.NoError(t, os.Remove(f.path))
	f.host.set(minimalOutput("local"), conversationLog("a"))
	collectAndSummarize(svc)
	require.NoError(t, svc.SummaryStoreError())
	stored := f.stored(t)
	require.Len(t, stored, 1)
	assert.Equal(t, "s10", stored[0].SessionID)
}

// A summary Haiku did not make says which model made it, over a restart too,
// and the server log says so once — naming the model and nothing of the
// conversation (issue #353).
func TestPersistedSummaries_AModelOtherThanHaikuIsShownAndLoggedOnce(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.summarizer.result.UnexpectedModel = "claude-opus-5-5"
	f.host.set(hostCollection(100, "idle"), conversationLog("Fix the flaky test", "Found the race"))
	first := f.start(t)
	_, views := collectAndSummarize(first)
	require.Equal(t, SummaryReady, views["local:claude:s10"].State)
	assert.Equal(t, "claude-opus-5-5", views["local:claude:s10"].UnexpectedModel)

	f.host.set(hostCollection(200, "idle"), conversationLog("Fix the flaky test", "Fixed the race"))
	_, views = collectAndSummarize(first)
	require.Equal(t, 2, f.summarizer.calls())
	assert.Equal(t, "claude-opus-5-5", views["local:claude:s10"].UnexpectedModel)
	first.Close()
	first.waitSummaries()

	logged := f.logged()
	assert.Equal(t, 1, strings.Count(logged, "claude-opus-5-5"), "logged once per model: %s", logged)
	assert.Contains(t, logged, summaryModel)
	assert.NotContains(t, logged, "race", "the conversation is never logged")

	second := f.start(t)
	_, views = collectAndSummarize(second)
	assert.Equal(t, "claude-opus-5-5", views["local:claude:s10"].UnexpectedModel)
}

// A summary Haiku made says nothing of its model, and nothing is logged.
func TestPersistedSummaries_HaikusSummaryNamesNoModel(t *testing.T) {
	f := newPersistedSummaryFixture(t)
	f.host.set(hostCollection(100, "idle"), conversationLog("Fix the flaky test", "Found the race"))
	svc := f.start(t)
	_, views := collectAndSummarize(svc)
	require.Equal(t, SummaryReady, views["local:claude:s10"].State)
	assert.Empty(t, views["local:claude:s10"].UnexpectedModel)
	assert.NotContains(t, f.logged(), "model")
}
