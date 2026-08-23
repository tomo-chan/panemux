package board

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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
// has no .git to describe). So the leading "v" and any describe suffix are
// stripped before comparing.
//
// A prerelease suffix ("1.2.0-rc.1") is deliberately NOT accepted as its
// release: it is a different build from the tag it precedes, and treating
// it as equal would be exactly the kind of quiet assumption this file
// exists to avoid. ok == false says "cannot reason about this", and every
// caller treats that conservatively.
func parseAgmsgRelease(raw string) (agmsgRelease, bool) {
	v := strings.TrimSpace(raw)
	v = strings.TrimPrefix(v, "v")

	// `git describe` appends "-<commits>-g<sha>" and possibly "-dirty".
	// Anything else after a "-" is a prerelease and is left in place, so it
	// fails the numeric parse below rather than being silently accepted.
	if i := describeSuffixIndex(v); i >= 0 {
		v = v[:i]
	}

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

// describeSuffixIndex returns the index at which a `git describe` suffix
// ("-<commits>-g<sha>", optionally followed by "-dirty") begins, or -1 when
// the string carries none.
func describeSuffixIndex(v string) int {
	for i, part := range strings.Split(v, "-") {
		if i == 0 {
			continue
		}
		// The describe suffix always starts with the commit count, and a
		// prerelease label ("rc", "beta") never does.
		if _, err := strconv.Atoi(part); err == nil {
			return strings.Index(v, "-"+part)
		}
		return -1
	}
	return -1
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
// It is deliberately narrower than "installed != TestedAgmsgVersion",
// because that comparison warned in two cases where it said nothing useful:
//
//  1. **An exactly-pinned install.** The provenance forms above meant a
//     clone-and-install of the tested version reported "v1.2.0" against a
//     pin of "1.2.0" and was warned about on every startup. Every one of
//     those warnings was false, and a warning that is usually false trains
//     its reader to skip the one that is not.
//  2. **A patch release at or past the pinned one, in the same minor line.**
//     agmsg ships roughly
//     every three days, so an operator on a current install is almost never
//     on the exact pinned patch, and moving the pin is this repository's
//     work rather than something the warning's reader can act on. What
//     makes the silence honest is that those releases are covered: Tier 2's
//     canary runs the real contract against each new agmsg release (see
//     docs/agent-board.md's agmsg compatibility contract), so a patch
//     release that breaks the interface surfaces in CI here rather than
//     going unnoticed. Stated plainly: agmsg makes no semver promise for
//     the scripts panemux's write path depends on, so this is a deliberate
//     trade of noise for canary coverage, not a claim that a patch release
//     cannot break anything.
//
// Everything else still warns — an older minor may predate an interface
// panemux depends on, a newer minor or major is exactly where that
// interface can move, and a version this cannot parse is reported rather
// than assumed to be fine. A patch OLDER than the pin warns too, for the
// reason releaseCovered gives: only releases from the pin forward have been
// through the canary.
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
	if !ok {
		return false
	}
	return releaseCovered(got, want)
}

// releaseCovered is the coverage rule itself, split from the pin so it can
// be tested at boundaries the current pin cannot reach — with a pinned
// patch of 0, no same-minor release is older than it, so the last clause
// below is unreachable today and would first take effect on a pin bump,
// which is exactly when nobody would be looking for it.
//
// Covered means: the same minor line, at or past the pinned patch.
//
// The asymmetry is deliberate. A NEWER patch is quiet because Tier 2's
// canary verifies each agmsg release as it ships, so those versions have
// been exercised even though the pin has not moved. An OLDER patch has no
// such backing — it may predate a fix or behavior the pin was moved for —
// so it is treated like any other version outside the tested line.
func releaseCovered(got, want agmsgRelease) bool {
	return got.major == want.major && got.minor == want.minor && got.patch >= want.patch
}
