package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/fileops"
	"panemux/internal/homedir"
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

// LoadOrDefault resolves the path from the home directory, so both of its arms
// are reachable by pointing the home-directory seam somewhere this test owns.
func TestLoadOrDefault_NoConfigAtTheDefaultPath_ReturnsDefaultsAimedAtIt(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)

	cfg, err := LoadOrDefault()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, filepath.Join(home, ".config", "panemux", "config.yaml"), cfg.filePath,
		"a later save must land at the default path, not nowhere")
	assert.Equal(t, defaultWorkspaceID, cfg.ActiveWorkspaceID())
}

func TestLoadOrDefault_ExistingConfig_IsLoaded(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)
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
	homedir.SetFailingForTest(t, errNoHomeDir)

	cfg, err := LoadOrDefault()

	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Empty(t, cfg.filePath, "with nowhere to save, write() must be a no-op rather than guessing a path")
	assert.Equal(t, 8080, cfg.Server.Port)
}

func TestDefaultConfigPath_NoHomeDirectory_Errors(t *testing.T) {
	homedir.SetFailingForTest(t, errNoHomeDir)

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

// The write goes through internal/fileops now, so the last step is a rename
// and a directory sitting at the target path is what makes it fail. The
// message has to name the file either way: this one is logged or returned to
// a dashboard request, and "rename ... file exists" alone says nothing about
// which file it was.
func TestWrite_PathIsADirectory_ReportsAWriteFailure(t *testing.T) {
	cfg := validConfig()
	cfg.filePath = filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.Mkdir(cfg.filePath, 0750))

	err := cfg.SaveWorkspaces()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "replacing config")
}

// The reason config.yaml stopped being written with os.WriteFile: it holds
// server.auth_token and the whole workspace layout, and a write that fails
// partway used to truncate it in place, leaving the next start to parse half
// a YAML document. Now the failure happens to a temp file that is removed,
// and the previous config is still there afterwards.
func TestWrite_FailureMidWrite_LeavesThePreviousConfigIntact(t *testing.T) {
	cfg := validConfig()
	cfg.filePath = filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, cfg.SaveWorkspaces())
	before, err := os.ReadFile(cfg.filePath)
	require.NoError(t, err)
	require.NotEmpty(t, before)

	injected := errors.New("no space left on device")
	fileops.SetOpsForTest(t, (&fileops.Spy{WriteErr: injected}).Ops())

	err = cfg.SaveWorkspaces()

	require.Error(t, err)
	assert.ErrorIs(t, err, injected)
	assert.Contains(t, err.Error(), "writing temp config")

	after, readErr := os.ReadFile(cfg.filePath)
	require.NoError(t, readErr, "the config must still be readable after a failed save")
	assert.Equal(t, before, after, "a failed save must not change the config that was already there")

	entries, readDirErr := os.ReadDir(filepath.Dir(cfg.filePath))
	require.NoError(t, readDirErr)
	assert.Len(t, entries, 1, "the temp file must not be left beside it")
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

// Keeping config.yaml in a dotfiles repo and symlinking it into
// ~/.config/panemux is an ordinary thing to do, and os.WriteFile wrote
// through the link. AtomicWrite renames onto the path it is given, and
// rename(2) replaces a symlink rather than following it — so without
// resolving first, the first save from the dashboard would swap the link for
// a regular file, silently detaching the config from the repo: no error, no
// log line, and the user's edits over there stop taking effect.
func TestWrite_SymlinkedConfig_WritesThroughTheLinkInsteadOfReplacingIt(t *testing.T) {
	dotfiles := t.TempDir()
	target := filepath.Join(dotfiles, "config.yaml")
	require.NoError(t, os.WriteFile(target, []byte("server:\n  port: 1234\n"), 0600))

	link := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.Symlink(target, link))

	cfg := validConfig()
	cfg.filePath = link
	require.NoError(t, cfg.SaveWorkspaces())

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the symlink itself must survive the save")

	written, err := os.ReadFile(target)
	require.NoError(t, err)
	assert.Contains(t, string(written), "workspaces:",
		"the save must land in the file the link points at, not beside it")

	entries, err := os.ReadDir(filepath.Dir(link))
	require.NoError(t, err)
	assert.Len(t, entries, 1, "no temp file may be left in the directory holding the link")
}

// writeTargetCase is one shape of path and the target it must resolve to.
type writeTargetCase struct {
	// build returns the path to resolve and what resolveWriteTarget must
	// return for it.
	build func(t *testing.T) (path, want string)
	name  string
}

func writeTargetCases() []writeTargetCase {
	return []writeTargetCase{
		{
			name: "no file here at all — a first run",
			build: func(t *testing.T) (string, string) {
				t.Helper()
				path := filepath.Join(t.TempDir(), "config.yaml")
				return path, path
			},
		},
		{
			name: "an ordinary file is already its own target",
			build: func(t *testing.T) (string, string) {
				t.Helper()
				path := filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, os.WriteFile(path, []byte("server:\n"), 0600))
				return path, path
			},
		},
		{
			name: "a link whose target exists",
			build: func(t *testing.T) (string, string) {
				t.Helper()
				target := filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, os.WriteFile(target, []byte("server:\n"), 0600))
				link := filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, os.Symlink(target, link))
				return link, target
			},
		},
		{
			name: "a dangling link, absolute target",
			build: func(t *testing.T) (string, string) {
				t.Helper()
				target := filepath.Join(t.TempDir(), "config.yaml")
				link := filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, os.Symlink(target, link))
				return link, target
			},
		},
		{
			name: "a dangling link, target relative to the link's own directory",
			build: func(t *testing.T) (string, string) {
				t.Helper()
				root := t.TempDir()
				require.NoError(t, os.Mkdir(filepath.Join(root, "cfg"), 0750))
				require.NoError(t, os.Mkdir(filepath.Join(root, "dotfiles"), 0750))
				link := filepath.Join(root, "cfg", "config.yaml")
				require.NoError(t, os.Symlink(filepath.Join("..", "dotfiles", "config.yaml"), link))
				return link, filepath.Join(root, "dotfiles", "config.yaml")
			},
		},
	}
}

// EvalSymlinks fails for two different reasons and only one of them means
// "leave this path alone". A dangling link is not an exotic state — it is a
// dotfiles setup mid-flight: the link is in place and the repo has no
// config.yaml in it yet, because the first save is what was supposed to create
// one. os.WriteFile did exactly that, since O_CREATE through a dangling link
// creates the target.
//
// The cases above cover both reasons the error cannot separate, and both link
// shapes, since a relative target has to resolve against the link's own
// directory rather than the working directory.
func TestResolveWriteTargetFollowsALinkWhoseTargetDoesNotExistYet(t *testing.T) {
	for _, tt := range writeTargetCases() {
		t.Run(tt.name, func(t *testing.T) {
			path, want := tt.build(t)

			assert.Equal(t, want, resolveWriteTarget(path))
		})
	}
}

// The end-to-end half of the case above: the link survives the save and the
// repo copy it points at is created, rather than the link being replaced by a
// regular file holding the only copy.
func TestWrite_SymlinkedConfigWithAMissingTarget_CreatesItThroughTheLink(t *testing.T) {
	dotfiles := t.TempDir()
	target := filepath.Join(dotfiles, "config.yaml")

	link := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.Symlink(target, link))

	cfg := validConfig()
	cfg.filePath = link
	require.NoError(t, cfg.SaveWorkspaces())

	info, err := os.Lstat(link)
	require.NoError(t, err)
	assert.NotZero(t, info.Mode()&os.ModeSymlink, "the symlink itself must survive the save")

	written, err := os.ReadFile(target)
	require.NoError(t, err, "the save must have created the file the link points at")
	assert.Contains(t, string(written), "workspaces:")
}

// ── Expanding ~ ──────────────────────────────────────────────────────────────

// KeyFile had a test; KnownHostsFile did not, and the two are separate `if`
// bodies, so the second one was never entered.
func TestExpandPaths_ExpandsBothSSHFileFields(t *testing.T) {
	home := t.TempDir()
	homedir.SetForTest(t, home)

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

// ── Expanding ~ without a home directory ─────────────────────────────────────

// Both ~ expansions in this package resolve the home directory and
// deliberately do not report a failure to resolve it: Load has nowhere to
// return an error from here, and a config carrying one unexpandable path is
// still a usable config. Issue #212 asked for that swallow to be decided
// rather than inherited, so it is pinned here — together with the half of it
// that was wrong.
//
// filepath.Join("", ".ssh/id_ed25519") is ".ssh/id_ed25519": joining against
// an empty home turned an absolute-by-intent path into one relative to
// whatever directory panemux happened to be started in, which resolves
// silently against the wrong file instead of failing where an operator can
// see it. Leaving the ~/ in place is the diagnosable answer, and it is what
// board.go's expandLocalAgmsgPath already documents itself as matching.
func TestExpandPaths_UnresolvableHomeDirectory_LeavesTildePathsAlone(t *testing.T) {
	homedir.SetFailingForTest(t, errNoHomeDir)

	cfg := validConfig()
	cfg.SSHConnections = map[string]SSHConnection{
		"prod": {
			Host:           "remote.example.com",
			KeyFile:        "~/.ssh/id_ed25519",
			KnownHostsFile: "~/.ssh/known_hosts",
		},
	}

	cfg.expandPaths()

	conn := cfg.SSHConnections["prod"]
	assert.Equal(t, "~/.ssh/id_ed25519", conn.KeyFile,
		"a key path must never become relative to the working directory")
	assert.Equal(t, "~/.ssh/known_hosts", conn.KnownHostsFile)
}

func TestExpandPanePaths_UnresolvableHomeDirectory_LeavesTildeCwdAlone(t *testing.T) {
	homedir.SetFailingForTest(t, errNoHomeDir)

	pane := &PaneConfig{ID: "main", Type: "local", Cwd: "~/src/panemux"}

	ExpandPanePaths(pane)

	assert.Equal(t, "~/src/panemux", pane.Cwd,
		"a cwd must never become relative to the working directory")
}

// The expansion still has to happen when the home directory does resolve —
// the failure arm above is only correct if it is genuinely an arm.
func TestExpandPanePaths_ResolvableHomeDirectory_ExpandsTheCwd(t *testing.T) {
	homedir.SetForTest(t, "/workspace/user/home")

	pane := &PaneConfig{ID: "main", Type: "local", Cwd: "~/src/panemux"}

	ExpandPanePaths(pane)

	assert.Equal(t, "/workspace/user/home/src/panemux", pane.Cwd)
}
