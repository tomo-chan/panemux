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
