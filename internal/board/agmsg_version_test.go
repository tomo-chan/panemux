package board

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeVersionFile(t *testing.T, dir, contents string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "VERSION"), []byte(contents), 0o600))
}

func TestLocalAgmsgVersion(t *testing.T) {
	dir := t.TempDir()

	// No VERSION file: not an error, just unknown. An agmsg install without
	// one must never be treated as broken.
	got, err := LocalAgmsgVersion(dir)
	require.NoError(t, err)
	assert.Empty(t, got)

	writeVersionFile(t, dir, "1.2.0\n")
	got, err = LocalAgmsgVersion(dir)
	require.NoError(t, err)
	assert.Equal(t, "1.2.0", got, "surrounding whitespace must be trimmed")
}

func TestLocalAgmsgVersionIgnoresJunk(t *testing.T) {
	dir := t.TempDir()
	writeVersionFile(t, dir, "   \n\t\n")

	got, err := LocalAgmsgVersion(dir)
	require.NoError(t, err)
	assert.Empty(t, got, "a whitespace-only VERSION reads as unknown, not as a version")
}

type fakeVersionExecutor struct {
	out  []byte
	err  error
	args []string
}

func (f *fakeVersionExecutor) RunBoardCommand(_ context.Context, args []string) ([]byte, error) {
	f.args = args
	return f.out, f.err
}

func TestRemoteAgmsgVersion(t *testing.T) {
	exec := &fakeVersionExecutor{out: []byte("1.2.0\n")}

	got, err := RemoteAgmsgVersion(context.Background(), exec, "/remote/home/demo/agmsg")

	require.NoError(t, err)
	assert.Equal(t, "1.2.0", got)
	// The path must arrive as a positional parameter, never interpolated
	// into the script text — the same discipline as the presence probe.
	assert.Contains(t, exec.args, "/remote/home/demo/agmsg/VERSION")
	assert.NotContains(t, exec.args[2], "/remote/home/demo",
		"the probe script itself must carry no caller-supplied data")
}

func TestRemoteAgmsgVersionMissingFileIsUnknownNotAnError(t *testing.T) {
	exec := &fakeVersionExecutor{out: []byte("")}

	got, err := RemoteAgmsgVersion(context.Background(), exec, "/remote/home/demo/agmsg")

	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestRemoteAgmsgVersionTransportFailureIsAnError(t *testing.T) {
	exec := &fakeVersionExecutor{err: errors.New("ssh: connection refused")}

	_, err := RemoteAgmsgVersion(context.Background(), exec, "/remote/home/demo/agmsg")

	require.Error(t, err, "a transport failure must not be reported as an unknown version")
}

func TestVersionMismatchWarning(t *testing.T) {
	// Unknown version: no warning. panemux cannot claim a mismatch it did
	// not observe, and agmsg installs predating the VERSION file exist.
	assert.Empty(t, VersionMismatchWarning("host-a", ""))

	// The tested version: no warning.
	assert.Empty(t, VersionMismatchWarning("host-a", TestedAgmsgVersion))

	warning := VersionMismatchWarning("host-a", "1.3.0")
	require.NotEmpty(t, warning)
	assert.Contains(t, warning, "host-a")
	assert.Contains(t, warning, "1.3.0")
	assert.Contains(t, warning, TestedAgmsgVersion)
}

func TestTestedAgmsgVersionIsPinned(t *testing.T) {
	// The design requires a specific pinned, tested version rather than
	// "whatever is installed" — see docs/agent-board.md's Version pinning.
	assert.Regexp(t, `^\d+\.\d+\.\d+$`, TestedAgmsgVersion)
}

// TestVersionMismatchWarning_InstallProvenanceForms is the regression test
// for a warning that fired on every startup for a correctly-pinned install.
//
// agmsg's VERSION file does not hold one canonical string. Its repository
// carries a bare "1.2.0", but install.sh writes a *provenance* version into
// the install root instead: `git describe` when installing from a checkout
// ("v1.2.0", or "v1.2.0-6-g1a2b3c4" past the tag, "-dirty" on a modified
// tree), falling back to the bare VERSION only for a tarball install
// (npx/setup.sh, which has no .git). Comparing that string to the pin
// verbatim meant an operator who had installed exactly the tested version
// from a clone — agmsg's own documented "always tracks latest" path — was
// warned that their agmsg was untested. Every one of those warnings was
// false, which is corrosive in a way that a missing warning is not: it
// trains the reader to ignore the real one.
func TestVersionMismatchWarning_InstallProvenanceForms(t *testing.T) {
	for _, installed := range []string{
		"1.2.0",                   // tarball install (npx / setup.sh)
		"v1.2.0",                  // git checkout sitting exactly on the tag
		"v1.2.0-6-g1a2b3c4",       // a checkout a few commits past the tag
		"v1.2.0-6-g1a2b3c4-dirty", // ...with local modifications
		" v1.2.0\n",               // whatever whitespace the file carries
	} {
		assert.Empty(t, VersionMismatchWarning("host-a", installed),
			"%q is the pinned version under one of install.sh's own provenance forms", installed)
	}
}

// TestVersionMismatchWarning_PatchReleasesInTheTestedLineAreQuiet covers the
// other half of the noise. agmsg ships a release every ~3 days (median gap
// 2.9 days over its first 22 releases), so an operator on a current install
// is almost never on the exact pinned patch, and warning them on every
// startup says nothing they can act on: bumping the pin is this
// repository's work, not theirs.
//
// A patch release at or past the pinned one is therefore quiet, and that
// silence is bought by something real rather than assumed — Tier 2's canary
// runs the contract against each new agmsg release (see
// docs/agent-board.md's agmsg compatibility contract). Stated plainly:
// agmsg makes no semver promise for the scripts panemux's write path
// depends on, so this is a deliberate noise-for-coverage trade, not a claim
// that patch releases cannot break anything.
func TestVersionMismatchWarning_PatchReleasesInTheTestedLineAreQuiet(t *testing.T) {
	for _, installed := range []string{"1.2.1", "v1.2.2", "1.2.17"} {
		assert.Empty(t, VersionMismatchWarning("host-a", installed),
			"%q is at or past the pinned %s in the same minor line, which the canary covers",
			installed, TestedAgmsgVersion)
	}
}

// TestVersionMismatchWarning_StillWarnsWhereItMatters pins what the
// narrowing above must NOT swallow. Anything outside the covered set,
// in either direction, still warns — that is where agmsg's script interface
// can have moved without the canary having said so about the version this
// operator is actually running.
func TestVersionMismatchWarning_StillWarnsWhereItMatters(t *testing.T) {
	tests := []struct {
		installed string
		reason    string
	}{
		{"1.1.13", "an older minor may predate an interface panemux depends on"},
		{"v1.1.9", "same, in git-describe form"},
		{"1.2.0-rc.1", "a prerelease of the tested version is not the tested version"},
		{"1.3.0", "a newer minor line is exactly where the interface can move"},
		{"2.0.0", "a new major even more so"},
		{"nightly", "an unparseable version cannot be reasoned about; say so rather than assume"},
		{"1.2", "a two-component version is not a release this can compare"},
	}

	for _, tt := range tests {
		t.Run(tt.installed, func(t *testing.T) {
			warning := VersionMismatchWarning("host-a", tt.installed)
			require.NotEmpty(t, warning, tt.reason)
			assert.Contains(t, warning, "host-a")
			assert.Contains(t, warning, tt.installed,
				"the warning must name the string actually found on the host, so it can be located")
			assert.Contains(t, warning, TestedAgmsgVersion)
		})
	}
}

// TestReleaseCovered pins the coverage rule at boundaries the current pin
// cannot reach. TestedAgmsgVersion's patch is 0, so no same-minor release is
// older than it and VersionMismatchWarning alone cannot exercise the
// older-patch branch — it would first take effect on a pin bump, which is
// exactly when an unnoticed change of behavior would be most costly.
func TestReleaseCovered(t *testing.T) {
	pin := agmsgRelease{major: 1, minor: 2, patch: 5}

	tests := []struct {
		name      string
		installed agmsgRelease
		covered   bool
	}{
		{"the pinned release itself", agmsgRelease{1, 2, 5}, true},
		{"a newer patch is covered by the canary", agmsgRelease{1, 2, 6}, true},
		{"a much newer patch, same line", agmsgRelease{1, 2, 40}, true},
		{"an older patch has no canary backing", agmsgRelease{1, 2, 4}, false},
		{"the first patch of the tested minor", agmsgRelease{1, 2, 0}, false},
		{"a newer minor is where the interface moves", agmsgRelease{1, 3, 0}, false},
		{"an older minor may predate what panemux needs", agmsgRelease{1, 1, 99}, false},
		{"a newer major, all the more so", agmsgRelease{2, 0, 0}, false},
		{"an older major", agmsgRelease{0, 9, 9}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.covered, releaseCovered(tt.installed, pin))
		})
	}
}
