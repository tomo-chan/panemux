package board

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"regexp"
	"strconv"
)

// BoardExecutor is satisfied by any session capable of running a command on
// a remote host over its own exec channel. internal/session's SSHSession
// and TmuxSSHSession both implement this structurally; internal/board does
// not import internal/session to avoid a dependency it does not otherwise
// need — see docs/agent-board.md's internal/session capability interfaces
// section for the canonical definition this mirrors.
type BoardExecutor interface {
	RunBoardCommand(ctx context.Context, args []string) ([]byte, error)
}

// validAgmsgIdentifier constrains team/from/to values (pane IDs, the
// configured team name, or SystemID) to a safe, constrained alphabet before
// they are ever placed in a RunBoardCommand argument list — the same
// regex-allowlist-then-quote shape internal/session/ssh.go's
// validRemotePath already applies to cwd.
var validAgmsgIdentifier = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// validBase64 constrains an already base64-encoded message body to its own
// alphabet before it is placed in a RunBoardCommand argument list. A
// message body is arbitrary agent-authored text and cannot be
// regex-allowlisted directly the way an identifier or path can — encoding
// it to a fixed alphabet first, then allowlisting the *encoded* string,
// mirrors validRemotePath's accepted shape for a value that couldn't
// otherwise take it. See docs/security.md and docs/agent-board.md's
// Security model section.
var validBase64 = regexp.MustCompile(`^[A-Za-z0-9+/]*={0,2}$`)

// sendBase64WrapperScript is a fixed script string, never built from
// tainted input: every piece of caller-supplied data ($1 through $5) is
// delivered as a separate, already-quoted shell positional parameter by
// RunBoardCommand/quoteArgs, never interpolated into this script text.
// send.sh has no stdin option for its body argument (see
// docs/agent-board.md's Integration with agmsg section), so this decodes
// the base64-encoded body back to its original bytes remotely, immediately
// before the one call site that needs the raw value.
const sendBase64WrapperScript = `send="$1"; team="$2"; from="$3"; to="$4"; ` +
	`body=$(printf '%s' "$5" | base64 -d) && exec "$send" "$team" "$from" "$to" "$body" --force`

// sendBase64WrapperScriptName is the dummy $0 value passed to the `sh -c`
// invocation that runs sendBase64WrapperScript — it becomes the script's
// $0, never inspected by the script itself, only present because `sh -c`
// requires a name argument before the real positional parameters ($1..$5).
const sendBase64WrapperScriptName = "board-send"

func validateAgmsgIdentifier(label, value string) error {
	if !validAgmsgIdentifier.MatchString(value) {
		return fmt.Errorf("agmsg: invalid %s %q: must match %s", label, value, validAgmsgIdentifier)
	}
	return nil
}

var _ AgmsgClient = (*RemoteAgmsgClient)(nil)

// RemoteAgmsgClient runs agmsg's scripts on a remote host over the SSH exec
// channel a session's BoardExecutor already exposes.
type RemoteAgmsgClient struct {
	executor  BoardExecutor
	hostID    string
	agmsgPath string // remote directory containing scripts/api.sh, scripts/send.sh (already ~-expanded)
}

// NewRemoteAgmsgClient returns a client for the agmsg installation rooted
// at agmsgPath on the host identified by hostID, reached through executor.
func NewRemoteAgmsgClient(hostID, agmsgPath string, executor BoardExecutor) *RemoteAgmsgClient {
	return &RemoteAgmsgClient{hostID: hostID, agmsgPath: agmsgPath, executor: executor}
}

func (c *RemoteAgmsgClient) HostID() string { return c.hostID }

func (c *RemoteAgmsgClient) apiScript() string  { return c.agmsgPath + "/scripts/api.sh" }
func (c *RemoteAgmsgClient) sendScript() string { return c.agmsgPath + "/scripts/send.sh" }

// Send always passes --force (via sendBase64WrapperScript's fixed "exec ...
// --force" tail) — see docs/agent-board.md's Integration with agmsg
// section for why every board-originated send does.
func (c *RemoteAgmsgClient) Send(ctx context.Context, team, from, to, body string) error {
	if err := validateIdentifiers(team, from, to); err != nil {
		return err
	}

	encoded := base64.StdEncoding.EncodeToString([]byte(body))
	if !validBase64.MatchString(encoded) {
		return errors.New("agmsg: encoded body failed base64 allowlist validation")
	}

	args := []string{
		"sh", "-c", sendBase64WrapperScript, sendBase64WrapperScriptName,
		c.sendScript(), team, from, to, encoded,
	}
	if _, err := c.executor.RunBoardCommand(ctx, args); err != nil {
		return fmt.Errorf("agmsg send.sh (remote): %w", err)
	}
	return nil
}

func (c *RemoteAgmsgClient) Since(ctx context.Context, team, afterID string, limit int) ([]Row, error) {
	if err := validateAgmsgIdentifier("team", team); err != nil {
		return nil, err
	}

	args := []string{c.apiScript(), "get", "teams", team, "messages", "--limit", strconv.Itoa(limit)}
	out, err := c.executor.RunBoardCommand(ctx, args)
	if err != nil {
		return nil, fmt.Errorf("agmsg api.sh (remote): %w", err)
	}
	return filterRowsAfter(parseAgmsgMessageRows(out, c.HostID()), afterID), nil
}

func validateIdentifiers(team, from, to string) error {
	if err := validateAgmsgIdentifier("team", team); err != nil {
		return err
	}
	if err := validateAgmsgIdentifier("from", from); err != nil {
		return err
	}
	if err := validateAgmsgIdentifier("to", to); err != nil {
		return err
	}
	return nil
}
