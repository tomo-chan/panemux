package session

import (
	"fmt"
	"io"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

var validTmuxSessionName = regexp.MustCompile(`^[a-zA-Z0-9_.-]+$`)

// TmuxSSHSession attaches to a tmux session on a remote host via SSH.
type TmuxSSHSession struct {
	stdin             io.WriteCloser
	reader            io.Reader
	client            *ssh.Client
	session           *ssh.Session
	jumpClient        *ssh.Client
	keepaliveStop     chan struct{}
	title             string
	tmuxSession       string
	connectionName    string
	state             State
	id                string
	mu                sync.RWMutex
	keepaliveStopOnce sync.Once
}

// NewTmuxSSH creates a session that attaches to a remote tmux session.
func NewTmuxSSH(id, title, tmuxSession string, cfg SSHConfig) (*TmuxSSHSession, error) {
	tmuxSession, err := validateTmuxSessionName(tmuxSession)
	if err != nil {
		return nil, err
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
		keepaliveStop:  make(chan struct{}),
	}

	monitorSSHSession(sess, pw, func(state State) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state = state
	})

	startSSHKeepalive(client, s.keepaliveStop, sshKeepaliveOnDeadFn(client, jumpClient))

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
	return cmd + " -c " + shellQuotePath(cfg.Cwd), nil
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
	out, err := runOutputWithTimeout(
		sess.Output,
		sess.Close,
		fmt.Sprintf("tmux display-message -p -t '%s' '#{pane_current_path}'", s.tmuxSession),
		sshRunTimeout,
	)
	if err != nil {
		return "", fmt.Errorf("tmux display-message over ssh: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetActiveWorkdirs returns every distinct working directory currently in
// play for the newest active interactive Codex or Claude process under the
// active remote tmux pane, including worktrees only visited by a delegated
// Claude Task subagent.
func (s *TmuxSSHSession) GetActiveWorkdirs() ([]string, error) {
	return tmuxSSHActiveWorkdirsFromSessionFactory(
		func() (sshSessionRunner, error) {
			sess, err := s.client.NewSession()
			if err != nil {
				return nil, err
			}
			return &timeoutSessionRunner{sess: sess}, nil
		},
		fmt.Sprintf("session=%s type=%s tmux_session=%s", s.id, s.Type(), s.tmuxSession),
		s.tmuxSession,
	)
}

// InspectGitContext resolves Git metadata on the remote host for the provided
// absolute working directory.
func (s *TmuxSSHSession) InspectGitContext(cwd string) (GitContext, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return GitContext{}, fmt.Errorf("new ssh session for tmux git context: %w", err)
	}
	defer sess.Close()

	return remoteGitContext(&timeoutSessionRunner{sess: sess}, cwd)
}

func parseRemoteTmuxPaneInfo(out []byte) (int, string, error) {
	fields := strings.SplitN(strings.TrimSpace(string(out)), "\t", 2)
	if len(fields) != 2 {
		return 0, "", fmt.Errorf("parse remote tmux pane info: unexpected output %q", strings.TrimSpace(string(out)))
	}

	panePID, err := strconv.Atoi(strings.TrimSpace(fields[0]))
	if err != nil {
		return 0, "", fmt.Errorf("parse remote tmux pane pid: %w", err)
	}

	return panePID, strings.TrimSpace(fields[1]), nil
}

func tmuxSSHActiveWorkdirs(runner sshSessionRunner, logScope, tmuxSession string) ([]string, error) {
	out, err := runner.Output(
		fmt.Sprintf(
			"tmux display-message -p -t '%s' '#{pane_pid}\t#{pane_current_path}'",
			tmuxSession,
		),
	)
	if err != nil {
		log.Printf("%s tmux pane info lookup failed: %v", logScope, err)
		return nil, fmt.Errorf("tmux pane info over ssh: %w", err)
	}
	panePID, baseCWD, err := parseRemoteTmuxPaneInfo(out)
	if err != nil {
		log.Printf("%s %v", logScope, err)
		return nil, err
	}
	log.Printf("%s active tmux pane pid=%d base_cwd=%q", logScope, panePID, baseCWD)

	return activeRemoteWorkdirs(runner, logScope, baseCWD, panePID)
}

func tmuxSSHActiveWorkdirsFromSessionFactory(
	newRunner func() (sshSessionRunner, error),
	logScope, tmuxSession string,
) ([]string, error) {
	paneRunner, err := newRunner()
	if err != nil {
		return nil, fmt.Errorf("new ssh session for active tmux pane info: %w", err)
	}
	defer paneRunner.Close()

	out, err := paneRunner.Output(
		fmt.Sprintf(
			"tmux display-message -p -t '%s' '#{pane_pid}\t#{pane_current_path}'",
			tmuxSession,
		),
	)
	if err != nil {
		log.Printf("%s tmux pane info lookup failed: %v", logScope, err)
		return nil, fmt.Errorf("tmux pane info over ssh: %w", err)
	}
	panePID, baseCWD, err := parseRemoteTmuxPaneInfo(out)
	if err != nil {
		log.Printf("%s %v", logScope, err)
		return nil, err
	}
	log.Printf("%s active tmux pane pid=%d base_cwd=%q", logScope, panePID, baseCWD)

	return activeRemoteWorkdirsWithOutput(
		outputFromSessionFactory(newRunner),
		logScope,
		baseCWD,
		panePID,
	)
}

func (s *TmuxSSHSession) Close() error {
	s.keepaliveStopOnce.Do(func() { close(s.keepaliveStop) })

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
