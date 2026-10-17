package session

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

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
	activity   *readActivity
	done       chan struct{}
}

// DialPreviewConn connects with the same dialer panes use, so ProxyJump,
// ProxyCommand, authentication and known_hosts verification behave
// identically, and starts SFTP on the connection.
func DialPreviewConn(cfg SSHConfig) (*PreviewConn, error) {
	activity := newReadActivity()
	client, jumpClient, err := dialSSHClientUntil(cfg, nowFn().Add(dialRetryBudget), activity.wrap)
	if err != nil {
		return nil, err
	}
	return newPreviewConn(client, jumpClient, activity)
}

func newPreviewConn(client, jumpClient *ssh.Client, activity *readActivity) (*PreviewConn, error) {
	files, err := sftp.NewClient(client)
	if err != nil {
		_ = client.Close()
		if jumpClient != nil {
			_ = jumpClient.Close()
		}
		return nil, fmt.Errorf("start sftp: %w", err)
	}
	c := &PreviewConn{client: client, jumpClient: jumpClient, files: files, activity: activity, done: make(chan struct{})}
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

// Ping reports whether the host still answers: see CommandConn.Ping.
func (c *PreviewConn) Ping(ctx context.Context) error { return pingSSH(ctx, c.client) }

// LastRead is when anything last arrived on the connection's transport.
func (c *PreviewConn) LastRead() time.Time { return c.activity.last() }

// SFTP is the connection's SFTP client.
func (c *PreviewConn) SFTP() *sftp.Client { return c.files }

// Done is closed once the connection has ended.
func (c *PreviewConn) Done() <-chan struct{} { return c.done }

// Close closes the connection and, after it, any ProxyJump connection. The
// SSH connection goes first: an SFTP request stalled on a host that stopped
// answering holds the SFTP client, whose own Close would wait behind it.
func (c *PreviewConn) Close() error {
	err := c.client.Close()
	_ = c.files.Close()
	if c.jumpClient != nil {
		_ = c.jumpClient.Close()
	}
	return err
}

// readActivity records when a transport last read anything. The preview
// counts arriving data as the host answering, as OpenSSH's
// ServerAliveInterval does: on a slow link a keepalive reply waits behind
// the data queued ahead of it, so the reply alone says nothing in time.
type readActivity struct {
	at atomic.Int64
}

func newReadActivity() *readActivity {
	a := &readActivity{}
	a.at.Store(nowFn().UnixNano())
	return a
}

func (a *readActivity) last() time.Time { return time.Unix(0, a.at.Load()) }

// wrap returns conn, recording each read that brings data.
func (a *readActivity) wrap(conn net.Conn) net.Conn { return activityConn{Conn: conn, activity: a} }

type activityConn struct {
	net.Conn
	activity *readActivity
}

func (c activityConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.activity.at.Store(nowFn().UnixNano())
	}
	return n, err
}
