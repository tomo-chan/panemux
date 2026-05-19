package session

import "io"

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

// ActiveWorkdirGetter is implemented by sessions that can detect the current
// working directory of an active Codex/Claude child process.
type ActiveWorkdirGetter interface {
	GetActiveWorkdir() (string, error)
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

// DirectoryEntry represents a browsable directory in a filesystem tree.
type DirectoryEntry struct {
	Name        string
	Path        string
	HasChildren bool
}
