package main

import (
	"os"
	"panemux/internal/agmsg"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/board"
	"panemux/internal/config"
	"panemux/internal/homedir"
	"panemux/internal/session"
)

func TestSetupBoard_ReturnsNonNilBootstrapWatcher(t *testing.T) {
	cfg := config.Default()
	manager := session.NewManager()

	_, _, bootstrap := setupTestBoard(t, cfg, manager)

	if bootstrap == nil {
		t.Fatal("expected setupBoard to return a non-nil bootstrapWatcher")
	}
	if bootstrap.HasWork() {
		t.Fatal("expected HasWork()=false for a config with no board-enabled panes")
	}
}

// TestAgmsgPresentOnHostLocal covers the gate that keeps the relay from
// polling a host with no agmsg. Without it the relay logged the same exec
// failure every few seconds forever — observed against a real server, 8
// identical errors in the first 25 seconds — which buries the one line
// naming the cause in precisely the situation the README calls the most
// likely first failure.

func paneConfig(id string) *config.PaneConfig {
	return &config.PaneConfig{ID: id, Type: "local"}
}

func configWithPanes(panes ...*config.PaneConfig) *config.Config {
	children := make([]config.LayoutChild, 0, len(panes))
	for _, pane := range panes {
		children = append(children, config.LayoutChild{Size: float64(100 / max(len(panes), 1)), Pane: pane})
	}
	return &config.Config{Data: config.Data{
		Workspaces: config.WorkspacesConfig{
			Active: "default",
			Items: []config.WorkspaceConfig{{
				ID:     "default",
				Title:  "Default",
				Layout: config.LayoutNode{Direction: "horizontal", Children: children},
			}},
		},
	}}
}

func TestCurrentPaneModes(t *testing.T) {
	cfg := configWithPanes(paneConfig("a"), paneConfig("b"))
	assert.Empty(t, currentPaneModes(cfg))
	cfg.AgentBoard.Enabled = true
	for _, mode := range []string{"monitor", "turn", "both", "off"} {
		cfg.AgentBoard.Mode = mode
		assert.Equal(t, map[string]string{"a": mode, "b": mode}, currentPaneModes(cfg))
	}
}

// The "ssh:" prefix is what keeps an SSH connection alias named literally
// "local" from colliding with boardHostIDLocal — nothing in the config's own
// connection-name validation forbids that alias, so the separation has to be
// structural. See boardHostForPane's own comment.
func TestPersistBoardCursors_WritesAFileLoadCursorFileCanReadBack(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "panemux"), 0o700))

	entries := []board.CursorEntry{{Host: "local", Team: "demo", Cursor: "abc"}}
	persistBoardCursors(entries)

	path, err := board.DefaultCursorFilePath()
	require.NoError(t, err)
	got, err := board.LoadCursorFile(path)
	require.NoError(t, err)
	assert.Equal(t, entries, got)
}

// unwritableHome points the home-directory seam at a directory whose .config
// can never be created, and returns once that is true.
//
// Making these saves actually fail takes more than an absent directory, which
// is the trap an earlier version of both tests below fell into: the persist
// helpers go through fileops.AtomicWrite, whose first statement is
// os.MkdirAll, so a missing home directory is simply created on demand and
// the write SUCCEEDS. Those tests therefore drove the happy path while claiming to cover
// the failure branch — tautological tests of exactly the shape this
// repository's own red-check (docs/quality-gateway.md, D4) exists to catch,
// and `go tool cover` showed it: both functions sat at 50%.
//
// A plain FILE where the directory has to go is genuinely unwritable, and
// unlike a permission bit it cannot be bypassed by running as root: MkdirAll
// fails with ENOTDIR for every uid. internal/config's
// TestEnsureAuthToken_WriteFailure_NonFatal_LoopbackHost uses the same shape.
//
// It is one helper rather than two copies so the two tests cannot drift into
// disagreeing about what "unwritable" means — one of them silently going green
// again is the failure being guarded against.
func unwritableHome(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	homedir.SetForTest(t, home)
	require.NoError(t, os.WriteFile(filepath.Join(home, ".config"), []byte("not a directory"), 0o600))
}

// A save failure is a warning, never a crash and never a startup failure: the
// relay's cursor is an optimisation, and losing it costs a re-read, not
// correctness.
func TestPersistBoardCursors_UnwritableLocation_DoesNotPanic(t *testing.T) {
	unwritableHome(t)

	assert.NotPanics(t, func() {
		persistBoardCursors([]board.CursorEntry{{Host: "local", Team: "demo", Cursor: "abc"}})
	})

	path, err := board.DefaultCursorFilePath()
	require.NoError(t, err)
	_, statErr := os.Stat(path)
	assert.Error(t, statErr, "the save must actually have failed for this test to mean anything")
}

func TestPersistBootstrapState_WritesAFileLoadBootstrapStateCanReadBack(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
	require.NoError(t, os.MkdirAll(filepath.Join(home, ".config", "panemux"), 0o700))

	persistBootstrapState([]string{"pane-a", "pane-b"})

	path, err := board.DefaultBootstrapStateFilePath()
	require.NoError(t, err)
	got, err := board.LoadBootstrapState(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"pane-a", "pane-b"}, got)
}

func TestPersistBootstrapState_UnwritableLocation_DoesNotPanic(t *testing.T) {
	unwritableHome(t)

	assert.NotPanics(t, func() { persistBootstrapState([]string{"pane-a"}) })

	path, err := board.DefaultBootstrapStateFilePath()
	require.NoError(t, err)
	_, statErr := os.Stat(path)
	assert.Error(t, statErr, "the save must actually have failed for this test to mean anything")
}

// installAgmsg creates the shape the presence probe looks for: an install
// root carrying scripts/api.sh. version, when non-empty, is written to the
// VERSION file agmsg's own installer maintains.
func installAgmsg(t *testing.T, version string) string {
	t.Helper()
	home, err := homedir.Dir()
	require.NoError(t, err)
	root := filepath.Join(home, ".agents", "skills", "agmsg")
	require.NoError(t, os.MkdirAll(filepath.Join(root, "scripts"), 0o700))
	// The presence probe only checks this file, so a non-executable stub is
	// enough — and gosec's G306 wants 0600 or less even on a fixture.
	require.NoError(t, os.WriteFile(filepath.Join(root, "scripts", "api.sh"), []byte("#!/bin/sh\n"), 0o600))
	if version != "" {
		require.NoError(t, os.WriteFile(filepath.Join(root, "VERSION"), []byte(version+"\n"), 0o600))
	}
	return root
}

func TestVersionMismatchWarning_DistinguishesMatchFromMismatch(t *testing.T) {
	assert.Empty(t, agmsg.VersionMismatchWarning("local", agmsg.TestedAgmsgVersion))
	assert.NotEmpty(t, agmsg.VersionMismatchWarning("local", "0.0.1"))
}
