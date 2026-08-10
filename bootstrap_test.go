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

	"panemux/internal/session"
)

// fakeAgentSession is a session.Session that also implements
// session.AgentTypeDetector and board.BoardExecutor, standing in for a real
// LocalSession/SSHSession in bootstrap tests. Detection results and write
// behavior are set directly on the struct; RunBoardCommand answers from a
// map the same way fakeBoardExecutor (internal/board) does.
type fakeAgentSession struct {
	detectErr    error
	writeErr     error
	boardErr     error
	boardOutputs map[string][]byte
	id           string
	detectType   string
	writes       [][]byte
	detectOK     bool
	writeShort   bool
}

func (f *fakeAgentSession) ID() string                     { return f.id }
func (f *fakeAgentSession) Type() session.Type             { return session.TypeLocal }
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

func (f *fakeAgentSession) DetectInteractiveAgentType() (string, bool, error) {
	return f.detectType, f.detectOK, f.detectErr
}

func (f *fakeAgentSession) RunBoardCommand(_ context.Context, args []string) ([]byte, error) {
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

func newLocalWatcher(manager *session.Manager, agmsgPath, team string, paneModes map[string]string) *bootstrapWatcher {
	return newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:       manager,
		PaneHosts:     map[string]string{"pane-a": boardHostIDLocal},
		PaneModes:     paneModes,
		ResolvedPaths: map[string]string{boardHostIDLocal: agmsgPath},
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

func TestBootstrapWatcher_DebouncesAcrossTwoTicks(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)

	w.pollOnce(context.Background())
	assert.Empty(t, sess.writes, "must not write on the first tick that detects the agent")

	w.pollOnce(context.Background())
	require.Len(t, sess.writes, 1, "must write on the second consecutive tick for the same session")
	assert.True(t, strings.HasSuffix(string(sess.writes[0]), "\r"))
}

func TestBootstrapWatcher_AlreadyBootstrapped_NoRewrite(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	require.Len(t, sess.writes, 1)

	// Further ticks, with the agent still detected on the same session,
	// must not write again.
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	assert.Len(t, sess.writes, 1)
}

func TestBootstrapWatcher_AgmsgNotPresent_NoWrite_WarnsOnce(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, false), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Empty(t, sess.writes)
	assert.True(t, w.presenceWarned["pane-a"])
}

func TestBootstrapWatcher_RemotePresenceCheck_YesWritesNoDoesNot(t *testing.T) {
	agmsgPath := "/home/remote-user/agmsg"
	probeArgs := []string{
		"sh", "-c", "test -f \"$1\" && printf 'yes' || printf 'no'", "board-agmsg-presence",
		agmsgPath + "/scripts/api.sh",
	}
	probeKey := strings.Join(probeArgs, "\x00")

	t.Run("present", func(t *testing.T) {
		manager := session.NewManager()
		sess := &fakeAgentSession{
			id: "pane-a", detectType: "codex", detectOK: true,
			boardOutputs: map[string][]byte{probeKey: []byte("yes")},
		}
		manager.Add(sess)

		w := newBootstrapWatcher(bootstrapWatcherConfig{
			Manager:       manager,
			PaneHosts:     map[string]string{"pane-a": "ssh:build-host"},
			ResolvedPaths: map[string]string{"ssh:build-host": agmsgPath},
			Team:          "panemux",
		})
		w.pollOnce(context.Background())
		w.pollOnce(context.Background())
		require.Len(t, sess.writes, 1)
	})

	t.Run("not present", func(t *testing.T) {
		manager := session.NewManager()
		sess := &fakeAgentSession{
			id: "pane-a", detectType: "codex", detectOK: true,
			boardOutputs: map[string][]byte{probeKey: []byte("no")},
		}
		manager.Add(sess)

		w := newBootstrapWatcher(bootstrapWatcherConfig{
			Manager:       manager,
			PaneHosts:     map[string]string{"pane-a": "ssh:build-host"},
			ResolvedPaths: map[string]string{"ssh:build-host": agmsgPath},
			Team:          "panemux",
		})
		w.pollOnce(context.Background())
		w.pollOnce(context.Background())
		assert.Empty(t, sess.writes)
	})
}

func TestBootstrapWatcher_RemotePresenceCheckTransportError_DistinctFromNo(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{
		id: "pane-a", detectType: "codex", detectOK: true,
		boardErr: errors.New("ssh: connection lost"),
	}
	manager.Add(sess)

	w := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:       manager,
		PaneHosts:     map[string]string{"pane-a": "ssh:build-host"},
		ResolvedPaths: map[string]string{"ssh:build-host": "/opt/agmsg"},
		Team:          "panemux",
	})
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Empty(t, sess.writes)
	assert.True(t, w.presenceWarned["pane-a"])
}

func TestBootstrapWatcher_ShortWrite_NotMarkedBootstrapped(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true, writeShort: true}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	require.NotEmpty(t, sess.writes, "a short write attempt still counts as an attempt")
	_, bootstrapped := w.bootstrapped["pane-a"]
	assert.False(t, bootstrapped, "a short write must not be treated as a successful bootstrap")
}

func TestBootstrapWatcher_WriteError_NotMarkedBootstrapped(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{
		id: "pane-a", detectType: "claude-code", detectOK: true,
		writeErr: errors.New("pty closed"),
	}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	_, bootstrapped := w.bootstrapped["pane-a"]
	assert.False(t, bootstrapped)
}

func TestBootstrapWatcher_NoAgentTypeDetectorCapability_NoOp(t *testing.T) {
	manager := session.NewManager()
	sess := &noAgentSession{id: "pane-a"}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	assert.NotPanics(t, func() {
		w.pollOnce(context.Background())
		w.pollOnce(context.Background())
	})
}

// TestBootstrapWatcher_PersistedSeed_PreventsReBootstrapOfLiveSession is the
// central regression test for the seeding design: a pane ID persisted from a
// previous panemux run, whose session is still alive and still shows an
// active known agent, must not be re-bootstrapped just because panemux
// itself restarted. A *different* Session object for the same pane ID
// (simulating that pane being restarted within the current process) must be
// bootstrapped again.
func TestBootstrapWatcher_PersistedSeed_PreventsReBootstrapOfLiveSession(t *testing.T) {
	manager := session.NewManager()
	original := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true}
	manager.Add(original)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.LoadPersistedState([]string{"pane-a"})

	// Even though the agent is detected on every tick, seeding must prevent
	// any write from ever happening for this still-live session.
	for i := 0; i < 5; i++ {
		w.pollOnce(context.Background())
	}
	assert.Empty(t, original.writes)

	// Now simulate the pane being restarted within this same process: a new
	// Session object takes its place under the same pane ID.
	require.NoError(t, manager.Remove("pane-a"))
	replacement := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true}
	manager.Add(replacement)

	w.pollOnce(context.Background())
	assert.Empty(t, replacement.writes, "first tick after restart must still debounce")
	w.pollOnce(context.Background())
	assert.Len(t, replacement.writes, 1, "the replacement session must be bootstrapped like any new one")
}

func TestBootstrapWatcher_PersistSuccessfulBootstrap(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true}
	manager.Add(sess)

	var persisted [][]string
	w := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:       manager,
		PaneHosts:     map[string]string{"pane-a": boardHostIDLocal},
		ResolvedPaths: map[string]string{boardHostIDLocal: localAgmsgDir(t, true)},
		Team:          "panemux",
		Persist:       func(paneIDs []string) { persisted = append(persisted, paneIDs) },
	})

	w.pollOnce(context.Background())
	assert.Empty(t, persisted, "no persist call before the actual write happens")
	w.pollOnce(context.Background())
	require.Len(t, persisted, 1)
	assert.Equal(t, []string{"pane-a"}, persisted[0])
}

func TestBootstrapWatcher_HasWork(t *testing.T) {
	manager := session.NewManager()

	empty := newBootstrapWatcher(bootstrapWatcherConfig{Manager: manager, PaneHosts: map[string]string{}})
	assert.False(t, empty.HasWork())

	withPanes := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:   manager,
		PaneHosts: map[string]string{"pane-a": boardHostIDLocal},
	})
	assert.True(t, withPanes.HasWork(), "must report true even before any agent has actually been detected")
}

func TestBootstrapWatcher_RunLoop_StopsOnContextCancel(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)

	ctx, cancel := context.WithCancel(context.Background())
	tick := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		w.runLoop(ctx, tick)
		close(done)
	}()

	tick <- time.Now() // second tick for the same session -> triggers the write
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runLoop did not stop after context cancellation")
	}
	assert.Len(t, sess.writes, 1)
}

func TestBuildBootstrapInstruction_ModeMonitorOrEmpty_NoDeliveryStep(t *testing.T) {
	for _, mode := range []string{"", "monitor", "off"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", mode)
		assert.NotContains(t, text, "delivery.sh", "mode %q must not include a delivery.sh step", mode)
	}
}

func TestBuildBootstrapInstruction_ModeTurnOrBoth_IncludesDeliveryStep(t *testing.T) {
	for _, mode := range []string{"turn", "both"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "gemini", mode)
		want := `/opt/agmsg/scripts/delivery.sh set "` + mode + `" "gemini" "$(pwd)"`
		assert.Contains(t, text, want, "mode %q must include its own delivery.sh invocation", mode)
	}
}

func TestBuildBootstrapInstruction_ContainsVerifiedScriptInvocations(t *testing.T) {
	text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "codex", "both")

	assert.Contains(t, text, `/opt/agmsg/scripts/join.sh "panemux" "pane-a" "codex" "$(pwd)" --force`)
	assert.Contains(t, text, `/opt/agmsg/scripts/send.sh "panemux" "pane-a" "<to>" "<body>" --force`)
	assert.Contains(t, text, `/opt/agmsg/scripts/delivery.sh set "both" "codex" "$(pwd)"`)
	assert.Contains(t, text, "AGMSG-DIRECTIVE")
	// The intro sentence explains why slash-command shorthand is avoided, so
	// it legitimately names both prefixes once; no *line* other than that
	// intro sentence may actually invoke either shorthand form.
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "slash-command") {
			continue
		}
		assert.False(t, strings.HasPrefix(trimmed, "/agmsg "),
			"line invokes claude-code-only slash-command shorthand: %q", trimmed)
		assert.False(t, strings.HasPrefix(trimmed, "$agmsg "),
			"line invokes codex/gemini-style slash-command shorthand: %q", trimmed)
	}
}
