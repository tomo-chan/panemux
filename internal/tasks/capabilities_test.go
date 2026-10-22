package tasks

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
)

func TestAgents_ListsTheRegistryWithItsCapabilitiesAndNotes(t *testing.T) {
	list := Agents()
	all := agent.All()
	require.Len(t, list, len(all))
	for i, d := range all {
		assert.Equal(t, d.ID, list[i].ID)
		assert.Equal(t, d.DisplayName, list[i].DisplayName)
		assert.Equal(t, d.Capabilities, list[i].Capabilities)
	}

	byID := map[agent.ID]AgentInfo{}
	for _, a := range list {
		byID[a.ID] = a
	}
	assert.Empty(t, byID[agent.Claude].Notes.Launch, "claude is listed under the session ID its launch names")
	assert.Contains(t, byID[agent.Codex].Notes.Launch, "Codex chooses its session ID once it has started its session")
	assert.Equal(t, []agent.Capability{agent.Detect, agent.Board}, byID["cursor"].Capabilities,
		"an agent Agent Board alone detects is listed with what it declares")
	assert.Empty(t, byID["cursor"].Notes.Launch)
}

// TestTaskActions covers which actions a task offers and why one cannot be
// taken: resume only on a stopped task of an agent that resumes, summary on
// a task of an agent that summarizes, neither on an agent the registry does
// not declare them for or does not know.
func TestTaskActions(t *testing.T) {
	const uuid = "019a1f2e-7c21-7e0a-8b3d-55f0c2b8e41a"
	stopped := func(agentName, sessionID, cwd string) Task {
		return Task{Agent: agentName, SessionID: sessionID, CWD: cwd, State: StateStop}
	}
	available := &TaskAction{Status: Available}
	sessionInfoMissing := func(detail string) *TaskAction {
		return &TaskAction{Status: Unavailable, Reason: ReasonSessionInfoMissing, Detail: detail}
	}
	tests := []struct {
		name  string
		probe []byte
		task  Task
		want  TaskActions
	}{
		{
			name: "a stopped claude task resumes and summarizes", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stopped(AgentClaude, uuid, "/workspace/user/project"),
			want: TaskActions{Resume: available, Summary: available},
		},
		{
			name: "a stopped codex task resumes and summarizes", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stopped(AgentCodex, uuid, "/workspace/user/project"),
			want: TaskActions{Resume: available, Summary: available},
		},
		{
			name: "a running task is not resumed", probe: probeAnswer("cli claude found", "cli codex found"),
			task: Task{Agent: AgentClaude, SessionID: uuid, CWD: "/workspace/user/project", State: StateBusy},
			want: TaskActions{Summary: available},
		},
		{
			name: "a missing CLI makes resume unavailable", probe: probeAnswer("cli claude found", "cli codex missing"),
			task: stopped(AgentCodex, uuid, "/workspace/user/project"),
			want: TaskActions{
				Resume:  &TaskAction{Status: Unavailable, Reason: ReasonCLIMissing, Detail: "codex is not on PATH or in a login shell's PATH"},
				Summary: available,
			},
		},
		{
			name: "a session ID the agent does not resume", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stopped(AgentClaude, "7c21e0a4", "/workspace/user/project"),
			want: TaskActions{
				Resume:  sessionInfoMissing("the session ID is not one claude resumes"),
				Summary: available,
			},
		},
		{
			name: "no recorded working directory", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stopped(AgentClaude, uuid, ""),
			want: TaskActions{
				Resume:  sessionInfoMissing("the session's working directory is not recorded"),
				Summary: available,
			},
		},
		{
			name: "no session ID", probe: probeAnswer("cli claude found", "cli codex found"),
			task: Task{Agent: AgentCodex, State: StateRun},
			want: TaskActions{Summary: sessionInfoMissing("the task has no session ID yet")},
		},
		{
			name: "a failed probe leaves resume to the server", probe: []byte("not a probe\n"),
			task: stopped(AgentClaude, uuid, "/workspace/user/project"),
			want: TaskActions{Resume: &TaskAction{Status: AvailabilityUnknown, Reason: ReasonProbeFailed}, Summary: available},
		},
		{
			name: "an agent that declares neither", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stopped("cursor", uuid, "/workspace/user/project"),
			want: TaskActions{},
		},
		{
			name: "an agent the registry does not know", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stopped("devin", uuid, "/workspace/user/project"),
			want: TaskActions{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := &probeHost{collection: minimalOutput("s"), probe: tt.probe}
			svc := New(Options{RunLocal: host.run})
			defer svc.Close()
			collectAndProbe(svc)

			got := svc.TaskActions(tt.task)
			if tt.want.Resume != nil && tt.want.Resume.Reason == ReasonProbeFailed {
				require.NotNil(t, got.Resume)
				assert.Equal(t, AvailabilityUnknown, got.Resume.Status)
				assert.Equal(t, ReasonProbeFailed, got.Resume.Reason)
				assert.NotEmpty(t, got.Resume.Detail)
				assert.Equal(t, tt.want.Summary, got.Summary)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestTaskActions_BeforeTheHostIsProbedResumeIsUnknown(t *testing.T) {
	svc := New(Options{RunLocal: (&probeHost{}).run})
	defer svc.Close()
	got := svc.TaskActions(Task{Agent: AgentClaude, SessionID: "019a1f2e-7c21-7e0a-8b3d-55f0c2b8e41a",
		CWD: "/workspace/user/project", State: StateStop})
	assert.Equal(t, &TaskAction{Status: AvailabilityUnknown, Reason: ReasonNotProbed}, got.Resume)
}

// TestStateNote covers the notes each agent's collection gives on the states
// it reports: what the state means for that agent, which the dashboard shows
// as is. A state the adapter says nothing about, and an agent without one,
// have no note.
func TestStateNote(t *testing.T) {
	tests := []struct {
		name string
		task Task
		want string
	}{
		{"a codex task in the shared daemon", Task{Agent: AgentCodex, State: StateBusy, Location: Location{Kind: LocationDaemon}},
			"This session runs in codex's shared daemon"},
		{"a codex process without a session", Task{Agent: AgentCodex, State: StateRun},
			"Codex has no session of its own yet"},
		{"a busy codex task", Task{Agent: AgentCodex, State: StateBusy},
			"a command waiting for approval also shows as working"},
		{"a codex task in an unknown state", Task{Agent: AgentCodex, State: StateUnknown},
			"Neither codex's thread history nor the end of its session log"},
		{"a running claude process", Task{Agent: AgentClaude, State: StateRun},
			"claude reports no detailed state"},
		{"a claude status outside the known set", Task{Agent: AgentClaude, SessionID: "s", State: StateUnknown},
			"Claude Code reported a status the dashboard does not recognize."},
		{"an unreadable claude state file", Task{Agent: AgentClaude, State: StateUnknown},
			"The session state file under ~/.claude/sessions could not be read"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Contains(t, StateNote(tt.task), tt.want)
		})
	}
	for _, task := range []Task{
		{Agent: AgentClaude, SessionID: "s", State: StateBusy},
		{Agent: AgentClaude, SessionID: "s", State: StateStop},
		{Agent: AgentCodex, SessionID: "s", State: StateWait},
		{Agent: "devin", State: StateRun},
	} {
		assert.Empty(t, StateNote(task), "%+v", task)
	}
}

// A summary is refused for an agent that does not declare one, and for one
// the registry does not know, before anything is read: what TaskActions
// leaves out, the API does not do either.
//
//efficacy:exempt the agents named here were refused by name before the registry decided it; the refusal is unchanged
func TestRequestAgentSummary_RefusesAnAgentThatDoesNotSummarize(t *testing.T) {
	svc := New(Options{RunLocal: (&probeHost{}).run})
	defer svc.Close()
	for _, name := range []string{"cursor", "devin", ""} {
		_, err := svc.RequestAgentSummary("", name, "019a1f2e-7c21-7e0a-8b3d-55f0c2b8e41a")
		assert.ErrorIs(t, err, ErrInvalidSummary, "%q", name)
	}
}
