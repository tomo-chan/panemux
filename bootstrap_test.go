package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/agent"
	"panemux/internal/host"
	"panemux/internal/session"
)

// fakeAgentSession supplies pane detection/write behavior and an independently
// injectable host Runner for bootstrap probes.
type fakeAgentSession struct {
	detectErr    error
	writeErr     error
	boardErr     error
	boardOutputs map[string][]byte
	id           string
	detectType   string
	sessionType  session.Type
	writes       [][]byte
	detectOK     bool
	writeShort   bool
}

func (f *fakeAgentSession) ID() string { return f.id }
func (f *fakeAgentSession) Type() session.Type {
	if f.sessionType == "" {
		return session.TypeLocal
	}
	return f.sessionType
}
func (f *fakeAgentSession) Title() string                  { return f.id }
func (f *fakeAgentSession) State() session.State           { return session.StateConnected }
func (f *fakeAgentSession) Read(p []byte) (int, error)     { return 0, nil }
func (f *fakeAgentSession) Resize(cols, rows uint16) error { return nil }
func (f *fakeAgentSession) Close() error                   { return nil }

func (f *fakeAgentSession) Write(p []byte) (int, error) {
	f.writes = append(f.writes, append([]byte(nil), p...))
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.writeShort {
		return len(p) - 1, nil
	}
	return len(p), nil
}

func (f *fakeAgentSession) DetectInteractiveAgent() (agent.ID, bool, error) {
	return agent.ID(f.detectType), f.detectOK, f.detectErr
}

func (f *fakeAgentSession) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	if f.boardErr != nil {
		return nil, f.boardErr
	}
	key := strings.Join(args, "\x00")
	return f.boardOutputs[key], nil
}

// noAgentSession implements session.Session only (no AgentTypeDetector), for
// exercising the "capability not present" no-op path.
type noAgentSession struct{ id string }

func (n *noAgentSession) ID() string                     { return n.id }
func (n *noAgentSession) Type() session.Type             { return session.TypeLocal }
func (n *noAgentSession) Title() string                  { return n.id }
func (n *noAgentSession) State() session.State           { return session.StateConnected }
func (n *noAgentSession) Read(p []byte) (int, error)     { return 0, nil }
func (n *noAgentSession) Write(p []byte) (int, error)    { return len(p), nil }
func (n *noAgentSession) Resize(cols, rows uint16) error { return nil }
func (n *noAgentSession) Close() error                   { return nil }

// localAgmsgDir creates a tempdir with (or without) scripts/api.sh present,
// suitable for use as a resolvedPaths["local"] entry.
func localAgmsgDir(t *testing.T, present bool) string {
	t.Helper()
	dir := t.TempDir()
	if present {
		require.NoError(t, os.MkdirAll(filepath.Join(dir, "scripts"), 0750))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "scripts", "api.sh"), []byte("#!/bin/sh\n"), 0600))
	}
	return dir
}

func localPresenceRunner() host.Runner {
	return hostRunnerFunc(func(_ context.Context, _ string, args ...string) ([]byte, error) {
		result := "no"
		if _, err := os.Stat(args[len(args)-1]); err == nil {
			result = "yes"
		}
		return []byte(result), nil
	})
}

func newLocalWatcher(manager *session.Manager, agmsgPath, team string, paneModes map[string]string) *bootstrapWatcher {
	return newTestBootstrapWatcher(testBootstrapWatcherConfig{
		Manager:       manager,
		PaneHosts:     map[string]string{"pane-a": boardHostIDLocal},
		ResolvedPaths: map[string]string{boardHostIDLocal: agmsgPath},
		RunnerForHost: func(string) host.Runner { return localPresenceRunner() },
		Team:          team,
	})
}

func TestBootstrapWatcher_NoAgentDetected_NoWrite(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectOK: false}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Empty(t, sess.writes)
}

func TestBootstrapWatcher_AgmsgNotPresent_NoWrite_WarnsOnce(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude", detectOK: true}
	manager.Add(sess)

	logs := captureBootstrapLog(t)
	w := newLocalWatcher(manager, localAgmsgDir(t, false), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Empty(t, sess.writes)
	// Counted, not inferred from the suppression flag: the flag being set says
	// nothing about how many lines were written before it was, so an
	// unconditional log.Printf beside it would keep that assertion green while
	// three ticks produced three lines.
	assert.Equal(t, 1, strings.Count(logs.String(), "agmsg not found on host"),
		"three ticks against a host with no agmsg must produce one warning, not three")
	assert.True(t, w.warned["pane-a"].kinds[warnKindAbsent],
		"and the suppression that produced that count is the absent-agmsg kind's")
}

func TestBootstrapWatcher_RemotePresenceCheckTransportError_DistinctFromNo(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{
		id: "pane-a", detectType: "codex", detectOK: true,
		boardErr: errors.New("ssh: connection lost"),
	}
	manager.Add(sess)

	logs := captureBootstrapLog(t)
	w := newTestBootstrapWatcher(testBootstrapWatcherConfig{
		Manager:       manager,
		RunnerForHost: func(string) host.Runner { return sess },
		PaneHosts:     map[string]string{"pane-a": "ssh:build-host"},
		ResolvedPaths: map[string]string{"ssh:build-host": "/opt/agmsg"},
		Team:          "panemux",
	})
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Empty(t, sess.writes)
	// Counted for the same reason as the absent-agmsg case above.
	assert.Equal(t, 1, strings.Count(logs.String(), "checking agmsg presence on host"),
		"a transport error that keeps recurring must be warned about once per streak, not per tick")
	assert.True(t, w.warned["pane-a"].kinds[warnKindProbe],
		"and the suppression that produced that count is the presence-probe kind's")
}

// Invalid identifiers cannot be addressed by the relay.
func TestBootstrapWatcher_InvalidPaneIDOrTeam_SkipsBootstrap(t *testing.T) {
	t.Run("invalid pane ID", func(t *testing.T) {
		manager := session.NewManager()
		sess := &fakeAgentSession{id: "pane a", detectType: "claude", detectOK: true}
		manager.Add(sess)

		w := newTestBootstrapWatcher(testBootstrapWatcherConfig{
			Manager:       manager,
			RunnerForHost: func(string) host.Runner { return sess },
			PaneHosts:     map[string]string{"pane a": boardHostIDLocal},
			ResolvedPaths: map[string]string{boardHostIDLocal: localAgmsgDir(t, true)},
			Team:          "panemux",
		})
		for i := 0; i < 3; i++ {
			w.pollOnce(context.Background())
		}
		assert.Empty(t, sess.writes)
	})

	t.Run("invalid team", func(t *testing.T) {
		manager := session.NewManager()
		sess := &fakeAgentSession{id: "pane-a", detectType: "claude", detectOK: true}
		manager.Add(sess)

		w := newLocalWatcher(manager, localAgmsgDir(t, true), "team; rm -rf", nil)
		for i := 0; i < 3; i++ {
			w.pollOnce(context.Background())
		}
		assert.Empty(t, sess.writes)
	})
}

func TestBootstrapWatcher_HasWork(t *testing.T) {
	manager := session.NewManager()

	empty := newTestBootstrapWatcher(testBootstrapWatcherConfig{Manager: manager, PaneHosts: map[string]string{}})
	assert.False(t, empty.HasWork())

	withPanes := newTestBootstrapWatcher(testBootstrapWatcherConfig{
		Manager:   manager,
		PaneHosts: map[string]string{"pane-a": boardHostIDLocal},
	})
	assert.True(t, withPanes.HasWork(), "must report true even before any agent has actually been detected")
}

func TestBootstrapWatcher_RunLoop_StopsOnContextCancel(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude", detectOK: true}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)

	ctx, cancel := context.WithCancel(context.Background())
	tick := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		w.runLoop(ctx, tick)
		close(done)
	}()

	tick <- time.Now() // another detection tick
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runLoop did not stop after context cancellation")
	}
	assert.Empty(t, sess.writes)
}

//efficacy:exempt preserves the existing no-detector behavior after removing retired bootstrap bookkeeping
func TestBootstrapWatcher_NoAgentTypeDetectorCapability_NoOp(t *testing.T) {
	manager := session.NewManager()
	sess := &noAgentSession{id: "pane-a"}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Empty(t, w.pending)
}
