package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
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
//
// The host and port are held to what the browser's URL parser (the WHATWG
// one behind the frontend's HttpUrlSchema) also accepts, since the browser
// rejects the whole GET /api/tasks response for one link it cannot parse.
// testdata/jira-url-validation.json is checked by both sides.
func validateTaskDashboard(d TaskDashboardConfig) []string {
	if d.JiraURL == "" {
		return nil
	}
	invalid := []string{fmt.Sprintf(
		"task_dashboard.jira_url %q must be an https URL with a valid host and port, "+
			"and no query, fragment or credentials",
		d.JiraURL)}
	if strings.IndexFunc(d.JiraURL, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 {
		return invalid
	}
	u, err := url.Parse(d.JiraURL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.HasSuffix(d.JiraURL, "#") {

		return invalid
	}
	if !validJiraHost(u.Hostname()) || !validJiraPort(u.Port()) {
		return invalid
	}
	return nil
}

// validJiraHost is an IP address, or dot-separated labels of ASCII letters,
// digits and hyphens. It is narrower than a browser allows, on purpose:
//   - a label starting "xn--" is punycode, which a browser decodes and rejects
//     when that fails; panemux has no decoder to check it with, so
//     internationalized hosts are refused rather than guessed at;
//   - a last label that is a number ("123", "0x1") makes a browser parse the
//     whole host as an IPv4 address, so such a host must be one.
func validJiraHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	labels := strings.Split(host, ".")
	for _, label := range labels {
		if label == "" || strings.HasPrefix(strings.ToLower(label), "xn--") {
			return false
		}
		if strings.IndexFunc(label, isNotHostLabelRune) >= 0 {
			return false
		}
	}
	return !isNumericHostLabel(labels[len(labels)-1])
}

func isNotHostLabelRune(r rune) bool {
	return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && r != '-'
}

// isNumericHostLabel reports whether the WHATWG URL parser reads label as a
// number: decimal digits, or "0x"/"0X" followed by hex digits (possibly none).
// validJiraHost never passes it an empty label.
func isNumericHostLabel(label string) bool {
	digits, base := label, "0123456789"
	if len(label) >= 2 && label[0] == '0' && (label[1] == 'x' || label[1] == 'X') {
		digits, base = label[2:], "0123456789abcdefABCDEF"
	}
	for _, r := range digits {
		if !strings.ContainsRune(base, r) {
			return false
		}
	}
	return true
}

// validJiraPort is no port, or a TCP port 1-65535. url.Parse has already
// refused anything but digits.
func validJiraPort(port string) bool {
	if port == "" {
		return true
	}
	n, err := strconv.Atoi(port)
	return err == nil && n >= 1 && n <= 65535
}
