package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"panemux/internal/agent"
	"panemux/internal/board"
	"panemux/internal/session"
)

type fakeParticipation struct {
	addressErr   error
	availableErr error
	present      bool
}

func (p *fakeParticipation) Description() board.ParticipationDescription {
	return board.ParticipationDescription{Name: "transport", PathSetting: "transport_path"}
}
func (p *fakeParticipation) ValidateAddress(string, string) error { return p.addressErr }
func (p *fakeParticipation) Available(context.Context) (bool, error) {
	return p.present, p.availableErr
}

func TestBootstrapWithoutAParticipationLookupDoesNotWrite(t *testing.T) {
	manager := session.NewManager()
	pane := &fakeAgentSession{id: "pane-a", detectType: string(agent.Claude), detectOK: true}
	manager.Add(pane)
	watcher := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager: manager, PaneHosts: map[string]string{"pane-a": "host-a"}, Team: "team-a",
	})
	watcher.pollOnce(context.Background())
	watcher.pollOnce(context.Background())
	assert.Empty(t, pane.writes)
	present, checked := watcher.messagingPresent(context.Background(), "pane-a", "host-a")
	assert.False(t, present)
	assert.False(t, checked)
}
