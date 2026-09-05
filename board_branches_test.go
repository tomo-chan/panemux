package main

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/session"
)

// These are branches `make coverage-blocks` reported as never entered in
// board.go — the wiring that decides which hosts get a board client, which
// agmsg install each one resolves to, and what panemux logs when it cannot
// tell. Issue #195.
//
// Every one of them is a startup decision an operator only ever sees through
// a log line, so a wrong answer here is silent: a host quietly off the board,
// or a stale cursor file quietly ignored.

// isolatedHome points $HOME at a directory this test owns, so anything
// resolving ~/.config/panemux touches the fixture and never the developer's
// real cursor or bootstrap-state files.
func isolatedHome(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("HOME", home)
	return home
}

// captureBoardLog redirects the standard logger for the test. Everything
// board.go reports at startup is a log line and nothing else — no error is
// returned to a caller — so the log is the only place these decisions are
// observable.
func captureBoardLog(t *testing.T) *bytes.Buffer {
	t.Helper()

	var buf bytes.Buffer
	originalWriter := log.Writer()
	originalFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(originalWriter)
		log.SetFlags(originalFlags)
	})
	return &buf
}

func panemuxConfigDir(t *testing.T, home string) string {
	t.Helper()

	dir := filepath.Join(home, ".config", "panemux")
	require.NoError(t, os.MkdirAll(dir, 0o750))
	return dir
}

func boardEnabledConfig(agmsgPath string, panes ...*config.PaneConfig) *config.Config {
	cfg := configWithPanes(panes...)
	cfg.AgentBoard = config.AgentBoardConfig{Team: "panemux", AgmsgPath: agmsgPath}
	return cfg
}

// ── setupBoard ───────────────────────────────────────────────────────────────

// The existing setupBoard test uses a default config, which has no
// board-enabled pane, so every loop body inside it was skipped. With one
// enabled pane and an agmsg install to find, the host is registered and a
// client is built for it.
func TestSetupBoard_BoardEnabledPane_BuildsAClientForItsHost(t *testing.T) {
	isolatedHome(t)
	cfg := boardEnabledConfig(
		installAgmsg(t, t.TempDir(), ""),
		paneConfig("pane-a", config.PaneAgentBoardConfig{Enabled: boolPtr(true), Mode: "both"}),
	)

	cache, relay, bootstrap := setupBoard(cfg, session.NewManager())

	require.NotNil(t, cache)
	require.NotNil(t, relay)
	require.NotNil(t, bootstrap)
	assert.True(t, bootstrap.HasWork(), "the enabled pane is the work the watcher has to do")
	assert.Equal(t, "both", bootstrap.modeFor("pane-a"),
		"the watcher reads modes from the live config, not a startup snapshot")
}

// With no agmsg installed the host is skipped rather than given a client that
// would fail on every poll for the life of the process.
func TestSetupBoard_NoAgmsgInstall_SkipsTheHost(t *testing.T) {
	isolatedHome(t)
	cfg := boardEnabledConfig(
		t.TempDir(), // a directory with no scripts/api.sh
		paneConfig("pane-a", config.PaneAgentBoardConfig{Enabled: boolPtr(true), Mode: "monitor"}),
	)

	_, relay, bootstrap := setupBoard(cfg, session.NewManager())

	require.NotNil(t, relay, "the relay is still built; it just has no client to poll")
	assert.True(t, bootstrap.HasWork(), "bootstrap eligibility is a separate decision from the relay client")
}

// A cursor file panemux cannot parse is logged and ignored. Startup continues
// with no cursors rather than failing, because the file is a resume
// optimization and losing it costs a re-poll, not correctness.
func TestSetupBoard_UnparsableCursorFile_LogsAndContinues(t *testing.T) {
	home := isolatedHome(t)
	dir := panemuxConfigDir(t, home)
	cursorPath, err := board.DefaultCursorFilePath()
	require.NoError(t, err)
	require.Equal(t, dir, filepath.Dir(cursorPath), "fixture must write where setupBoard reads")
	require.NoError(t, os.WriteFile(cursorPath, []byte("{not json"), 0o600))

	buf := captureBoardLog(t)
	cfg := boardEnabledConfig(
		installAgmsg(t, t.TempDir(), ""),
		paneConfig("pane-a", config.PaneAgentBoardConfig{Enabled: boolPtr(true), Mode: "monitor"}),
	)

	_, relay, _ := setupBoard(cfg, session.NewManager())

	require.NotNil(t, relay)
	assert.Contains(t, buf.String(), "loading relay cursor file")
}

func TestSetupBoard_UnparsableBootstrapStateFile_LogsAndContinues(t *testing.T) {
	home := isolatedHome(t)
	panemuxConfigDir(t, home)
	statePath, err := board.DefaultBootstrapStateFilePath()
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(statePath, []byte("{not json"), 0o600))

	buf := captureBoardLog(t)
	cfg := boardEnabledConfig(
		installAgmsg(t, t.TempDir(), ""),
		paneConfig("pane-a", config.PaneAgentBoardConfig{Enabled: boolPtr(true), Mode: "monitor"}),
	)

	_, _, bootstrap := setupBoard(cfg, session.NewManager())

	require.NotNil(t, bootstrap)
	assert.Contains(t, buf.String(), "loading bootstrap state file")
}

// A well-formed cursor file is loaded rather than ignored — the counterpart
// that makes the two failure tests above mean something.
func TestSetupBoard_ValidCursorFile_IsLoaded(t *testing.T) {
	home := isolatedHome(t)
	panemuxConfigDir(t, home)
	cursorPath, err := board.DefaultCursorFilePath()
	require.NoError(t, err)
	require.NoError(t, board.SaveCursorFile(cursorPath, []board.CursorEntry{
		{Host: boardHostIDLocal, Team: "panemux", Cursor: "42"},
	}))

	buf := captureBoardLog(t)
	cfg := boardEnabledConfig(
		installAgmsg(t, t.TempDir(), ""),
		paneConfig("pane-a", config.PaneAgentBoardConfig{Enabled: boolPtr(true), Mode: "monitor"}),
	)

	_, relay, _ := setupBoard(cfg, session.NewManager())

	require.NotNil(t, relay)
	assert.NotContains(t, buf.String(), "loading relay cursor file")
}

// ── agmsg version warning ────────────────────────────────────────────────────

// A VERSION file that exists but cannot be read is a real problem, so it is
// logged rather than treated as "version unknown".
func TestWarnOnAgmsgVersionMismatch_UnreadableVersionFile_Logs(t *testing.T) {
	root := installAgmsg(t, t.TempDir(), "")
	require.NoError(t, os.Mkdir(filepath.Join(root, "VERSION"), 0o750)) // a directory, not a file
	buf := captureBoardLog(t)

	warnOnAgmsgVersionMismatch(
		session.NewManager(),
		map[string]string{"pane-a": boardHostIDLocal},
		map[string]string{boardHostIDLocal: root},
	)

	assert.Contains(t, buf.String(), "reading agmsg version on host")
}

// This retires no block: TestWarnOnAgmsgVersionMismatch_DoesNotPanicAcrossHostShapes
// already runs a mismatched local install through this function. What it does
// not do is look at the result — it asserts only that nothing panicked, and
// the match/mismatch decision is pinned one layer down in
// TestVersionMismatchWarning_DistinguishesMatchFromMismatch, against the pure
// function. Nothing said the warning actually reaches the log, which is the
// entire output of warnOnAgmsgVersionMismatch.
func TestWarnOnAgmsgVersionMismatch_UntestedVersion_Warns(t *testing.T) {
	root := installAgmsg(t, t.TempDir(), "0.0.1")
	buf := captureBoardLog(t)

	warnOnAgmsgVersionMismatch(
		session.NewManager(),
		map[string]string{"pane-a": boardHostIDLocal},
		map[string]string{boardHostIDLocal: root},
	)

	assert.Contains(t, buf.String(), "has agmsg 0.0.1")
	assert.Contains(t, buf.String(), "panemux was tested against")
}

// A remote host reads its VERSION over the pane's own SSH session. The
// existing table reaches this branch only with no executor, where it returns
// early — so the probe itself, and the warning built from what it read, were
// never exercised.
func TestWarnOnAgmsgVersionMismatch_RemoteHost_ProbesOverTheLiveSession(t *testing.T) {
	manager := session.NewManager()
	manager.Add(&fakeBoardSession{id: "pane-a", tag: "0.0.1"})
	buf := captureBoardLog(t)

	warnOnAgmsgVersionMismatch(
		manager,
		map[string]string{"pane-a": "ssh:build-host"},
		map[string]string{"ssh:build-host": "/remote/home/demo/agmsg"},
	)

	assert.Contains(t, buf.String(), `host "ssh:build-host" has agmsg 0.0.1`)
}

// A dead session makes the probe fail, and a version panemux could not read
// is logged rather than assumed to match.
func TestWarnOnAgmsgVersionMismatch_RemoteProbeFails_Logs(t *testing.T) {
	manager := session.NewManager()
	manager.Add(&failingBoardSession{id: "pane-a"})
	buf := captureBoardLog(t)

	warnOnAgmsgVersionMismatch(
		manager,
		map[string]string{"pane-a": "ssh:build-host"},
		map[string]string{"ssh:build-host": "/remote/home/demo/agmsg"},
	)

	assert.Contains(t, buf.String(), "reading agmsg version on host")
}

// ── Persisting relay and bootstrap state ─────────────────────────────────────

// Both persist helpers resolve their own path, and both are called from a
// background goroutine where a returned error would have nowhere to go — so
// the path failure is logged, not propagated.
func TestPersistHelpers_NoHomeDirectory_LogAndGiveUp(t *testing.T) {
	for _, tt := range []struct {
		name    string
		persist func()
		want    string
	}{
		{
			name:    "relay cursors",
			persist: func() { persistBoardCursors([]board.CursorEntry{{Host: "local", Team: "panemux"}}) },
			want:    "resolving relay cursor file path",
		},
		{
			name:    "bootstrap state",
			persist: func() { persistBootstrapState([]string{"pane-a"}) },
			want:    "resolving bootstrap state file path",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HOME", "")
			buf := captureBoardLog(t)

			tt.persist()

			assert.Contains(t, buf.String(), tt.want)
		})
	}
}

// ── Host resolution ──────────────────────────────────────────────────────────

// Two panes on the same host are one host to poll, not two.
func TestDistinctBoardHosts_DeduplicatesAndKeepsEachHostOnce(t *testing.T) {
	hosts := distinctBoardHosts(map[string]string{
		"pane-a": "ssh:build-host",
		"pane-b": "ssh:build-host",
		"pane-c": boardHostIDLocal,
	})

	assert.ElementsMatch(t, []string{"ssh:build-host", boardHostIDLocal}, hosts)
}

// A remote host that answers the presence probe gets a remote client, which
// is the arm the local-host tests never reach.
func TestNewAgmsgClientForHost_RemoteWithAgmsgPresent_BuildsARemoteClient(t *testing.T) {
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: "/opt/agmsg"}}
	manager := session.NewManager()
	manager.Add(&fakeBoardSession{id: "pane-a", tag: "yes"})
	paneHosts := map[string]string{"pane-a": "ssh:build-host"}

	client, ok := newAgmsgClientForHost(cfg, manager, paneHosts, "ssh:build-host")

	require.True(t, ok)
	assert.NotNil(t, client)
}

// The same host answering "no" is skipped, with the one log line that names
// the path it looked at.
func TestNewAgmsgClientForHost_RemoteWithoutAgmsg_SkipsTheHost(t *testing.T) {
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: "/opt/agmsg"}}
	manager := session.NewManager()
	manager.Add(&fakeBoardSession{id: "pane-a", tag: "no"})
	paneHosts := map[string]string{"pane-a": "ssh:build-host"}
	buf := captureBoardLog(t)

	client, ok := newAgmsgClientForHost(cfg, manager, paneHosts, "ssh:build-host")

	assert.False(t, ok)
	assert.Nil(t, client)
	assert.Contains(t, buf.String(), "no agmsg installation at")
	assert.Contains(t, buf.String(), "/opt/agmsg")
}

// A host whose path cannot be resolved never reaches the presence probe.
func TestNewAgmsgClientForHost_UnresolvablePath_SkipsTheHost(t *testing.T) {
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: "/opt/agmsg"}}

	client, ok := newAgmsgClientForHost(
		cfg, session.NewManager(), map[string]string{"pane-a": "ssh:build-host"}, "ssh:build-host",
	)

	assert.False(t, ok)
	assert.Nil(t, client)
}

// A reachable executor whose probe fails is reported present anyway. Refusing
// the host would turn a transient connection problem into a host that stays
// off the board for the rest of the process's life.
func TestAgmsgPresentOnHost_RemoteProbeFails_ReportsPresent(t *testing.T) {
	manager := session.NewManager()
	manager.Add(&failingBoardSession{id: "pane-a"})
	buf := captureBoardLog(t)

	present := agmsgPresentOnHost(
		manager, map[string]string{"pane-a": "ssh:build-host"}, "ssh:build-host", "/opt/agmsg",
	)

	assert.True(t, present)
	assert.Contains(t, buf.String(), "checking agmsg on host")
}

func TestAgmsgPresentOnHost_RemoteProbeAnswersNo_ReportsAbsent(t *testing.T) {
	manager := session.NewManager()
	manager.Add(&fakeBoardSession{id: "pane-a", tag: "no"})

	assert.False(t, agmsgPresentOnHost(
		manager, map[string]string{"pane-a": "ssh:build-host"}, "ssh:build-host", "/opt/agmsg",
	))
}

// A `~/`-prefixed agmsg_path has to be resolved against the remote host's own
// home directory, so it needs a working session. When the probe fails the host
// is skipped rather than falling back to panemux's own home, which would point
// at a path that does not exist over there.
func TestResolveAgmsgPathForHost_RemoteProbeFails_SkipsTheHost(t *testing.T) {
	cfg := &config.Config{AgentBoard: config.AgentBoardConfig{AgmsgPath: "~/.agents/skills/agmsg"}}
	manager := session.NewManager()
	manager.Add(&failingBoardSession{id: "pane-a"})
	buf := captureBoardLog(t)

	path, ok := resolveAgmsgPathForHost(
		cfg, manager, map[string]string{"pane-a": "ssh:build-host"}, "ssh:build-host",
	)

	assert.False(t, ok)
	assert.Empty(t, path)
	assert.Contains(t, buf.String(), "resolving agmsg_path on host")
}

// With no home directory to expand against, the path is left as written
// rather than guessed at — the same thing internal/config does for the other
// local-only paths it expands.
func TestExpandLocalAgmsgPath_NoHomeDirectory_LeavesThePathAlone(t *testing.T) {
	t.Setenv("HOME", "")

	assert.Equal(t, "~/.agents/skills/agmsg", expandLocalAgmsgPath("~/.agents/skills/agmsg"))
}
