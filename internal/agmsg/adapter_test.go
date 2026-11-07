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
	"panemux/internal/hosttools"
)

func TestAdapterMapsRegisteredIDsToTransportTypes(t *testing.T) {
	for id, want := range map[agent.ID]string{
		agent.Claude: "claude-code", agent.Codex: "codex", agent.Cursor: "cursor",
		agent.Gemini: "gemini", agent.GrokBuild: "grok-build", agent.OpenCode: "opencode",
	} {
		t.Run(string(id), func(t *testing.T) {
			assert.Equal(t, want, boardAdaptersByID[id])
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
	_, err = client.Available(ctx)
	require.NoError(t, err)
	assert.Equal(t, before, runner.homes, "successful path resolution is reused")
	require.ErrorContains(t, client.Send(ctx, "panemux", "pane-a", "pane-b", "hello"), "not installed")
}

func TestAvailableClassifiesOnlyPathFailures(t *testing.T) {
	sentinel := errors.New("offline")
	for path, wantPathError := range map[string]bool{"~/agmsg": true, "/opt/agmsg": false} {
		t.Run(path, func(t *testing.T) {
			client := NewClient(ClientConfig{Path: path, Runner: &fakeBoardExecutor{err: sentinel}})
			present, err := client.Available(context.Background())
			assert.False(t, present)
			require.ErrorIs(t, err, sentinel)
			assert.Equal(t, wantPathError, errors.Is(err, board.ErrParticipationPath))
			want := board.ParticipationDescription{Name: "agmsg", PathSetting: "agmsg installation path"}
			assert.Equal(t, want, client.Description())
		})
	}
}

func TestAvailabilityClassifiesOnlyManagedPauses(t *testing.T) {
	for _, cause := range []error{hosttools.ErrUpdating, errors.New("admission failed")} {
		t.Run(cause.Error(), func(t *testing.T) {
			client := NewClient(ClientConfig{BeginCall: func() (func(), error) { return nil, cause }})
			present, err := client.Available(context.Background())
			require.False(t, present)
			require.ErrorIs(t, err, cause)
			require.Equal(t, errors.Is(cause, hosttools.ErrUpdating), errors.Is(err, board.ErrParticipationPaused))
		})
	}
}
