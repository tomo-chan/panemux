package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/session"
)

// The bootstrap watcher runs on a timer against panes that come and go while
// it polls. Every arm covered here is one where a pane it was told about is
// not there, or cannot answer — the ordinary consequence of a pane being
// deleted or restarted between two ticks. None of them may stop the watcher
// or leave it retrying the same pane forever.

// captureBootstrapLog redirects the standard logger and returns what was
// written. It restores the previous writer rather than nil, since a nil
// writer makes log.Printf panic rather than discard.
func captureBootstrapLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prevWriter, prevFlags := log.Writer(), log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevWriter)
		log.SetFlags(prevFlags)
	})
	return &buf
}

// A persisted pane id names a pane from a previous run of panemux. By the
// time the first tick arrives that pane may simply not exist — the operator
// deleted it while panemux was stopped. Seeding must skip it and go on to the
// rest, not stop at the first missing one.
func TestPollOnceSkipsAPersistedPaneThatNoLongerExists(t *testing.T) {
	manager := session.NewManager()
	survivor := &fakeAgentSession{id: "pane-b", sessionType: session.TypeTmux}
	manager.Add(survivor)
	t.Cleanup(manager.CloseAll)

	w := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:       manager,
		PaneHosts:     map[string]string{"pane-b": boardHostIDLocal},
		PaneModes:     func() map[string]string { return nil },
		ResolvedPaths: map[string]string{boardHostIDLocal: localAgmsgDir(t, true)},
		Team:          "panemux",
	})
	w.LoadPersistedState([]string{"pane-gone", "pane-b"})

	w.pollOnce(context.Background())

	assert.True(t, w.seeded)
	assert.NotContains(t, w.bootstrapped, "pane-gone", "a pane that is not there cannot be seeded")
	assert.Contains(t, w.bootstrapped, "pane-b",
		"the missing pane must not stop the ones after it from being seeded")
}

// A pane that vanishes while it is pending has to be forgotten. Leaving it in
// the map means every later tick looks it up again, forever, for a pane that
// is never coming back.
func TestCheckPaneForgetsAPaneThatDisappeared(t *testing.T) {
	manager := session.NewManager()
	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pending["pane-a"] = &fakeAgentSession{id: "pane-a"}

	w.checkPane(context.Background(), "pane-a", boardHostIDLocal)

	assert.NotContains(t, w.pending, "pane-a",
		"a pane that is gone from the manager must be dropped, not retried on every tick")
}

// Detection runs a command inside the pane, so it can fail for reasons that
// have nothing to do with what is running there — the tmux server went away,
// the SSH connection dropped. It is logged, the pane is dropped from pending,
// and nothing is ever written to it.
//
// It is logged *again on every tick*, and this pins that rather than the
// once-per-pane behavior an earlier revision of this comment claimed.
// Deleting from pending does not suppress anything: the next pollOnce finds
// the same live session and calls checkPane again. Measured, not assumed —
// three ticks produce three lines. warnOnce exists in this file and is used
// for the presence warning, so applying it here is a real option; that is a
// behavior change and is tracked in #218.
func TestCheckPaneLogsADetectionFailureAndDropsThePane(t *testing.T) {
	logs := captureBootstrapLog(t)
	manager := session.NewManager()
	sess := &fakeAgentSession{
		id:          "pane-a",
		sessionType: session.TypeTmux,
		detectErr:   errors.New("no server running on /tmp/sample-tmux/default"),
	}
	manager.Add(sess)
	t.Cleanup(manager.CloseAll)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pending["pane-a"] = sess

	w.checkPane(context.Background(), "pane-a", boardHostIDLocal)

	assert.Contains(t, logs.String(), "detecting agent type for pane \"pane-a\"",
		"the pane id has to be in the line, or an operator cannot tell which pane is failing")
	assert.Contains(t, logs.String(), "no server running")
	assert.NotContains(t, w.pending, "pane-a")
	assert.Empty(t, sess.writes, "a pane whose agent could not be detected must never be written to")

	afterOne := logs.Len()
	w.pollOnce(context.Background())
	assert.Greater(t, logs.Len(), afterOne,
		"the warning repeats per tick today — if this ever stops, the comment above needs rewriting, "+
			"not this assertion deleting")
}

// A pane whose host has no resolved agmsg path is not "agmsg is absent" — it
// is "we do not know", and the two must not be conflated: reporting absence
// would let the watcher decide the host is ineligible on the strength of a
// lookup that never happened. Hence the second return value.
func TestAgmsgPresentReportsUncheckedWhenTheHostHasNoResolvedPath(t *testing.T) {
	logs := captureBootstrapLog(t)
	w := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:       session.NewManager(),
		PaneHosts:     map[string]string{"pane-a": "workstation"},
		PaneModes:     func() map[string]string { return nil },
		ResolvedPaths: map[string]string{},
		Team:          "panemux",
	})

	present, checked := w.agmsgPresent(context.Background(), "pane-a", "workstation")

	assert.False(t, present)
	assert.False(t, checked, "an unresolved path is not evidence of absence")
	assert.Contains(t, logs.String(), `no resolved agmsg_path for host "workstation"`)
	assert.Contains(t, logs.String(), `pane "pane-a"`)

	before := logs.Len()
	_, _ = w.agmsgPresent(context.Background(), "pane-a", "workstation")
	assert.Equal(t, before, logs.Len(),
		"the warning is once per pane — a poll loop would otherwise repeat it forever")
}

// newLocalWatcher's manager is shared by the tests above; this pins that the
// helper's own assumption still holds, so a failure there does not read as a
// failure of the arms under test.
func TestNewLocalWatcherRegistersTheLocalHost(t *testing.T) {
	w := newLocalWatcher(session.NewManager(), "/tmp/sample-agmsg", "panemux", nil)

	require.Contains(t, w.resolvedPaths, boardHostIDLocal)
	assert.Equal(t, "/tmp/sample-agmsg", w.resolvedPaths[boardHostIDLocal])
}
