package session

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"golang.org/x/crypto/ssh"
)

// loopbackHost is the address a pane's callback listener binds on the host
// its shell runs on.
const loopbackHost = "127.0.0.1"

// LoopbackDialer is implemented by sessions whose shell runs on a different
// host than panemux itself, so a loopback port on that host can be reached
// from the panemux process. internal/portforward uses it to republish an
// OAuth callback listener at the identical port on the panemux host.
//
// Local and local-tmux sessions deliberately do not implement it: their
// callback listener is already on the panemux host, so there is nothing to
// forward.
type LoopbackDialer interface {
	DialLoopback(ctx context.Context, port int) (net.Conn, error)
}

// dialSSHLoopback opens a direct-tcpip channel to 127.0.0.1:port on the
// remote host, which is what an `ssh -L` forward does client-side.
func dialSSHLoopback(ctx context.Context, client *ssh.Client, port int) (net.Conn, error) {
	if client == nil {
		return nil, errors.New("ssh connection is not established")
	}
	if port < 1 || port > 65535 {
		return nil, fmt.Errorf("invalid remote port %d", port)
	}
	addr := net.JoinHostPort(loopbackHost, strconv.Itoa(port))
	conn, err := client.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("dialing %s on the remote host: %w", addr, err)
	}
	return conn, nil
}

// DialLoopback connects to a loopback TCP port on the SSH host.
func (s *SSHSession) DialLoopback(ctx context.Context, port int) (net.Conn, error) {
	return dialSSHLoopback(ctx, s.client, port)
}

// DialLoopback connects to a loopback TCP port on the SSH host the tmux
// session is attached on.
func (s *TmuxSSHSession) DialLoopback(ctx context.Context, port int) (net.Conn, error) {
	return dialSSHLoopback(ctx, s.client, port)
}
