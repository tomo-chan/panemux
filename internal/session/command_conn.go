package session

import (
	"context"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

// CommandConn is an SSH connection kept open to run short, non-interactive
// commands, one exec channel each. The task dashboard holds one per
// ssh_connections entry (internal/tasks) and reuses it across collections;
// it is never shared with a pane, which keeps its own connection.
type CommandConn struct {
	client     *ssh.Client
	jumpClient *ssh.Client // non-nil when connected via ProxyJump; closed after client
}

// DialCommandConn connects with the same dialer panes use, so ProxyJump,
// ProxyCommand and known_hosts verification behave identically.
func DialCommandConn(cfg SSHConfig) (*CommandConn, error) {
	client, jumpClient, err := dialSSHClient(cfg)
	if err != nil {
		return nil, err
	}
	return newCommandConn(client, jumpClient), nil
}

func newCommandConn(client, jumpClient *ssh.Client) *CommandConn {
	return &CommandConn{client: client, jumpClient: jumpClient}
}

// Run runs cmd in a new exec channel with stdin as its input and returns its
// standard output. A command that exits non-zero returns *ssh.ExitError,
// whose ExitStatus method tells it apart from a failed connection. Canceling
// ctx closes the channel.
func (c *CommandConn) Run(ctx context.Context, cmd string, stdin io.Reader) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sess, err := c.client.NewSession()
	if err != nil {
		return nil, fmt.Errorf("new ssh session: %w", err)
	}
	defer sess.Close()
	sess.Stdin = stdin

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			sess.Close()
		case <-done:
		}
	}()

	out, err := sess.Output(cmd)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return out, err
	}
	return out, nil
}

// Ping reports whether the connection still answers: it sends an OpenSSH
// keepalive request and waits for any reply. A server that does not know the
// request still replies (with a refusal), so only a dead or stalled
// connection makes Ping fail.
func (c *CommandConn) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	replied := make(chan error, 1)
	go func() {
		_, _, err := c.client.SendRequest("keepalive@openssh.com", true, nil)
		replied <- err
	}()
	select {
	case err := <-replied:
		if err != nil {
			return fmt.Errorf("ssh keepalive: %w", err)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// InspectGitContext resolves Git metadata for cwd on the remote host, with
// the same command and the same cwd validation an ssh pane's header uses.
func (c *CommandConn) InspectGitContext(ctx context.Context, cwd string) (GitContext, error) {
	return remoteGitContext(commandConnRunner{ctx: ctx, conn: c}, cwd)
}

// Close closes the connection and, after it, any ProxyJump connection.
func (c *CommandConn) Close() error {
	err := c.client.Close()
	if c.jumpClient != nil {
		_ = c.jumpClient.Close()
	}
	return err
}

// commandConnRunner adapts CommandConn to sshSessionRunner, the shape
// remoteGitContext takes.
type commandConnRunner struct {
	ctx  context.Context
	conn *CommandConn
}

func (r commandConnRunner) Output(cmd string) ([]byte, error) {
	return r.conn.Run(r.ctx, cmd, nil)
}

func (r commandConnRunner) Close() error { return nil }
