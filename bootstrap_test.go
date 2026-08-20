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
		PaneModes:     func() map[string]string { return paneModes },
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

// TestBootstrapWatcher_ShortWrite_GivesUpImmediately_NeverRetries is the
// regression test for the compounding-partial-write finding: once any bytes
// have already been typed into the pane, retrying would type the full
// instruction again on top of that half-written line rather than fixing
// anything, so a short write must give up on the very first attempt, not
// just avoid being marked bootstrapped.
func TestBootstrapWatcher_ShortWrite_GivesUpImmediately_NeverRetries(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true, writeShort: true}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	require.Len(t, sess.writes, 1, "a short write attempt still counts as one attempt")
	_, bootstrapped := w.bootstrapped["pane-a"]
	assert.False(t, bootstrapped, "a short write must not be treated as a successful bootstrap")

	// Further ticks (even with the debounce satisfied again) must not
	// attempt another write at all — givenUp must block it immediately.
	for i := 0; i < 4; i++ {
		w.pollOnce(context.Background())
	}
	assert.Len(t, sess.writes, 1, "a short write must never be retried, since it would compound a half-typed line")
}

// TestBootstrapWatcher_WriteError_RetriedUpToLimitThenGivesUp covers a clean
// (n==0) write failure, which is safe to retry a bounded number of times
// since nothing has actually been typed into the pane yet.
func TestBootstrapWatcher_WriteError_RetriedUpToLimitThenGivesUp(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{
		id: "pane-a", detectType: "claude-code", detectOK: true,
		writeErr: errors.New("pty closed"),
	}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	for i := 0; i < 2+maxBootstrapWriteAttempts+2; i++ {
		w.pollOnce(context.Background())
	}

	assert.Len(t, sess.writes, maxBootstrapWriteAttempts,
		"must retry a clean write failure up to the cap, then stop attempting entirely")
	_, bootstrapped := w.bootstrapped["pane-a"]
	assert.False(t, bootstrapped)
}

func TestBootstrapWatcher_NoAgentTypeDetectorCapability_NoOp(t *testing.T) {
	manager := session.NewManager()
	sess := &noAgentSession{id: "pane-a"}
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	assert.Empty(t, w.bootstrapped)
	assert.Empty(t, w.pending)
}

// TestBootstrapWatcher_InvalidPaneIDOrTeam_SkipsBootstrap is the regression
// test for embedding an unvalidated identifier into the onboarding
// instruction's shell command lines: a pane ID or team outside agmsg's own
// identifier alphabet (board.ValidAgmsgIdentifier) can never be addressed
// correctly by the relay even if the write succeeded, so bootstrap must
// refuse rather than write a broken instruction.
func TestBootstrapWatcher_InvalidPaneIDOrTeam_SkipsBootstrap(t *testing.T) {
	t.Run("invalid pane ID", func(t *testing.T) {
		manager := session.NewManager()
		sess := &fakeAgentSession{id: "pane a", detectType: "claude-code", detectOK: true}
		manager.Add(sess)

		w := newBootstrapWatcher(bootstrapWatcherConfig{
			Manager:       manager,
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
		sess := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true}
		manager.Add(sess)

		w := newLocalWatcher(manager, localAgmsgDir(t, true), "team; rm -rf", nil)
		for i := 0; i < 3; i++ {
			w.pollOnce(context.Background())
		}
		assert.Empty(t, sess.writes)
	})
}

// TestBootstrapWatcher_RemotePresenceCheck_FailsOverPastDeadFirstCandidate
// is the regression test for trusting only the alphabetically-first
// board-enabled pane on a host to answer the presence probe: that pane's
// own session can be registered but dead (State() can lag reality), which
// must not block bootstrap for the rest of the host when another pane on
// it is genuinely reachable.
func TestBootstrapWatcher_RemotePresenceCheck_FailsOverPastDeadFirstCandidate(t *testing.T) {
	agmsgPath := "/opt/agmsg"
	probeKey := strings.Join([]string{
		"sh", "-c", "test -f \"$1\" && printf 'yes' || printf 'no'", "board-agmsg-presence",
		agmsgPath + "/scripts/api.sh",
	}, "\x00")

	manager := session.NewManager()
	// "pane-a" sorts first, so dynamicBoardExecutor tries it first; it must
	// fail over to "pane-b" rather than reporting checked=false forever.
	dead := &fakeAgentSession{
		id: "pane-a", detectType: "codex", detectOK: true,
		boardErr: errors.New("ssh: connection lost"),
	}
	alive := &fakeAgentSession{id: "pane-b", boardOutputs: map[string][]byte{probeKey: []byte("yes")}}
	manager.Add(dead)
	manager.Add(alive)

	w := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:       manager,
		PaneHosts:     map[string]string{"pane-a": "ssh:build-host", "pane-b": "ssh:build-host"},
		ResolvedPaths: map[string]string{"ssh:build-host": agmsgPath},
		Team:          "panemux",
	})
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	require.Len(t, dead.writes, 1,
		"pane-a must still bootstrap via pane-b's working session, despite pane-a's own RunBoardCommand failing")
}

// TestBootstrapWatcher_PersistedSeed_TmuxPane_PreventsReBootstrapOfLiveSession
// is the central regression test for the seeding design as it applies to a
// tmux-backed pane: a persisted pane ID whose session is still alive and
// still shows an active known agent must not be re-bootstrapped just
// because panemux itself restarted — this is the case seeding is meant for,
// since a tmux session (unlike a local/SSH one) reattaches to the same
// still-running shell across a panemux restart. A *different* Session
// object for the same pane ID (simulating that pane being restarted within
// the current process) must be bootstrapped again.
func TestBootstrapWatcher_PersistedSeed_TmuxPane_PreventsReBootstrapOfLiveSession(t *testing.T) {
	manager := session.NewManager()
	original := &fakeAgentSession{
		id: "pane-a", detectType: "claude-code", detectOK: true, sessionType: session.TypeTmux,
	}
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
	replacement := &fakeAgentSession{
		id: "pane-a", detectType: "claude-code", detectOK: true, sessionType: session.TypeTmux,
	}
	manager.Add(replacement)

	w.pollOnce(context.Background())
	assert.Empty(t, replacement.writes, "first tick after restart must still debounce")
	w.pollOnce(context.Background())
	assert.Len(t, replacement.writes, 1, "the replacement session must be bootstrapped like any new one")
}

// TestBootstrapWatcher_PersistedSeed_LocalPane_StillBootstraps is the
// regression test for the bug the tmux-only seeding restriction fixes: a
// local (or SSH) pane's underlying process cannot survive a panemux
// restart — CreateFromConfig always spawns a brand-new, empty shell — so a
// pane ID persisted from a previous run must NOT be treated as
// already-onboarded here, or that pane would never be bootstrapped again on
// any future run even though the fresh shell has never received the
// instruction.
func TestBootstrapWatcher_PersistedSeed_LocalPane_StillBootstraps(t *testing.T) {
	manager := session.NewManager()
	sess := &fakeAgentSession{id: "pane-a", detectType: "claude-code", detectOK: true} // sessionType zero value -> local
	manager.Add(sess)

	w := newLocalWatcher(manager, localAgmsgDir(t, true), "panemux", nil)
	w.LoadPersistedState([]string{"pane-a"}) // as if this pane was bootstrapped before the restart

	w.pollOnce(context.Background())
	assert.Empty(t, sess.writes, "first tick must still debounce")
	w.pollOnce(context.Background())
	assert.Len(t, sess.writes, 1, "a local pane's fresh shell must be bootstrapped despite being in persisted state")
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

// TestBuildBootstrapInstruction_DefinesWhatASummaryShouldSay covers the
// half of "show what each pane is doing" that no amount of dashboard work
// can fix: the dashboard can only render a summary the agent chose to
// write. The instruction used to list "summary" as one field name among
// eight with no guidance at all, and "send an update whenever your state
// changes meaningfully" left both the content and the cadence to the
// agent's discretion — which is how a board of many panes ends up with
// state pills and nothing to read.
func TestBuildBootstrapInstruction_DefinesWhatASummaryShouldSay(t *testing.T) {
	text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", "monitor")

	// The summary must be described as the current task in the operator's
	// terms, not left as a bare field name.
	assert.Contains(t, text, "summary")
	assert.Contains(t, text, "one short sentence")
	assert.Contains(t, text, "what you are working on right now")

	// A cadence the agent can actually act on, rather than "meaningfully".
	assert.Contains(t, text, "starting a new task")
	assert.Contains(t, text, "finishing one")
	assert.Contains(t, text, "blocked")
}

// The instruction is written into a PTY and read by a person as often as by
// an agent, so its prose must not grow into a wall. Command lines are
// exempt: they carry an operator-configured agmsg path and a generated pane
// ID, so their length is not something this file can choose, and wrapping
// one would make it uncopyable.
func TestBuildBootstrapInstruction_StaysReadableInATerminal(t *testing.T) {
	text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", "both")

	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "/scripts/") {
			continue
		}
		assert.LessOrEqual(t, len(line), 100, "line is too long to read in a pane: %q", line)
	}
}

// TestBuildBootstrapInstruction_ClaimsItsOwnIdentity covers the collision
// two board-enabled panes in the SAME project directory otherwise have.
//
// Verified against agmsg's own source (v1.2.0, the pinned tested version,
// and unchanged in v1.2.2): scripts/watch.sh resolves its subscription with
// identities.sh <project> <type>, which returns EVERY (team, agent) pair
// registered for that project and type — both pane IDs. Without a 4th
// <agent> argument the watcher does a broad subscribe: it drops only pairs
// another live session already holds an actas lock on, and claims nothing
// itself. Two panes in one repo therefore each receive messages addressed
// to the other. agmsg's own scripts/lib/actas-lock.sh names this exact
// failure ("every concurrent CC session in that project would subscribe to
// every registered identity's messages"), and scripts/actas-claim.sh is the
// remedy its claude-code template calls right after join.sh.
func TestBuildBootstrapInstruction_ClaimsItsOwnIdentity(t *testing.T) {
	text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", "monitor")

	want := `/opt/agmsg/scripts/actas-claim.sh "$(pwd)" "claude-code" "pane-a" "$CLAUDE_CODE_SESSION_ID"`
	assert.Contains(t, text, want)
	// status=held is the case that must not be silently ignored: another
	// live session owns this pane ID, and the template's own rule is to
	// stop rather than disturb the running Monitor.
	assert.Contains(t, text, "status=held")
}

// The session-id source is per agent type — agmsg's own type.conf `detect`
// key is CLAUDE_CODE_SESSION_ID for claude-code, CODEX_THREAD_ID for codex,
// and absent entirely for opencode and cursor. panemux has only verified
// the claude-code invocation against agmsg's own template, so the step is
// emitted for that type alone rather than guessing an env var for the rest.
func TestBuildBootstrapInstruction_OmitsClaimForUnverifiedTypes(t *testing.T) {
	for _, agentType := range []string{"codex", "gemini", "opencode", "cursor"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", agentType, "monitor")
		assert.NotContains(t, text, "actas-claim.sh",
			"type %q has no verified session-id source", agentType)
		assert.NotContains(t, text, "CLAUDE_CODE_SESSION_ID",
			"type %q must not be handed claude-code's own env var", agentType)
	}
}

// Claiming the lock is not enough on its own: the watcher this session is
// already running was launched with no <agent> argument, so it keeps its
// broad subscription until it is replaced. watch.sh's 4th argument is what
// narrows it, and re-claims the lock.
func TestBuildBootstrapInstruction_RearmsTheMonitorForItsOwnPaneOnly(t *testing.T) {
	for _, mode := range []string{"", "monitor", "both"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", mode)
		want := `/opt/agmsg/scripts/watch.sh "$CLAUDE_CODE_SESSION_ID" "$(pwd)" "claude-code" "pane-a"`
		assert.Contains(t, text, want, "mode %q runs a Monitor and must re-arm it", mode)
		assert.Contains(t, text, "agmsg inbox stream", "mode %q must name the task to stop", mode)
	}
}

// turn mode has no Monitor to re-arm (delivery is a Stop hook), and off
// means the operator wants no automatic delivery at all — starting one
// would contradict the setting. Both still claim the lock, which is what
// check-inbox.sh's own subscription filtering reads.
func TestBuildBootstrapInstruction_DoesNotStartAMonitorForTurnOrOff(t *testing.T) {
	for _, mode := range []string{"turn", "off"} {
		text := buildBootstrapInstruction("/opt/agmsg", "panemux", "pane-a", "claude-code", mode)
		assert.NotContains(t, text, "watch.sh", "mode %q must not launch a watcher", mode)
		assert.Contains(t, text, "actas-claim.sh", "mode %q still needs the exclusivity claim", mode)
	}
}

// TestBootstrapWatcherReadsModeLive covers a bug reported from real use:
// setting a pane's mode to "both" in the pane settings dialog left agmsg's
// own delivery mode at "off". paneModes was a snapshot taken once in
// setupBoard, so a mode changed at runtime never reached the instruction —
// the watcher kept using the startup value, and for the default "monitor"
// that means delivery.sh is never run at all, which is exactly what leaves
// agmsg reporting "off".
func TestBootstrapWatcherReadsModeLive(t *testing.T) {
	modes := map[string]string{"api": boardModeMonitor}
	b := newBootstrapWatcher(bootstrapWatcherConfig{
		Manager:       session.NewManager(),
		PaneHosts:     map[string]string{"api": boardHostIDLocal},
		PaneModes:     func() map[string]string { return modes },
		ResolvedPaths: map[string]string{boardHostIDLocal: "/workspace/user/agmsg"},
		Team:          "panemux",
	})

	assert.Equal(t, boardModeMonitor, b.modeFor("api"))

	// The dialog writes a new mode into config; the watcher must see it.
	modes["api"] = boardModeBoth
	assert.Equal(t, boardModeBoth, b.modeFor("api"),
		"a mode changed after startup must reach the next bootstrap instruction")
}
