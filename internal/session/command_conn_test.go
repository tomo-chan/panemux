package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func TestCommandConnRunReturnsStdoutOverARealExecChannel(t *testing.T) {
	client, transport := startSessionTestSSHServer(t, func(command string) testSSHResponse {
		if command == "sh -s" {
			return testSSHResponse{stdout: "collected\n"}
		}
		return testSSHResponse{stderr: "unexpected command", status: 1}
	})
	conn := newCommandConn(client, nil)

	out, err := conn.Run(context.Background(), "sh -s", strings.NewReader("echo collected\n"))
	require.NoError(t, err)
	assert.Equal(t, "collected\n", string(out))
	commands, _ := transport.snapshot()
	assert.Equal(t, []string{"sh -s"}, commands)
	transport.mu.Lock()
	defer transport.mu.Unlock()
	assert.Equal(t, []string{"echo collected\n"}, transport.stdins)
}

func TestCommandConnRunReportsACommandFailureAsAnExitStatus(t *testing.T) {
	client, _ := startSessionTestSSHServer(t, func(string) testSSHResponse {
		return testSSHResponse{stdout: "partial", status: 3}
	})
	conn := newCommandConn(client, nil)

	out, err := conn.Run(context.Background(), "false", nil)
	var exit interface{ ExitStatus() int }
	require.ErrorAs(t, err, &exit)
	assert.Equal(t, 3, exit.ExitStatus())
	assert.Equal(t, "partial", string(out))
}

func TestCommandConnRunStopsOnAnAlreadyCancelledContext(t *testing.T) {
	client, transport := startSessionTestSSHServer(t, nil)
	conn := newCommandConn(client, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := conn.Run(ctx, "sh -s", nil)
	require.ErrorIs(t, err, context.Canceled)
	commands, _ := transport.snapshot()
	assert.Empty(t, commands)
}

func TestCommandConnRunReturnsTheContextErrorWhenCancelledMidCommand(t *testing.T) {
	// "tmux new-session " is the one command the in-process server keeps
	// open (it echoes stdin back), which is what a hung remote command
	// looks like from here.
	client, _ := startSessionTestSSHServer(t, nil)
	conn := newCommandConn(client, nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := conn.Run(ctx, "tmux new-session -A -s hang", nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}

func TestCommandConnRunFailsOnAClosedConnection(t *testing.T) {
	client, _ := startSessionTestSSHServer(t, nil)
	conn := newCommandConn(client, nil)
	require.NoError(t, conn.Close())

	_, err := conn.Run(context.Background(), "sh -s", nil)
	require.Error(t, err)
	var exit *ssh.ExitError
	assert.False(t, errors.As(err, &exit), "a dead connection is not a command failure")
}

func TestCommandConnInspectGitContext(t *testing.T) {
	client, _ := startSessionTestSSHServer(t, func(command string) testSSHResponse {
		if strings.HasPrefix(command, "cd '/remote/home/demo/app' && root=") {
			return testSSHResponse{stdout: "/remote/home/demo/app\n/remote/home/demo/app/.git\nmain\n" +
				"git@example.test:demo/app.git\n"}
		}
		return testSSHResponse{
			stdout: "__PANEMUX_GIT_CONTEXT_ERROR__\nshow-toplevel\nfatal: not a git repository\n",
			status: 128,
		}
	})
	conn := newCommandConn(client, nil)

	gitCtx, err := conn.InspectGitContext(context.Background(), "/remote/home/demo/app")
	require.NoError(t, err)
	assert.Equal(t, GitContext{
		Root: "/remote/home/demo/app", CommonDir: "/remote/home/demo/app/.git", Branch: "main",
		OriginURL: "git@example.test:demo/app.git", Repo: "app",
	}, gitCtx)

	_, err = conn.InspectGitContext(context.Background(), "/remote/home/demo/plain")
	var gitErr *GitContextError
	require.ErrorAs(t, err, &gitErr)
	assert.Equal(t, GitContextCauseNotGitRepo, gitErr.Cause)

	_, err = conn.InspectGitContext(context.Background(), "relative/path")
	require.ErrorAs(t, err, &gitErr)
	assert.Equal(t, GitContextCauseInvalidCWD, gitErr.Cause)
}

func TestDialCommandConnReportsADialFailure(t *testing.T) {
	_, err := DialCommandConn(SSHConfig{Host: "example.invalid", KeyFile: "relative/key"})
	require.Error(t, err)
}
