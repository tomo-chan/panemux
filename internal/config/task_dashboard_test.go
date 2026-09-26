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

func TestValidate_TaskDashboardJiraURL(t *testing.T) {
	tests := []struct {
		value string
		ok    bool
	}{
		{"", true},
		{"https://example.atlassian.net", true},
		{"https://example.atlassian.net/", true},
		{"https://jira.example.invalid/jira", true},
		{"https://jira.example.invalid:8443", true},
		{"http://jira.example.invalid", false},
		{"javascript:alert(1)", false},
		{"example.atlassian.net", false},
		{"https://", false},
		{"https:///browse", false},
		{"https://jira.example.invalid/?a=1", false},
		{"https://jira.example.invalid/#top", false},
		{"https://jira.example.invalid/?", false},
		{"https://jira.example.invalid/#", false},
		{"https://user:pass@jira.example.invalid", false},
		{"https://jira.example.invalid/a b", false},
		{"https://jira.example.invalid/\n", false},
		{" https://jira.example.invalid", false},
	}
	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			cfg := validConfig()
			cfg.TaskDashboard.JiraURL = tt.value
			err := cfg.Validate()
			if tt.ok {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), "task_dashboard.jira_url")
		})
	}
}

func TestJiraBrowseURL(t *testing.T) {
	tests := []struct {
		base, key, want string
	}{
		{"", "PAY-418", ""},
		{"https://example.atlassian.net", "PAY-418", "https://example.atlassian.net/browse/PAY-418"},
		{"https://example.atlassian.net/", "PAY-418", "https://example.atlassian.net/browse/PAY-418"},
		{"https://jira.example.invalid/jira//", "OPS-7", "https://jira.example.invalid/jira/browse/OPS-7"},
	}
	for _, tt := range tests {
		d := TaskDashboardConfig{JiraURL: tt.base}
		assert.Equal(t, tt.want, d.JiraBrowseURL(tt.key), "base %q", tt.base)
	}
}

func TestLoad_ReadsTaskDashboardJiraURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path,
		[]byte("server:\n  port: 8080\ntask_dashboard:\n  jira_url: https://example.atlassian.net\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "https://example.atlassian.net", cfg.TaskDashboard.JiraURL)

	require.NoError(t, cfg.SaveLayout(cfg.Layout))
	reloaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, "https://example.atlassian.net", reloaded.TaskDashboard.JiraURL)
}

// An operator who never set a Jira site must not find a task_dashboard block
// written into their file by an unrelated save.
func TestSaveLayout_TaskDashboardIsWrittenOnlyWhenSet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8080\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	require.NoError(t, cfg.SaveLayout(cfg.Layout))
	saved, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotContains(t, string(saved), "task_dashboard:")
}
