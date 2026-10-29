package board

import "time"

// PaneParticipation distinguishes detection, a sent instruction, and a self-report.
// Instructed is not a claim that the agent executed the instruction.
type PaneParticipation struct {
	LastReport *time.Time `json:"last_report,omitempty"`
	PaneID     string     `json:"pane_id"`
	Host       string     `json:"host"`
	AgentType  string     `json:"agent_type"`
	Instructed bool       `json:"instructed"`
}

// RecordParticipation publishes a bootstrap snapshot for concurrent API readers.
func (c *BoardCache) RecordParticipation(paneID, host, agentType string, instructed bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.participation[paneID] = PaneParticipation{PaneID: paneID, Host: host, AgentType: agentType, Instructed: instructed}
}

func (c *BoardCache) ParticipationSnapshot() map[string]PaneParticipation {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]PaneParticipation, len(c.participation))
	for id, state := range c.participation {
		result[id] = state
	}
	for id, status := range c.status {
		state := result[id]
		state.PaneID = id
		at := status.UpdatedAt
		state.LastReport = &at
		result[id] = state
	}
	return result
}
