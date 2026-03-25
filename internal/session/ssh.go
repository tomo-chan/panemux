package session

import (
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// validRemotePath is the CodeQL-recommended regex guard for shell arguments.
// It matches absolute paths that contain no shell metacharacters, making the
// value safe to embed in a remote shell command via shellQuotePath.
// Allowed: any character except shell metacharacters (;|&$`'"<>(){}[]!\)
// and control characters (newlines, null bytes, etc.).
var validRemotePath = regexp.MustCompile(`^(/[^;|&$` + "`" + `'"<>()\[\]{}!\\\x00-\x1f\x7f]*)+$`)

// SSHSession manages an SSH connection with a PTY.
type SSHSession struct {
	mu      sync.RWMutex
	id      string
	title   string
	state   State
	client  *ssh.Client
	session *ssh.Session
	stdin   io.WriteCloser
	stdout  io.Reader
	// combined reader for stdout+stderr
	reader         io.Reader
	connectionName string
	jumpClient     *ssh.Client // non-nil when connected via ProxyJump; closed after client
}

// SSHConfig holds parameters for establishing an SSH connection.
type SSHConfig struct {
	Host           string
	Port           int
	User           string
	KeyFile        string
	Password       string
	KnownHostsFile string
	Cwd            string     // initial working directory on the remote host
	ConnectionName string     // alias used in panemux (for VSCode Remote SSH)
	JumpHost       *SSHConfig // non-nil when ProxyJump is configured
	ProxyCommand   string     // shell command used as stdin/stdout pipe (ProxyCommand directive)
}

// shellQuotePath wraps path in single quotes and escapes any single quotes
// within the path, making it safe to embed in a POSIX shell command.
func shellQuotePath(path string) string {
	return "'" + strings.ReplaceAll(path, "'", `'\''`) + "'"
}

// resolveKnownHostsFile returns the known_hosts file path, defaulting to ~/.ssh/known_hosts.
func resolveKnownHostsFile(knownHostsFile string) (string, error) {
	if knownHostsFile != "" {
		return knownHostsFile, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home dir: %w", err)
	}
	return filepath.Join(home, ".ssh", "known_hosts"), nil
}

// dialSSHClient establishes an SSH client connection, transparently handling ProxyJump
// and ProxyCommand. Returns (client, jumpClient, error). jumpClient is non-nil only when
// a ProxyJump is used; the caller must close jumpClient after closing client.
func dialSSHClient(cfg SSHConfig) (*ssh.Client, *ssh.Client, error) {
	authMethods, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, nil, err
	}

	hkCallback, err := buildHostKeyCallback(cfg.KnownHostsFile)
	if err != nil {
		return nil, nil, err
	}

	port := cfg.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(cfg.Host, fmt.Sprintf("%d", port))

	sshCfg := &ssh.ClientConfig{
		User:            cfg.User,
		Auth:            authMethods,
		HostKeyCallback: hkCallback,
		Timeout:         30 * time.Second,
	}

	var conn net.Conn
	var jumpClient *ssh.Client

	switch {
	case cfg.JumpHost != nil:
		conn, jumpClient, err = dialThroughJump(*cfg.JumpHost, addr)
	case cfg.ProxyCommand != "":
		conn, err = dialViaProxyCommand(cfg.ProxyCommand, cfg.Host, port)
	default:
		conn, err = net.DialTimeout("tcp", addr, 30*time.Second)
	}
	if err != nil {
		return nil, nil, err
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(conn, addr, sshCfg)
	if err != nil {
		conn.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
		return nil, nil, fmt.Errorf("ssh handshake: %w", err)
	}

	return ssh.NewClient(sshConn, chans, reqs), jumpClient, nil
}

// dialThroughJump connects to targetAddr by tunneling through a ProxyJump host.
// Returns (conn to target, jumpClient, error). The jumpClient must be kept open
// as long as conn is in use and closed when the target session ends.
func dialThroughJump(jumpCfg SSHConfig, targetAddr string) (net.Conn, *ssh.Client, error) {
	jumpClient, nestedJump, err := dialSSHClient(jumpCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("dial jump host: %w", err)
	}
	// nestedJump would be non-nil for multi-hop chains; close it when jumpClient closes.
	// ssh.Client.Close() closes the underlying connection, which closes nestedJump's channel.
	// Still, hold a reference so we can close it explicitly on error.
	if nestedJump != nil {
		defer func() {
			if err != nil {
				nestedJump.Close()
			}
		}()
	}

	conn, err := jumpClient.Dial("tcp", targetAddr)
	if err != nil {
		jumpClient.Close()
		return nil, nil, fmt.Errorf("dial target through jump host: %w", err)
	}

	return conn, jumpClient, nil
}

// proxyCommandConn wraps an exec.Cmd's stdin/stdout as a net.Conn, mirroring
// OpenSSH's ProxyCommand behaviour where a subprocess acts as a transparent
// bidirectional pipe to the remote host.
type proxyCommandConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
}

func (c *proxyCommandConn) Read(p []byte) (int, error)  { return c.stdout.Read(p) }
func (c *proxyCommandConn) Write(p []byte) (int, error) { return c.stdin.Write(p) }
func (c *proxyCommandConn) Close() error {
	c.stdin.Close()
	c.stdout.Close()
	if c.cmd.Process != nil {
		c.cmd.Process.Kill() //nolint:errcheck
	}
	return c.cmd.Wait()
}
func (c *proxyCommandConn) LocalAddr() net.Addr                { return proxyAddr("proxy-local") }
func (c *proxyCommandConn) RemoteAddr() net.Addr               { return proxyAddr("proxy-remote") }
func (c *proxyCommandConn) SetDeadline(_ time.Time) error      { return nil }
func (c *proxyCommandConn) SetReadDeadline(_ time.Time) error  { return nil }
func (c *proxyCommandConn) SetWriteDeadline(_ time.Time) error { return nil }

// proxyAddr is a minimal net.Addr used by proxyCommandConn.
type proxyAddr string

func (a proxyAddr) Network() string { return "proxy" }
func (a proxyAddr) String() string  { return string(a) }

// substituteProxyCommand replaces %h (hostname), %p (port), and %% (literal %)
// in a ProxyCommand string, matching OpenSSH token substitution.
func substituteProxyCommand(cmd, host string, port int) string {
	// Temporarily replace %% to avoid double-substitution
	result := strings.ReplaceAll(cmd, "%%", "\x00")
	result = strings.ReplaceAll(result, "%h", host)
	result = strings.ReplaceAll(result, "%p", fmt.Sprintf("%d", port))
	return strings.ReplaceAll(result, "\x00", "%")
}

// dialViaProxyCommand runs the ProxyCommand and returns a net.Conn backed by the
// subprocess stdin/stdout, mirroring how OpenSSH handles ProxyCommand.
// The command is passed to /bin/sh -c so shell quoting and features work as expected.
func dialViaProxyCommand(proxyCmd, host string, port int) (net.Conn, error) {
	cmd := substituteProxyCommand(proxyCmd, host, port)
	// Pass to /bin/sh -c so the command is interpreted by a shell, matching
	// OpenSSH behaviour. /bin/sh is a hardcoded trusted binary.
	c := exec.Command("/bin/sh", "-c", cmd) //nolint:gosec -- cmd is from trusted ~/.ssh/config
	c.Stderr = os.Stderr

	stdin, err := c.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("proxy command stdin: %w", err)
	}
	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("proxy command stdout: %w", err)
	}
	if err := c.Start(); err != nil {
		return nil, fmt.Errorf("starting proxy command: %w", err)
	}
	return &proxyCommandConn{cmd: c, stdin: stdin, stdout: stdout}, nil
}

// NewSSH creates and starts a new SSH terminal session.
func NewSSH(id, title string, cfg SSHConfig) (*SSHSession, error) {
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

	// Request PTY
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

	// Start the shell. If a working directory is configured, validate it with
	// the regex guard (CodeQL go/command-injection recommended pattern for
	// arguments) before embedding it in the remote shell command.
	// sess.Shell() and sess.Start() are mutually exclusive in the SSH protocol.
	var startErr error
	if cfg.Cwd != "" {
		if !validRemotePath.MatchString(cfg.Cwd) {
			sess.Close()
			client.Close()
			if jumpClient != nil {
				jumpClient.Close()
			}
			return nil, fmt.Errorf("invalid working directory %q: must be an absolute path with no shell metacharacters", cfg.Cwd)
		}
		startErr = sess.Start(fmt.Sprintf("cd %s && exec $SHELL", shellQuotePath(cfg.Cwd)))
	} else {
		startErr = sess.Shell()
	}
	if startErr != nil {
		sess.Close()
		client.Close()
		if jumpClient != nil {
			jumpClient.Close()
		}
		return nil, fmt.Errorf("start shell: %w", startErr)
	}

	s := &SSHSession{
		id:             id,
		title:          title,
		state:          StateConnected,
		client:         client,
		session:        sess,
		stdin:          stdin,
		reader:         pr,
		connectionName: cfg.ConnectionName,
		jumpClient:     jumpClient,
	}

	// Monitor session exit
	go func() {
		sess.Wait()
		pw.Close()
		s.mu.Lock()
		s.state = StateExited
		s.mu.Unlock()
	}()

	return s, nil
}

func buildHostKeyCallback(knownHostsFile string) (ssh.HostKeyCallback, error) {
	path, err := resolveKnownHostsFile(knownHostsFile)
	if err != nil {
		return nil, err
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, fmt.Errorf("loading known_hosts %s: %w", path, err)
	}
	return cb, nil
}

func buildAuthMethods(cfg SSHConfig) ([]ssh.AuthMethod, error) {
	var methods []ssh.AuthMethod

	if cfg.KeyFile != "" {
		keyData, err := os.ReadFile(cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("reading key file %s: %w", cfg.KeyFile, err)
		}
		signer, err := ssh.ParsePrivateKey(keyData)
		if err != nil {
			return nil, fmt.Errorf("parsing private key: %w", err)
		}
		methods = append(methods, ssh.PublicKeys(signer))
	}

	if cfg.Password != "" {
		methods = append(methods, ssh.Password(cfg.Password))
	}

	// If no explicit auth method, try common default key files (mirrors OpenSSH behaviour).
	if len(methods) == 0 {
		home, _ := os.UserHomeDir()
		for _, name := range []string{"id_ed25519", "id_rsa", "id_ecdsa"} {
			keyData, err := os.ReadFile(filepath.Join(home, ".ssh", name))
			if err != nil {
				continue
			}
			signer, err := ssh.ParsePrivateKey(keyData)
			if err != nil {
				continue
			}
			methods = append(methods, ssh.PublicKeys(signer))
			break
		}
	}

	if len(methods) == 0 {
		return nil, fmt.Errorf("no auth methods configured for SSH connection")
	}

	return methods, nil
}

func (s *SSHSession) ID() string    { return s.id }
func (s *SSHSession) Type() Type    { return TypeSSH }
func (s *SSHSession) Title() string { return s.title }

func (s *SSHSession) State() State {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

func (s *SSHSession) Read(p []byte) (int, error) {
	return s.reader.Read(p)
}

func (s *SSHSession) Write(p []byte) (int, error) {
	return s.stdin.Write(p)
}

func (s *SSHSession) Resize(cols, rows uint16) error {
	return s.session.WindowChange(int(rows), int(cols))
}

func (s *SSHSession) Close() error {
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

// ConnectionName returns the panemux connection alias for this SSH session.
func (s *SSHSession) ConnectionName() string { return s.connectionName }

// GetCWD runs `pwd` on a new exec channel over the existing SSH connection.
func (s *SSHSession) GetCWD() (string, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new ssh session for pwd: %w", err)
	}
	defer sess.Close()
	out, err := sess.Output("pwd")
	if err != nil {
		return "", fmt.Errorf("pwd over ssh: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}
