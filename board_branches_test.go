package main

import (
	"bytes"
	"context"
	"errors"
	"log"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/fileops"
	"panemux/internal/homedir"
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

// isolatedHome points the home-directory seam at a directory this test owns,
// so anything resolving ~/.config/panemux touches the fixture and never the
// developer's real cursor or bootstrap-state files.
func isolatedHome(t *testing.T) string {
	t.Helper()

	home := t.TempDir()
	homedir.SetForTest(t, home)
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

func boardEnabledConfig(panes ...*config.PaneConfig) *config.Config {
	cfg := configWithPanes(panes...)
	cfg.AgentBoard = config.AgentBoardConfig{Enabled: true, Mode: "monitor", Team: "panemux"}
	return cfg
}

// ── setupBoard ───────────────────────────────────────────────────────────────

// The existing setupBoard test uses a default config, which has no
// board-enabled pane, so every loop body inside it was skipped. With one
// enabled pane and an agmsg install to find, the host is registered and a
// client is built for it.
func TestSetupBoard_BoardEnabledPane_BuildsAClientForItsHost(t *testing.T) {
	isolatedHome(t)
	installAgmsg(t, "")
	cfg := boardEnabledConfig(paneConfig("pane-a"))

	cache, relay, bootstrap := setupTestBoard(t, cfg, session.NewManager())

	require.NotNil(t, cache)
	require.NotNil(t, relay)
	require.NotNil(t, bootstrap)
	assert.True(t, relay.HasClients(),
		"the enabled pane's host must reach the relay's client map, or it is silently off the board")
	assert.True(t, bootstrap.HasWork(), "the enabled pane is the work the watcher has to do")

	// Read through the live config rather than the value setupBoard saw: a
	// mode changed in the pane settings dialog has to land on the next tick,
	// and asserting the startup value would pass either way.
	cfg.AgentBoard.Mode = "turn"
	assert.Equal(t, "turn", bootstrap.modeFor("pane-a"),
		"the watcher reads modes from the live config, not a startup snapshot")
}

// With no agmsg installed the host is skipped rather than given a client that
// would fail on every poll for the life of the process.
func TestSetupBoard_NoAgmsgInstall_SkipsTheHost(t *testing.T) {
	isolatedHome(t)
	cfg := boardEnabledConfig(
		paneConfig("pane-a"),
	)

	_, relay, bootstrap := setupTestBoard(t, cfg, session.NewManager())

	require.NotNil(t, relay, "the relay is still built; it just has no client to poll")
	assert.True(t, relay.HasClients(), "the lazy host wrapper remembers a confirmed absent install")
	relay.Poll(context.Background())
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
	installAgmsg(t, "")
	cfg := boardEnabledConfig(paneConfig("pane-a"))

	_, relay, _ := setupTestBoard(t, cfg, session.NewManager())

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
	installAgmsg(t, "")
	cfg := boardEnabledConfig(paneConfig("pane-a"))

	_, _, bootstrap := setupTestBoard(t, cfg, session.NewManager())

	require.NotNil(t, bootstrap)
	assert.Contains(t, buf.String(), "loading bootstrap state file")
}

// Well-formed persisted state is installed, not merely read without
// complaint. Asserting only that the error branch was skipped would pass
// with the parsed values thrown away: the relay would re-poll from the
// beginning, and every already-bootstrapped pane would be sent its
// onboarding instruction again after a restart.
func TestSetupBoard_ValidPersistedState_IsLoadedIntoTheRelayAndWatcher(t *testing.T) {
	home := isolatedHome(t)
	panemuxConfigDir(t, home)

	cursorPath, err := board.DefaultCursorFilePath()
	require.NoError(t, err)
	cursors := []board.CursorEntry{{Host: boardHostIDLocal, Team: "panemux", Cursor: "42"}}
	require.NoError(t, board.SaveCursorFile(cursorPath, cursors))

	statePath, err := board.DefaultBootstrapStateFilePath()
	require.NoError(t, err)
	require.NoError(t, board.SaveBootstrapState(statePath, []string{"pane-a"}))

	buf := captureBoardLog(t)
	installAgmsg(t, "")
	cfg := boardEnabledConfig(paneConfig("pane-a"))

	_, relay, bootstrap := setupTestBoard(t, cfg, session.NewManager())

	require.NotNil(t, relay)
	assert.Equal(t, cursors, relay.Cursors(), "the parsed cursors must reach the relay")
	assert.Equal(t, []string{"pane-a"}, bootstrap.persistedPaneIDs,
		"and the parsed pane IDs must reach the watcher")
	assert.NotContains(t, buf.String(), "loading relay cursor file")
	assert.NotContains(t, buf.String(), "loading bootstrap state file")
}

// ── agmsg version warning ────────────────────────────────────────────────────

// A VERSION file that exists but cannot be read is a real problem, so it is
// logged rather than treated as "version unknown".
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
			homedir.SetFailingForTest(t, errNoHomeDir)
			buf := captureBoardLog(t)

			tt.persist()

			assert.Contains(t, buf.String(), tt.want)
		})
	}
}

// A write that fails partway through — the disk filling, the filesystem going
// read-only — is the failure the temp-file-plus-rename discipline exists to
// survive, and it is also the one the persist helpers' second arm reports.
// Neither is reachable against a real filesystem: the file being written is
// one internal/fileops created itself moments earlier, and the suite runs as
// root in CI.
//
// That these two arms are driven from *this* package is the reason the seam is
// a shared package rather than a variable inside internal/board: the callers
// and the tests do not line up, so a package-private override would have been
// invisible from here. Same shape as internal/homedir, same reason (#212).
func TestPersistHelpers_FailureMidWrite_LogAndGiveUp(t *testing.T) {
	for _, tt := range []struct {
		persist func()
		name    string
		want    string
	}{
		{
			name:    "relay cursors",
			persist: func() { persistBoardCursors([]board.CursorEntry{{Host: "local", Team: "panemux"}}) },
			want:    "persisting relay cursor",
		},
		{
			name:    "bootstrap state",
			persist: func() { persistBootstrapState([]string{"pane-a"}) },
			want:    "persisting bootstrap state",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			homedir.SetForTest(t, home)
			fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: errors.New("no space left on device")}).Ops())
			buf := captureBoardLog(t)

			tt.persist()

			assert.Contains(t, buf.String(), tt.want)
			assert.Contains(t, buf.String(), "no space left on device",
				"the underlying failure must survive into the log line, or an operator "+
					"learns only that something went wrong")

			entries, err := os.ReadDir(filepath.Join(home, ".config", "panemux"))
			require.NoError(t, err, "the directory is created before the write fails")
			assert.Empty(t, entries, "no partial or temp file may be left in the operator's config directory")
		})
	}
}
