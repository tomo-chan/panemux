package session

import (
	"fmt"
	"io"
	"net"
	"regexp"
	"sync"

	"golang.org/x/crypto/ssh"
)

var validTmuxSessionName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// TmuxSSHSession attaches to a tmux session on a remote host via SSH.
type TmuxSSHSession struct {
	mu          sync.RWMutex
	id          string
	title       string
	tmuxSession string
	state       State
	client      *ssh.Client
	session     *ssh.Session
	stdin       io.WriteCloser
	reader      io.Reader
}

// NewTmuxSSH creates a session that attaches to a remote tmux session.
func NewTmuxSSH(id, title, tmuxSession string, cfg SSHConfig) (*TmuxSSHSession, error) {
	if tmuxSession == "" {
		tmuxSession = "0"
	}
	if !validTmuxSessionName.MatchString(tmuxSession) {
		return nil, fmt.Errorf("invalid tmux session name %q: must match ^[a-zA-Z0-9_.-]+$", tmuxSession)
	}

	authMethods, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, err
	}

	hkCallback, err := buildHostKeyCallback(cfg.KnownHostsFile)
	if err != nil {
		return nil, err
	}

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: hkCallback,
	}

	port := cfg.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", port))

	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", addr, err)
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("ssh handshake: %w", err)
	}

	client := ssh.NewClient(sshConn, chans, reqs)

	sess, err := client.NewSession()
	if err != nil {
		client.Close()
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
		return nil, fmt.Errorf("request pty: %w", err)
	}

	stdin, err := sess.StdinPipe()
	if err != nil {
		sess.Close()
		client.Close()
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
			return nil, fmt.Errorf("invalid working directory %q: must be an absolute path with no shell metacharacters", cfg.Cwd)
		}
		tmuxCmd += fmt.Sprintf(" -c %s", shellQuotePath(cfg.Cwd))
	}
	if err := sess.Start(tmuxCmd); err != nil {
		sess.Close()
		client.Close()
		return nil, fmt.Errorf("starting tmux attach: %w", err)
	}

	s := &TmuxSSHSession{
		id:          id,
		title:       title,
		tmuxSession: tmuxSession,
		state:       StateConnected,
		client:      client,
		session:     sess,
		stdin:       stdin,
		reader:      pr,
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

func (s *TmuxSSHSession) Close() error {
	s.mu.Lock()
	s.state = StateExited
	s.mu.Unlock()

	s.stdin.Close()
	s.session.Close()
	return s.client.Close()
}
