package session

import (
	"regexp"
	"slices"
	"strings"
)

// paneIDEnvName is the environment variable a local or SSH pane's shell is
// started with, naming the pane. Every process started from that shell
// inherits it, which is how the task dashboard finds the pane an agent
// running outside tmux belongs to (issue #254). tmux panes do not set it:
// the dashboard finds those through tmux itself.
const paneIDEnvName = "PANEMUX_PANE_ID"

// validPaneEnvID is the pane ID a shell is told about. It matches
// internal/tasks's validPaneID, the check the dashboard's collection applies
// to the value it reads back, so a pane is never given an ID the dashboard
// would discard. A pane whose ID does not match gets no variable at all. The
// charset also keeps the value inert in the remote command it is quoted into.
var validPaneEnvID = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,128}$`)

// paneIDEnv returns base with the pane's ID set. An ID inherited from the
// environment panemux itself was started in (panemux run inside one of its
// own panes) is always removed, so a pane never carries another pane's ID.
func paneIDEnv(base []string, id string) []string {
	env := slices.DeleteFunc(slices.Clone(base), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return name == paneIDEnvName
	})
	if validPaneEnvID.MatchString(id) {
		env = append(env, paneIDEnvName+"="+id)
	}
	return env
}

// remotePaneIDSetup returns the shell snippet that exports the pane's ID on
// an SSH host, or "" when the ID is not one a shell is told about. The ID has
// already been limited to validPaneEnvID and is quoted as well.
func remotePaneIDSetup(id string) string {
	if !validPaneEnvID.MatchString(id) {
		return ""
	}
	return paneIDEnvName + "=" + shellQuotePath(id) + "; export " + paneIDEnvName + "; "
}
