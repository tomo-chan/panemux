package board

import (
	"context"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeBoardExecutor struct {
	outputs map[string][]byte
	err     error
	calls   [][]string
}

func (f *fakeBoardExecutor) RunBoardCommand(_ context.Context, args []string) ([]byte, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return nil, f.err
	}
	key := strings.Join(args, "\x00")
	if out, ok := f.outputs[key]; ok {
		return out, nil
	}
	return nil, errors.New("unexpected board command args: " + strings.Join(args, " "))
}

func TestRemoteAgmsgClient_HostID(t *testing.T) {
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", &fakeBoardExecutor{})
	assert.Equal(t, "host-a", c.HostID())
}

func TestRemoteAgmsgClient_Send_BuildsWrapperScriptArgs(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: map[string][]byte{}}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	encoded := base64.StdEncoding.EncodeToString([]byte("hello"))
	key := strings.Join([]string{
		"sh", "-c", sendBase64WrapperScript, "board-send",
		"/opt/agmsg/scripts/send.sh", "team-a", "from-a", "to-a", encoded,
	}, "\x00")
	exec.outputs[key] = []byte("")

	err := c.Send(context.Background(), "team-a", "from-a", "to-a", "hello")
	require.NoError(t, err)
	require.Len(t, exec.calls, 1)
	assert.Equal(t, "sh", exec.calls[0][0])
	assert.Equal(t, "-c", exec.calls[0][1])
	assert.Equal(t, sendBase64WrapperScript, exec.calls[0][2])
}

func TestRemoteAgmsgClient_Send_MetacharacterBody_RoundTripsViaBase64(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: map[string][]byte{}}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	body := `it's; $(evil) && ` + "`echo hi`" + ` | rm -rf /`
	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	key := strings.Join([]string{
		"sh", "-c", sendBase64WrapperScript, "board-send",
		"/opt/agmsg/scripts/send.sh", "team", "from", "to", encoded,
	}, "\x00")
	exec.outputs[key] = []byte("")

	err := c.Send(context.Background(), "team", "from", "to", body)
	require.NoError(t, err)
	require.Len(t, exec.calls, 1)

	// The encoded token itself must be pure base64 alphabet — no
	// metacharacter from body may leak into the argument RunBoardCommand
	// receives.
	sentToken := exec.calls[0][8]
	assert.True(t, validBase64.MatchString(sentToken))

	decoded, decErr := base64.StdEncoding.DecodeString(sentToken)
	require.NoError(t, decErr)
	assert.Equal(t, body, string(decoded))
}

func TestRemoteAgmsgClient_Send_InvalidTeam_RejectedBeforeExecutorCall(t *testing.T) {
	exec := &fakeBoardExecutor{}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	err := c.Send(context.Background(), "team; rm -rf /", "from", "to", "body")
	require.Error(t, err)
	assert.Empty(t, exec.calls, "executor must never be invoked once identifier validation fails")
}

func TestRemoteAgmsgClient_Send_InvalidFrom_Rejected(t *testing.T) {
	exec := &fakeBoardExecutor{}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	err := c.Send(context.Background(), "team", "from $(evil)", "to", "body")
	require.Error(t, err)
	assert.Empty(t, exec.calls)
}

func TestRemoteAgmsgClient_Send_InvalidTo_Rejected(t *testing.T) {
	exec := &fakeBoardExecutor{}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	err := c.Send(context.Background(), "team", "from", "to`whoami`", "body")
	require.Error(t, err)
	assert.Empty(t, exec.calls)
}

func TestRemoteAgmsgClient_Send_ValidSystemIDIdentifier_Accepted(t *testing.T) {
	exec := &fakeBoardExecutor{outputs: map[string][]byte{}}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	encoded := base64.StdEncoding.EncodeToString([]byte("status"))
	key := strings.Join([]string{
		"sh", "-c", sendBase64WrapperScript, "board-send",
		"/opt/agmsg/scripts/send.sh", "panemux", "pane-a", SystemID, encoded,
	}, "\x00")
	exec.outputs[key] = []byte("")

	err := c.Send(context.Background(), "panemux", "pane-a", SystemID, "status")
	assert.NoError(t, err)
}

func TestRemoteAgmsgClient_Send_ExecutorError_Wrapped(t *testing.T) {
	wantErr := errors.New("ssh: connection lost")
	exec := &fakeBoardExecutor{err: wantErr}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	err := c.Send(context.Background(), "team", "from", "to", "body")
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestRemoteAgmsgClient_Since_BuildsExpectedArgs(t *testing.T) {
	jsonl := `{"type":"message_sent","id":"5","team":"t","from":"a","to":"b","body":"hi","at":"2026-01-01T00:00:00Z"}
`
	wantArgs := []string{"/opt/agmsg/scripts/api.sh", "get", "teams", "t", "messages", "--limit", "100"}
	exec := &fakeBoardExecutor{outputs: map[string][]byte{
		strings.Join(wantArgs, "\x00"): []byte(jsonl),
	}}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	rows, err := c.Since(context.Background(), "t", "1", 100)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	assert.Equal(t, "hi", rows[0].Body)
	assert.Equal(t, "host-a", rows[0].Host)
}

func TestRemoteAgmsgClient_Since_InvalidTeam_RejectedBeforeExecutorCall(t *testing.T) {
	exec := &fakeBoardExecutor{}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	_, err := c.Since(context.Background(), "team with spaces", "", 100)
	require.Error(t, err)
	assert.Empty(t, exec.calls)
}

func TestRemoteAgmsgClient_Since_ExecutorError_Wrapped(t *testing.T) {
	wantErr := errors.New("ssh: connection lost")
	exec := &fakeBoardExecutor{err: wantErr}
	c := NewRemoteAgmsgClient("host-a", "/opt/agmsg", exec)

	rows, err := c.Since(context.Background(), "t", "", 10)
	require.Error(t, err)
	assert.Nil(t, rows)
	assert.ErrorIs(t, err, wantErr)
}
