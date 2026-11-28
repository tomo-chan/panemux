package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/board"
	"panemux/internal/session"
)

func TestBoardDetectionNeverWritesToPTY(t *testing.T) {
	for _, failure := range []string{"ready", "absent", "offline", "path", "invalid"} {
		t.Run(failure, func(t *testing.T) {
			manager := session.NewManager()
			pane := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
			manager.Add(pane)
			participant := &fakeParticipation{present: true}
			switch failure {
			case "absent":
				participant.present = false
			case "offline":
				participant.availableErr = errors.New("offline")
			case "path":
				participant.availableErr = board.ErrParticipationPath
			case "invalid":
				participant.addressErr = errors.New("invalid")
			}
			cache := board.NewBoardCache()
			watcher := newBootstrapWatcher(bootstrapWatcherConfig{
				Manager: manager, PaneHosts: map[string]string{"pane-a": "local"}, Team: "team",
				Observe:              cache.RecordParticipation,
				ParticipationForHost: func(string) board.Participation { return participant },
			})
			for range 5 {
				watcher.pollOnce(context.Background())
			}
			require.Empty(t, pane.writes, "Board must never write to Session.Write")
			require.False(t, cache.ParticipationSnapshot()["pane-a"].Instructed)
			// Recovery and a restarted session must also remain read-only.
			participant.present, participant.availableErr, participant.addressErr = true, nil, nil
			replacement := &fakeAgentSession{id: "pane-a", detectType: "claude", detectOK: true}
			manager.Add(replacement)
			for range 5 {
				watcher.pollOnce(context.Background())
			}
			require.Empty(t, replacement.writes)
			require.Equal(t, "claude", cache.ParticipationSnapshot()["pane-a"].AgentType)
			require.False(t, cache.ParticipationSnapshot()["pane-a"].Instructed)
		})
	}
}
