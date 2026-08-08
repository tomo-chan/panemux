package session

import (
	"context"
	"io"
)

// Type represents the type of terminal session.
type Type string

const (
	TypeLocal   Type = "local"
	TypeSSH     Type = "ssh"
	TypeTmux    Type = "tmux"
	TypeSSHTmux Type = "ssh_tmux"
)

// State represents the current state of a session.
type State string

const (
	StateConnecting   State = "connecting"
	StateConnected    State = "connected"
	StateDisconnected State = "disconnected"
	StateExited       State = "exited"
)

// Session is the interface all terminal session types must implement.
type Session interface {
	// ID returns the unique session identifier.
	ID() string

	// Type returns the session type.
	Type() Type

	// Title returns the human-readable session title.
	Title() string

	// State returns the current connection state.
	State() State

	// Read reads output from the terminal (implements io.Reader).
	Read(p []byte) (n int, err error)

	// Write sends input to the terminal (implements io.Writer).
	Write(p []byte) (n int, err error)

	// Resize resizes the terminal window.
	Resize(cols, rows uint16) error

	// Close terminates the session.
	Close() error
}

// ensure Session embeds io.ReadWriter
var _ io.ReadWriter = (Session)(nil)

// CWDGetter is implemented by sessions that can report their live working directory.
type CWDGetter interface {
	GetCWD() (string, error)
}

// ActiveWorkdirGetter is implemented by sessions that can detect every
// distinct working directory currently in play for an active Codex/Claude
// child process, including sibling worktrees only visited by a delegated
// Claude Task subagent.
type ActiveWorkdirGetter interface {
	GetActiveWorkdirs() ([]string, error)
}

// GitContext describes the repository state for a session's working directory.
type GitContext struct {
	Branch    string
	CommonDir string
	OriginURL string
	Repo      string
	Root      string
}

// GitContextGetter is implemented by sessions that must inspect Git state
// somewhere other than the local filesystem, such as a remote SSH host.
type GitContextGetter interface {
	InspectGitContext(cwd string) (GitContext, error)
}

// SSHConnNamer is implemented by sessions that have an SSH connection name.
type SSHConnNamer interface {
	ConnectionName() string
}

// LocalBoardHostID is the BoardHostID() value shared by local and local-tmux
// sessions: the agmsg installation on the host panemux itself runs on.
const LocalBoardHostID = "local"

// BoardHostID is implemented by every session type. It returns the
// identifier of the host whose agmsg installation this session's pane
// participates in: "local" for local/tmux sessions, the SSH connection name
// for ssh/ssh_tmux sessions. See docs/agent-board.md's "internal/session
// capability interfaces".
type BoardHostID interface {
	BoardHostID() string
}

// BoardExecutor is implemented by SSH-backed sessions. It runs an agmsg
// script (scriptPath, an absolute remote path) on the remote host over the
// session's existing exec channel, passing args (opaque values: verb, flag,
// team name, message body, ...) to it. RunBoardCommand's implementation
// (internal/session/ssh.go) validates scriptPath the same way cwd already
// is, and delivers every element of args to the remote shell over the SSH
// session's stdin rather than the command string — see the
// RunBoardCommand/buildBoardCommand implementation and
// docs/agent-board.md#security-model's "Open implementation question" for
// why. scriptPath and args are deliberately two separate parameters, not
// one combined slice: a single []string mixing a trusted, validated path
// with untrusted, potentially agent-authored argument content is not
// something this repository's CodeQL setup can prove safe purely by which
// index a value is read from — see the note in
// docs/agent-board.md#security-model for the two prior approaches on PR
// #163 that CodeQL rejected before this shape did. The caller passes raw,
// unescaped values for both, exactly like exec.Command's own argv contract,
// so there is exactly one place this can be gotten wrong rather than one
// per call site. Neither api.sh (reads) nor send.sh (writes) has a stdin
// option of its own for these values — RunBoardCommand's generated remote
// shell script is what bridges panemux's stdin delivery back to the
// positional-argument form those scripts require.
type BoardExecutor interface {
	RunBoardCommand(ctx context.Context, scriptPath string, args []string) ([]byte, error)
}

// BoardHomeDirer is implemented by SSH-backed sessions. It resolves the
// remote user's home directory once per SSH connection (a single
// `echo -n "$HOME"` probe over the existing exec channel), cached for the
// life of that connection, so a leading `~` in agent_board.agmsg_path can be
// expanded locally before it ever reaches a BoardExecutor argument — a
// literal `~` placed inside RunBoardCommand's escaping would never expand on
// the remote side. See docs/agent-board.md's "Integration with agmsg".
type BoardHomeDirer interface {
	BoardHomeDir(ctx context.Context) (string, error)
}

// DirectoryEntry represents a browsable directory in a filesystem tree.
type DirectoryEntry struct {
	Name        string
	Path        string
	HasChildren bool
}
