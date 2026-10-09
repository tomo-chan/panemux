package session

import (
	"context"
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
	client         *ssh.Client
	session        *ssh.Session
	jumpClient     *ssh.Client // non-nil when connected via ProxyJump; closed after client
	stdin          io.WriteCloser
	reader         io.Reader
	id             string
	title          string
	tmuxSession    string
	connectionName string
	state          State
	mu             sync.RWMutex
}

// NewTmuxSSH creates a session that attaches to a remote tmux session.
func NewTmuxSSH(id, title, tmuxSession string, cfg SSHConfig) (*TmuxSSHSession, error) {
	validatedSession, err := validateTmuxSessionName(tmuxSession)
	if err != nil {
		return nil, err
	}

	client, jumpClient, err := dialSSHClient(cfg)
	if err != nil {
		return nil, err
	}
	return newTmuxSSHSessionFromClient(id, title, validatedSession, cfg, client, jumpClient)
}

// NewTmuxSSHAttach attaches a new tmux client to the running session
// tmuxSession on the remote host and never creates one; see
// NewTmuxLocalAttach.
func NewTmuxSSHAttach(id, title, tmuxSession string, cfg SSHConfig) (*TmuxSSHSession, error) {
	validatedSession, err := validateTmuxAttachName(tmuxSession)
	if err != nil {
		return nil, err
	}

	client, jumpClient, err := dialSSHClient(cfg)
	if err != nil {
		return nil, err
	}
	return newTmuxSSHAttachFromClient(id, title, validatedSession, cfg, client, jumpClient)
}

// newTmuxSSHAttachFromClient checks that tmuxSession is running on an
// established SSH transport and attaches to it. tmuxSession must already have
// passed validateTmuxAttachName.
func newTmuxSSHAttachFromClient(
	id, title, tmuxSession string,
	cfg SSHConfig,
	client, jumpClient *ssh.Client,
) (*TmuxSSHSession, error) {
	if err := checkTmuxSSHSession(client, tmuxSession); err != nil {
		closeSSHResources(nil, client, jumpClient)
		return nil, err
	}
	command := tmuxSSHAttachCommand(tmuxSession)
	return startTmuxSSHSession(id, title, tmuxSession, command, cfg, client, jumpClient)
}

// checkTmuxSSHSession fails unless a tmux session named exactly tmuxSession is
// running on the remote host. See checkTmuxLocalSession for why.
func checkTmuxSSHSession(client *ssh.Client, tmuxSession string) error {
	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("new ssh session for tmux has-session: %w", err)
	}
	defer sess.Close()
	if err := sess.Run(tmuxSSHHasSessionCommand(tmuxSession)); err != nil {
		return tmuxSessionNotRunning(tmuxSession, err)
	}
	return nil
}

// tmuxSSHHasSessionCommand quotes the target as tmuxSSHAttachCommand does.
func tmuxSSHHasSessionCommand(tmuxSession string) string {
	return fmt.Sprintf("tmux has-session -t '=%s'", tmuxSession)
}

// newTmuxSSHSessionFromClient completes the remote tmux lifecycle over an
// established SSH transport. It is the host-independent seam used by the
// protocol contract tests and the production constructor alike.
func newTmuxSSHSessionFromClient(
	id, title, tmuxSession string,
	cfg SSHConfig,
	client, jumpClient *ssh.Client,
) (*TmuxSSHSession, error) {
	validatedSession, err := validateTmuxSessionName(tmuxSession)
	if err != nil {
		closeSSHResources(nil, client, jumpClient)
		return nil, err
	}
	tmuxSession = validatedSession

	tmuxCmd, err := tmuxSSHCommand(tmuxSession, cfg)
	if err != nil {
		closeSSHResources(nil, client, jumpClient)
		return nil, err
	}
	return startTmuxSSHSession(id, title, tmuxSession, tmuxCmd, cfg, client, jumpClient)
}

// startTmuxSSHSession runs tmuxCmd on a PTY over an established SSH
// transport. tmuxSession must already be validated.
func startTmuxSSHSession(
	id, title, tmuxSession, tmuxCmd string,
	cfg SSHConfig,
	client, jumpClient *ssh.Client,
) (*TmuxSSHSession, error) {
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

	monitorSSHSession(sess, pw, func(state State) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state = state
	})

	return s, nil
}

// tmuxSSHAttachCommand builds the remote command of the board's attach: the
// "=" prefix makes tmux match the name exactly and never create a session (see
// tmuxLocalAttachArgs). tmuxSession has passed validateTmuxAttachName, so it
// holds no quote and the single quotes keep it one word.
func tmuxSSHAttachCommand(tmuxSession string) string {
	return fmt.Sprintf("tmux %s -t '=%s'", tmuxAttachSubcommand, tmuxSession)
}

// tmuxSSHCommand builds the remote tmux command.
// -As attaches to an existing session or creates one if absent.
// -c sets the working directory for newly created sessions only; it has no
// effect when attaching to an existing session.
func tmuxSSHCommand(tmuxSession string, cfg SSHConfig) (string, error) {
	cmd := fmt.Sprintf("tmux new-session -As '%s'", tmuxSession)
	if cfg.Cwd != "" {
		if err := validateRemotePath("working directory", cfg.Cwd); err != nil {
			return "", err
		}
		cmd += " -c " + shellQuotePath(cfg.Cwd)
	}
	if !browserShimEnabled.Load() {
		return cmd, nil
	}
	// -e and the shell-command apply only when -A creates a session; they leave
	// existing sessions and already-running shells alone. The bootstrap starts
	// tmux's configured default shell/command after setting the opener PATH.
	// tmux 3.3 introduced the pane-scoped allow-passthrough option. Older tmux
	// and failed installs keep their ordinary attach/create behavior.
	setup := `if tmux -V 2>/dev/null | awk '{ split($2,v,"."); ` +
		`supported=(v[1]+0>3 || (v[1]+0==3 && v[2]+0>=3)) } END { exit (supported==0) }'; then ` +
		remoteBrowserShimInstall() +
		`if [ -x "$PANEMUX_SHIM_DIR/panemux-open" ]; then exec ` + cmd +
		` -e "BROWSER=$PANEMUX_SHIM_DIR/panemux-open"` +
		` -e PANEMUX_SHIM_TMUX=1 ` +
		shellQuotePath("exec /bin/sh -c "+shellQuotePath(remoteTmuxBrowserShell)) +
		`; fi; fi; exec ` + cmd
	// sshd's login shell can be fish or tcsh; the setup is POSIX, one line,
	// quoted into /bin/sh just like the ordinary SSH pane's setup.
	return "exec /bin/sh -c " + shellQuotePath(setup), nil
}

// tmux takes a new pane's PATH from its attaching client, overriding even
// new-session -e PATH. Set it inside the pane before its configured command
// runs instead. The command is operator-owned tmux configuration, handed as
// one argument to the same default shell tmux would otherwise have invoked.
const remoteTmuxBrowserShell = `PANEMUX_SHIM_FALLBACK_PATH="$PATH"; export PANEMUX_SHIM_FALLBACK_PATH; ` +
	`PATH="${BROWSER%/*}:$PATH"; export PATH; ` +
	`PANEMUX_SHIM_COMMAND=$(tmux display-message -p -t "$TMUX_PANE" '#{default-command}'); ` +
	`if [ -n "$PANEMUX_SHIM_COMMAND" ]; then ` +
	`exec "$SHELL" -c "$PANEMUX_SHIM_COMMAND"; fi; ` +
	`case "${SHELL##*/}" in bash|zsh|fish|sh|dash|ksh|csh|tcsh) exec "$SHELL" -l;; *) exec "$SHELL";; esac`

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

// BoardHostID identifies this session's Agent Board host as its SSH
// connection name — the same identifier ConnectionName already returns.
func (s *TmuxSSHSession) BoardHostID() string { return s.connectionName }

// RunBoardCommand runs an agmsg script on the remote host over a new SSH
// exec channel, matching the pattern GetCWD/InspectGitContext already use.
func (s *TmuxSSHSession) RunBoardCommand(ctx context.Context, args []string) ([]byte, error) {
	return runBoardCommand(ctx, func() (sshSessionRunner, error) {
		return s.client.NewSession()
	}, args)
}

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

// GetActiveWorkdirs returns every distinct working directory currently in
// play for the newest active interactive Codex or Claude process under the
// active remote tmux pane, including worktrees only visited by a delegated
// Claude Task subagent.
func (s *TmuxSSHSession) GetActiveWorkdirs() ([]string, error) {
	return tmuxSSHActiveWorkdirsFromSessionFactory(
		func() (sshSessionRunner, error) {
			return s.client.NewSession()
		},
		fmt.Sprintf("session=%s type=%s tmux_session=%s", s.id, s.Type(), s.tmuxSession),
		s.tmuxSession,
	)
}

// DetectInteractiveAgentType reports the agmsg type name of any live agent
// process currently running under the active remote tmux pane — see
// AgentTypeDetector.
func (s *TmuxSSHSession) DetectInteractiveAgentType() (string, bool, error) {
	return tmuxSSHDetectInteractiveAgentTypeFromSessionFactory(
		func() (sshSessionRunner, error) {
			return s.client.NewSession()
		},
		s.tmuxSession,
	)
}

func tmuxSSHDetectInteractiveAgentTypeFromSessionFactory(
	newRunner func() (sshSessionRunner, error), tmuxSession string,
) (string, bool, error) {
	paneRunner, err := newRunner()
	if err != nil {
		return "", false, fmt.Errorf("new ssh session for active tmux pane info: %w", err)
	}
	defer paneRunner.Close()

	out, err := paneRunner.Output(
		fmt.Sprintf(
			"tmux display-message -p -t '%s' '#{pane_pid}\t#{pane_current_path}'",
			tmuxSession,
		),
	)
	if err != nil {
		return "", false, fmt.Errorf("tmux pane info over ssh: %w", err)
	}
	panePID, _, err := parseRemoteTmuxPaneInfo(out)
	if err != nil {
		return "", false, err
	}

	return detectRemoteAgentType(outputFromSessionFactory(newRunner), panePID)
}

// InspectGitContext resolves Git metadata on the remote host for the provided
// absolute working directory.
func (s *TmuxSSHSession) InspectGitContext(cwd string) (GitContext, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return GitContext{}, fmt.Errorf("new ssh session for tmux git context: %w", err)
	}
	defer sess.Close()

	return remoteGitContext(sess, cwd)
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
