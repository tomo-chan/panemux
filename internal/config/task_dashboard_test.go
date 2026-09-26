package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTaskDashboardShortcutKey(t *testing.T) {
	tests := map[string]string{
		"":  "S",
		"s": "S",
		"S": "S",
		"j": "J",
	}
	for configured, want := range tests {
		d := DisplayConfig{TaskDashboardShortcut: configured}
		assert.Equal(t, want, d.TaskDashboardShortcutKey(), "configured %q", configured)
	}
}

func TestValidate_TaskDashboardShortcut(t *testing.T) {
	tests := []struct {
		value string
		ok    bool
	}{
		{"", true},
		{"S", true},
		{"s", true},
		{"Z", true},
		{"K", false}, // the command palette's key
		{"b", false}, // the Agent Board dashboard's key
		{"SS", false},
		{"1", false},
		{" ", false},
		{"ß", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			cfg := validConfig()
			cfg.Display.TaskDashboardShortcut = tt.value
			err := cfg.Validate()
			if tt.ok {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "display.task_dashboard_shortcut")
		})
	}
}

// An operator who never set the key must not find one written into their
// file by an unrelated save; one who did must keep it.
func TestSaveLayout_TaskDashboardShortcutIsWrittenOnlyWhenSet(t *testing.T) {
	unset := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(unset, []byte("server:\n  port: 8080\n"), 0o600))
	cfg, err := Load(unset)
	require.NoError(t, err)
	require.NoError(t, cfg.SaveLayout(cfg.Layout))
	saved, err := os.ReadFile(unset)
	require.NoError(t, err)
	assert.NotContains(t, string(saved), "task_dashboard_shortcut")

	set := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(set,
		[]byte("server:\n  port: 8080\ndisplay:\n  show_header: true\n  task_dashboard_shortcut: j\n"), 0o600))
	cfg, err = Load(set)
	require.NoError(t, err)
	require.NoError(t, cfg.SaveLayout(cfg.Layout))
	reloaded, err := Load(set)
	require.NoError(t, err)
	assert.Equal(t, "J", reloaded.Display.TaskDashboardShortcutKey())
}

// Summaries send a host's conversation text to claude on the panemux host,
// so they are off unless the operator turns them on, and an unrelated save
// neither turns them on nor writes the key.
func TestTaskDashboardSummary_IsOffUnlessEnabled(t *testing.T) {
	unset := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(unset, []byte("server:\n  port: 8080\n"), 0o600))
	cfg, err := Load(unset)
	require.NoError(t, err)
	assert.False(t, cfg.TaskDashboard.Summary.Enabled)
	assert.False(t, Default().TaskDashboard.Summary.Enabled)
	require.NoError(t, cfg.SaveLayout(cfg.Layout))
	saved, err := os.ReadFile(unset)
	require.NoError(t, err)
	assert.NotContains(t, string(saved), "task_dashboard")

	set := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(set,
		[]byte("server:\n  port: 8080\ntask_dashboard:\n  summary:\n    enabled: true\n"), 0o600))
	cfg, err = Load(set)
	require.NoError(t, err)
	assert.True(t, cfg.TaskDashboard.Summary.Enabled)
	require.NoError(t, cfg.SaveLayout(cfg.Layout))
	reloaded, err := Load(set)
	require.NoError(t, err)
	assert.True(t, reloaded.TaskDashboard.Summary.Enabled, "an enabled summary survives a save")
}
