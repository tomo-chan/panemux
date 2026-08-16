package board

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TestedAgmsgVersion is the agmsg release panemux's integration was checked
// against: the script argument shapes in
// docs/agent-board.md#integration-with-agmsg were read from this version's
// own source, not inferred.
//
// docs/agent-board.md's "Version pinning" section requires a pinned tested
// version because agmsg's own compatibility promise covers reading through
// scripts/api.sh only. Everything panemux's write and bootstrap paths
// depend on — send.sh's argument order, join.sh, delivery.sh set, and the
// per-type hooks_file layout — carries no such promise, so an agmsg upgrade
// can change them without that being a bug on agmsg's side.
const TestedAgmsgVersion = "1.2.0"

// agmsgVersionFile is agmsg's own version marker at its install root.
const agmsgVersionFile = "VERSION"

// readVersion normalizes a VERSION file's contents. An absent or
// whitespace-only file reads as "" — unknown, not an error: agmsg installs
// predating the file exist, and panemux must not treat an install it cannot
// version as a broken one.
func readVersion(raw []byte) string {
	return strings.TrimSpace(string(raw))
}

// LocalAgmsgVersion reads agmsg's VERSION from a local install root
// (already ~-expanded). Returns "" when the file is absent or empty; an
// error only when the file exists but cannot be read, which is a real
// problem worth surfacing rather than silently treating as unknown.
func LocalAgmsgVersion(agmsgPath string) (string, error) {
	path := filepath.Join(agmsgPath, agmsgVersionFile)
	// G304: operator-configured agmsg install path, same trust level as config.yaml.
	raw, err := os.ReadFile(path) //nolint:gosec
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("agmsg: reading %s: %w", path, err)
	}
	return readVersion(raw), nil
}

// remoteAgmsgVersionProbeScript mirrors remoteAgmsgPresenceProbeScript's
// shape: fixed script text, with the only variable input arriving as a
// positional parameter rather than interpolated into the script. It always
// exits 0 and prints nothing when the file is absent, so a missing VERSION
// is distinguishable from a transport failure (which surfaces as a non-nil
// error from RunBoardCommand) rather than being conflated with it.
const remoteAgmsgVersionProbeScript = `cat "$1" 2>/dev/null || true`
const remoteAgmsgVersionProbeScriptName = "board-agmsg-version"

// RemoteAgmsgVersion reads agmsg's VERSION from an install root on the host
// reached through executor. Returns "" when absent, matching
// LocalAgmsgVersion.
func RemoteAgmsgVersion(ctx context.Context, executor BoardExecutor, agmsgPath string) (string, error) {
	versionPath := agmsgPath + "/" + agmsgVersionFile
	args := []string{
		"sh", "-c", remoteAgmsgVersionProbeScript, remoteAgmsgVersionProbeScriptName, versionPath,
	}
	out, err := executor.RunBoardCommand(ctx, args)
	if err != nil {
		return "", fmt.Errorf("agmsg: reading remote %s: %w", versionPath, err)
	}
	return readVersion(out), nil
}

// VersionMismatchWarning returns the operator-facing warning for an agmsg
// install that is not the tested version, or "" when there is nothing to
// warn about.
//
// A mismatch is deliberately a warning and never a failure. Agent Board is
// additive, never load-bearing (see docs/agent-board.md's Design
// principles), and refusing to run against an untested agmsg would turn a
// possible incompatibility into a certain outage. What this buys is the
// difference between a pane that silently stops communicating and a log
// line naming the version that changed — the failure mode
// docs/agent-board.md's compatibility contract exists to avoid.
func VersionMismatchWarning(host, installed string) string {
	if installed == "" || installed == TestedAgmsgVersion {
		return ""
	}
	return fmt.Sprintf(
		"agent board: host %q has agmsg %s, but panemux was tested against %s; "+
			"board messaging may misbehave if agmsg's script interface changed",
		host, installed, TestedAgmsgVersion,
	)
}
