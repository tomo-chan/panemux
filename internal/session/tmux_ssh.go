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
		return nil, fmt.Errorf("invalid tmux session name %q: must match ^[a-zA-Z0-9_.-]+$", tmuxSession)
	}

	client, jumpClient, err := dialSSHClient(cfg)
	if err != nil {
		return nil, err
	}

	sess, err := client.NewSession()
	if err != nil {
		client.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
		return nil, fmt.Errorf("new ssh session: %w", err)
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		sess.Close()
		client.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}

	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw

	// Attach to existing tmux session or create it if absent.
	// -c sets the working directory for newly created sessions; it has no
	// effect when attaching to an existing session.
	// (tmuxSession is validated as [a-zA-Z0-9_.-]+ by config)
	tmuxCmd := fmt.Sprintf("tmux new-session -As '%s'", tmuxSession)
	if cfg.Cwd != "" {
		// Validate cwd with the regex guard before embedding in the shell command
		// (CodeQL go/command-injection recommended pattern for arguments).
		if !validRemotePath.MatchString(cfg.Cwd) {
			sess.Close()
			client.Close()
			if jumpClient != nil {
				jumpClient.Close()
			}
			return nil, fmt.Errorf("invalid working directory %q: must be an absolute path with no shell metacharacters", cfg.Cwd)
		}
		tmuxCmd += fmt.Sprintf(" -c %s", shellQuotePath(cfg.Cwd))
	}
	if err := sess.Start(tmuxCmd); err != nil {
		sess.Close()
		client.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
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

	go func() {
		sess.Wait()
		pw.Close()
		s.mu.Lock()
		s.state = StateExited
		s.mu.Unlock()
	}()

	return s, nil
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
