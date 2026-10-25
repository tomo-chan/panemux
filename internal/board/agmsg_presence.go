package board

import (
	"context"
	"fmt"
	"panemux/internal/host"
	"strings"
)

// remoteAgmsgPresenceProbeScript is a fixed, non-tainted script that checks
// whether $1 exists, mirroring sendBase64WrapperScript's shape (fixed
// script text, tainted-but-quoted data arrives only via a positional
// parameter, never interpolated into the script itself). It always exits 0
// — the caller reads "yes"/"no" from stdout rather than the exit code, the
// same convention remoteHomeProbeCmd's caller uses, so a real
// transport/exec failure (a non-nil error from Runner.Run) is never
// confused with "the file doesn't exist".
const remoteAgmsgPresenceProbeScript = `test -f "$1" && printf 'yes' || printf 'no'`
const remoteAgmsgPresenceProbeScriptName = "board-agmsg-presence"

// RemoteAgmsgPresent reports whether scripts/api.sh exists under agmsgPath
// (already ~-expanded to an absolute path) on the remote host reached
// through executor.
func RemoteAgmsgPresent(ctx context.Context, executor host.Runner, agmsgPath string) (bool, error) {
	apiScript := agmsgPath + "/scripts/api.sh"
	args := []string{
		"sh", "-c", remoteAgmsgPresenceProbeScript, remoteAgmsgPresenceProbeScriptName, apiScript,
	}
	out, err := executor.Run(ctx, execArguments, args...)
	if err != nil {
		return false, fmt.Errorf("agmsg: checking remote scripts/api.sh presence: %w", err)
	}
	return strings.TrimSpace(string(out)) == "yes", nil
}
