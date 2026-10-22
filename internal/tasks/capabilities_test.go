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
	for _, tt := range taskActionCases() {
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

type taskActionCase struct {
	want  TaskActions
	name  string
	probe []byte
	task  Task
}

const actionSessionID = "019a1f2e-7c21-7e0a-8b3d-55f0c2b8e41a"

func stoppedTask(agentName, sessionID, cwd string) Task {
	return Task{Agent: agentName, SessionID: sessionID, CWD: cwd, State: StateStop}
}

func sessionInfoMissingAction(detail string) *TaskAction {
	return &TaskAction{Status: Unavailable, Reason: ReasonSessionInfoMissing, Detail: detail}
}

var availableAction = &TaskAction{Status: Available}

func taskActionCases() []taskActionCase {
	return append(resumeActionCases(), []taskActionCase{
		{
			name: "no session ID", probe: probeAnswer("cli claude found", "cli codex found"),
			task: Task{Agent: AgentCodex, State: StateRun},
			want: TaskActions{Summary: sessionInfoMissingAction("the task has no session ID yet")},
		},
		{
			name: "a failed probe leaves resume to the server", probe: []byte("not a probe\n"),
			task: stoppedTask(AgentClaude, actionSessionID, "/workspace/user/project"),
			want: TaskActions{
				Resume:  &TaskAction{Status: AvailabilityUnknown, Reason: ReasonProbeFailed},
				Summary: availableAction,
			},
		},
		{
			name: "an agent that declares neither", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stoppedTask("cursor", actionSessionID, "/workspace/user/project"),
			want: TaskActions{},
		},
		{
			name: "an agent the registry does not know", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stoppedTask("devin", actionSessionID, "/workspace/user/project"),
			want: TaskActions{},
		},
	}...)
}

func resumeActionCases() []taskActionCase {
	return []taskActionCase{
		{
			name: "a stopped claude task resumes and summarizes", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stoppedTask(AgentClaude, actionSessionID, "/workspace/user/project"),
			want: TaskActions{Resume: availableAction, Summary: availableAction},
		},
		{
			name: "a stopped codex task resumes and summarizes", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stoppedTask(AgentCodex, actionSessionID, "/workspace/user/project"),
			want: TaskActions{Resume: availableAction, Summary: availableAction},
		},
		{
			name: "a running task is not resumed", probe: probeAnswer("cli claude found", "cli codex found"),
			task: Task{Agent: AgentClaude, SessionID: actionSessionID, CWD: "/workspace/user/project", State: StateBusy},
			want: TaskActions{Summary: availableAction},
		},
		{
			name: "a missing CLI makes resume unavailable", probe: probeAnswer("cli claude found", "cli codex missing"),
			task: stoppedTask(AgentCodex, actionSessionID, "/workspace/user/project"),
			want: TaskActions{
				Resume: &TaskAction{Status: Unavailable, Reason: ReasonCLIMissing,
					Detail: "codex is not on PATH or in a login shell's PATH"},
				Summary: availableAction,
			},
		},
		{
			name: "a session ID the agent does not resume", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stoppedTask(AgentClaude, "7c21e0a4", "/workspace/user/project"),
			want: TaskActions{
				Resume:  sessionInfoMissingAction("the session ID is not one claude resumes"),
				Summary: availableAction,
			},
		},
		{
			name: "no recorded working directory", probe: probeAnswer("cli claude found", "cli codex found"),
			task: stoppedTask(AgentClaude, actionSessionID, ""),
			want: TaskActions{
				Resume:  sessionInfoMissingAction("the session's working directory is not recorded"),
				Summary: availableAction,
			},
		},
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
		want string
		task Task
	}{
		{name: "a codex task in the shared daemon",
			task: Task{Agent: AgentCodex, State: StateBusy, Location: Location{Kind: LocationDaemon}},
			want: "This session runs in codex's shared daemon"},
		{name: "a codex process without a session", task: Task{Agent: AgentCodex, State: StateRun},
			want: "Codex has no session of its own yet"},
		{name: "a busy codex task", task: Task{Agent: AgentCodex, State: StateBusy},
			want: "a command waiting for approval also shows as working"},
		{name: "a codex task in an unknown state", task: Task{Agent: AgentCodex, State: StateUnknown},
			want: "Neither codex's thread history nor the end of its session log"},
		{name: "a running claude process", task: Task{Agent: AgentClaude, State: StateRun},
			want: "claude reports no detailed state"},
		{name: "a claude status outside the known set", task: Task{Agent: AgentClaude, SessionID: "s", State: StateUnknown},
			want: "Claude Code reported a status the dashboard does not recognize."},
		{name: "an unreadable claude state file", task: Task{Agent: AgentClaude, State: StateUnknown},
			want: "The session state file under ~/.claude/sessions could not be read"},
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
