package session

import (
	"io"
	"panemux/internal/agent"

	"panemux/internal/config"
)

// Type represents the type of terminal session.
type Type string

// The wire values themselves are owned by internal/config, which defines
// PaneConfig.Type's schema; createSession converts a pane's raw string with
// Type(pane.Type), so the two sets must agree exactly.
const (
	TypeLocal   Type = config.PaneTypeLocal
	TypeSSH     Type = config.PaneTypeSSH
	TypeTmux    Type = config.PaneTypeTmux
	TypeSSHTmux Type = config.PaneTypeSSHTmux
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

// AgentTypeDetector reports the registered ID of a live interactive agent
// descended from the pane shell. It uses the registry Board rules without
// resolving transcript workdirs; the messaging adapter handles participation.
type AgentTypeDetector interface {
	DetectInteractiveAgent() (agentID agent.ID, ok bool, err error)
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

// PreviewTarget is implemented by sessions whose shell runs on an SSH host.
// The split preview reaches that host as the session does (issue #387):
// PreviewSSHConfig is the connection the session was dialed with, not its
// ssh_connections entry as it reads now, which may have been edited since.
type PreviewTarget interface {
	SSHConnNamer
	PreviewSSHConfig() SSHConfig
}

// previewSSHConfig is cfg as the host is reached: where a pane's shell
// starts on it is the pane's own, not the connection's.
func previewSSHConfig(cfg SSHConfig) SSHConfig {
	cfg.Cwd, cfg.Shell = "", ""
	return cfg
}

// boardHostIDLocal is the BoardHostID value shared by every local/tmux-local
// session, identifying the host panemux itself runs on.
const boardHostIDLocal = "local"

// BoardHostID is implemented by every session type. It returns the
// identifier of the messaging host this session's pane
// participates in: "local" for local/tmux sessions, the SSH connection
// name for ssh/ssh_tmux sessions.
type BoardHostID interface {
	BoardHostID() string
}

// DirectoryEntry represents a browsable directory in a filesystem tree.
type DirectoryEntry struct {
	Name        string
	Path        string
	HasChildren bool
}
