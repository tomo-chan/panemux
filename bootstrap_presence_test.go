package main

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/agmsg"
	"panemux/internal/board"
	"panemux/internal/host"
	"panemux/internal/hosttools"
	"panemux/internal/session"
)

type countingParticipation struct {
	fakeParticipation
	calls int
}

func (p *countingParticipation) Available(ctx context.Context) (bool, error) {
	p.calls++
	return p.fakeParticipation.Available(ctx)
}

func TestBoardPresenceDebouncesAndStopsAfterSessionConfirmation(t *testing.T) {
	manager := session.NewManager()
	pane := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager.Add(pane)
	participant := &countingParticipation{fakeParticipation: fakeParticipation{present: true}}
	current := map[string]string{"pane-a": "local"}
	cache := board.NewBoardCache()
	watcher := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager: manager, PaneHosts: current, Team: "team", Observe: cache.RecordParticipation,
		CurrentPaneHosts:     func() map[string]string { return current },
		ParticipationForHost: func(string) board.Participation { return participant },
	})
	poll := func() { watcher.pollOnce(context.Background()) }
	poll()
	require.Zero(t, participant.calls, "first detection is display-only")
	require.Equal(t, "codex", cache.ParticipationSnapshot()["pane-a"].AgentType)
	pane.detectOK = false
	poll()
	pane.detectOK = true
	poll()
	require.Zero(t, participant.calls, "an interrupted detection must restart the debounce")
	poll()
	require.Equal(t, 1, participant.calls)
	for range 20 {
		poll()
	}
	require.Equal(t, 1, participant.calls, "confirmed sessions must not repeat host commands")
	pane.detectType = "claude"
	poll()
	require.Equal(t, "claude", cache.ParticipationSnapshot()["pane-a"].AgentType,
		"detection continues after presence confirmation")
	require.Equal(t, 1, participant.calls)
	require.Empty(t, pane.writes)
}

func TestBoardPresenceConfirmationFollowsPaneAndHostLifecycle(t *testing.T) {
	manager := session.NewManager()
	pane := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager.Add(pane)
	participant := &countingParticipation{fakeParticipation: fakeParticipation{present: true}}
	current := map[string]string{"pane-a": "local"}
	watcher := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager: manager, PaneHosts: current, Team: "team",
		CurrentPaneHosts:     func() map[string]string { return current },
		ParticipationForHost: func(string) board.Participation { return participant },
	})
	poll := func() { watcher.pollOnce(context.Background()) }
	poll()
	poll()
	require.Equal(t, 1, participant.calls)
	replacement := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager.Add(replacement)
	poll()
	require.Equal(t, 1, participant.calls)
	poll()
	require.Equal(t, 2, participant.calls, "a replacement session is checked again")
	current = map[string]string{"pane-a": "ssh:build"}
	poll()
	require.Equal(t, 2, participant.calls)
	poll()
	require.Equal(t, 3, participant.calls, "changing the owning host invalidates confirmation")
	current = map[string]string{}
	poll()
	current = map[string]string{"pane-a": "ssh:build"}
	poll()
	require.Equal(t, 3, participant.calls)
	poll()
	require.Equal(t, 4, participant.calls, "a removed pane loses confirmation")
	require.Empty(t, pane.writes)
	require.Empty(t, replacement.writes)
}

func TestBoardPresenceRetriesUntilConfirmed(t *testing.T) {
	for _, failure := range []string{"absent", "probe error", "path error"} {
		t.Run(failure, func(t *testing.T) {
			manager := session.NewManager()
			pane := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
			manager.Add(pane)
			participant := &countingParticipation{}
			if failure == "probe error" {
				participant.availableErr = errors.New("offline")
			}
			if failure == "path error" {
				participant.availableErr = board.ErrParticipationPath
			}
			watcher := newBootstrapWatcher(bootstrapWatcherConfig{
				Manager: manager, PaneHosts: map[string]string{"pane-a": "local"}, Team: "team",
				ParticipationForHost: func(string) board.Participation { return participant },
			})
			for range 3 {
				watcher.pollOnce(context.Background())
			}
			require.Equal(t, 2, participant.calls)
			participant.present, participant.availableErr = true, nil
			watcher.pollOnce(context.Background())
			require.Equal(t, 3, participant.calls)
			for range 20 {
				watcher.pollOnce(context.Background())
			}
			require.Equal(t, 3, participant.calls, "successful recovery ends retries")
			require.Empty(t, pane.writes)
		})
	}
}

func TestBoardPresencePauseIsSilentAndRetryable(t *testing.T) {
	logs := captureBootstrapLog(t)
	gate := &hosttools.CallGate{}
	resume, err := gate.Pause(context.Background(), hosttools.AgmsgTool, host.Local)
	require.NoError(t, err)
	defer resume()
	calls := 0
	client := agmsg.NewClient(agmsg.ClientConfig{
		HostID: "local", Path: "/tmp/sample-agmsg",
		BeginCall: func() (func(), error) { return gate.Begin(hosttools.AgmsgTool, host.Local) },
		Runner: hostRunnerFunc(func(context.Context, string, ...string) ([]byte, error) {
			calls++
			return []byte("yes"), nil
		}),
	})
	manager := session.NewManager()
	pane := &fakeAgentSession{id: "pane-a", detectType: "codex", detectOK: true}
	manager.Add(pane)
	cache := board.NewBoardCache()
	watcher := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager: manager, PaneHosts: map[string]string{"pane-a": "local"}, Team: "team", Observe: cache.RecordParticipation,
		ParticipationForHost: func(string) board.Participation { return client },
	})
	for range 3 {
		watcher.pollOnce(context.Background())
	}
	require.Empty(t, logs.String(), "a managed update is not a probe failure")
	require.Zero(t, calls)
	require.Equal(t, "codex", cache.ParticipationSnapshot()["pane-a"].AgentType)
	resume()
	watcher.pollOnce(context.Background())
	require.Equal(t, 1, calls)
	require.Empty(t, logs.String())
	require.Empty(t, pane.writes)
}
