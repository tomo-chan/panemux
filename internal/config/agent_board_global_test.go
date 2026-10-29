package config

import (
	"github.com/stretchr/testify/require"
	"os"
	"path/filepath"
	"testing"
)

func TestGlobalBoardConfiguration(t *testing.T) {
	for _, mode := range []string{"monitor", "turn", "both", "off", "invalid"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			content := "server:\n  port: 8080\nagent_board:\n  enabled: true\n  mode: " + mode + "\n  agmsg_auto_update: true\n"
			require.NoError(t, os.WriteFile(path, []byte(content), 0600))
			cfg, err := Load(path)
			if mode == "invalid" {
				require.ErrorContains(t, err, "agent_board.mode")
				return
			}
			require.NoError(t, err)
			require.True(t, cfg.AgentBoard.Enabled)
			require.Equal(t, mode, cfg.AgentBoard.Mode)
			require.True(t, cfg.AgentBoard.AgmsgAutoUpdate)
		})
	}
	cfg := Default()
	require.False(t, cfg.AgentBoard.Enabled)
	require.Equal(t, "monitor", cfg.AgentBoard.Mode)
	require.False(t, cfg.AgentBoard.AgmsgAutoUpdate)
}

func TestRemovedBoardSettingsFailWithMigrationHelp(t *testing.T) {
	for _, input := range []string{
		"agent_board:\n  agmsg_path: /tmp/old-agmsg\n",
		"panes:\n  - id: shell\n    type: local\n    agent_board:\n      enabled: true\n",
		`workspaces:
  items:
    - id: work
      layout:
        direction: horizontal
        children:
          - pane:
              id: shell
              type: local
              agent_board:
                mode: turn
`,
	} {
		t.Run(input, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(input), 0600))
			_, err := Load(path)
			require.ErrorContains(t, err, "removed")
			require.ErrorContains(t, err, "agent_board")
		})
	}
}

func TestGlobalBoardSettingsPersistOnUnrelatedLayoutSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  port: 8080\nagent_board:\n  enabled: true\n  mode: both\n  agmsg_auto_update: true\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.NoError(t, cfg.SaveLayout(cfg.Layout))
	again, err := Load(path)
	require.NoError(t, err)
	require.Equal(t, cfg.AgentBoard, again.AgentBoard)
}

func TestBoardMigrationDoesNotConfuseSSHHostNames(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	content := "server:\n  port: 8080\nssh_connections:\n  agent_board:\n    host: example.invalid\n"
	require.NoError(t, os.WriteFile(path, []byte(content), 0600))
	_, err := Load(path)
	require.NoError(t, err)
}

func TestRemovedBoardSettingsInYAMLAliasesAndMerges(t *testing.T) {
	for _, content := range []string{
		"defaults: &old\n  agmsg_path: /tmp/agmsg\nagent_board: *old\n",
		"defaults: &old\n  agent_board:\n    agmsg_path: /tmp/agmsg\n<<: *old\n",
		`defaults: &old
  id: shell
  agent_board:
    enabled: false
layout:
  children:
    - pane:
        <<: *old
`,
	} {
		require.ErrorContains(t, rejectRemovedBoardSettings([]byte(content)), "removed")
	}
}
