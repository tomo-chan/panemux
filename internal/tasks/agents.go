package tasks

import "panemux/internal/agent"

// The task board's agents and their process rules come from the agent
// registry.
var (
	claudeTask = taskRule(agent.Claude)
	codexTask  = taskRule(agent.Codex)
)

func taskRule(id agent.ID) agent.TaskRule {
	d, ok := agent.Lookup(id)
	//coverage:exempt runs only if registry.go drops claude or codex, which TestRegisteredAgents forbids
	if !ok || d.Detection.Task == nil {
		panic("tasks: agent " + string(id) + " has no task rule")
	}
	return *d.Detection.Task
}

// isClaudeProcess is any Claude Code process, interactive or not.
func isClaudeProcess(command string) bool { return claudeTask.Process(command) }

// isInteractiveClaude is a Claude Code process that is a session.
func isInteractiveClaude(command string) bool { return claudeTask.Interactive(command) }

// isCodexProgram is any codex process, interactive or not.
func isCodexProgram(command string) bool { return codexTask.Process(command) }

// isInteractiveCodex is a codex process that is a session.
func isInteractiveCodex(command string) bool { return codexTask.Interactive(command) }
