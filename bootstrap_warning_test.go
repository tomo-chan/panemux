package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"panemux/internal/agmsg"
	"panemux/internal/board"
	"panemux/internal/session"
)

func TestBootstrapPreservesSeparatePathAndPresenceWarnings(t *testing.T) {
	logs := captureBootstrapLog(t)
	homeFails, presenceFails := true, true
	runner := hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "$HOME") {
			if homeFails {
				return nil, errors.New("home offline")
			}
			return []byte("/remote/home/demo"), nil
		}
		if presenceFails {
			return nil, errors.New("presence offline")
		}
		return []byte("yes"), nil
	})
	client := agmsg.NewClient(agmsg.ClientConfig{HostID: "ssh:dev", Path: "~/agmsg", Runner: runner})
	watcher := newBootstrapWatcher(bootstrapWatcherConfig{
		ParticipationForHost: func(string) board.Participation { return client },
	})
	pathWarning := `agent board bootstrap: no resolved agmsg installation path for host "ssh:dev" (pane "pane-a")`
	probeWarning := `agent board bootstrap: checking agmsg presence on host "ssh:dev" (pane "pane-a"): ` +
		`agmsg: checking remote scripts/api.sh presence: presence offline`
	probe := func() {
		_, checked := watcher.messagingPresent(context.Background(), "pane-a", "ssh:dev")
		assert.False(t, checked)
	}
	probe()
	probe()
	assert.Equal(t, 1, strings.Count(logs.String(), pathWarning))
	homeFails = false
	probe()
	probe()
	assert.Equal(t, 1, strings.Count(logs.String(), probeWarning), "a distinct failing condition must be logged")
	presenceFails = false
	present, checked := watcher.messagingPresent(context.Background(), "pane-a", "ssh:dev")
	assert.True(t, present)
	assert.True(t, checked)
	presenceFails = true
	probe()
	assert.Equal(t, 2, strings.Count(logs.String(), probeWarning), "recovery ends the presence warning streak")
}

func TestBootstrapPreservesIdentifierWarning(t *testing.T) {
	for _, ids := range [][2]string{{"panemux", "bad pane"}, {"bad team", "pane-a"}} {
		t.Run(ids[0]+ids[1], func(t *testing.T) {
			logs := captureBootstrapLog(t)
			manager := session.NewManager()
			pane := &fakeAgentSession{id: ids[1], detectOK: true, detectType: "codex"}
			manager.Add(pane)
			client := agmsg.NewClient(agmsg.ClientConfig{Path: "/opt/agmsg"})
			watcher := newBootstrapWatcher(bootstrapWatcherConfig{
				Manager: manager, PaneHosts: map[string]string{ids[1]: "host-a"}, Team: ids[0],
				ParticipationForHost: func(string) board.Participation { return client },
			})
			watcher.pollOnce(context.Background())
			watcher.pollOnce(context.Background())
			want := `pane "` + ids[1] + `" or team "` + ids[0] + `" is not a valid agmsg identifier, skipping bootstrap`
			require.Equal(t, 1, strings.Count(logs.String(), want))
			assert.Empty(t, pane.writes)
		})
	}
}
