package config

import (
	"fmt"
	"regexp"
	"strings"
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
