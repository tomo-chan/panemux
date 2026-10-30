package board

import (
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestParticipationSnapshotTracksDetectionInstructionAndReport(t *testing.T) {
	c := NewBoardCache()
	c.RecordParticipation("shell", "local", "", false)
	c.RecordParticipation("agent", "ssh:build", "codex", false)
	c.RecordParticipation("agent", "ssh:build", "codex", true)
	c.RecordStatus("shell", Status{State: "working"})
	got := c.ParticipationSnapshot()
	require.Equal(t, "codex", got["agent"].AgentType)
	require.Equal(t, "agent", got["agent"].PaneID)
	require.Equal(t, "ssh:build", got["agent"].Host)
	require.True(t, got["agent"].Instructed)
	require.Nil(t, got["agent"].LastReport)
	require.WithinDuration(t, time.Now(), *got["shell"].LastReport, time.Second)
	delete(got, "agent")
	require.Contains(t, c.ParticipationSnapshot(), "agent")
	c.RecordParticipation("agent", "ssh:build", "", false)
	require.Empty(t, c.ParticipationSnapshot()["agent"].AgentType)
	require.False(t, c.ParticipationSnapshot()["agent"].Instructed)
}

func TestParticipationTracksHostChangesAndRemovalWithoutDroppingReports(t *testing.T) {
	c := NewBoardCache()
	c.SyncParticipation(map[string]string{"pane": "host-a", "shell": "local"})
	c.RecordParticipation("pane", "host-a", "codex", true)
	c.RecordStatus("pane", Status{State: "working"})
	c.SyncParticipation(map[string]string{"pane": "host-a"})
	require.True(t, c.ParticipationSnapshot()["pane"].Instructed)
	require.NotContains(t, c.ParticipationSnapshot(), "shell")
	c.SyncParticipation(map[string]string{"pane": "host-b"})
	state := c.ParticipationSnapshot()["pane"]
	require.Equal(t, "host-b", state.Host)
	require.False(t, state.Instructed)
	require.Empty(t, state.AgentType)
	require.NotNil(t, state.LastReport)
	c.SyncParticipation(nil)
	state = c.ParticipationSnapshot()["pane"]
	require.Empty(t, state.Host)
	require.NotNil(t, state.LastReport)
}
