package board

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// LocalHostID is the reserved AgmsgClient.HostID() value for the host
// panemux itself runs on.
const LocalHostID = "local"

// LocalAgmsgClient shells out to a local agmsg installation's
// scripts/api.sh (reads) and scripts/send.sh (writes). Because this is a
// local exec.Command invocation, Go passes each argument as a genuine argv
// element with no intermediate shell, so no argument carries shell-injection
// risk here regardless of its content (see docs/agent-board.md's Package
// layout section).
type LocalAgmsgClient struct {
	agmsgPath string // expanded, absolute path to the agmsg install root
	execFn    func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// NewLocalAgmsgClient creates a client rooted at the given expanded,
// absolute agmsg install path (the directory containing scripts/).
func NewLocalAgmsgClient(agmsgPath string) *LocalAgmsgClient {
	return &LocalAgmsgClient{
		agmsgPath: agmsgPath,
		execFn:    exec.CommandContext,
	}
}

func (c *LocalAgmsgClient) HostID() string { return LocalHostID }

func (c *LocalAgmsgClient) apiScript() string  { return filepath.Join(c.agmsgPath, "scripts", "api.sh") }
func (c *LocalAgmsgClient) sendScript() string { return filepath.Join(c.agmsgPath, "scripts", "send.sh") }

// Send always passes --force, per AgmsgClient's contract.
func (c *LocalAgmsgClient) Send(ctx context.Context, team, from, to, body string) error {
	cmd := c.execFn(ctx, c.sendScript(), team, from, to, body, "--force")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("local agmsg send.sh: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// Since calls `api.sh get teams <team> messages --limit <limit>` (no
// --before-id — see AgmsgClient's contract) and filters client-side to rows
// after afterID.
func (c *LocalAgmsgClient) Since(ctx context.Context, team, afterID string, limit int) ([]Row, error) {
	cmd := c.execFn(ctx, c.apiScript(), "get", "teams", team, "messages", "--limit", strconv.Itoa(limit))
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("local agmsg api.sh: %w", err)
	}
	rows, err := parseMessageRows(out, c.HostID())
	if err != nil {
		return nil, err
	}
	return filterAfterID(rows, afterID), nil
}
