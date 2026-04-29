package config

import (
	"bytes"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidate_ValidConfig_NoError(t *testing.T) {
	cfg := validConfig()
	assert.NoError(t, cfg.Validate())
}

func TestValidate_InvalidDirection_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Direction = "diagonal"
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "direction")
}

func TestValidate_ChildSizesNotSumTo100_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Size = 50.0
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "size")
}

func TestValidate_NegativeSize_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children = []LayoutChild{
		{Size: -10.0, Pane: &PaneConfig{ID: "p1", Type: "local"}},
		{Size: 110.0, Pane: &PaneConfig{ID: "p2", Type: "local"}},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "size")
}

func TestValidate_DuplicatePaneIDs_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children = []LayoutChild{
		{Size: 50.0, Pane: &PaneConfig{ID: "dup", Type: "local"}},
		{Size: 50.0, Pane: &PaneConfig{ID: "dup", Type: "local"}},
	}
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate")
}

func TestValidate_DuplicatePaneIDsAcrossWorkspaces_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Workspaces = WorkspacesConfig{
		Active:      "one",
		TabPosition: "top",
		Items: []WorkspaceConfig{
			{
				ID:    "one",
				Title: "One",
				Layout: LayoutNode{
					Direction: "horizontal",
					Children:  []LayoutChild{{Size: 100, Pane: &PaneConfig{ID: "dup", Type: "local"}}},
				},
			},
			{
				ID:    "two",
				Title: "Two",
				Layout: LayoutNode{
					Direction: "horizontal",
					Children:  []LayoutChild{{Size: 100, Pane: &PaneConfig{ID: "dup", Type: "local"}}},
				},
			},
		},
	}
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate pane id")
}

func TestValidate_InvalidWorkspaceTabPosition_Error(t *testing.T) {
	cfg := validConfig()
	cfg.normalizeWorkspaces()
	cfg.Workspaces.TabPosition = "diagonal"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tab_position")
}

func TestValidate_ActiveWorkspaceMissing_Error(t *testing.T) {
	cfg := validConfig()
	cfg.normalizeWorkspaces()
	cfg.Workspaces.Active = "missing"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "active workspace")
}

func TestValidate_WorkspaceTabPositions_AllValid(t *testing.T) {
	for _, position := range []string{"top", "bottom", "left", "right"} {
		t.Run(position, func(t *testing.T) {
			cfg := validConfig()
			cfg.normalizeWorkspaces()
			cfg.Workspaces.TabPosition = position
			require.NoError(t, cfg.Validate())
		})
	}
}

func TestValidate_WorkspaceIdentityErrors(t *testing.T) {
	tests := []struct {
		name       string
		want       string
		workspaces WorkspacesConfig
	}{
		{
			name: "empty workspace id",
			workspaces: WorkspacesConfig{
				Active:      "one",
				TabPosition: "top",
				Items: []WorkspaceConfig{
					{ID: "", Title: "Blank", Layout: singlePaneLayout("blank")},
					{ID: "one", Title: "One", Layout: singlePaneLayout("one-main")},
				},
			},
			want: "id must not be empty",
		},
		{
			name: "duplicate workspace id",
			workspaces: WorkspacesConfig{
				Active:      "dup",
				TabPosition: "top",
				Items: []WorkspaceConfig{
					{ID: "dup", Title: "One", Layout: singlePaneLayout("one-main")},
					{ID: "dup", Title: "Two", Layout: singlePaneLayout("two-main")},
				},
			},
			want: "duplicate workspace id",
		},
		{
			name: "empty items",
			workspaces: WorkspacesConfig{
				Active:      "missing",
				TabPosition: "top",
				Items:       []WorkspaceConfig{},
			},
			want: "workspaces.items must not be empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errs := validateWorkspaces(tt.workspaces, nil)
			require.NotEmpty(t, errs)
			assert.Contains(t, strings.Join(errs, "; "), tt.want)
		})
	}
}

func TestValidate_PaneEmptyID_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.ID = ""
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "id")
}

func TestValidate_PaneInvalidType_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.Type = "invalid"
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "type")
}

func TestValidate_SSHPaneConnectionNotDefined_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.Type = "ssh"
	cfg.Layout.Children[0].Pane.Connection = "nonexistent"
	// Point at an empty temp dir so ~/.ssh/config is not read
	cfg.sshConfigPath = filepath.Join(t.TempDir(), "config")
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection")
}

// TestValidate_SSHPaneInSSHConfig_NoError verifies that a pane whose connection
// name is defined in ~/.ssh/config (not in yaml ssh_connections) passes validation.
// This is the positive counterpart of TestValidate_SSHPaneConnectionNotDefined_Error.
func TestValidate_SSHPaneInSSHConfig_NoError(t *testing.T) {
	sshCfg := filepath.Join(t.TempDir(), "config")
	require.NoError(t, os.WriteFile(sshCfg, []byte("Host myserver\n    HostName 192.168.1.1\n    User deploy\n"), 0600))

	cfg := validConfig()
	cfg.Layout.Children[0].Pane.Type = "ssh"
	cfg.Layout.Children[0].Pane.Connection = "myserver"
	cfg.sshConfigPath = sshCfg

	assert.NoError(t, cfg.Validate())
}

func TestValidate_ServerPortOutOfRange_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Server.Port = 0
	err := cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port")

	cfg.Server.Port = 99999
	err = cfg.Validate()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "port")
}

func TestValidate_NestedLayout_Validates(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children = []LayoutChild{
		{
			Size:      50.0,
			Direction: "vertical",
			Children: []LayoutChild{
				{Size: 50.0, Pane: &PaneConfig{ID: "p1", Type: "local"}},
				{Size: 50.0, Pane: &PaneConfig{ID: "p2", Type: "local"}},
			},
		},
		{Size: 50.0, Pane: &PaneConfig{ID: "p3", Type: "local"}},
	}
	assert.NoError(t, cfg.Validate())
}

func TestValidate_Default_NoError(t *testing.T) {
	cfg := Default()
	assert.NoError(t, cfg.Validate())
}

func TestLoad_ValidFile(t *testing.T) {
	content := `
server:
  port: 8080
  host: "127.0.0.1"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
`
	f := writeTempFile(t, content)
	cfg, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "horizontal", cfg.Layout.Direction)
	assert.Equal(t, "default", cfg.Workspaces.Active)
	assert.Equal(t, "top", cfg.Workspaces.TabPosition)
	require.Len(t, cfg.Workspaces.Items, 1)
	assert.Equal(t, "Default", cfg.Workspaces.Items[0].Title)
}

func TestLoad_WorkspacesFile(t *testing.T) {
	content := `
server:
  port: 8080
  host: "127.0.0.1"
workspaces:
  active: ops
  tab_position: left
  items:
    - id: dev
      title: Dev
      layout:
        direction: horizontal
        children:
          - size: 100
            pane:
              id: dev-main
              type: local
    - id: ops
      title: Ops
      layout:
        direction: vertical
        children:
          - size: 100
            pane:
              id: ops-main
              type: local
`
	f := writeTempFile(t, content)
	cfg, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "ops", cfg.Workspaces.Active)
	assert.Equal(t, "left", cfg.Workspaces.TabPosition)
	assert.Equal(t, "vertical", cfg.ActiveLayout().Direction)
}

func TestLoad_WorkspacesTakePrecedenceOverLegacyLayout(t *testing.T) {
	content := `
server:
  port: 8080
  host: "127.0.0.1"
layout:
  direction: vertical
  children:
    - size: 100
      pane:
        id: legacy-main
        type: local
workspaces:
  active: dev
  tab_position: bottom
  items:
    - id: dev
      title: Dev
      layout:
        direction: horizontal
        children:
          - size: 100
            pane:
              id: dev-main
              type: local
`
	f := writeTempFile(t, content)
	cfg, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "bottom", cfg.Workspaces.TabPosition)
	assert.Equal(t, "horizontal", cfg.Layout.Direction)
	require.Len(t, cfg.AllPanes(), 1)
	assert.Equal(t, "dev-main", cfg.AllPanes()[0].ID)
}

func TestLoad_WorkspacesDefaultsActiveAndTabPosition(t *testing.T) {
	content := `
server:
  port: 8080
  host: "127.0.0.1"
workspaces:
  items:
    - id: first
      title: First
      layout:
        direction: vertical
        children:
          - size: 100
            pane:
              id: first-main
              type: local
`
	f := writeTempFile(t, content)
	cfg, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, "first", cfg.Workspaces.Active)
	assert.Equal(t, "top", cfg.Workspaces.TabPosition)
	assert.Equal(t, "vertical", cfg.ActiveLayout().Direction)
}

func TestLoad_TightensExistingFilePermissions(t *testing.T) {
	content := `
server:
  port: 8080
  host: "127.0.0.1"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
`
	f := writeTempFile(t, content)
	require.NoError(t, os.Chmod(f, 0644)) //nolint:gosec // G302: legacy config permission under test

	_, err := Load(f)
	require.NoError(t, err)

	info, err := os.Stat(f)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestLoad_ChmodFailureWarnsAndContinues(t *testing.T) {
	content := `
server:
  port: 8080
  host: "127.0.0.1"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
`
	f := writeTempFile(t, content)
	require.NoError(t, os.Chmod(f, 0644)) //nolint:gosec // G302: legacy config permission under test

	oldChmod := chmodConfigFile
	chmodConfigFile = func(string, os.FileMode) error {
		return errors.New("read-only filesystem")
	}
	t.Cleanup(func() { chmodConfigFile = oldChmod })

	var logs bytes.Buffer
	oldOutput := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(oldOutput) })

	cfg, err := Load(f)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Contains(t, logs.String(), "Warning: failed to tighten config file permissions")
}

func TestLoad_InvalidYAML(t *testing.T) {
	f := writeTempFile(t, "::invalid yaml::")
	_, err := Load(f)
	assert.Error(t, err)
}

func TestLoad_Nonexistent(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	assert.Error(t, err)
}

func TestAllPanes_FlatList(t *testing.T) {
	cfg := &Config{
		Layout: LayoutNode{
			Direction: "horizontal",
			Children: []LayoutChild{
				{Size: 50, Pane: &PaneConfig{ID: "p1", Type: "local"}},
				{
					Size:      50,
					Direction: "vertical",
					Children: []LayoutChild{
						{Size: 50, Pane: &PaneConfig{ID: "p2", Type: "local"}},
						{Size: 50, Pane: &PaneConfig{ID: "p3", Type: "local"}},
					},
				},
			},
		},
	}
	panes := cfg.AllPanes()
	assert.Len(t, panes, 3)
}

func TestAllPanes_IncludesAllWorkspaces(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active:      "one",
			TabPosition: "top",
			Items: []WorkspaceConfig{
				{
					ID:    "one",
					Title: "One",
					Layout: LayoutNode{
						Direction: "horizontal",
						Children:  []LayoutChild{{Size: 100, Pane: &PaneConfig{ID: "one-main", Type: "local"}}},
					},
				},
				{
					ID:    "two",
					Title: "Two",
					Layout: LayoutNode{
						Direction: "horizontal",
						Children:  []LayoutChild{{Size: 100, Pane: &PaneConfig{ID: "two-main", Type: "local"}}},
					},
				},
			},
		},
	}
	panes := cfg.AllPanes()
	require.Len(t, panes, 2)
	assert.Equal(t, "one-main", panes[0].ID)
	assert.Equal(t, "two-main", panes[1].ID)
}

func TestAllPanes_Empty(t *testing.T) {
	cfg := &Config{Layout: LayoutNode{Direction: "horizontal"}}
	panes := cfg.AllPanes()
	assert.Empty(t, panes)
}

func TestReadMethods_DoNotNormalizeConfigInPlace(t *testing.T) {
	cfg := validConfig()
	require.Empty(t, cfg.Workspaces.Items)

	view := cfg.WorkspacesView()
	require.Len(t, view.Items, 1)
	assert.Equal(t, "default", view.Active)
	assert.Empty(t, cfg.Workspaces.Items)
	assert.Empty(t, cfg.Workspaces.Active)

	panes := cfg.AllPanes()
	require.Len(t, panes, 1)
	assert.Equal(t, "main", panes[0].ID)
	assert.Empty(t, cfg.Workspaces.Items)

	require.NoError(t, cfg.Validate())
	assert.Empty(t, cfg.Workspaces.Items)
}

func TestAddDefaultWorkspace_CreatesUniqueLocalWorkspace(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active:      "default",
			TabPosition: "top",
			Items: []WorkspaceConfig{
				{ID: "default", Title: "Default", Layout: singlePaneLayout("local-main")},
				{ID: "workspace-2", Title: "Existing", Layout: singlePaneLayout("workspace-2-main")},
			},
		},
	}

	workspace := cfg.AddDefaultWorkspace()

	assert.Equal(t, "workspace-3", workspace.ID)
	assert.Equal(t, "Workspace 3", workspace.Title)
	assert.Equal(t, "workspace-3", cfg.Workspaces.Active)
	require.Len(t, workspace.Layout.Children, 1)
	assert.Equal(t, "workspace-3-main", workspace.Layout.Children[0].Pane.ID)
	assert.Equal(t, "local", workspace.Layout.Children[0].Pane.Type)
}

func TestRemoveWorkspace_RemovesTargetAndSelectsNextActive(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active:      "two",
			TabPosition: "top",
			Items: []WorkspaceConfig{
				{ID: "one", Title: "One", Layout: singlePaneLayout("one-main")},
				{ID: "two", Title: "Two", Layout: singlePaneLayout("two-main")},
				{ID: "three", Title: "Three", Layout: singlePaneLayout("three-main")},
			},
		},
	}

	removed, ok := cfg.RemoveWorkspace("two")

	require.True(t, ok)
	assert.Equal(t, "two", removed.ID)
	assert.Equal(t, "three", cfg.Workspaces.Active)
	assert.Equal(t, "three-main", cfg.Layout.Children[0].Pane.ID)
	require.Len(t, cfg.Workspaces.Items, 2)
	assert.Equal(t, "one", cfg.Workspaces.Items[0].ID)
	assert.Equal(t, "three", cfg.Workspaces.Items[1].ID)
}

func TestRemoveWorkspace_InactiveKeepsActive(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active:      "one",
			TabPosition: "top",
			Items: []WorkspaceConfig{
				{ID: "one", Title: "One", Layout: singlePaneLayout("one-main")},
				{ID: "two", Title: "Two", Layout: singlePaneLayout("two-main")},
			},
		},
	}

	_, ok := cfg.RemoveWorkspace("two")

	require.True(t, ok)
	assert.Equal(t, "one", cfg.Workspaces.Active)
	assert.Equal(t, "one-main", cfg.Layout.Children[0].Pane.ID)
	require.Len(t, cfg.Workspaces.Items, 1)
	assert.Equal(t, "one", cfg.Workspaces.Items[0].ID)
}

func TestRemoveWorkspace_NotFound(t *testing.T) {
	cfg := validConfig()
	_, ok := cfg.RemoveWorkspace("missing")
	assert.False(t, ok)
}

func TestSaveLayout_WithFile(t *testing.T) {
	content := `
server:
  port: 8080
  host: "127.0.0.1"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
`
	f := writeTempFile(t, content)
	cfg, err := Load(f)
	require.NoError(t, err)

	newLayout := LayoutNode{
		Direction: "vertical",
		Children: []LayoutChild{
			{Size: 100.0, Pane: &PaneConfig{ID: "main", Type: "local"}},
		},
	}
	require.NoError(t, cfg.SaveLayout(newLayout))

	data, err := os.ReadFile(f)
	require.NoError(t, err)
	assert.Contains(t, string(data), "vertical")
	assert.Contains(t, string(data), "workspaces:")
	assert.NotContains(t, string(data), "\nlayout:")
}

func TestSaveLayout_UpdatesOnlyActiveWorkspace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := &Config{
		Server: ServerConfig{Port: 8080, Host: "127.0.0.1"},
		Workspaces: WorkspacesConfig{
			Active:      "two",
			TabPosition: "right",
			Items: []WorkspaceConfig{
				{ID: "one", Title: "One", Layout: singlePaneLayout("one-main")},
				{ID: "two", Title: "Two", Layout: singlePaneLayout("two-main")},
			},
		},
		filePath: path,
	}

	newLayout := LayoutNode{
		Direction: "vertical",
		Children:  []LayoutChild{{Size: 100, Pane: &PaneConfig{ID: "two-main", Type: "local"}}},
	}
	require.NoError(t, cfg.SaveLayout(newLayout))

	assert.Equal(t, "horizontal", cfg.Workspaces.Items[0].Layout.Direction)
	assert.Equal(t, "vertical", cfg.Workspaces.Items[1].Layout.Direction)

	loaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "two", loaded.Workspaces.Active)
	assert.Equal(t, "right", loaded.Workspaces.TabPosition)
	assert.Equal(t, "horizontal", loaded.Workspaces.Items[0].Layout.Direction)
	assert.Equal(t, "vertical", loaded.Workspaces.Items[1].Layout.Direction)
}

func TestSaveLayout_NoFile_MemoryOnly(t *testing.T) {
	cfg := validConfig()
	// filePath is empty, SaveLayout should succeed without writing any file
	newLayout := LayoutNode{
		Direction: "vertical",
		Children: []LayoutChild{
			{Size: 100.0, Pane: &PaneConfig{ID: "main", Type: "local"}},
		},
	}
	err := cfg.SaveLayout(newLayout)
	assert.NoError(t, err)
	assert.Equal(t, "vertical", cfg.Layout.Direction)
}

func TestValidateLayout_Valid(t *testing.T) {
	node := LayoutNode{
		Direction: "horizontal",
		Children: []LayoutChild{
			{Size: 100.0, Pane: &PaneConfig{ID: "p1", Type: "local"}},
		},
	}
	err := ValidateLayout(node)
	assert.NoError(t, err)
}

func TestValidateLayout_Invalid(t *testing.T) {
	node := LayoutNode{
		Direction: "diagonal",
		Children: []LayoutChild{
			{Size: 100.0, Pane: &PaneConfig{ID: "p1", Type: "local"}},
		},
	}
	err := ValidateLayout(node)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "direction")
}

func TestValidate_TmuxPaneEmptyTmuxSession_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.Type = "tmux"
	cfg.Layout.Children[0].Pane.TmuxSession = ""
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tmux_session")
}

func TestExpandPaths_SSHKeyTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	content := `
server:
  port: 8080
  host: "127.0.0.1"
ssh_connections:
  myserver:
    host: 192.168.1.1
    port: 22
    user: user
    key_file: ~/.ssh/id_rsa
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
`
	f := writeTempFile(t, content)
	cfg, err := Load(f)
	require.NoError(t, err)

	conn, ok := cfg.SSHConnections["myserver"]
	require.True(t, ok)
	assert.Equal(t, filepath.Join(home, ".ssh/id_rsa"), conn.KeyFile)
	assert.False(t, strings.HasPrefix(conn.KeyFile, "~/"))
}

func TestExpandPanesCwd_Tilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	content := `
server:
  port: 8080
  host: "127.0.0.1"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
        cwd: ~/mydir
`
	f := writeTempFile(t, content)
	cfg, err := Load(f)
	require.NoError(t, err)

	require.Len(t, cfg.Layout.Children, 1)
	require.NotNil(t, cfg.Layout.Children[0].Pane)
	assert.Equal(t, filepath.Join(home, "mydir"), cfg.Layout.Children[0].Pane.Cwd)
	assert.False(t, strings.HasPrefix(cfg.Layout.Children[0].Pane.Cwd, "~/"))
}

func TestExpandPanesCwd_AllWorkspaces(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	content := `
server:
  port: 8080
  host: "127.0.0.1"
workspaces:
  active: one
  items:
    - id: one
      title: One
      layout:
        direction: horizontal
        children:
          - size: 100
            pane:
              id: one-main
              type: local
              cwd: ~/one
    - id: two
      title: Two
      layout:
        direction: horizontal
        children:
          - size: 100
            pane:
              id: two-main
              type: local
              cwd: ~/two
`
	f := writeTempFile(t, content)
	cfg, err := Load(f)
	require.NoError(t, err)

	assert.Equal(t, filepath.Join(home, "one"), cfg.Workspaces.Items[0].Layout.Children[0].Pane.Cwd)
	assert.Equal(t, filepath.Join(home, "two"), cfg.Workspaces.Items[1].Layout.Children[0].Pane.Cwd)
}

func TestValidate_LocalPaneShellRelativePath_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.Shell = "bash" // relative, not absolute
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "absolute path")
}

func TestValidate_LocalPaneShellAbsolutePath_NoError(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.Shell = "/bin/bash"
	assert.NoError(t, cfg.Validate())
}

func TestValidate_TmuxSessionInvalidChars_Error(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.Type = "tmux"
	cfg.Layout.Children[0].Pane.TmuxSession = "foo;bar"
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid characters")
}

func TestValidate_TmuxSessionValidChars_NoError(t *testing.T) {
	cfg := validConfig()
	cfg.Layout.Children[0].Pane.Type = "tmux"
	cfg.Layout.Children[0].Pane.TmuxSession = "my-session.1"
	assert.NoError(t, cfg.Validate())
}

func TestDefaultConfigPath_ContainsExpectedSuffix(t *testing.T) {
	path, err := DefaultConfigPath()
	require.NoError(t, err)
	assert.True(t, strings.HasSuffix(path, filepath.Join(".config", "panemux", "config.yaml")),
		"expected path to end with .config/panemux/config.yaml, got %s", path)
}

func TestLoadOrDefault_FileNotExist_ReturnsDefaultWithPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nonexistent", "config.yaml")
	cfg, err := loadOrDefaultAt(path)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, path, cfg.filePath)
}

func TestLoadOrDefault_FileExists_LoadsIt(t *testing.T) {
	content := `
server:
  port: 9999
  host: "127.0.0.1"
layout:
  direction: horizontal
  children:
    - size: 100
      pane:
        id: main
        type: local
`
	f := writeTempFile(t, content)
	cfg, err := loadOrDefaultAt(f)
	require.NoError(t, err)
	assert.Equal(t, 9999, cfg.Server.Port)
	assert.Equal(t, f, cfg.filePath)
}

func TestLoadOrDefault_FileExistsButInvalid_ReturnsError(t *testing.T) {
	f := writeTempFile(t, "::invalid yaml::")
	_, err := loadOrDefaultAt(f)
	assert.Error(t, err)
}

func TestSaveLayout_CreatesParentDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "dirs")
	path := filepath.Join(dir, "config.yaml")
	cfg := Default()
	cfg.filePath = path
	err := cfg.SaveLayout(cfg.Layout)
	require.NoError(t, err)
	info, statErr := os.Stat(path)
	require.NoError(t, statErr, "config file should have been created")
	assert.Equal(t, os.FileMode(0600), info.Mode().Perm())
}

func TestSaveLayout_NewFilePreservesYAMLKeyOrder(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg := Default()
	cfg.filePath = path

	require.NoError(t, cfg.SaveLayout(cfg.Layout))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(data)
	serverIndex := strings.Index(content, "server:")
	layoutIndex := strings.Index(content, "layout:")
	require.NotEqual(t, -1, serverIndex)
	require.NotEqual(t, -1, layoutIndex)
	assert.Less(t, serverIndex, layoutIndex)
}

func TestUpdateLayout_UpdatesMemoryOnly(t *testing.T) {
	cfg := &Config{}
	newLayout := LayoutNode{Direction: "horizontal", Children: []LayoutChild{{Size: 100}}}
	cfg.UpdateLayout(newLayout)
	if cfg.Layout.Direction != "horizontal" {
		t.Errorf("expected horizontal, got %s", cfg.Layout.Direction)
	}
	require.Len(t, cfg.Workspaces.Items, 1)
	assert.Equal(t, "horizontal", cfg.Workspaces.Items[0].Layout.Direction)
}

func TestWorkspaceLayoutMutationHelpers(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active:      "one",
			TabPosition: "top",
			Items: []WorkspaceConfig{
				{ID: "one", Title: "One", Layout: singlePaneLayout("one-main")},
				{ID: "two", Title: "Two", Layout: singlePaneLayout("two-main")},
			},
		},
	}
	cfg.normalizeWorkspaces()

	inactiveLayout := LayoutNode{
		Direction: "vertical",
		Children:  []LayoutChild{{Size: 100, Pane: &PaneConfig{ID: "two-main", Type: "local"}}},
	}
	assert.True(t, cfg.UpdateWorkspaceLayout("two", inactiveLayout))
	assert.Equal(t, "horizontal", cfg.Layout.Direction)
	assert.Equal(t, "vertical", cfg.Workspaces.Items[1].Layout.Direction)

	assert.True(t, cfg.SetActiveWorkspace("two"))
	assert.Equal(t, "two", cfg.ActiveWorkspaceID())
	assert.Equal(t, "vertical", cfg.Layout.Direction)

	assert.False(t, cfg.SetActiveWorkspace("missing"))
	assert.Equal(t, "two", cfg.ActiveWorkspaceID())
	assert.False(t, cfg.UpdateWorkspaceLayout("missing", singlePaneLayout("missing-main")))
}

func TestRemovePaneFromLayout_AllWorkspaces(t *testing.T) {
	cfg := &Config{
		Workspaces: WorkspacesConfig{
			Active:      "two",
			TabPosition: "top",
			Items: []WorkspaceConfig{
				{
					ID:    "one",
					Title: "One",
					Layout: LayoutNode{
						Direction: "horizontal",
						Children: []LayoutChild{
							{Size: 50, Pane: &PaneConfig{ID: "remove-one", Type: "local"}},
							{Size: 50, Pane: &PaneConfig{ID: "keep-one", Type: "local"}},
						},
					},
				},
				{
					ID:    "two",
					Title: "Two",
					Layout: LayoutNode{
						Direction: "horizontal",
						Children: []LayoutChild{
							{Size: 50, Pane: &PaneConfig{ID: "remove-two", Type: "local"}},
							{Size: 50, Pane: &PaneConfig{ID: "keep-two", Type: "local"}},
						},
					},
				},
			},
		},
	}

	cfg.RemovePaneFromLayout("remove-one")
	cfg.RemovePaneFromLayout("remove-two")

	require.Len(t, cfg.Workspaces.Items[0].Layout.Children, 1)
	assert.Equal(t, "keep-one", cfg.Workspaces.Items[0].Layout.Children[0].Pane.ID)
	require.Len(t, cfg.Workspaces.Items[1].Layout.Children, 1)
	assert.Equal(t, "keep-two", cfg.Workspaces.Items[1].Layout.Children[0].Pane.ID)
	assert.Equal(t, "keep-two", cfg.Layout.Children[0].Pane.ID)
}

func TestValidatePane_ShellOnSSH_AbsolutePathOK(t *testing.T) {
	p := &PaneConfig{ID: "p1", Type: "ssh", Connection: "host1", Shell: "/usr/bin/zsh"}
	errs := validatePane(p, map[string]SSHConnection{"host1": {Host: "host1.example.com"}})
	// Should not have a shell-related error
	for _, e := range errs {
		assert.NotContains(t, e, "shell must be an absolute path")
	}
}

func TestValidatePane_ShellOnSSH_RelativePath_Error(t *testing.T) {
	p := &PaneConfig{ID: "p1", Type: "ssh", Connection: "host1", Shell: "zsh"}
	errs := validatePane(p, map[string]SSHConnection{"host1": {Host: "host1.example.com"}})
	hasShellError := false
	for _, e := range errs {
		if strings.Contains(e, "shell must be an absolute path") {
			hasShellError = true
		}
	}
	assert.True(t, hasShellError)
}

// helpers

func validConfig() *Config {
	return &Config{
		Server: ServerConfig{Port: 8080, Host: "127.0.0.1"},
		Layout: LayoutNode{
			Direction: "horizontal",
			Children: []LayoutChild{
				{Size: 100.0, Pane: &PaneConfig{ID: "main", Type: "local"}},
			},
		},
	}
}

func singlePaneLayout(id string) LayoutNode {
	return LayoutNode{
		Direction: "horizontal",
		Children:  []LayoutChild{{Size: 100, Pane: &PaneConfig{ID: id, Type: "local"}}},
	}
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte(content), 0600))
	return f
}
