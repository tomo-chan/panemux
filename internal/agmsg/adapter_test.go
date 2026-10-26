package agmsg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/agent"
	"panemux/internal/board"
)

func TestAdapterImplementsBoardPorts(t *testing.T) {
	var messages board.MessageClient = NewClient(ClientConfig{HostID: "host-a"})
	var participation board.Participation = NewClient(ClientConfig{HostID: "host-a", Path: "/opt/agmsg"})
	assert.Equal(t, "host-a", messages.HostID())
	text, err := participation.JoinInstruction(context.Background(), board.JoinRequest{
		Agent: agent.Claude, Team: "panemux", PaneID: "pane-a", Mode: "both",
	})
	require.NoError(t, err)
	assert.Contains(t, text, `/opt/agmsg/scripts/join.sh "panemux" "pane-a" "claude-code" "$(pwd)" --force`)
	assert.Contains(t, text, `/opt/agmsg/scripts/delivery.sh set "both" "claude-code" "$(pwd)"`)
}

func TestAdapterRejectsInvalidParticipationBeforeExecution(t *testing.T) {
	for _, request := range []board.JoinRequest{
		{Agent: agent.Claude, Team: "bad;team", PaneID: "pane-a"},
		{Agent: agent.Claude, Team: "panemux", PaneID: "bad pane"},
		{Agent: "unknown", Team: "panemux", PaneID: "pane-a"},
	} {
		t.Run(request.Team+request.PaneID+string(request.Agent), func(t *testing.T) {
			client := NewClient(ClientConfig{Path: "~/agmsg"})
			_, err := client.JoinInstruction(context.Background(), request)
			require.Error(t, err)
		})
	}
}

func TestAdapterPathConfiguration(t *testing.T) {
	assert.Equal(t, "~/.agents/skills/agmsg", DefaultPath)
	for _, path := range []string{"", "/opt/agmsg", "~/tools/agmsg"} {
		assert.NoError(t, ValidatePath(path))
	}
	for _, path := range []string{"relative", "~other/tools"} {
		assert.Error(t, ValidatePath(path))
	}
}

func TestAdapterMapsRegisteredIDsToTransportTypes(t *testing.T) {
	for id, want := range map[agent.ID]string{
		agent.Claude: "claude-code", agent.Codex: "codex", agent.Cursor: "cursor",
		agent.Gemini: "gemini", agent.GrokBuild: "grok-build", agent.OpenCode: "opencode",
	} {
		t.Run(string(id), func(t *testing.T) {
			client := NewClient(ClientConfig{Path: "/opt/agmsg"})
			text, err := client.JoinInstruction(context.Background(), board.JoinRequest{
				Agent: id, Team: "panemux", PaneID: "pane-a",
			})
			require.NoError(t, err)
			assert.Contains(t, text, `"panemux" "pane-a" "`+want+`" "$(pwd)" --force`)
		})
	}
}

type participationRunner struct {
	homes    int
	probes   int
	failHome bool
	present  bool
}

func (r *participationRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if strings.Contains(strings.Join(args, " "), "$HOME") {
		r.homes++
		if r.failHome {
			return nil, errors.New("offline")
		}
		return []byte("/remote/home/demo"), nil
	}
	r.probes++
	if r.present {
		return []byte("yes"), nil
	}
	return []byte("no"), nil
}
func TestParticipationRetriesDiscoveryAndPresenceIndependentlyOfRelay(t *testing.T) {
	ctx := context.Background()
	runner := &participationRunner{failHome: true}
	client := NewClient(ClientConfig{HostID: "host-a", Path: "~/agmsg", Runner: runner})
	_, err := client.Available(ctx)
	require.ErrorContains(t, err, "offline")
	_, err = client.JoinInstruction(ctx, board.JoinRequest{Agent: agent.Codex, Team: "panemux", PaneID: "pane-a"})
	require.ErrorContains(t, err, "offline")
	runner.failHome = false
	present, err := client.Available(ctx)
	require.NoError(t, err)
	assert.False(t, present)
	// Relay remembers absence, but participation still observes a later install.
	_, err = client.Since(ctx, "panemux", "", 10)
	require.NoError(t, err)
	runner.present = true
	present, err = client.Available(ctx)
	require.NoError(t, err)
	assert.True(t, present)
	before := runner.homes
	text, err := client.JoinInstruction(ctx, board.JoinRequest{Agent: agent.Codex, Team: "panemux", PaneID: "pane-a"})
	require.NoError(t, err)
	assert.Contains(t, text, "/remote/home/demo/agmsg/scripts/join.sh")
	assert.Equal(t, before, runner.homes, "successful path resolution is reused")
	require.ErrorContains(t, client.Send(ctx, "panemux", "pane-a", "pane-b", "hello"), "not installed")
}
