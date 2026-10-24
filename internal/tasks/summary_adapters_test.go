package tasks

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/agent"
)

// thirdAgent is an agent only these tests register: one with nothing but a
// summary adapter, added without touching the common summary code
// (issue #347). Its UI and E2E checks are stage G's (#377).
const thirdAgent agent.ID = "third"

var thirdDefinition = agent.Definition{
	ID: thirdAgent, DisplayName: "Third", Capabilities: []agent.Capability{agent.Detect, agent.Summary},
}

type thirdSummarySource struct{}

func (thirdSummarySource) validSessionID(id string) bool {
	return validSessionID.MatchString(id) && strings.HasPrefix(id, "t-")
}

func (s thirdSummarySource) logScript(sessionID string, _ LogVersion) (string, error) {
	if !s.validSessionID(sessionID) {
		return "", ErrInvalidSummary
	}
	return "third-log " + sessionID, nil
}

func (thirdSummarySource) excerpt(out []byte, _ string) (string, bool, error) {
	switch text := string(out); text {
	case "":
		return "", false, nil
	case "garbled":
		return "", false, errors.New("third conversation log could not be read")
	default:
		return "third: " + text, true, nil
	}
}

func (thirdSummarySource) version() string { return "third-source-1" }

type thirdSummaryRunner struct{}

func (thirdSummaryRunner) version() string { return "third-runner-1" }
func (thirdSummaryRunner) unexpectedModelWarning(model string) string {
	return "third answered with " + model + ", not its usual model"
}
func (thirdSummaryRunner) newSummarizer() SummarizeFunc {
	return func(context.Context, string) (Summary, error) {
		return Summary{}, errors.New("third has no CLI in these tests")
	}
}

var thirdRegistration = registeredSummary{id: thirdAgent, source: thirdSummarySource{}, runner: thirdSummaryRunner{}}

func thirdSummaryPlan(t *testing.T) *summaryPlanned {
	t.Helper()
	plan, err := newSummaryPlan(append(agent.All(), thirdDefinition), append(summaryAdapters(), thirdRegistration))
	require.NoError(t, err)
	return &plan
}

// Every agent the registry declares summary for has exactly one adapter,
// and no adapter is registered for an agent that does not declare it.
func TestSummaryAdapters_MatchTheRegistry(t *testing.T) {
	plan, err := newSummaryPlan(agent.All(), summaryAdapters())
	require.NoError(t, err)
	for _, d := range agent.All() {
		_, ok := plan.lookup(string(d.ID))
		assert.Equal(t, d.Has(agent.Summary), ok, d.ID)
	}
	_, ok := plan.lookup("aider")
	assert.False(t, ok, "an agent the registry does not know has no adapter")
}

func TestNewSummaryPlan_RejectsAMismatchedRegistration(t *testing.T) {
	defs := []agent.Definition{
		thirdDefinition,
		{ID: "fourth", Capabilities: []agent.Capability{agent.Detect}},
	}
	adapter := func(id agent.ID) registeredSummary {
		return registeredSummary{id: id, source: thirdSummarySource{}, runner: thirdSummaryRunner{}}
	}
	for _, tt := range []struct {
		name string
		want string
		regs []registeredSummary
	}{
		{"duplicate", `summary adapter "third" is registered twice`,
			[]registeredSummary{thirdRegistration, thirdRegistration}},
		{"undeclared", `summary adapter "fourth" does not declare summary in the agent registry`,
			[]registeredSummary{thirdRegistration, adapter("fourth")}},
		{"unknown agent", `summary adapter "fifth" is not registered in the agent registry`,
			[]registeredSummary{thirdRegistration, adapter("fifth")}},
		{"declared without adapter", `agent "third" declares summary but has no summary adapter`, nil},
		{"no source", `summary adapter "third" has no source`,
			[]registeredSummary{{id: thirdAgent, runner: thirdSummaryRunner{}}}},
		{"no runner", `summary adapter "third" has no runner`,
			[]registeredSummary{{id: thirdAgent, source: thirdSummarySource{}}}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := newSummaryPlan(defs, tt.regs)
			assert.EqualError(t, err, tt.want)
		})
	}
	_, err := newSummaryPlan(defs, []registeredSummary{thirdRegistration})
	assert.NoError(t, err)
}

// checkSummaryAdapterContract is what every summary adapter must do, given a
// session ID it accepts and what its log script printed for that session.
// A new adapter's test calls it with its own fixture.
func checkSummaryAdapterContract(t *testing.T, reg registeredSummary, sessionID string, out []byte) {
	t.Helper()
	src := reg.source
	require.True(t, src.validSessionID(sessionID), "the fixture's session ID is valid")
	for _, bad := range []string{"", "a'b", "a b", "../x", "a/b", "a;b", "$(x)", "a\nb"} {
		assert.False(t, src.validSessionID(bad), "%q", bad)
	}
	// Whatever the adapter refuses — the IDs above, and well-formed IDs its
	// own rule does not accept, such as another agent's — never reaches a
	// script.
	for _, id := range summaryContractIDs {
		if src.validSessionID(id) {
			continue
		}
		_, err := src.logScript(id, LogVersion{})
		assert.ErrorIs(t, err, ErrInvalidSummary, "%q is never put in a script", id)
	}
	script, err := src.logScript(sessionID, LogVersion{})
	require.NoError(t, err)
	assert.NotEmpty(t, script)

	excerpt, ok, err := src.excerpt(out, sessionID)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.NotEmpty(t, excerpt)
	for _, broken := range [][]byte{nil, []byte("garbled")} {
		excerpt, ok, err := src.excerpt(broken, sessionID)
		assert.True(t, err != nil || !ok, "a log it cannot read is an error or unreadable, never an excerpt: %q", excerpt)
	}

	assert.Equal(t, src.version(), src.version())
	assert.Equal(t, reg.runner.version(), reg.runner.version())
	assert.NotEmpty(t, reg.runner.version(), "the runner's instruction is part of the summary's identity")
	if warning := reg.runner.unexpectedModelWarning("model-x-1"); warning != "" {
		assert.Contains(t, warning, "model-x-1", "a warning names the model that answered")
	}
	assert.NotNil(t, reg.runner.newSummarizer())
}

// summaryContractIDs are session IDs checked against every adapter: the
// malformed ones no adapter may accept, and well-formed ones of several
// shapes, each refused by some adapter's own rule.
var summaryContractIDs = []string{
	"", "a'b", "a b", "../x", "a/b", "a;b", "$(x)", "a\nb", "-x", "s10", "t-1", "x-1", summarySessionID,
	"7C21E0A4-0000-4000-8000-00000000000Z",
}

func TestSummaryAdapters_Contract(t *testing.T) {
	regs := map[agent.ID]registeredSummary{}
	for _, r := range append(summaryAdapters(), thirdRegistration) {
		regs[r.id] = r
	}
	t.Run("claude", func(t *testing.T) {
		checkSummaryAdapterContract(t, regs[agent.Claude], "s10", conversationLog("Fix the test"))
	})
	t.Run("codex", func(t *testing.T) {
		checkSummaryAdapterContract(t, regs[agent.Codex], summarySessionID, codexTranscript("Fix the test"))
		assert.False(t, regs[agent.Codex].source.validSessionID("s10"), "codex sessions are UUIDs")
	})
	t.Run("third", func(t *testing.T) {
		checkSummaryAdapterContract(t, regs[thirdAgent], "t-1", []byte("Fix the test"))
	})
}

// Claude's and Codex's summarizer versions are what they were before the
// adapters, so the summaries saved by an earlier build stay current.
//
//efficacy:exempt pins hashes the adapter refactoring must leave unchanged
func TestSummarizerVersion_KeepsClaudeAndCodexHashes(t *testing.T) {
	assert.Equal(t, "aef5808a03ed7ecc08a7ec4251e2bc2c671113475f53b711896bba9efd0cfa5e", summarizerVersion(AgentClaude))
	assert.Equal(t, "de0cf733e38c4773f59761a8a3aee578971ee0e5ccb5a082e319dfd7a82ed056", summarizerVersion(AgentCodex))
}

// A source's or runner's version is part of the summarizer's identity: a
// change to either makes earlier summaries outdated.
func TestSummarizerVersion_FoldsInTheAdaptersVersions(t *testing.T) {
	plan := thirdSummaryPlan(t)
	base := plan.summarizerVersion(string(thirdAgent))
	require.NotEmpty(t, base)
	for name, reg := range map[string]registeredSummary{
		"source": {id: thirdAgent, source: versionedSource{thirdSummarySource{}, "third-source-2"},
			runner: thirdSummaryRunner{}},
		"runner": {id: thirdAgent, source: thirdSummarySource{},
			runner: versionedRunner{thirdSummaryRunner{}, "third-runner-2"}},
	} {
		changed, err := newSummaryPlan([]agent.Definition{thirdDefinition}, []registeredSummary{reg})
		require.NoError(t, err)
		assert.NotEqual(t, base, changed.summarizerVersion(string(thirdAgent)), name)
	}
	assert.Empty(t, plan.summarizerVersion("aider"), "an agent with no adapter has no summarizer")
}

type versionedSource struct {
	thirdSummarySource
	v string
}

func (s versionedSource) version() string { return s.v }

type versionedRunner struct {
	thirdSummaryRunner
	v string
}

func (r versionedRunner) version() string { return r.v }

func newThirdSummaryService(t *testing.T, host *summaryHost, summarizer *fakeSummarizer) *Service {
	t.Helper()
	svc := New(Options{
		RunLocal:    host.run,
		Summarizers: map[agent.ID]SummarizeFunc{thirdAgent: summarizer.summarize},
		Now:         func() time.Time { return collectedAt },
		summaryPlan: thirdSummaryPlan(t),
	})
	t.Cleanup(svc.Close)
	return svc
}

// An agent added with only a summary adapter is asked for, read, summarized
// and shown by the common code, apart from a Claude task with the same
// session ID on the same host.
func TestSummaries_AThirdAgentIsSummarizedThroughItsAdapter(t *testing.T) {
	host := &summaryHost{log: []byte("Fix the flaky test")}
	summarizer := &fakeSummarizer{result: Summary{Text: "Third summary", Remaining: []string{"docs"}}}
	svc := newThirdSummaryService(t, host, summarizer)
	log := LogVersion{Size: 18}
	third := Task{ID: "local:third:t-1", Agent: string(thirdAgent), SessionID: "t-1", State: StateStop, Log: &log}
	claude := Task{ID: "local:claude:t-1", Agent: AgentClaude, SessionID: "t-1", State: StateStop, Log: &log}
	svc.rememberSummaryTasks("", []Task{third, claude})

	assert.Equal(t, &TaskAction{Status: Available}, svc.TaskActions(third).Summary)
	view, err := svc.RequestAgentSummary("", string(thirdAgent), "t-1")
	require.NoError(t, err)
	assert.Equal(t, SummaryPending, view.State)
	svc.waitSummaries()

	views := svc.Summaries([]Task{third, claude})
	require.Contains(t, views, third.ID)
	assert.Equal(t, SummaryReady, views[third.ID].State)
	assert.Equal(t, "Third summary", views[third.ID].Text)
	assert.NotContains(t, views, claude.ID, "the Claude task with the same session ID is not summarized")
	assert.Equal(t, []string{"third-log t-1"}, host.fetches)
	assert.Equal(t, []string{"third: Fix the flaky test"}, summarizer.excerpts)

	// The same log again reuses the answer.
	_, err = svc.RequestAgentSummary("", string(thirdAgent), "t-1")
	require.NoError(t, err)
	svc.waitSummaries()
	assert.Equal(t, 1, summarizer.calls())
}

// The adapter's session ID rule decides the action and the request alike.
func TestSummaries_AThirdAgentsSessionRuleRefusesOthers(t *testing.T) {
	svc := newThirdSummaryService(t, &summaryHost{}, &fakeSummarizer{})
	log := LogVersion{Size: 1}
	bad := Task{Agent: string(thirdAgent), SessionID: "x-1", State: StateStop, Log: &log}
	svc.rememberSummaryTasks("", []Task{bad})

	action := svc.TaskActions(bad).Summary
	require.NotNil(t, action)
	assert.Equal(t, ReasonSessionInfoMissing, action.Reason)
	_, err := svc.RequestAgentSummary("", string(thirdAgent), "x-1")
	assert.ErrorIs(t, err, ErrInvalidSummary)
	assert.Empty(t, svc.Summaries([]Task{bad}))
}

// A log the adapter cannot read is an error, one with nothing to summarize
// is unreadable, and its runner's failure is shown as it said it.
func TestSummaries_AThirdAgentsFailuresAreKeptApart(t *testing.T) {
	for _, tt := range []struct {
		name, log string
		err       error
		state     SummaryState
		message   string
	}{
		{"garbled log", "garbled", nil, SummaryFailed, "third conversation log could not be read"},
		{"empty log", "", nil, SummaryUnreadable, ""},
		{"runner failure", "Fix it", errors.New("third did not finish summarizing in time"), SummaryFailed,
			"third did not finish summarizing in time"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			summarizer := &fakeSummarizer{err: tt.err, result: Summary{Text: "x"}}
			svc := newThirdSummaryService(t, &summaryHost{log: []byte(tt.log)}, summarizer)
			log := LogVersion{Size: 4}
			task := Task{ID: "t", Agent: string(thirdAgent), SessionID: "t-1", State: StateStop, Log: &log}
			svc.rememberSummaryTasks("", []Task{task})
			_, err := svc.RequestAgentSummary("", string(thirdAgent), "t-1")
			require.NoError(t, err)
			svc.waitSummaries()
			view := svc.Summaries([]Task{task})["t"]
			require.NotNil(t, view)
			assert.Equal(t, tt.state, view.State)
			assert.Equal(t, tt.message, view.Error)
		})
	}
}

// Without an override, an agent's summary is made by its own runner; no
// other agent's CLI stands in for it.
func TestSummaries_UseTheAgentsOwnRunnerWithoutAnOverride(t *testing.T) {
	svc := New(Options{
		RunLocal: (&summaryHost{log: []byte("text")}).run,
		Summarizers: map[agent.ID]SummarizeFunc{agent.Claude: func(context.Context, string) (Summary, error) {
			t.Error("Claude's runner made a third agent's summary")
			return Summary{}, nil
		}},
		summaryPlan: thirdSummaryPlan(t),
	})
	t.Cleanup(svc.Close)
	log := LogVersion{Size: 4}
	task := Task{ID: "t", Agent: string(thirdAgent), SessionID: "t-1", State: StateStop, Log: &log}
	svc.rememberSummaryTasks("", []Task{task})
	_, err := svc.RequestAgentSummary("", string(thirdAgent), "t-1")
	require.NoError(t, err)
	svc.waitSummaries()
	assert.Equal(t, "third has no CLI in these tests", svc.Summaries([]Task{task})["t"].Error)
}

// A CLI missing on the panemux host is said so, in fixed words naming no
// path, for each agent's runner.
func TestSummarizers_ACLIMissingOnThePanemuxHost(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	_, err := newClaudeSummarizer(claudeBin, nil)(context.Background(), "excerpt")
	assert.EqualError(t, err, "claude CLI was not found on the panemux host")
	_, err = newCodexSummarizer()(context.Background(), "excerpt")
	assert.EqualError(t, err, "codex CLI was not found on the panemux host")
	_, err = exec.LookPath(claudeBin)
	require.Error(t, err, "the test's PATH has no claude")
}

// A model other than the one asked for is logged in the runner's own words
// (once per model, as TestPersistedSummaries_AModelOtherThanHaikuIsShownAndLoggedOnce
// checks for Claude); a runner that says nothing logs nothing.
func TestSummaries_AnUnexpectedModelIsLoggedInTheRunnersWords(t *testing.T) {
	for _, tt := range []struct {
		name   string
		runner summaryRunner
		want   []string
	}{
		{"third", thirdSummaryRunner{}, []string{"task summaries: third answered with big-model-1, not its usual model"}},
		{"silent", silentRunner{}, nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			plan, err := newSummaryPlan([]agent.Definition{thirdDefinition},
				[]registeredSummary{{id: thirdAgent, source: thirdSummarySource{}, runner: tt.runner}})
			require.NoError(t, err)
			var logged []string
			summarizer := &fakeSummarizer{result: Summary{Text: "x", UnexpectedModel: "big-model-1"}}
			svc := New(Options{
				RunLocal:    (&summaryHost{log: []byte("Fix it")}).run,
				Summarizers: map[agent.ID]SummarizeFunc{thirdAgent: summarizer.summarize},
				Logf:        func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) },
				summaryPlan: &plan,
			})
			t.Cleanup(svc.Close)
			log := LogVersion{Size: 6}
			svc.rememberSummaryTasks("", []Task{{Agent: string(thirdAgent), SessionID: "t-1", Log: &log}})
			_, err = svc.RequestAgentSummary("", string(thirdAgent), "t-1")
			require.NoError(t, err)
			svc.waitSummaries()
			assert.Equal(t, 1, summarizer.calls())
			assert.Equal(t, tt.want, logged)
		})
	}
}

type silentRunner struct{ thirdSummaryRunner }

func (silentRunner) unexpectedModelWarning(string) string { return "" }
