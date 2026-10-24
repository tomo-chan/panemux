package tasks

import (
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
)

// The composed scripts carry every section under its qualified name: the
// common ones bare, an adapter's under its agent's ID.
func TestCollectScripts_NameEverySectionInItsNamespace(t *testing.T) {
	assert.True(t, strings.Contains(collectScript, "echo '"+collectHeader+"'"))
	assert.Equal(t, "::panemux-tasks v2", collectHeader)
	for _, name := range []string{"ps", "tmux", sectionCWD, "env", "claude/state", "codex/open", "codex/permission"} {
		assert.Contains(t, collectScript, "echo '::section "+name+"'", name)
		assert.Contains(t, attentionScript, "echo '::section "+name+"'", name)
	}
	for _, name := range []string{"claude/transcripts", "codex/rollouts"} {
		assert.Contains(t, collectScript, "echo '::section "+name+"'", name)
		assert.NotContains(t, attentionScript, "::section "+name, name)
	}
	assert.Equal(t,
		[]string{
			"claude/state", "codex/open", "codex/permission", "ps", "tmux", sectionCWD, "env",
			"claude/transcripts", "codex/rollouts",
		},
		collectPlan.full.sections)
	assert.Equal(t,
		[]string{"claude/state", "codex/open", "codex/permission", "ps", "tmux", sectionCWD, "env"},
		collectPlan.live.sections)
}

// Every agent the registry says collects has an adapter registered under
// its ID, and no adapter is registered for an agent that does not collect.
func TestCollectAdapters_MatchTheRegistry(t *testing.T) {
	require.NoError(t, validateCollectAdapters(agent.All(), collectAdapters()))

	registered := map[agent.ID]bool{}
	for _, r := range collectAdapters() {
		registered[r.id] = true
	}
	for _, d := range agent.All() {
		assert.Equal(t, d.Has(agent.Collect), registered[d.ID], d.ID)
	}
}

type fakeAdapter struct {
	secs  []collectSection
	hints []string
}

func (f fakeAdapter) sections() []collectSection   { return f.secs }
func (f fakeAdapter) processHints() []string       { return f.hints }
func (f fakeAdapter) newCollected() agentCollected { return &claudeCollected{} }
func (f fakeAdapter) stateNote(Task) string        { return "" }
func (f fakeAdapter) withSections(s ...collectSection) fakeAdapter {
	f.secs = s
	return f
}

func TestComposeCollect_RejectsAMismatchedRegistration(t *testing.T) {
	collects := agent.Definition{ID: "third", Capabilities: []agent.Capability{agent.Detect, agent.Collect}}
	detectsOnly := agent.Definition{ID: "fourth", Capabilities: []agent.Capability{agent.Detect}}
	ok := fakeAdapter{hints: []string{"third"}}.withSections(collectSection{name: "state", stage: stageLive})

	defs := []agent.Definition{collects}
	only := func(a collectAdapter) []registeredCollect { return []registeredCollect{{id: "third", adapter: a}} }
	tests := []struct {
		name     string
		want     string
		defs     []agent.Definition
		adapters []registeredCollect
	}{
		{"valid", "", defs, only(ok)},
		{"a collecting agent without an adapter", `"third" declares collect`, defs, nil},
		{"an adapter for an unregistered agent", `"fifth" is not registered`, defs,
			[]registeredCollect{{id: "third", adapter: ok}, {id: "fifth", adapter: ok}}},
		{"an adapter for an agent that does not collect", `"fourth" does not declare collect`,
			[]agent.Definition{collects, detectsOnly},
			[]registeredCollect{{id: "third", adapter: ok}, {id: "fourth", adapter: ok}}},
		{"two adapters for one agent", `"third" is registered twice`, defs,
			[]registeredCollect{{id: "third", adapter: ok}, {id: "third", adapter: ok}}},
		{"a duplicate section", `section "third/state" is declared twice`, defs, only(ok.withSections(
			collectSection{name: "state"}, collectSection{name: "state", stage: stageStopped}))},
		{"a section name with a slash", `invalid section name "a/b"`, defs,
			only(ok.withSections(collectSection{name: "a/b"}))},
		{"an empty section name", `invalid section name ""`, defs, only(ok.withSections(collectSection{name: ""}))},
		{"a process hint the prefilter cannot carry", `invalid process hint "a|b"`, defs,
			only(fakeAdapter{secs: ok.secs, hints: []string{"a|b"}})},
		{"no process hint", `"third" has no process hint`, defs, only(fakeAdapter{secs: ok.secs})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := composeCollect(tc.defs, tc.adapters)
			if tc.want == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// A composed plan puts each adapter's live sections before the common ones
// and its stopped sections after them, and only the full run has the latter.
func TestComposeCollect_OrdersSectionsByStage(t *testing.T) {
	defs := []agent.Definition{{ID: "third", Capabilities: []agent.Capability{agent.Collect}}}
	adapter := fakeAdapter{hints: []string{"third"}}.withSections(
		collectSection{name: "logs", stage: stageStopped, script: "echo logs\n"},
		collectSection{name: "state", stage: stageLive, files: true, script: "echo state\n"},
	)
	plan, err := composeCollect(defs, []registeredCollect{{id: "third", adapter: adapter}})
	require.NoError(t, err)

	assert.Equal(t, []string{"third/state", "ps", "tmux", sectionCWD, "env", "third/logs"}, plan.full.sections)
	assert.Equal(t, []string{"third/state", "ps", "tmux", sectionCWD, "env"}, plan.live.sections)
	assert.Less(t, strings.Index(plan.full.script, "echo state"), strings.Index(plan.full.script, "::section ps"))
	assert.Greater(t, strings.Index(plan.full.script, "echo logs"), strings.Index(plan.full.script, "::section env"))
	assert.NotContains(t, plan.live.script, "echo logs")
	assert.True(t, strings.HasSuffix(plan.full.script, collectEnd))
	assert.True(t, strings.HasSuffix(plan.live.script, collectEnd))
	assert.Contains(t, plan.full.script, `~ /third/`)
}

// The cwd and env prefilter is composed from the adapters' hints, and lets
// through every process a task rule lists: a process it dropped would be
// listed without its working directory or pane.
// taskRuleSampleCommands are command lines the agents' Task rules are
// checked against, lower case: the prefilters compare case-sensitively while
// the rules do not, a gap left to #375.
var taskRuleSampleCommands = []string{
	"claude", "/usr/local/bin/claude --verbose", "claude -p hello", "claude-code",
	"/home/user/.local/share/claude/versions/2.1.0", "claude --resume 5d0b91c2",
	"node /usr/lib/node_modules/@anthropic-ai/claude-code/cli.js",
	"bun /opt/claude-code/cli.js --continue", "deno run claude/cli.ts",
	"codex", "/usr/local/bin/codex --yolo", "codex exec 'fix it'", "codex app-server --listen unix://",
	"/opt/homebrew/bin/codex resume 01a0e2b9-d054-7cc2-9278-000000000001", "codex -c model=o3 review",
	"codex-helper", "/usr/local/bin/codexx", "node /opt/codex/cli.js",
	// The Task rules compare base names in lower case, so the prefilter does
	// too (issue #375).
	"/Applications/Claude.app/Contents/MacOS/CLAUDE", "/opt/Claude-Code/bin/x", "NODE /x/Claude/cli.js",
	"/usr/local/bin/CODEX", "Codex resume",
}

func TestCollectPrefilter_CoversEveryTaskRule(t *testing.T) {
	var hints []string
	for _, r := range collectAdapters() {
		hints = append(hints, r.adapter.processHints()...)
	}
	assert.Equal(t, strings.Join(hints, "|"), collectPlan.prefilter)
	prefilter := regexp.MustCompile(`awk '(tolower\(\$2 " " \$3\) ~ /` +
		regexp.QuoteMeta(collectPlan.prefilter) + `/ \{ print \$1 \})'`)
	m := prefilter.FindStringSubmatch(collectScript)
	require.NotNil(t, m, "the cwd section's prefilter")
	samples := slices.Concat(taskRuleSampleCommands, fixtureSampleCommands)
	var rows []string
	for i, command := range samples {
		rows = append(rows, "  "+strconv.Itoa(1000+i)+" "+command)
	}
	cmd := exec.Command("awk", m[1]) //nolint:gosec // the program is collectScript's own constant
	cmd.Stdin = strings.NewReader(strings.Join(rows, "\n") + "\n")
	out, err := cmd.Output()
	require.NoError(t, err)
	passed := map[string]bool{}
	for _, pid := range strings.Fields(string(out)) {
		passed[pid] = true
	}

	for _, d := range agent.All() {
		if d.Detection.Task == nil {
			continue
		}
		matched := 0
		for _, command := range samples {
			if !d.Detection.Task.Process(command) {
				continue
			}
			matched++
			assert.True(t, passed[strconv.Itoa(1000+slices.Index(samples, command))], "%s: %q", d.ID, command)
		}
		assert.Positive(t, matched, "%s matched none of the sample commands", d.ID)
	}
}

// The codex/open section's own filter — the base name of a process's
// program — passes every process codex's Task rule takes, and nothing else:
// a process it drops is never matched to its open rollout.
func TestCodexOpenFilter_PassesEveryProcessTheTaskRuleTakes(t *testing.T) {
	m := regexp.MustCompile(`(?m)^awk '([^']*)'`).FindStringSubmatch(codexOpenScript)
	require.NotNil(t, m, "codex/open's first awk program")
	var rows []string
	want := map[string]bool{}
	for i, command := range taskRuleSampleCommands {
		pid := strconv.Itoa(1000 + i)
		rows = append(rows, "  "+pid+" 05:00 "+command)
		want[pid] = agent.IsCodexProgram(command)
	}
	cmd := exec.Command("awk", m[1]) //nolint:gosec // the program is codexOpenScript's own constant
	cmd.Stdin = strings.NewReader(strings.Join(rows, "\n") + "\n")
	out, err := cmd.Output()
	require.NoError(t, err)

	got := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		pid, etime, ok := strings.Cut(line, " ")
		require.True(t, ok, "%q", line)
		assert.Equal(t, "05:00", etime, "%q", line)
		got[pid] = true
	}
	for i, command := range taskRuleSampleCommands {
		pid := strconv.Itoa(1000 + i)
		assert.Equal(t, want[pid], got[pid], "%q", command)
	}
	assert.True(t, slices.ContainsFunc(taskRuleSampleCommands, agent.IsCodexProgram))
}

func TestParseCollectOutput_FailsWhenADeclaredSectionIsMissing(t *testing.T) {
	for _, missing := range collectPlan.full.sections {
		t.Run(missing, func(t *testing.T) {
			lines := []string{collectHeader, "::now 1000"}
			for _, name := range collectPlan.full.sections {
				if name != missing {
					lines = append(lines, "::section "+name)
				}
			}
			_, err := parseCollectOutput(joinLines(append(lines, "::end")...), collectPlan.full)
			require.Error(t, err)
			assert.Contains(t, err.Error(), `missing section "`+missing+`"`)
		})
	}
}

// The attention collection asks for the live sections only. A stopped
// section in its output is still a section it knows: it is read, and the
// live tasks do not use it.
func TestParseCollectOutput_AttentionNeedsTheLiveSectionsOnly(t *testing.T) {
	lines := []string{collectHeader, "::now 1000"}
	for _, name := range collectPlan.live.sections {
		lines = append(lines, "::section "+name)
	}
	_, err := parseCollectOutput(joinLines(append(lines, "::end")...), collectPlan.live)
	require.NoError(t, err)

	_, err = parseCollectOutput(joinLines(append(lines, "::section claude/transcripts", "::end")...), collectPlan.live)
	require.NoError(t, err)
	_, err = parseCollectOutput(joinLines(append(lines, "::section claude/other", "::end")...), collectPlan.live)
	require.Error(t, err)
	assert.Contains(t, err.Error(), `unknown section "claude/other"`)
}

func TestParseCollectOutput_RejectsTheFirstProtocolVersion(t *testing.T) {
	_, err := parseCollectOutput(fullOutput("::panemux-tasks v1", "::now 1000", "::end"), collectPlan.full)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no header")
}

// The bare section names of the first protocol are no longer sections.
func TestParseCollectOutput_RejectsUnqualifiedAgentSections(t *testing.T) {
	for _, old := range []string{"state", "transcripts", "codex-open", "codex-rollouts"} {
		_, err := parseCollectOutput(fullOutput(collectHeader, "::now 1000", "::section "+old, "::end"), collectPlan.full)
		require.Error(t, err, old)
		assert.Contains(t, err.Error(), "unknown section", old)
	}
}

// ::file is accepted only in a section that declares it.
func TestParseCollectOutput_AcceptsFilesOnlyWhereDeclared(t *testing.T) {
	for _, name := range collectPlan.full.sections {
		out := fullOutput(collectHeader, "::now 1000", "::section "+name, "::file 1.json", "{}", "::end")
		_, err := parseCollectOutput(out, collectPlan.full)
		if name == "claude/state" {
			assert.NoError(t, err, name)
		} else {
			assert.Error(t, err, name)
		}
	}
}

// parityOutput is one host's output: the live sections with a waiting
// claude session, one in a state panemux does not know, a claude process no
// state file explains, a codex session waiting on a question and a codex
// process without a rollout yet; and the stopped sections.
func parityOutput() (live, stopped []string) {
	const rollout = "/home/demo/.codex/sessions/2026/10/16/" +
		"rollout-2026-10-16T00-00-00-01a0e2b9-d054-7cc2-9278-000000000001.jsonl"
	live = []string{
		collectHeader,
		"::now 1790346631",
		"::section claude/state",
		"::file 121.json",
		`{"pid":121,"sessionId":"5d0b91c2","cwd":"/workspace/user/project","status":"waiting",` +
			`"waitingFor":"approve Bash","statusUpdatedAt":1790346600000}`,
		"::file 122.json",
		`{"pid":122,"sessionId":"77aa","cwd":"/workspace/user/other","status":"mystery"}`,
		"::section codex/open",
		"131\t05:00",
		"131\t05:00\t1790346500 900\t\t" +
			`{"timestamp":"2026-10-16T00:00:00.000Z","type":"event_msg","payload":{"type":"task_started"}}` + "\t" +
			`{"timestamp":"2026-10-16T00:01:00.000Z","type":"response_item",` +
			`"payload":{"type":"function_call","name":"request_user_input"}}` +
			"\t\"cwd\":\"/workspace/user/api\"\t\"originator\":\"codex-tui\"\t" + rollout,
		"::section codex/permission",
		"::section ps",
		"  89     1 bash",
		"  121    89 claude",
		"  122    89 claude --resume 77aa",
		"  123    89 claude",
		"  131    89 codex",
		"  132    89 codex",
		"::section tmux",
		"89 task-7c21",
		"::section cwd",
		"123 /workspace/user/unexplained",
		"132 /workspace/user/fresh",
		"::section env",
	}
	stopped = []string{
		"::section claude/transcripts",
		"1790346000\t5d0b91c2.jsonl\t\"cwd\":\"/workspace/user/project\"\t2048",
		"1790340000\told-session.jsonl\t\t17",
		"::section codex/rollouts",
		"1790300000\t500\trollout-2026-10-15T00-00-00-01a0e2b9-d054-7cc2-9278-000000000009.jsonl" +
			"\t\"cwd\":\"/w\"\t\"originator\":\"codex-tui\"",
	}
	return live, stopped
}

// One output read by the full and the attention collection lists the same
// running tasks: both decide a live task's state from the live sections
// alone. The full collection adds the stopped sessions and, from the
// conversation logs, a claude task's log version; nothing else differs.
func TestLiveTasks_AreTheSameInTheFullAndTheAttentionCollection(t *testing.T) {
	live, stopped := parityOutput()
	fullRaw, err := parseCollectOutput(joinLines(slices.Concat(live, stopped, []string{"::end"})...), collectPlan.full)
	require.NoError(t, err)
	attentionRaw, err := parseCollectOutput(joinLines(slices.Concat(live, []string{"::end"})...), collectPlan.live)
	require.NoError(t, err)

	full := buildTasks("build-box", fullRaw, collectedAt)
	attention := buildLiveTasks("build-box", attentionRaw, collectedAt)

	var fullLive []Task
	for _, task := range full {
		if task.Location.Kind == LocationNone {
			assert.Equal(t, StateStop, task.State, task.ID)
			continue
		}
		if task.Agent == AgentClaude {
			task.Log = nil
		}
		fullLive = append(fullLive, task)
	}
	require.Len(t, attention, 5)
	assert.Equal(t, attention, fullLive)

	states := map[string]State{}
	for _, task := range attention {
		states[task.ID] = task.State
		if task.State != StateWait {
			assert.Empty(t, task.WaitSignature, "%s: no wait is guessed", task.ID)
		}
	}
	assert.Equal(t, map[string]State{
		"ssh:build-box:claude:5d0b91c2":                            StateWait,
		"ssh:build-box:claude:77aa":                                StateUnknown,
		"ssh:build-box:claude:pid-123":                             StateUnknown,
		"ssh:build-box:codex:01a0e2b9-d054-7cc2-9278-000000000001": StateWait,
		"ssh:build-box:codex:pid-132":                              StateRun,
	}, states)
}

// A session ID names a session only within its agent on its host: the
// same ID under the two agents, or on two hosts, is two tasks.
func TestBuildTasks_TellsTheSameSessionIDApartByHostAndAgent(t *testing.T) {
	const id = "01a0e2b9-d054-7cc2-9278-000000000007"
	raw := fixtureRaw{
		Now:           hostNow,
		Transcripts:   []transcript{{SessionID: id, ModTime: hostNow - 60}},
		CodexRollouts: []codexRollout{{SessionID: id, Originator: codexOriginatorTUI, ModTime: hostNow - 30}},
	}.snapshot()

	ids := map[string]agent.SessionKey{}
	for _, host := range []string{"", "build-box"} {
		for _, task := range buildTasks(host, raw, collectedAt) {
			ids[task.ID] = task.Key()
		}
	}
	assert.Equal(t, map[string]agent.SessionKey{
		"local:claude:" + id:         {Host: "", Agent: agent.Claude, SessionID: id},
		"local:codex:" + id:          {Host: "", Agent: agent.Codex, SessionID: id},
		"ssh:build-box:claude:" + id: {Host: "build-box", Agent: agent.Claude, SessionID: id},
		"ssh:build-box:codex:" + id:  {Host: "build-box", Agent: agent.Codex, SessionID: id},
	}, ids)
}

// A session's log version is its newest transcript's; among transcripts of
// one session with the same time (one ID in two project directories), the
// host's first is kept.
func TestBuildTasks_LogVersionIsTheNewestTranscript(t *testing.T) {
	raw := fixtureRaw{
		Now: hostNow,
		Transcripts: []transcript{
			{SessionID: "sess-a", ModTime: hostNow - 60, Size: 10},
			{SessionID: "sess-a", ModTime: hostNow - 60, Size: 20},
			{SessionID: "sess-a", ModTime: hostNow - 600, Size: 30},
		},
	}.snapshot()
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 1)
	assert.Equal(t, &LogVersion{ModTime: hostNow - 60, Size: 10}, tasks[0].Log)
}

// A snapshot with no part for an adapter is read as that adapter having
// reported nothing: a running claude no state file explains is still listed.
func TestBuildTasks_AnAdapterWithoutAPartReportsNothing(t *testing.T) {
	raw := rawSnapshot{Now: hostNow, Processes: []process{claudeProc(121, 1)}}
	tasks := buildTasks("", raw, collectedAt)
	require.Len(t, tasks, 1)
	assert.Equal(t, "local:claude:pid-121", tasks[0].ID)
	assert.Equal(t, StateUnknown, tasks[0].State)
}
