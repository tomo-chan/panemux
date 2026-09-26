package config

import (
	"encoding/json"
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

// jiraURLCases is testdata/jira-url-validation.json, which the frontend's
// schema test reads too: every site accepted here has to produce a browse
// URL the browser's HttpUrlSchema accepts, or one setting would make the
// browser reject the whole GET /api/tasks response.
type jiraURLCases struct {
	Accepted []struct {
		JiraURL   string `json:"jira_url"`
		BrowseURL string `json:"browse_url"`
	} `json:"accepted"`
	Refused []string `json:"refused"`
}

func readJiraURLCases(t *testing.T) jiraURLCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "jira-url-validation.json"))
	require.NoError(t, err)
	var cases jiraURLCases
	require.NoError(t, json.Unmarshal(raw, &cases))
	require.NotEmpty(t, cases.Accepted)
	require.NotEmpty(t, cases.Refused)
	return cases
}

func TestValidate_TaskDashboardJiraURL(t *testing.T) {
	cases := readJiraURLCases(t)
	for _, tc := range cases.Accepted {
		t.Run(tc.JiraURL, func(t *testing.T) {
			cfg := validConfig()
			cfg.TaskDashboard.JiraURL = tc.JiraURL
			require.NoError(t, cfg.Validate())
			assert.Equal(t, tc.BrowseURL, cfg.TaskDashboard.JiraBrowseURL("PAY-418"))
		})
	}
	for _, value := range cases.Refused {
		t.Run(value, func(t *testing.T) {
			cfg := validConfig()
			cfg.TaskDashboard.JiraURL = value
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "task_dashboard.jira_url")
		})
	}
}

// The edges of each range a host label may use, and the byte on either side.
func TestIsNotHostLabelRune(t *testing.T) {
	for _, r := range "azAZ09-" {
		assert.False(t, isNotHostLabelRune(r), "%q", r)
	}
	for _, r := range "`{@[/:_.é" {
		assert.True(t, isNotHostLabelRune(r), "%q", r)
	}
}

func TestJiraBrowseURL(t *testing.T) {
	assert.Empty(t, TaskDashboardConfig{}.JiraBrowseURL("PAY-418"))
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
