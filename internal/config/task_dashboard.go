package config

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"unicode"
)

// defaultTaskDashboardShortcut is the task dashboard's layer-switching key
// when display.task_dashboard_shortcut is not set.
const defaultTaskDashboardShortcut = "S"

// reservedShortcutKeys are the Cmd/Ctrl+Shift letters panemux already binds:
// K opens the command center palette and B the Agent Board dashboard.
var reservedShortcutKeys = map[string]string{
	"K": "the command center palette",
	"B": "the Agent Board dashboard",
}

var shortcutLetterRe = regexp.MustCompile(`^[A-Za-z]$`)

// TaskDashboardShortcutKey is the effective layer-switching letter, in upper
// case. An unset value stays unset in the file, so an unrelated save never
// writes a key the operator did not choose.
func (d DisplayConfig) TaskDashboardShortcutKey() string {
	if d.TaskDashboardShortcut == "" {
		return defaultTaskDashboardShortcut
	}
	return strings.ToUpper(d.TaskDashboardShortcut)
}

func validateDisplay(d DisplayConfig) []string {
	if d.TaskDashboardShortcut == "" {
		return nil
	}
	if !shortcutLetterRe.MatchString(d.TaskDashboardShortcut) {
		return []string{fmt.Sprintf(
			"display.task_dashboard_shortcut %q must be a single letter A-Z", d.TaskDashboardShortcut)}
	}
	if owner, taken := reservedShortcutKeys[strings.ToUpper(d.TaskDashboardShortcut)]; taken {
		return []string{fmt.Sprintf(
			"display.task_dashboard_shortcut %q is already used by %s", d.TaskDashboardShortcut, owner)}
	}
	return nil
}

// TaskDashboardConfig holds the top-level task_dashboard settings.
type TaskDashboardConfig struct {
	// JiraURL is the Jira site a task's Jira keys link into, as
	// <JiraURL>/browse/<key>. Empty means no Jira links.
	JiraURL string `yaml:"jira_url,omitempty" json:"jira_url,omitempty"`
}

// JiraBrowseURL is the page of the Jira issue key on the configured site, or
// "" when no site is configured.
func (d TaskDashboardConfig) JiraBrowseURL(key string) string {
	if d.JiraURL == "" {
		return ""
	}
	return strings.TrimRight(d.JiraURL, "/") + "/browse/" + key
}

// validateTaskDashboard accepts only an absolute https URL with a host and
// nothing a /browse/<key> suffix could not be appended to: no query,
// fragment, credentials, whitespace or control characters. The URL becomes
// the href of a link the dashboard opens, so nothing but https reaches it.
func validateTaskDashboard(d TaskDashboardConfig) []string {
	if d.JiraURL == "" {
		return nil
	}
	invalid := []string{fmt.Sprintf(
		"task_dashboard.jira_url %q must be an https URL with a host and no query, fragment or credentials",
		d.JiraURL)}
	if strings.IndexFunc(d.JiraURL, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return invalid
	}
	u, err := url.Parse(d.JiraURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.HasSuffix(d.JiraURL, "#") {

		return invalid
	}
	return nil
}
