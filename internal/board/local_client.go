package board

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
)

const boardHostIDLocal = "local"

var _ AgmsgClient = (*LocalAgmsgClient)(nil)

// runLocalCommandFn is injectable for tests.
type runLocalCommandFn func(ctx context.Context, name string, args ...string) ([]byte, error)

// execLocalCommand runs name/args as discrete argv elements with no
// intermediate shell — see LocalAgmsgClient's own doc comment for why that
// makes their content shell-injection-safe regardless of source.
func execLocalCommand(ctx context.Context, name string, args ...string) ([]byte, error) {
	//nolint:gosec // G204: panemux-controlled agmsg script path, literal argv
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("exec %s: %w", name, err)
	}
	return out, nil
}

// LocalAgmsgClient shells out to the local agmsg installation's own
// scripts: scripts/api.sh for reads, scripts/send.sh --force for writes.
// Because this is a local exec.Command invocation, Go passes each argument
// as a genuine array element with no intermediate shell, so no argument to
// either script carries shell-injection risk here regardless of its
// content.
type LocalAgmsgClient struct {
	run       runLocalCommandFn
	agmsgPath string // directory containing scripts/api.sh, scripts/send.sh
}

// NewLocalAgmsgClient returns a client for the local agmsg installation
// rooted at agmsgPath (already ~-expanded to an absolute path).
func NewLocalAgmsgClient(agmsgPath string) *LocalAgmsgClient {
	return &LocalAgmsgClient{agmsgPath: agmsgPath, run: execLocalCommand}
}

func (c *LocalAgmsgClient) HostID() string { return boardHostIDLocal }

func (c *LocalAgmsgClient) apiScript() string { return filepath.Join(c.agmsgPath, "scripts", "api.sh") }
func (c *LocalAgmsgClient) sendScript() string {
	return filepath.Join(c.agmsgPath, "scripts", "send.sh")
}

// Send always passes --force — see docs/agent-board.md's Integration with
// agmsg section for why every board-originated send does.
func (c *LocalAgmsgClient) Send(ctx context.Context, team, from, to, body string) error {
	_, err := c.run(ctx, c.sendScript(), team, from, to, body, "--force")
	if err != nil {
		return fmt.Errorf("agmsg send.sh: %w", err)
	}
	return nil
}

func (c *LocalAgmsgClient) Since(ctx context.Context, team, afterID string, limit int) ([]Row, error) {
	out, err := c.run(ctx, c.apiScript(), "get", "teams", team, "messages", "--limit", strconv.Itoa(limit))
	if err != nil {
		return nil, fmt.Errorf("agmsg api.sh: %w", err)
	}
	return filterRowsAfter(parseAgmsgMessageRows(out, c.HostID()), afterID), nil
}
