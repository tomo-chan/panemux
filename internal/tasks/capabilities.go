package tasks

import (
	"panemux/internal/agent"
)

// AgentInfo is one registered agent as the API describes it (issue #376):
// what it declares and the notes its adapters give on its own constraints.
type AgentInfo struct {
	ID           agent.ID           `json:"id"`
	DisplayName  string             `json:"display_name"`
	Notes        AgentNotes         `json:"notes"`
	Capabilities []agent.Capability `json:"capabilities"`
}

// AgentNotes are fixed texts an agent's adapters give, shown as they are.
// They name nothing about a host or a request.
type AgentNotes struct {
	// Launch is what to know before starting a new session of the agent.
	Launch string `json:"launch,omitempty"`
}

// Agents is every registered agent, in registry order.
func Agents() []AgentInfo {
	all := agent.All()
	list := make([]AgentInfo, 0, len(all))
	for _, d := range all {
		info := AgentInfo{ID: d.ID, DisplayName: d.DisplayName, Capabilities: d.Capabilities}
		if a, ok := launchPlan.launch[d.ID]; ok {
			info.Notes.Launch = a.note()
		}
		list = append(list, info)
	}
	return list
}

// TaskAction is whether one action can be taken on a task, and why not.
type TaskAction struct {
	Status AvailabilityStatus `json:"status"`
	Reason AvailabilityReason `json:"reason,omitempty"`
	Detail string             `json:"detail,omitempty"`
}

// TaskActions are the actions a task offers. An action is absent when the
// task's agent does not declare it, or when it does not apply to the task:
// resume to a task that is not stopped.
type TaskActions struct {
	Resume  *TaskAction `json:"resume,omitempty"`
	Summary *TaskAction `json:"summary,omitempty"`
}

// TaskActions decides the actions task offers from its agent's adapters and
// what the probe of its host found. Launch and Resume refuse the same way
// whatever this says.
func (s *Service) TaskActions(task Task) TaskActions {
	var actions TaskActions
	id := agent.ID(task.Agent)
	if adapter, ok := launchPlan.resume[id]; ok && task.State == StateStop {
		actions.Resume = s.resumeAction(task, adapter)
	}
	if summarizable(task.Agent) {
		action := &TaskAction{Status: Available}
		if !validSummarySession(task.Agent, task.SessionID) {
			action = sessionInfoMissing("the task has no session ID yet")
		}
		actions.Summary = action
	}
	return actions
}

func (s *Service) resumeAction(task Task, adapter resumeAdapter) *TaskAction {
	id := agent.ID(task.Agent)
	switch {
	case !adapter.validSessionID(task.SessionID):
		return sessionInfoMissing("the session ID is not one " + task.Agent + " resumes")
	case task.CWD == "":
		return sessionInfoMissing("the session's working directory is not recorded")
	}
	conn := s.currentConn(task.Host)
	s.mu.Lock()
	entry, probed := s.probes[task.Host]
	s.mu.Unlock()
	a := cliAvailability(id, agent.Resume, entry, probed && entry.conn == conn)
	return &TaskAction{Status: a.Status, Reason: a.Reason, Detail: a.Detail}
}

func sessionInfoMissing(detail string) *TaskAction {
	return &TaskAction{Status: Unavailable, Reason: ReasonSessionInfoMissing, Detail: detail}
}

// validSummarySession reports whether sessionID names a session of
// agentName a summary can be made of.
func validSummarySession(agentName, sessionID string) bool {
	return validSessionID.MatchString(sessionID) && (agentName != AgentCodex || validUUID.MatchString(sessionID))
}

// StateNote is what the task's agent says its state means, "" when it says
// nothing about it.
func StateNote(task Task) string {
	for _, r := range collectPlan.full.adapters {
		if string(r.id) == task.Agent {
			return r.adapter.stateNote(task)
		}
	}
	return ""
}
