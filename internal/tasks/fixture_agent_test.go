//go:build panemux_fixture_agent

package tasks

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
)

// These tests run only with the panemux_fixture_agent tag, which registers
// the fixture agent (issue #377). Its adapters are added through the
// adapter lists alone; nothing here reaches into the common code.

// fixtureSampleCommands extend taskRuleSampleCommands, so the prefilter
// contract covers the fixture agent's Task rule too.
var fixtureSampleCommands = []string{
	"panemux-fixture-agent", "/workspace/user/bin/panemux-fixture-agent --session-id=x", "panemux-fixture-agent --print",
}

const (
	fixtureBusy    = "11111111-2222-4333-8444-555555555501"
	fixtureWaiting = "11111111-2222-4333-8444-555555555502"
	fixtureMystery = "11111111-2222-4333-8444-555555555503"
	fixtureGone    = "11111111-2222-4333-8444-555555555504"
	fixtureOneShot = "11111111-2222-4333-8444-555555555505"
)

func fixtureStateJSON(pid, sessionID, status string) string {
	return `{"pid":` + pid + `,"sessionId":"` + sessionID + `","cwd":"/workspace/user/project","status":"` + status +
		`","waitingFor":"approve the plan","statusUpdatedAt":1790346600000}`
}

// fixtureLive are the live sections of one host's output: a busy, a
// waiting and an unknown session, a state file whose process is gone, one
// of a one-shot run, and two files the fake CLI never writes.
func fixtureLive() []string {
	return []string{
		collectHeader,
		"::now 1790346631",
		"::section panemux-fixture-agent/state",
		"::file 301.json", fixtureStateJSON("301", fixtureBusy, "busy"),
		"::file 302.json", fixtureStateJSON("302", fixtureWaiting, "waiting"),
		"::file 303.json", fixtureStateJSON("303", fixtureMystery, "mystery"),
		"::file 304.json", fixtureStateJSON("304", fixtureGone, "busy"),
		"::file 305.json", fixtureStateJSON("305", fixtureOneShot, "busy"),
		"::file 306.json", "not json",
		"::file 307.json", `{"pid":307,"sessionId":"../x","status":"busy"}`,
		"::section ps",
		"  89     1 bash",
		"  301    89 panemux-fixture-agent --session-id=" + fixtureBusy,
		"  302    89 /workspace/user/bin/panemux-fixture-agent",
		"  303    89 panemux-fixture-agent",
		"  305    89 panemux-fixture-agent --print",
		"  307    89 panemux-fixture-agent",
		"::section tmux",
		"89 task-11111111",
	}
}

var fixtureStopped = []string{
	"::section panemux-fixture-agent/logs",
	"1790346000\t2048\t" + fixtureBusy + ".log",
	"1790346500\t4096\t" + fixtureBusy + ".log",
	"1790340000\t17\t" + fixtureGone + ".log",
	"1790340000\t17\tnot-a-session.log",
	"garbled",
}

// withEmptySections adds every section of run that lines leave out, empty,
// before "::end".
func withEmptySections(run collectRun, lines []string) []byte {
	present := map[string]bool{}
	for _, line := range lines {
		if name, ok := strings.CutPrefix(line, "::section "); ok {
			present[name] = true
		}
	}
	out := slices.Clone(lines)
	for _, name := range run.sections {
		if !present[name] {
			out = append(out, "::section "+name)
		}
	}
	return joinLines(append(out, "::end")...)
}

func TestFixtureAgent_CollectListsItsRunningSessions(t *testing.T) {
	full := withEmptySections(collectPlan.full, slices.Concat(fixtureLive(), fixtureStopped))
	raw, err := parseCollectOutput(full, collectPlan.full)
	require.NoError(t, err)
	tasks := buildTasks("build-box", raw, collectedAt)

	states := map[string]State{}
	for _, task := range tasks {
		assert.Equal(t, fixtureAgentName, task.Agent, task.ID)
		assert.Equal(t, agent.SessionKey{Host: "build-box", Agent: agent.FixtureAgent, SessionID: task.SessionID}, task.Key())
		states[task.SessionID] = task.State
	}
	assert.Equal(t, map[string]State{
		fixtureBusy: StateBusy, fixtureWaiting: StateWait, fixtureMystery: StateUnknown,
	}, states, "a gone or one-shot process, and a file the fake CLI never writes, are no session")

	busy := findTask(t, tasks, "ssh:build-box:panemux-fixture-agent:"+fixtureBusy)
	assert.Equal(t, &LogVersion{ModTime: 1790346500, Size: 4096}, busy.Log, "the newest log of the session")
	assert.Equal(t, "/workspace/user/project", busy.CWD)
	assert.Equal(t, 301, busy.PID)
	assert.Equal(t, Location{Kind: LocationTmux, TmuxSession: "task-11111111", Attachable: true}, busy.Location)
	assert.Empty(t, busy.WaitSignature)

	waiting := findTask(t, tasks, "ssh:build-box:panemux-fixture-agent:"+fixtureWaiting)
	assert.Equal(t, "approve the plan", waiting.WaitingFor)
	assert.NotEmpty(t, waiting.WaitSignature)
	assert.Nil(t, waiting.Log, "a session without a log has no log version")

	mystery := findTask(t, tasks, "ssh:build-box:panemux-fixture-agent:"+fixtureMystery)
	assert.Empty(t, mystery.WaitSignature, "an unknown status is not guessed to be a wait")
	assert.Equal(t, "The fixture agent reported a status the dashboard does not recognize.", StateNote(mystery))
	assert.Empty(t, StateNote(busy))
}

// The full and the attention collection decide the fixture agent's live
// tasks from the same live sections.
func TestFixtureAgent_LiveTasksAreTheSameInTheFullAndTheAttentionCollection(t *testing.T) {
	fullOut := withEmptySections(collectPlan.full, slices.Concat(fixtureLive(), fixtureStopped))
	fullRaw, err := parseCollectOutput(fullOut, collectPlan.full)
	require.NoError(t, err)
	attentionRaw, err := parseCollectOutput(withEmptySections(collectPlan.live, fixtureLive()), collectPlan.live)
	require.NoError(t, err)

	var full []Task
	for _, task := range buildTasks("build-box", fullRaw, collectedAt) {
		task.Log = nil
		full = append(full, task)
	}
	attention := buildLiveTasks("build-box", attentionRaw, collectedAt)
	require.Len(t, attention, 3)
	assert.Equal(t, attention, full)
}

func TestFixtureAgent_LaunchesButDoesNotResume(t *testing.T) {
	assert.True(t, CanLaunch(fixtureAgentName))
	assert.Contains(t, LaunchAgents(), fixtureAgentName)
	_, resumes := launchPlan.resume[agent.FixtureAgent]
	assert.False(t, resumes)
	assert.Contains(t, launchPlan.script, "panemux-fixture-agent:new)\n")
	assert.NotContains(t, launchPlan.script, "panemux-fixture-agent:resume)")
	assert.Equal(t, "panemux-fixture-agent was not found on the host",
		launchPlan.messages[refusedNoAgent(agent.FixtureAgent)])

	l := launchPlan.launch[agent.FixtureAgent]
	id, tmuxSession, err := l.newSession(strings.NewReader(strings.Repeat("\x01", 16)))
	require.NoError(t, err)
	assert.True(t, l.validNewSessionID(id))
	assert.False(t, l.validNewSessionID(""), "the fixture agent is started with the ID panemux mints")
	assert.Equal(t, "task-"+id[:8], tmuxSession)

	host := &probeHost{collection: minimalOutput("s"), probe: fixtureProbe("found")}
	svc := New(Options{RunLocal: host.run})
	defer svc.Close()
	collectAndProbe(svc)
	runs := host.collects
	_, err = svc.Resume(context.Background(), "", fixtureAgentName, fixtureBusy)
	require.ErrorIs(t, err, ErrInvalidLaunch)
	assert.Equal(t, runs, host.collects, "a refused resume runs nothing on the host")
}

func fixtureProbe(fixture string) []byte {
	return probeAnswer("cli claude found", "cli codex found", "cli panemux-fixture-agent "+fixture)
}

// The probe asks about the fixture CLI as about every launchable agent's,
// and a missing one makes launch unavailable without touching collect.
func TestFixtureAgent_Availability(t *testing.T) {
	assert.Contains(t, probeAgents, agent.FixtureAgent)
	for _, tt := range []struct {
		probe  string
		status AvailabilityStatus
	}{{"found", Available}, {"missing", Unavailable}} {
		host := &probeHost{collection: minimalOutput("s"), probe: fixtureProbe(tt.probe)}
		svc := New(Options{RunLocal: host.run})
		collectAndProbe(svc)
		list := svc.Availability("")
		svc.Close()
		assert.Equal(t, tt.status, availabilityOf(list, agent.FixtureAgent, agent.Launch).Status, tt.probe)
		assert.Equal(t, Available, availabilityOf(list, agent.FixtureAgent, agent.Collect).Status, tt.probe)
		assert.Equal(t, Availability{}, availabilityOf(list, agent.FixtureAgent, agent.Resume), "resume is not declared")
	}
}

func TestFixtureAgent_TaskActions(t *testing.T) {
	host := &probeHost{collection: minimalOutput("s"), probe: fixtureProbe("found")}
	svc := New(Options{RunLocal: host.run})
	defer svc.Close()
	collectAndProbe(svc)

	running := Task{
		Agent: fixtureAgentName, SessionID: fixtureBusy, State: StateBusy, Log: &LogVersion{ModTime: 1, Size: 2},
	}
	assert.Equal(t, TaskActions{Summary: availableAction}, svc.TaskActions(running), "summary, and no resume")

	noSession := Task{Agent: fixtureAgentName, State: StateBusy}
	got := svc.TaskActions(noSession)
	assert.Nil(t, got.Resume)
	require.NotNil(t, got.Summary)
	assert.Equal(t, ReasonSessionInfoMissing, got.Summary.Reason)
}

func TestFixtureAgent_SummaryAdapterContract(t *testing.T) {
	reg, ok := summaryPlan.lookup(fixtureAgentName)
	require.True(t, ok)
	checkSummaryAdapterContract(t, reg, fixtureBusy, []byte(`{"text":"Fix the test"}`+"\n"))

	excerpt, ok, err := reg.source.excerpt([]byte(`{"text":"one"}`+"\n"+`{"text":"two"}`+"\n"), fixtureBusy)
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, "one\ntwo", excerpt)
	_, ok, err = reg.source.excerpt([]byte(`{"text":"one"}`+"\n"+`{"other":1}`), fixtureBusy)
	assert.Error(t, err, "a line without text makes the log unreadable")
	assert.False(t, ok)

	script, err := reg.source.logScript(fixtureBusy, LogVersion{})
	require.NoError(t, err)
	assert.Equal(t, `cat -- "$HOME/.panemux-fixture-agent/logs/`+fixtureBusy+`.log" 2>/dev/null; exit 0`+"\n", script)
}

// writeFixtureStub writes an executable stub for the fixture CLI that
// prints answer, so no test runs a CLI it does not control.
func writeFixtureStub(t *testing.T, answer string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "panemux-fixture-agent")
	script := "#!/bin/sh\ncat >/dev/null\nprintf '%s' '" + answer + "'\n"
	require.NoError(t, os.WriteFile(bin, []byte(script), 0o700)) //nolint:gosec // a test stub
	return bin
}

func TestFixtureAgent_SummaryRunner(t *testing.T) {
	ctx := context.Background()
	answer := `{"summary":" Fixing the test ","remaining":["run it", " ", "push it"]}`
	got, err := fixtureSummaryRunner{bin: writeFixtureStub(t, answer)}.newSummarizer()(ctx, "excerpt")
	require.NoError(t, err)
	assert.Equal(t, Summary{Text: "Fixing the test", Remaining: []string{"run it", "push it"}}, got)

	for answer, want := range map[string]string{
		"not json":                    "the fixture agent's answer was not JSON",
		`{"summary":"  "}`:            "the fixture agent's answer had no summary",
		`{"remaining":["only this"]}`: "the fixture agent's answer had no summary",
	} {
		_, gotErr := fixtureSummaryRunner{bin: writeFixtureStub(t, answer)}.newSummarizer()(ctx, "excerpt")
		assert.EqualError(t, gotErr, want, answer)
	}

	_, err = fixtureSummaryRunner{bin: filepath.Join(t.TempDir(), "panemux-fixture-agent")}.newSummarizer()(ctx, "excerpt")
	assert.Error(t, err)
	_, err = fixtureSummaryRunner{bin: "panemux-fixture-agent-not-on-path"}.newSummarizer()(ctx, "excerpt")
	assert.EqualError(t, err, errSummaryCLIMissing(agent.FixtureAgent).Error())

	reg, _ := summaryPlan.lookup(fixtureAgentName)
	assert.Equal(t, fixtureSummaryRunner{bin: "panemux-fixture-agent"}, reg.runner,
		"production runs the CLI by its literal name")
}
