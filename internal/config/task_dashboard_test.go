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

// autolinkURLCases is testdata/autolink-url-validation.json, which the
// frontend's schema test reads too: every template accepted here has to give
// a URL the browser's HttpUrlSchema accepts, or one setting would make the
// browser reject the whole GET /api/tasks response.
type autolinkURLCases struct {
	Accepted []struct {
		URLTemplate string `json:"url_template"`
		Num         string `json:"num"`
		URL         string `json:"url"`
	} `json:"accepted"`
	Refused []string `json:"refused"`
}

func readAutolinkURLCases(t *testing.T) autolinkURLCases {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "autolink-url-validation.json"))
	require.NoError(t, err)
	var cases autolinkURLCases
	require.NoError(t, json.Unmarshal(raw, &cases))
	require.NotEmpty(t, cases.Accepted)
	require.NotEmpty(t, cases.Refused)
	return cases
}

func TestValidate_TaskDashboardAutolinkURLTemplate(t *testing.T) {
	cases := readAutolinkURLCases(t)
	for _, tc := range cases.Accepted {
		t.Run(tc.URLTemplate, func(t *testing.T) {
			link := AutolinkConfig{KeyPrefix: "JIRA-", URLTemplate: tc.URLTemplate}
			cfg := validConfig()
			cfg.TaskDashboard.Autolinks = []AutolinkConfig{link}
			require.NoError(t, cfg.Validate())
			assert.Equal(t, tc.URL, link.URL(tc.Num))
		})
	}
	for _, template := range cases.Refused {
		t.Run(template, func(t *testing.T) {
			cfg := validConfig()
			cfg.TaskDashboard.Autolinks = []AutolinkConfig{{KeyPrefix: "JIRA-", URLTemplate: template}}
			err := cfg.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), "task_dashboard.autolinks[0].url_template")
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

func TestValidate_TaskDashboardAutolinkKeyPrefix(t *testing.T) {
	const template = "https://jira.example.com/<num>"
	tests := []struct {
		name     string
		wantErr  string
		prefixes []string
	}{
		{"one", "", []string{"JIRA-"}},
		{"several", "", []string{"JIRA-", "OPS-", "SUPPORT-"}},
		{"no dash", "", []string{"TICKET"}},
		{"empty", "task_dashboard.autolinks[0].key_prefix", []string{""}},
		{"space", "task_dashboard.autolinks[0].key_prefix", []string{"JIRA -"}},
		{"control character", "task_dashboard.autolinks[0].key_prefix", []string{"JIRA-\t"}},
		{"overlapping, as GitHub refuses", "task_dashboard.autolinks[1].key_prefix", []string{"TICKET", "TICK"}},
		{"overlapping the other way", "task_dashboard.autolinks[1].key_prefix", []string{"TICK", "TICKET"}},
		{"the same twice", "task_dashboard.autolinks[2].key_prefix", []string{"JIRA-", "OPS-", "JIRA-"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validConfig()
			for _, prefix := range tt.prefixes {
				cfg.TaskDashboard.Autolinks = append(cfg.TaskDashboard.Autolinks,
					AutolinkConfig{KeyPrefix: prefix, URLTemplate: template})
			}
			err := cfg.Validate()
			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

func TestAutolinkURL_ReplacesEveryPlaceholder(t *testing.T) {
	link := AutolinkConfig{KeyPrefix: "JIRA-", URLTemplate: "https://jira.example.com/<num>?k=JIRA-<num>"}
	assert.Equal(t, "https://jira.example.com/123?k=JIRA-123", link.URL("123"))
}

// The example from GitHub's own autolink settings page, written the way
// config.yaml carries it; is_alphanumeric defaults to false (numeric).
func TestLoad_ReadsTaskDashboardAutolinks(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server:\n  port: 8080\ntask_dashboard:\n  autolinks:\n"+
		"    - key_prefix: JIRA-\n      url_template: https://jira.example.com/JIRA-<num>\n"+
		"    - key_prefix: TICKET-\n      url_template: https://tickets.example.com/<num>\n"+
		"      is_alphanumeric: true\n"), 0o600))
	cfg, err := Load(path)
	require.NoError(t, err)
	want := []AutolinkConfig{
		{KeyPrefix: "JIRA-", URLTemplate: "https://jira.example.com/JIRA-<num>"},
		{KeyPrefix: "TICKET-", URLTemplate: "https://tickets.example.com/<num>", IsAlphanumeric: true},
	}
	assert.Equal(t, want, cfg.TaskDashboard.Autolinks)

	require.NoError(t, cfg.SaveLayout(cfg.Layout))
	reloaded, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, want, reloaded.TaskDashboard.Autolinks)
}

// An operator who never set autolinks must not find a task_dashboard block
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
