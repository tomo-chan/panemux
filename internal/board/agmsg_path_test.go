package board

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveRemoteAgmsgPath_TildePrefixed_ExpandsAgainstRemoteHome(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: map[string][]byte{
		strings.Join([]string{"sh", "-c", remoteHomeProbeCmd}, "\x00"): []byte("/home/remote-user"),
	}}

	got, err := ResolveRemoteAgmsgPath(context.Background(), exec, "~/.agents/skills/agmsg")
	require.NoError(t, err)
	assert.Equal(t, "/home/remote-user/.agents/skills/agmsg", got)
}

func TestResolveRemoteAgmsgPath_AbsolutePath_ReturnedUnchanged_NoExecutorCall(t *testing.T) {
	exec := &fakeBoardExecutor{}

	got, err := ResolveRemoteAgmsgPath(context.Background(), exec, "/opt/agmsg")
	require.NoError(t, err)
	assert.Equal(t, "/opt/agmsg", got)
	assert.Empty(t, exec.calls, "an already-absolute path must never trigger a RunBoardCommand call")
}

func TestResolveRemoteAgmsgPath_ExecutorError_Wrapped(t *testing.T) {
	wantErr := errors.New("ssh: connection lost")
	exec := &fakeBoardExecutor{err: wantErr}

	_, err := ResolveRemoteAgmsgPath(context.Background(), exec, "~/agmsg")
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestResolveRemoteAgmsgPath_EmptyHomeResponse_Error(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: map[string][]byte{
		strings.Join([]string{"sh", "-c", remoteHomeProbeCmd}, "\x00"): []byte(""),
	}}

	_, err := ResolveRemoteAgmsgPath(context.Background(), exec, "~/agmsg")
	assert.Error(t, err)
}

func TestResolveRemoteAgmsgPath_TrimsWhitespaceFromProbeResponse(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: map[string][]byte{
		strings.Join([]string{"sh", "-c", remoteHomeProbeCmd}, "\x00"): []byte("/home/remote-user\n"),
	}}

	got, err := ResolveRemoteAgmsgPath(context.Background(), exec, "~/agmsg")
	require.NoError(t, err)
	assert.Equal(t, "/home/remote-user/agmsg", got)
}

func TestResolveRemoteAgmsgPath_NonAbsoluteProbeResponse_Error(t *testing.T) {
	// Regression test: a non-POSIX echo -n printing "-n" literally (fixed by
	// switching to printf), or any other shell quirk that corrupts the
	// probe response, must fail loudly rather than silently building a
	// broken, relative agmsg path.
	exec := &fakeBoardExecutor{outputs: map[string][]byte{
		strings.Join([]string{"sh", "-c", remoteHomeProbeCmd}, "\x00"): []byte("-n /home/remote-user"),
	}}

	_, err := ResolveRemoteAgmsgPath(context.Background(), exec, "~/agmsg")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an absolute path")
}
