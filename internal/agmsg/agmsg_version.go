package agmsg

import (
	"context"
	"fmt"
	"panemux/internal/host"
	"strconv"
	"strings"
)

// TestedAgmsgVersion is the agmsg release panemux's integration was checked
// against: the script argument shapes in
// docs/agent-board/agmsg-integration.md#integration-with-agmsg were read from this version's
// own source, not inferred.
//
// docs/agent-board/agmsg-integration.md's "Version coverage" section
// requires a pinned tested version because agmsg's own compatibility promise
// covers reading through scripts/api.sh only. Everything panemux's write and
// bootstrap paths depend on — send.sh's argument order, join.sh, delivery.sh
// set, and the per-type hooks_file layout — carries no such promise, so an
// agmsg upgrade can change them without that being a bug on agmsg's side.
const TestedAgmsgVersion = "1.7.0"

// agmsgVersionFile is agmsg's own version marker at its install root.
const agmsgVersionFile = "VERSION"

// readVersion normalizes a VERSION file's contents. An absent or
// whitespace-only file reads as "" — unknown, not an error: agmsg installs
// predating the file exist, and panemux must not treat an install it cannot
// version as a broken one.
func readVersion(raw []byte) string {
	return strings.TrimSpace(string(raw))
}

// remoteAgmsgVersionProbeScript mirrors remoteAgmsgPresenceProbeScript's
// shape: fixed script text, with the only variable input arriving as a
// positional parameter rather than interpolated into the script. It always
// exits 0 and prints nothing when the file is absent, so a missing VERSION
// is distinguishable from a transport failure (which surfaces as a non-nil
// error from Runner.Run) rather than being conflated with it.
const remoteAgmsgVersionProbeScript = `cat "$1" 2>/dev/null || true`
const remoteAgmsgVersionProbeScriptName = "board-agmsg-version"

// RemoteAgmsgVersion reads agmsg's VERSION from an install root on the host
// reached through executor. Returns "" when absent, matching
// the local execution path.
func RemoteAgmsgVersion(ctx context.Context, executor host.Runner, agmsgPath string) (string, error) {
	versionPath := agmsgPath + "/" + agmsgVersionFile
	args := []string{
		"sh", "-c", remoteAgmsgVersionProbeScript, remoteAgmsgVersionProbeScriptName, versionPath,
	}
	out, err := executor.Run(ctx, execArguments, args...)
	if err != nil {
		return "", fmt.Errorf("agmsg: reading remote %s: %w", versionPath, err)
	}
	return readVersion(out), nil
}

// agmsgRelease is a parsed major.minor.patch, the only part of an agmsg
// version string panemux compares.
type agmsgRelease struct {
	major, minor, patch int
}

// parseAgmsgRelease extracts the release from one of the several strings an
// agmsg install's VERSION file can hold.
//
// There is no single canonical form. agmsg's repository carries a bare
// "1.2.0", but install.sh writes a provenance version into the install root
// instead: `git describe` output when installing from a checkout ("v1.2.0",
// "v1.2.0-6-g1a2b3c4" past the tag, "-dirty" on a modified tree), falling
// back to the bare VERSION only for a tarball install (npx/setup.sh, which
// has no .git to describe). Only a leading "v" is normalized.
//
// Non-release provenance (describe counts, dirty trees and prereleases) cannot
// be ordered against the pinned release. Return unknown instead of treating a
// different source tree as equal or offering a possible downgrade.
func parseAgmsgRelease(raw string) (agmsgRelease, bool) {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(v, "v")

	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return agmsgRelease{}, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return agmsgRelease{}, false
		}
		nums[i] = n
	}
	return agmsgRelease{major: nums[0], minor: nums[1], patch: nums[2]}, true
}

// VersionMismatchWarning returns the operator-facing warning for an agmsg
// install panemux has no coverage for, or "" when there is nothing to warn
// about.
//
// A mismatch is deliberately a warning and never a failure. Agent Board is
// additive, never load-bearing (see docs/agent-board.md's Design
// principles), and refusing to run against an untested agmsg would turn a
// possible incompatibility into a certain outage. What this buys is the
// difference between a pane that silently stops communicating and a log
// line naming the version that changed — the failure mode
// docs/agent-board.md's compatibility contract exists to avoid.
//
// Release comparison normalizes install provenance but now warns for every
// release different from the pinned release, including newer patches.
func VersionMismatchWarning(host, installed string) string {
	// Unknown version: panemux does not claim a mismatch it could not
	// observe. agmsg installs predating the VERSION file exist.
	if strings.TrimSpace(installed) == "" {
		return ""
	}
	if coveredByTestedVersion(installed) {
		return ""
	}
	return fmt.Sprintf(
		"agent board: host %q has agmsg %s, but panemux was tested against %s; "+
			"board messaging may misbehave if agmsg's script interface changed",
		host, strings.TrimSpace(installed), TestedAgmsgVersion,
	)
}

// coveredByTestedVersion reports whether an installed version string is one
// this repository has coverage for. A version neither side can parse is
// never covered.
func coveredByTestedVersion(installed string) bool {
	got, ok := parseAgmsgRelease(installed)
	if !ok {
		return false
	}
	want, ok := parseAgmsgRelease(TestedAgmsgVersion)
	//coverage:exempt the tested-version pin is a valid build-time constant; malformed installed versions are tested
	if !ok {
		return false
	}
	return releaseCovered(got, want)
}

// releaseCovered compares the normalized release with the tested pin.
func releaseCovered(got, want agmsgRelease) bool {
	return got == want
}
