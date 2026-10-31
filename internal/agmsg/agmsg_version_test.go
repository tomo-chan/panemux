package agmsg

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeVersionExecutor struct {
	out  []byte
	err  error
	args []string
}

func (f *fakeVersionExecutor) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
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

	warning := VersionMismatchWarning("host-a", newerAgmsgPatch(t, 1))
	require.NotEmpty(t, warning)
	assert.Contains(t, warning, "host-a")
	assert.Contains(t, warning, newerAgmsgPatch(t, 1))
	assert.Contains(t, warning, TestedAgmsgVersion)
}

//efficacy:exempt documentation-only reference update; implementation behavior is unchanged
func TestTestedAgmsgVersionIsPinned(t *testing.T) {
	// The design requires a specific pinned, tested version rather than
	// "whatever is installed" — see docs/agent-board/agmsg-integration.md's
	// Version coverage section.
	assert.Regexp(t, `^\d+\.\d+\.\d+$`, TestedAgmsgVersion)
}

// Exact release/tag spellings are equivalent. Non-release and dirty provenance
// are covered by TestNonReleaseProvenanceIsNotInstalledOrSilentlyDowngraded.
func TestVersionMismatchWarning_InstallProvenanceForms(t *testing.T) {
	for _, installed := range []string{
		TestedAgmsgVersion,               // tarball install (npx / setup.sh)
		"v" + TestedAgmsgVersion,         // git checkout sitting exactly on the tag
		" v" + TestedAgmsgVersion + "\n", // whatever whitespace the file carries
	} {
		assert.Empty(t, VersionMismatchWarning("host-a", installed),
			"%q is the pinned version under one of install.sh's own provenance forms", installed)
	}
}

func TestVersionMismatchWarning_NewerPatchesWarn(t *testing.T) {
	for _, installed := range []string{newerAgmsgPatch(t, 1), "v" + newerAgmsgPatch(t, 2), newerAgmsgPatch(t, 17)} {
		assert.NotEmpty(t, VersionMismatchWarning("host-a", installed))
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
		{TestedAgmsgVersion + "-rc.1", "a prerelease of the tested version is not the tested version"},
		{newerAgmsgPatch(t, 1), "a newer minor line is exactly where the interface can move"},
		{newerAgmsgPatch(t, 100), "a new major even more so"},
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
		{"a newer patch is outside the pin", agmsgRelease{1, 2, 6}, false},
		{"a much newer patch is outside the pin", agmsgRelease{1, 2, 40}, false},
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

func newerAgmsgPatch(t *testing.T, offset int) string {
	t.Helper()
	release, ok := parseAgmsgRelease(TestedAgmsgVersion)
	require.True(t, ok)
	return fmt.Sprintf("%d.%d.%d", release.major, release.minor, release.patch+offset)
}
