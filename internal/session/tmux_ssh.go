package session

import (
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

var validTmuxSessionName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// TmuxSSHSession attaches to a tmux session on a remote host via SSH.
type TmuxSSHSession struct {
	mu             sync.RWMutex
	id             string
	title          string
	tmuxSession    string
	state          State
	client         *ssh.Client
	session        *ssh.Session
	stdin          io.WriteCloser
	reader         io.Reader
	connectionName string
	jumpClient     *ssh.Client // non-nil when connected via ProxyJump; closed after client
}

// NewTmuxSSH creates a session that attaches to a remote tmux session.
func NewTmuxSSH(id, title, tmuxSession string, cfg SSHConfig) (*TmuxSSHSession, error) {
	if tmuxSession == "" {
		tmuxSession = "0"
	}
	if !validTmuxSessionName.MatchString(tmuxSession) {
		return nil, fmt.Errorf(
			"invalid tmux session name %q: must match ^[a-zA-Z0-9_.-]+$",
			tmuxSession,
		)
	}

	client, jumpClient, err := dialSSHClient(cfg)
	if err != nil {
		return nil, err
	}

	sess, err := client.NewSession()
	if err != nil {
		closeSSHResources(nil, client, jumpClient)
		return nil, fmt.Errorf("new ssh session: %w", err)
	}

	stdin, pr, pw, err := setupSSHPTY(sess)
	if err != nil {
		closeSSHResources(sess, client, jumpClient)
		return nil, err
	}

	tmuxCmd, err := tmuxSSHCommand(tmuxSession, cfg)
	if err != nil {
		closeSSHResources(sess, client, jumpClient)
		return nil, err
	}

	if err := sess.Start(tmuxCmd); err != nil {
		closeSSHResources(sess, client, jumpClient)
		return nil, fmt.Errorf("starting tmux attach: %w", err)
	}

	s := &TmuxSSHSession{
		id:             id,
		title:          title,
		tmuxSession:    tmuxSession,
		state:          StateConnected,
		client:         client,
		session:        sess,
		stdin:          stdin,
		reader:         pr,
		connectionName: cfg.ConnectionName,
		jumpClient:     jumpClient,
	}

	monitorSSHSession(sess, pw, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state = StateExited
	})

	return s, nil
}

// tmuxSSHCommand builds the remote tmux command.
// -As attaches to an existing session or creates one if absent.
// -c sets the working directory for newly created sessions only; it has no
// effect when attaching to an existing session.
func tmuxSSHCommand(tmuxSession string, cfg SSHConfig) (string, error) {
	cmd := fmt.Sprintf("tmux new-session -As '%s'", tmuxSession)
	if cfg.Cwd == "" {
		return cmd, nil
	}
	if err := validateRemotePath("working directory", cfg.Cwd); err != nil {
		return "", err
	}
	return cmd + fmt.Sprintf(" -c %s", shellQuotePath(cfg.Cwd)), nil
}

func (s *TmuxSSHSession) ID() string    { return s.id }
func (s *TmuxSSHSession) Type() Type    { return TypeSSHTmux }
func (s *TmuxSSHSession) Title() string { return s.title }

func (s *TmuxSSHSession) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *TmuxSSHSession) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

func (s *TmuxSSHSession) Write(p []byte) (int, error) {
	return s.stdin.Write(p)
}

func (s *TmuxSSHSession) Resize(cols, rows uint16) error {
	return s.session.WindowChange(int(rows), int(cols))
}

// ConnectionName returns the panemux connection alias for this SSH session.
func (s *TmuxSSHSession) ConnectionName() string { return s.connectionName }

// GetCWD runs `tmux display-message` over a new SSH exec channel to get the active pane's CWD.
func (s *TmuxSSHSession) GetCWD() (string, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new ssh session for tmux cwd: %w", err)
	}
	defer sess.Close()
	out, err := sess.Output(fmt.Sprintf("tmux display-message -p -t '%s' '#{pane_current_path}'", s.tmuxSession))
	if err != nil {
		return "", fmt.Errorf("tmux display-message over ssh: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func (s *TmuxSSHSession) Close() error {
	s.mu.Lock()
	s.state = StateExited
	s.mu.Unlock()

	s.stdin.Close()
	s.session.Close()
	err := s.client.Close()
	if s.jumpClient != nil {
		s.jumpClient.Close()
	}
	return err
}
