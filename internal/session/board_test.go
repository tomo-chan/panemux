package session

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLocalSession_BoardHostID_ReturnsLocal(t *testing.T) {
	s := &LocalSession{}
	assert.Equal(t, "local", s.BoardHostID())
}

func TestTmuxLocalSession_BoardHostID_ReturnsLocal(t *testing.T) {
	s := &TmuxLocalSession{}
	assert.Equal(t, "local", s.BoardHostID())
}

func TestSSHSession_BoardHostID_ReturnsConnectionName(t *testing.T) {
	s := &SSHSession{connectionName: "my-server"}
	assert.Equal(t, "my-server", s.BoardHostID())
}

func TestTmuxSSHSession_BoardHostID_ReturnsConnectionName(t *testing.T) {
	s := &TmuxSSHSession{connectionName: "remote-box"}
	assert.Equal(t, "remote-box", s.BoardHostID())
}

func TestQuoteArgs_EscapesShellMetacharacters(t *testing.T) {
	got := quoteArgs([]string{"send.sh", "team", "from", "to", `it's; $(evil) && ` + "`echo hi`"})
	assert.Equal(
		t,
		`'send.sh' 'team' 'from' 'to' 'it'\''s; $(evil) && `+"`echo hi`"+`'`,
		got,
	)
}

func TestQuoteArgs_EmptyList(t *testing.T) {
	assert.Equal(t, "", quoteArgs(nil))
}

func TestRunBoardCommand_BuildsQuotedCommandAndReturnsOutput(t *testing.T) {
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			`'send.sh' 'team' 'a' 'b' 'hello world'`: []byte("ok\n"),
		},
	}

	out, err := runBoardCommand(context.Background(), func() (sshSessionRunner, error) {
		return runner, nil
	}, []string{"send.sh", "team", "a", "b", "hello world"})

	require.NoError(t, err)
	assert.Equal(t, "ok\n", string(out))
}

func TestRunBoardCommand_MetacharacterBody_RoundTripsAsSingleArgument(t *testing.T) {
	body := `it's; $(evil) && ` + "`echo hi`"
	cmd := `'send.sh' 'team' 'a' 'b' ` + shellQuotePath(body)
	runner := &fakeSSHRunner{
		outputs: map[string][]byte{
			cmd: []byte("ok\n"),
		},
	}

	out, err := runBoardCommand(context.Background(), func() (sshSessionRunner, error) {
		return runner, nil
	}, []string{"send.sh", "team", "a", "b", body})

	require.NoError(t, err)
	assert.Equal(t, "ok\n", string(out))
}

func TestRunBoardCommand_NewSessionError_Propagates(t *testing.T) {
	wantErr := errors.New("dial failed")
	_, err := runBoardCommand(context.Background(), func() (sshSessionRunner, error) {
		return nil, wantErr
	}, []string{"api.sh"})

	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

func TestRunBoardCommand_OutputError_Propagates(t *testing.T) {
	runner := &fakeSSHRunner{
		errs: map[string]error{
			`'api.sh'`: errors.New("remote failure"),
		},
	}

	_, err := runBoardCommand(context.Background(), func() (sshSessionRunner, error) {
		return runner, nil
	}, []string{"api.sh"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "remote failure")
}

func TestRunBoardCommand_ContextAlreadyCanceled_NeverOpensSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	called := false
	_, err := runBoardCommand(ctx, func() (sshSessionRunner, error) {
		called = true
		return nil, nil
	}, []string{"api.sh"})

	require.Error(t, err)
	assert.False(t, called, "newSession must not be invoked once ctx is already canceled")
}
