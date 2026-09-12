package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
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
// It is logged *once per failing streak*, not once per tick. Deleting from
// pending suppresses nothing on its own: the next pollOnce finds the same
// live session and calls checkPane again, so before #218 a pane whose
// detection kept failing — which is the normal shape, since a dead tmux
// server or a dropped SSH connection stays dead — wrote a warning line every
// tick for as long as panemux ran.
func TestCheckPaneLogsADetectionFailureOncePerStreakAndDropsThePane(t *testing.T) {
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
	w.pollOnce(context.Background())
	assert.Equal(t, afterOne, logs.Len(),
		"the same failure must not be re-logged on every tick — one broken pane would drown out "+
			"everything else panemux writes")
}

// The streak is what is warned about, not the pane: a failure that clears and
// comes back is news again. "Warn once ever" would silently swallow the
// second outage, which is the one an operator most needs to see, since by
// then they have been told the pane recovered.
func TestCheckPaneWarnsAgainAfterADetectionFailureStreakEnds(t *testing.T) {
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
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	require.Equal(t, 1, strings.Count(logs.String(), "detecting agent type for pane"))

	// Detection succeeds again — no agent is running in the pane, but the
	// detection itself worked, which is what ends the streak.
	sess.detectErr = nil
	w.pollOnce(context.Background())

	sess.detectErr = errors.New("no server running on /tmp/sample-tmux/default")
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Equal(t, 2, strings.Count(logs.String(), "detecting agent type for pane"),
		"a failure that returns after the pane recovered is a new streak and must be logged again — "+
			"once for each streak, not once per tick and not once for the life of the process")
}

// The warnings a pane can produce are independent of each other: they report
// different conditions, and a pane can hit them in sequence. Sharing one
// per-pane flag between them would let whichever fired first silence the
// rest for the life of the process.
//
// It guards the single-shared-flag implementation of #218 rather than the
// behavior before it, so it does not go red on its own: the code this replaces
// logged detection failures with a bare log.Printf and so had no per-pane
// warning state for them to occupy at all.
//
//efficacy:exempt guards the single-shared-flag implementation of #218, not the behavior before it
func TestBootstrapWarningsOfDifferentKindsDoNotSuppressEachOther(t *testing.T) {
	logs := captureBootstrapLog(t)
	manager := session.NewManager()
	sess := &fakeAgentSession{
		id:          "pane-a",
		sessionType: session.TypeTmux,
		detectErr:   errors.New("no server running on /tmp/sample-tmux/default"),
	}
	manager.Add(sess)
	t.Cleanup(manager.CloseAll)

	// agmsg is absent from the local install path, so a pane that gets past
	// detection reaches the presence warning.
	w := newLocalWatcher(manager, localAgmsgDir(t, false), "panemux", nil)
	w.pollOnce(context.Background())
	require.Contains(t, logs.String(), "detecting agent type for pane")

	sess.detectErr = nil
	sess.detectOK = true
	sess.detectType = "claude-code"
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Contains(t, logs.String(), "agmsg not found on host",
		"the presence warning must not be suppressed by an earlier detection warning for the same pane")
	assert.Empty(t, sess.writes)
}

// A pane that is restarted is a new Session object — the same signal
// bootstrapped and givenUp already compare by identity — so nothing the old
// session's failures suppressed may carry over to it. Without this, the one
// case where the operator hears *nothing* would be the strongest recovery
// signal there is: the pane was torn down and recreated.
func TestARestartedPaneWarnsAboutTheSameFailureAgain(t *testing.T) {
	logs := captureBootstrapLog(t)
	manager := session.NewManager()
	first := &fakeAgentSession{
		id:          "pane-a",
		sessionType: session.TypeTmux,
		detectErr:   errors.New("no server running on /tmp/sample-tmux/default"),
	}
	manager.Add(first)
	t.Cleanup(manager.CloseAll)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	require.Equal(t, 1, strings.Count(logs.String(), "detecting agent type for pane"))

	require.NoError(t, manager.Remove("pane-a"))
	restarted := &fakeAgentSession{
		id:          "pane-a",
		sessionType: session.TypeTmux,
		detectErr:   errors.New("no server running on /tmp/sample-tmux/default"),
	}
	manager.Add(restarted)

	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Equal(t, 2, strings.Count(logs.String(), "detecting agent type for pane"),
		"the restarted pane's failure is its own, and must be reported once for it")
}

// A pane the manager no longer knows about is not in a failing streak: if it
// comes back it starts a new one. Dropping the entry is also what keeps the
// map from growing for panes that no longer exist.
func TestAPaneThatDisappearsDropsItsSuppressedWarnings(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{
		id:          "pane-a",
		sessionType: session.TypeTmux,
		detectErr:   errors.New("no server running on /tmp/sample-tmux/default"),
	}
	manager.Add(sess)
	t.Cleanup(manager.CloseAll)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pollOnce(context.Background())
	require.NotEmpty(t, w.warned["pane-a"].kinds)

	require.NoError(t, manager.Remove("pane-a"))
	w.pollOnce(context.Background())

	assert.NotContains(t, w.warned, "pane-a")
}

// Clearing one kind's streak must leave the others' suppression intact, so a
// recovered detection does not re-open the presence warning the operator has
// already been shown.
//
// The per-pane entry outlives its kinds — it carries the session identity
// noteSession compares against — so clearing the last kind empties the map of
// kinds and leaves the entry standing. sessionFor's disappeared-pane arm is
// the only place it is dropped. Deleting it here instead would leave the next
// warning recorded against no session, and the following tick would read that
// as a replacement and re-warn: every tick, the behavior #218 removed.
func TestClearingOneWarningKindLeavesTheOthersSuppressed(t *testing.T) {
	logs := captureBootstrapLog(t)
	w := newLocalWatcher(session.NewManager(), localAgmsgDir(t, true), "panemux", nil)

	w.warnOnce("pane-a", warnKindDetect, "detect failed")
	w.warnOnce("pane-a", warnKindProbe, "probe failed")
	require.Equal(t, 1, strings.Count(logs.String(), "detect failed"))
	require.Equal(t, 1, strings.Count(logs.String(), "probe failed"))

	w.clearWarning("pane-a", warnKindDetect)

	w.warnOnce("pane-a", warnKindDetect, "detect failed")
	w.warnOnce("pane-a", warnKindProbe, "probe failed")

	assert.Equal(t, 2, strings.Count(logs.String(), "detect failed"),
		"the cleared kind starts a new streak")
	assert.Equal(t, 1, strings.Count(logs.String(), "probe failed"),
		"the kind that never cleared stays suppressed")
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
