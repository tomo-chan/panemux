package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/agent"
	"panemux/internal/board"
	"panemux/internal/session"
)

type fakeParticipation struct {
	addressErr     error
	availableErr   error
	instructionErr error
	requests       []board.JoinRequest
	present        bool
}

type pausingParticipation struct {
	*fakeParticipation
	leases int
	paused bool
}

func (p *pausingParticipation) LeaseParticipation() (board.Participation, func(), error) {
	if p.paused {
		return nil, nil, errors.New("tool is updating")
	}
	p.leases++
	return p.fakeParticipation, func() { p.leases-- }, nil
}

func TestBootstrapWaitsForToolPauseWithoutWritingOrJoining(t *testing.T) {
	manager := session.NewManager()
	pane := &fakeAgentSession{id: "pane-a", detectType: string(agent.Claude), detectOK: true}
	manager.Add(pane)
	participant := &pausingParticipation{fakeParticipation: &fakeParticipation{present: true}, paused: true}
	watcher := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager: manager, PaneHosts: map[string]string{"pane-a": "host-a"}, Team: "team-a",
		ParticipationForHost: func(string) board.Participation { return participant },
	})
	watcher.pollOnce(context.Background())
	watcher.pollOnce(context.Background())
	require.Empty(t, pane.writes)
	require.Empty(t, participant.requests)
	participant.paused = false
	watcher.pollOnce(context.Background())
	watcher.pollOnce(context.Background())
	require.Len(t, pane.writes, 1)
	require.Len(t, participant.requests, 1)
	require.Zero(t, participant.leases)
}

func (p *fakeParticipation) Description() board.ParticipationDescription {
	return board.ParticipationDescription{Name: "transport", PathSetting: "transport_path"}
}
func (p *fakeParticipation) ValidateAddress(string, string) error { return p.addressErr }
func (p *fakeParticipation) Available(context.Context) (bool, error) {
	return p.present, p.availableErr
}
func (p *fakeParticipation) JoinInstruction(_ context.Context, request board.JoinRequest) (string, error) {
	p.requests = append(p.requests, request)
	return "transport-owned participation text", p.instructionErr
}
func TestBootstrapUsesOnlyTheParticipationPort(t *testing.T) {
	tests := []struct {
		participant     *fakeParticipation
		name            string
		wantInstruction bool
		wantWrite       bool
	}{
		{name: "ready", participant: &fakeParticipation{present: true}, wantInstruction: true, wantWrite: true},
		{name: "invalid address", participant: &fakeParticipation{addressErr: errors.New("invalid address")}},
		{name: "absent", participant: &fakeParticipation{}},
		{name: "probe error", participant: &fakeParticipation{availableErr: errors.New("offline")}},
		{name: "instruction error", participant: &fakeParticipation{
			present: true, instructionErr: errors.New("instruction unavailable"),
		}, wantInstruction: true},
		{name: "no transport"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manager := session.NewManager()
			pane := &fakeAgentSession{id: "pane-a", detectType: string(agent.Claude), detectOK: true}
			manager.Add(pane)
			watcher := newBootstrapWatcher(bootstrapWatcherConfig{
				Manager: manager, PaneHosts: map[string]string{"pane-a": "host-a"}, Team: "team-a",
				PaneModes: func() map[string]string { return map[string]string{"pane-a": "both"} },
				ParticipationForHost: func(host string) board.Participation {
					require.Equal(t, "host-a", host)
					if tt.participant == nil {
						return nil
					}
					return tt.participant
				},
			})
			watcher.pollOnce(context.Background())
			watcher.pollOnce(context.Background())
			if tt.wantInstruction {
				require.Equal(t, []board.JoinRequest{
					{Agent: agent.Claude, Team: "team-a", PaneID: "pane-a", Mode: "both"},
				}, tt.participant.requests)
			} else if tt.participant != nil {
				assert.Empty(t, tt.participant.requests)
			}
			if tt.wantWrite {
				require.Len(t, pane.writes, 1)
				assert.Contains(t, string(pane.writes[0]), "transport-owned participation text")
			} else {
				assert.Empty(t, pane.writes)
			}
		})
	}
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
