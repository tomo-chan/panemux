package config

import (
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
	err := cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "connection")
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

func TestAllPanes_Empty(t *testing.T) {
	cfg := &Config{Layout: LayoutNode{Direction: "horizontal"}}
	panes := cfg.AllPanes()
	assert.Empty(t, panes)
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

func TestUpdateLayout_UpdatesMemoryOnly(t *testing.T) {
	cfg := &Config{}
	newLayout := LayoutNode{Direction: "horizontal", Children: []LayoutChild{{Size: 100}}}
	cfg.UpdateLayout(newLayout)
	if cfg.Layout.Direction != "horizontal" {
		t.Errorf("expected horizontal, got %s", cfg.Layout.Direction)
	}
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

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	f := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(f, []byte(content), 0644))
	return f
}
