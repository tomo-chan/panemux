package config

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAgentBoardModeWarnsOnceWhenLoaded(t *testing.T) {
	for _, mode := range []string{"monitor", "turn", "both", "off", "", "omitted"} {
		t.Run(mode, func(t *testing.T) {
			var output bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&output)
			t.Cleanup(func() { log.SetOutput(previous) })
			input := "server:\n  port: 8080\nagent_board:\n  enabled: true\n"
			if mode != "omitted" {
				input += "  mode: \"" + mode + "\"\n"
			}
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(input), 0600))
			cfg, err := Load(path)
			require.NoError(t, err)
			require.NoError(t, cfg.Validate())
			want := 1
			if mode == "omitted" {
				want = 0
			}
			require.Equal(t, want, strings.Count(output.String(), "agent_board.mode is deprecated and ignored"))
		})
	}
}

func TestSavingConfigDoesNotPersistLegacyBoardMode(t *testing.T) {
	for _, input := range []string{
		"server:\n  port: 8080\n",
		"server:\n  port: 8080\nagent_board:\n  enabled: true\n",
		"server:\n  port: 8080\nagent_board:\n  mode: both\n",
	} {
		t.Run(input, func(t *testing.T) {
			var output bytes.Buffer
			previous := log.Writer()
			log.SetOutput(&output)
			t.Cleanup(func() { log.SetOutput(previous) })
			path := filepath.Join(t.TempDir(), "config.yaml")
			require.NoError(t, os.WriteFile(path, []byte(input), 0600))
			cfg, err := Load(path)
			require.NoError(t, err)
			originalMode := cfg.AgentBoard.Mode
			require.NotEmpty(t, originalMode)
			require.NoError(t, cfg.SaveLayout(cfg.Layout))
			require.Equal(t, originalMode, cfg.AgentBoard.Mode, "saving must not change the API's in-memory mode")
			saved, err := os.ReadFile(path)
			require.NoError(t, err)
			require.NotContains(t, string(saved), "mode:")
			output.Reset()
			again, err := Load(path)
			require.NoError(t, err)
			require.Equal(t, "monitor", again.AgentBoard.Mode)
			require.NotContains(t, output.String(), "deprecated and ignored")
		})
	}
}

func TestPaneBoardMigrationDoesNotRecommendDeprecatedMode(t *testing.T) {
	err := rejectRemovedBoardSettings([]byte("panes:\n  - id: pane-a\n    agent_board:\n      enabled: true\n"))
	require.ErrorContains(t, err, "agent_board.enabled")
	require.NotContains(t, err.Error(), "agent_board.mode")
}
