package board

import (
	"context"
	"fmt"
	"path"
	"strconv"
)

// BoardExecutor is the subset of internal/session's BoardExecutor interface
// RemoteAgmsgClient depends on. Declared locally so this package does not
// import internal/session (avoiding a session <-> board import cycle,
// since session.CreateFromConfig will need to construct board clients);
// internal/session.SSHSession and TmuxSSHSession satisfy it structurally.
type BoardExecutor interface {
	RunBoardCommand(ctx context.Context, args []string) ([]byte, error)
}

// RemoteAgmsgClient runs agmsg's scripts.sh scripts on a remote host over
// an existing SSH exec channel (via BoardExecutor), rather than a local
// exec.Command. Escaping of every argument happens inside
// BoardExecutor.RunBoardCommand's implementation (internal/session), not
// here — see docs/agent-board.md's Package layout section: "there is
// exactly one place this can be gotten wrong rather than one per call
// site." RemoteAgmsgClient itself only ever builds raw, unescaped argv.
type RemoteAgmsgClient struct { //nolint:govet // fieldalignment: clarity preferred
	hostID    string // SSH connection name
	agmsgPath string // expanded, absolute path to the agmsg install root on the remote host
	executor  BoardExecutor
}

// NewRemoteAgmsgClient creates a client for the given SSH-backed host,
// rooted at the given already-expanded, absolute agmsg install path on that
// host. Callers must expand a leading `~` in agmsgPath themselves (e.g. via
// session.BoardHomeDirer) before calling this constructor — see
// docs/agent-board.md's "Integration with agmsg" for why expansion must
// happen locally, before the path ever reaches a BoardExecutor argument.
func NewRemoteAgmsgClient(hostID, agmsgPath string, executor BoardExecutor) *RemoteAgmsgClient {
	return &RemoteAgmsgClient{hostID: hostID, agmsgPath: agmsgPath, executor: executor}
}

func (c *RemoteAgmsgClient) HostID() string { return c.hostID }

func (c *RemoteAgmsgClient) apiScript() string  { return path.Join(c.agmsgPath, "scripts", "api.sh") }
func (c *RemoteAgmsgClient) sendScript() string { return path.Join(c.agmsgPath, "scripts", "send.sh") }

// Send always passes --force, per AgmsgClient's contract.
func (c *RemoteAgmsgClient) Send(ctx context.Context, team, from, to, body string) error {
	_, err := c.executor.RunBoardCommand(ctx, []string{
		c.sendScript(), team, from, to, body, "--force",
	})
	if err != nil {
		return fmt.Errorf("remote agmsg send.sh on %s: %w", c.hostID, err)
	}
	return nil
}

// Since calls `api.sh get teams <team> messages --limit <limit>` (no
// --before-id) over the remote exec channel and filters client-side to rows
// after afterID.
func (c *RemoteAgmsgClient) Since(ctx context.Context, team, afterID string, limit int) ([]Row, error) {
	out, err := c.executor.RunBoardCommand(ctx, []string{
		c.apiScript(), "get", "teams", team, "messages", "--limit", strconv.Itoa(limit),
	})
	if err != nil {
		return nil, fmt.Errorf("remote agmsg api.sh on %s: %w", c.hostID, err)
	}
	rows, err := parseMessageRows(out, c.HostID())
	if err != nil {
		return nil, err
	}
	return filterAfterID(rows, afterID), nil
}
