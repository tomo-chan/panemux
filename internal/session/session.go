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

// AgentTypeDetector is implemented by every session type. It reports the
// agmsg-recognized type name (e.g. "claude-code", "codex", "gemini") of any
// live, interactive coding-agent process currently running as a descendant
// of this pane's shell, among the set agmsg's own type.conf `detect_proc`
// key considers reliably process-detectable — see agmsgDetectableAgentTypes.
// This is narrower than ActiveWorkdirGetter (which only distinguishes
// Codex/Claude, and additionally resolves transcript-derived workdirs at
// real I/O cost) and returns WHICH type rather than a bare bool, since
// Agent Board's bootstrap flow writes a different onboarding instruction
// per agent type.
type AgentTypeDetector interface {
	DetectInteractiveAgentType() (agmsgType string, ok bool, err error)
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

// boardHostIDLocal is the BoardHostID value shared by every local/tmux-local
// session, identifying the host panemux itself runs on.
const boardHostIDLocal = "local"

// BoardHostID is implemented by every session type. It returns the
// identifier of the host whose agmsg installation this session's pane
// participates in: "local" for local/tmux sessions, the SSH connection
// name for ssh/ssh_tmux sessions.
type BoardHostID interface {
	BoardHostID() string
}

// BoardExecutor is implemented by SSH-backed sessions. It runs an agmsg
// script on the remote host over the session's existing exec channel, as a
// single shell command string built from args. RunBoardCommand itself
// single-quote-escapes every element of args (the same discipline
// internal/session/ssh.go already applies to cwd) before building that
// string — the caller passes raw, unescaped values, exactly like
// exec.Command's own argv contract, so there is exactly one place this can
// be gotten wrong rather than one per call site.
type BoardExecutor interface {
	RunBoardCommand(ctx context.Context, args []string) ([]byte, error)
}

// DirectoryEntry represents a browsable directory in a filesystem tree.
type DirectoryEntry struct {
	Name        string
	Path        string
	HasChildren bool
}
