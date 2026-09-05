package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The branches in this file are ones `make coverage-blocks` reported as never
// entered — the shape the 80% statement threshold cannot see, because the happy
// path around a fallback or an `if err != nil { ... }` carries its function over
// the threshold whether or not any test enters the body. Issue #195.
//
// They are not all error paths. Several are the default a lookup falls back to
// when the config does not say, and those decide what an operator's dashboard
// shows when their config.yaml omits a key — worth pinning for their own sake,
// not only for the count.

// ── Loading ──────────────────────────────────────────────────────────────────

func TestLoad_MalformedYAML_ReportsAParseFailure(t *testing.T) {
	path := writeTempFile(t, "server:\n\tport: [unclosed\n")

	cfg, err := Load(path)

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "parsing config")
}

func TestLoad_MissingFile_ReportsAReadFailure(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "no-such-config.yaml"))

	require.Error(t, err)
	assert.Nil(t, cfg)
	assert.Contains(t, err.Error(), "reading config")
}

// LoadOrDefault resolves the path from $HOME, so both of its arms are reachable
// by pointing $HOME somewhere this test owns.
func TestLoadOrDefault_NoConfigAtTheDefaultPath_ReturnsDefaultsAimedAtIt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := LoadOrDefault()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, filepath.Join(home, ".config", "panemux", "config.yaml"), cfg.filePath,
		"a later save must land at the default path, not nowhere")
	assert.Equal(t, defaultWorkspaceID, cfg.ActiveWorkspaceID())
}

func TestLoadOrDefault_ExistingConfig_IsLoaded(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".config", "panemux", "config.yaml")
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0750))
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 9090\n  host: \"127.0.0.1\"\n"), 0600))

	cfg, err := LoadOrDefault()

	require.NoError(t, err)
	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, path, cfg.filePath)
}

// With no home directory there is no default path to load from or save to.
// Startup falls back to in-memory defaults rather than refusing to run, which
// is the historical behavior defaultAfterConfigPathError exists to preserve.
func TestLoadOrDefault_NoHomeDirectory_FallsBackToDefaults(t *testing.T) {
	t.Setenv("HOME", "")

	cfg, err := LoadOrDefault()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.filePath, "with nowhere to save, write() must be a no-op rather than guessing a path")
	assert.Equal(t, 8080, cfg.Server.Port)
}

func TestDefaultConfigPath_NoHomeDirectory_Errors(t *testing.T) {
	t.Setenv("HOME", "")

	path, err := DefaultConfigPath()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "getting home directory")
	assert.Empty(t, path)
}

// tightenConfigFilePermissions runs on every Load and is best-effort there, so
// its own failure is only ever visible through the error it returns.
func TestTightenConfigFilePermissions_MissingFile_Errors(t *testing.T) {
	err := tightenConfigFilePermissions(filepath.Join(t.TempDir(), "absent.yaml"))

	require.Error(t, err)
	assert.Contains(t, err.Error(), "checking config permissions")
}

// ── Writing ──────────────────────────────────────────────────────────────────

// The directory the config lives in is created first, so reaching the write
// failure needs a path whose parent is fine and whose own name is taken by
// something os.WriteFile cannot open — a directory.
func TestWrite_PathIsADirectory_ReportsAWriteFailure(t *testing.T) {
	cfg := validConfig()
	cfg.filePath = filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.Mkdir(cfg.filePath, 0750))

	err := cfg.SaveWorkspaces()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "writing config")
}

// This retires no block — an in-memory config saves on many other paths
// already. It is here as the counterpart to the failure above, because "write
// reports a failure" and "write deliberately reports success without writing"
// are one decision, and a test file that pins only the first reads as though
// the second were an accident.
func TestWrite_NoFilePath_IsANoOp(t *testing.T) {
	cfg := validConfig()

	require.NoError(t, cfg.SaveWorkspaces(), "a config with nowhere to save must not fail the request")
}

// ── Expanding ~ ──────────────────────────────────────────────────────────────

// KeyFile had a test; KnownHostsFile did not, and the two are separate `if`
// bodies, so the second one was never entered.
func TestExpandPaths_ExpandsBothSSHFileFields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cfg := validConfig()
	cfg.SSHConnections = map[string]SSHConnection{
		"prod": {
			Host:           "remote.example.com",
			KeyFile:        "~/.ssh/id_ed25519",
			KnownHostsFile: "~/.ssh/known_hosts",
		},
		"absolute": {
			Host:           "other.example.com",
			KeyFile:        "/etc/panemux/id_ed25519",
			KnownHostsFile: "/etc/panemux/known_hosts",
		},
	}

	cfg.expandPaths()

	assert.Equal(t, filepath.Join(home, ".ssh/id_ed25519"), cfg.SSHConnections["prod"].KeyFile)
	assert.Equal(t, filepath.Join(home, ".ssh/known_hosts"), cfg.SSHConnections["prod"].KnownHostsFile)
	assert.Equal(t, "/etc/panemux/id_ed25519", cfg.SSHConnections["absolute"].KeyFile,
		"an absolute path must be left alone")
	assert.Equal(t, "/etc/panemux/known_hosts", cfg.SSHConnections["absolute"].KnownHostsFile)
}

// ── Which workspace is active ────────────────────────────────────────────────

func TestActiveWorkspace_NoMatchingID_ReportsNotFound(t *testing.T) {
	cfg := &Config{Workspaces: WorkspacesConfig{
		Active: "gone",
		Items:  []WorkspaceConfig{{ID: "one", Layout: singlePaneLayout("one-main")}},
	}}

	workspace, ok := cfg.ActiveWorkspace()

	assert.False(t, ok)
	assert.Equal(t, WorkspaceConfig{}, workspace)
}

// ActiveLayout falls through to the compatibility layout when the active ID
// names no workspace, which is what keeps a config whose `active` was
// hand-edited to a typo rendering something rather than nothing.
func TestActiveLayout_ActiveIDNamesNoWorkspace_UsesTheCompatibilityLayout(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active: "gone",
			Items:  []WorkspaceConfig{{ID: "one", Layout: singlePaneLayout("one-main")}},
		},
		Layout: singlePaneLayout("fallback-main"),
	}

	layout := cfg.ActiveLayout()

	require.Len(t, layout.Children, 1)
	require.NotNil(t, layout.Children[0].Pane)
	assert.Equal(t, "fallback-main", layout.Children[0].Pane.ID)
}

func TestActiveWorkspaceID_FallsBackInOrder(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		want string
	}{
		{
			name: "the active id wins when it is set",
			cfg: &Config{Workspaces: WorkspacesConfig{
				Active: "two",
				Items:  []WorkspaceConfig{{ID: "one"}, {ID: "two"}},
			}},
			want: "two",
		},
		{
			name: "no active id falls back to the first workspace",
			cfg: &Config{Workspaces: WorkspacesConfig{
				Items: []WorkspaceConfig{{ID: "one"}, {ID: "two"}},
			}},
			want: "one",
		},
		{
			name: "no workspaces at all falls back to the default id",
			cfg:  &Config{},
			want: defaultWorkspaceID,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.cfg.ActiveWorkspaceID())
		})
	}
}

// UpdateLayout writes through to the active workspace, and falls back to the
// compatibility layout when the active ID names none.
//
// "No workspaces at all" does not reach that fallback: UpdateLayout normalizes
// first, and normalizedWorkspaces synthesizes a `default` workspace from the
// compatibility layout, so a matching workspace always exists by then. What
// does reach it is an `active:` that names a workspace the items list does not
// have — a hand-edited config.yaml with a typo in it.
func TestUpdateLayout_ActiveIDNamesNoWorkspace_WritesTheCompatibilityLayout(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active: "gone",
			Items:  []WorkspaceConfig{{ID: "one", Layout: singlePaneLayout("one-main")}},
		},
		Layout: singlePaneLayout("old-main"),
	}

	cfg.UpdateLayout(singlePaneLayout("new-main"))

	require.Len(t, cfg.Layout.Children, 1)
	require.NotNil(t, cfg.Layout.Children[0].Pane)
	assert.Equal(t, "new-main", cfg.Layout.Children[0].Pane.ID)
	require.Len(t, cfg.Workspaces.Items, 1)
	require.NotNil(t, cfg.Workspaces.Items[0].Layout.Children[0].Pane)
	assert.Equal(t, "one-main", cfg.Workspaces.Items[0].Layout.Children[0].Pane.ID,
		"the workspace the active id does not name must be left alone")
}

// A workspace whose layout is nothing but the default synthesized from the
// compatibility layout still updates through the workspace, not the fallback.
func TestUpdateLayout_NoWorkspaces_UpdatesTheSynthesizedDefault(t *testing.T) {
	cfg := &Config{Layout: singlePaneLayout("old-main")}

	cfg.UpdateLayout(singlePaneLayout("new-main"))

	require.Len(t, cfg.Workspaces.Items, 1)
	assert.Equal(t, defaultWorkspaceID, cfg.Workspaces.Items[0].ID)
	require.NotNil(t, cfg.Workspaces.Items[0].Layout.Children[0].Pane)
	assert.Equal(t, "new-main", cfg.Workspaces.Items[0].Layout.Children[0].Pane.ID)
}

// ── Naming a new pane ────────────────────────────────────────────────────────

func TestNextPaneID_SuffixesUntilTheNameIsFree(t *testing.T) {
	cfg := &Config{Workspaces: WorkspacesConfig{
		Active: "one",
		Items: []WorkspaceConfig{{
			ID: "one",
			Layout: LayoutNode{
				Direction: "horizontal",
				Children: []LayoutChild{
					{Size: 34, Pane: &PaneConfig{ID: "one-main", Type: "local"}},
					{Size: 33, Pane: &PaneConfig{ID: "one-main-2", Type: "local"}},
					{Size: 33, Pane: &PaneConfig{ID: "one-main-3", Type: "local"}},
				},
			},
		}},
	}}

	assert.Equal(t, "one-main-4", cfg.nextPaneID("one-main"), "the first three suffixes are taken")
	assert.Equal(t, "free", cfg.nextPaneID("free"), "an unused base is used as-is")
}

// ── Removing a pane from the tree ────────────────────────────────────────────

// removePaneChildren has three arms for a group whose children it just
// rewrote, and only the flat case had a test. Each arm is a different tree, so
// each gets its own test rather than one function long enough to hide an arm.

// layoutWithGroup is a workspace holding one group of two panes beside a plain
// pane — the shape the dashboard builds when a pane is split and one half is
// split again.
func layoutWithGroup() LayoutNode {
	return LayoutNode{
		Direction: "horizontal",
		Children: []LayoutChild{
			{
				Size:      50,
				Direction: "vertical",
				Children: []LayoutChild{
					{Size: 50, Pane: &PaneConfig{ID: "grouped-a", Type: "local"}},
					{Size: 50, Pane: &PaneConfig{ID: "grouped-b", Type: "local"}},
				},
			},
			{Size: 50, Pane: &PaneConfig{ID: "solo", Type: "local"}},
		},
	}
}

// workspaceWith wraps a layout in the single-workspace config the removal
// tests below operate on.
func workspaceWith(layout LayoutNode) *Config {
	return &Config{Workspaces: WorkspacesConfig{
		Active: "one",
		Items:  []WorkspaceConfig{{ID: "one", Layout: layout}},
	}}
}

func TestRemovePaneFromLayout_GroupLeftWithOneChild_CollapsesUpward(t *testing.T) {
	cfg := workspaceWith(layoutWithGroup())

	cfg.RemovePaneFromLayout("grouped-a")

	children := cfg.Workspaces.Items[0].Layout.Children
	require.Len(t, children, 2)
	require.NotNil(t, children[0].Pane, "the surviving child took the group's place")
	assert.Equal(t, "grouped-b", children[0].Pane.ID)
	assert.InDelta(t, 50, children[0].Size, 0.001, "and the group's size, not its own")
	assert.Empty(t, children[0].Children)
}

// Removing the two panes one after the other does NOT reach this arm: the
// first removal already collapsed the group into a plain pane. A group is only
// ever left with nothing when it held exactly one child to begin with, which
// the dashboard never builds but a hand-written config can.
func TestRemovePaneFromLayout_GroupLeftWithNoChildren_IsDropped(t *testing.T) {
	cfg := workspaceWith(LayoutNode{
		Direction: "horizontal",
		Children: []LayoutChild{
			{
				Size:      50,
				Direction: "vertical",
				Children: []LayoutChild{
					{Size: 100, Pane: &PaneConfig{ID: "only-child", Type: "local"}},
				},
			},
			{Size: 50, Pane: &PaneConfig{ID: "solo", Type: "local"}},
		},
	})

	cfg.RemovePaneFromLayout("only-child")

	children := cfg.Workspaces.Items[0].Layout.Children
	require.Len(t, children, 1)
	require.NotNil(t, children[0].Pane)
	assert.Equal(t, "solo", children[0].Pane.ID)
}

func TestRemovePaneFromLayout_GroupWithChildrenLeft_StaysAGroup(t *testing.T) {
	layout := layoutWithGroup()
	layout.Children[0].Children = append(
		layout.Children[0].Children,
		LayoutChild{Size: 34, Pane: &PaneConfig{ID: "grouped-c", Type: "local"}},
	)
	cfg := workspaceWith(layout)

	cfg.RemovePaneFromLayout("grouped-a")

	children := cfg.Workspaces.Items[0].Layout.Children
	require.Len(t, children, 2)
	assert.Nil(t, children[0].Pane, "still a group, not collapsed into a pane")
	assert.Equal(t, "vertical", children[0].Direction, "and it kept its own direction")
	require.Len(t, children[0].Children, 2)
	ids := []string{children[0].Children[0].Pane.ID, children[0].Children[1].Pane.ID}
	assert.Equal(t, "grouped-b,grouped-c", strings.Join(ids, ","))
}

func TestRemovePaneFromLayout_UnknownPane_ChangesNothing(t *testing.T) {
	cfg := workspaceWith(layoutWithGroup())

	cfg.RemovePaneFromLayout("no-such-pane")

	require.Len(t, cfg.Workspaces.Items[0].Layout.Children, 2)
	require.Len(t, cfg.Workspaces.Items[0].Layout.Children[0].Children, 2)
}
