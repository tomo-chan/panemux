package session

import (
	"context"
	"fmt"
	"net"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// PreviewConn is the SSH connection the split preview keeps to an ssh or
// ssh_tmux pane's host (issue #387): files are read over its SFTP
// subsystem and loopback URLs reached over its direct-tcpip channels, so no
// remote shell parses a path or a port. It is never a pane's own
// connection, so a preview opened in its own tab outlives the pane.
type PreviewConn struct {
	client     *ssh.Client
	jumpClient *ssh.Client // non-nil when connected via ProxyJump; closed after client
	files      *sftp.Client
	done       chan struct{}
}

// DialPreviewConn connects with the same dialer panes use, so ProxyJump,
// ProxyCommand, authentication and known_hosts verification behave
// identically, and starts SFTP on the connection.
func DialPreviewConn(cfg SSHConfig) (*PreviewConn, error) {
	client, jumpClient, err := dialSSHClient(cfg)
	if err != nil {
		return nil, err
	}
	return newPreviewConn(client, jumpClient)
}

func newPreviewConn(client, jumpClient *ssh.Client) (*PreviewConn, error) {
	files, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		if jumpClient != nil {
			_ = jumpClient.Close()
		}
		return nil, fmt.Errorf("start sftp: %w", err)
	}
	c := &PreviewConn{client: client, jumpClient: jumpClient, files: files, done: make(chan struct{})}
	go func() {
		_ = client.Wait()
		close(c.done)
	}()
	return c, nil
}

// Dial opens a TCP connection from the remote host to addr, which is what
// an `ssh -L` forward does client-side.
func (c *PreviewConn) Dial(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := c.client.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dialing %s on the remote host: %w", addr, err)
	}
	return conn, nil
}

// SFTP is the connection's SFTP client.
func (c *PreviewConn) SFTP() *sftp.Client { return c.files }

// Done is closed once the connection has ended.
func (c *PreviewConn) Done() <-chan struct{} { return c.done }

// Close closes the connection and, after it, any ProxyJump connection.
func (c *PreviewConn) Close() error {
	_ = c.files.Close()
	err := c.client.Close()
	if c.jumpClient != nil {
		_ = c.jumpClient.Close()
	}
	return err
}
