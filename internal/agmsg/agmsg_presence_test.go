package agmsg

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// remoteAgmsgPresenceProbeKey builds the fakeBoardExecutor outputs key for
// the fixed presence-probe command RemoteAgmsgPresent issues against
// apiScriptPath, mirroring RemoteAgmsgPresent's own args construction.
func remoteAgmsgPresenceProbeKey(apiScriptPath string) string {
	return strings.Join(
		[]string{"sh", "-c", remoteAgmsgPresenceProbeScript, remoteAgmsgPresenceProbeScriptName, apiScriptPath},
		"\x00",
	)
}

func TestRemoteAgmsgPresent_ScriptsExist_True(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: map[string][]byte{
		remoteAgmsgPresenceProbeKey("/home/remote-user/agmsg/scripts/api.sh"): []byte("yes"),
	}}

	present, err := RemoteAgmsgPresent(context.Background(), exec, "/home/remote-user/agmsg")
	require.NoError(t, err)
	assert.True(t, present)
}

func TestRemoteAgmsgPresent_ScriptsMissing_False(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: map[string][]byte{
		remoteAgmsgPresenceProbeKey("/home/remote-user/agmsg/scripts/api.sh"): []byte("no"),
	}}

	present, err := RemoteAgmsgPresent(context.Background(), exec, "/home/remote-user/agmsg")
	require.NoError(t, err)
	assert.False(t, present)
}

func TestRemoteAgmsgPresent_TrimsWhitespaceFromProbeResponse(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: map[string][]byte{
		remoteAgmsgPresenceProbeKey("/home/remote-user/agmsg/scripts/api.sh"): []byte("yes\n"),
	}}

	present, err := RemoteAgmsgPresent(context.Background(), exec, "/home/remote-user/agmsg")
	require.NoError(t, err)
	assert.True(t, present)
}

func TestRemoteAgmsgPresent_ExecutorError_Wrapped(t *testing.T) {
	wantErr := errors.New("ssh: connection lost")
	exec := &fakeBoardExecutor{err: wantErr}

	_, err := RemoteAgmsgPresent(context.Background(), exec, "/home/remote-user/agmsg")
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}
