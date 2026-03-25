package session

import (
	"bufio"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os"
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

// knownHostsAlgorithms returns the host key algorithms stored in knownHostsFile
// for the given address. It supports both plaintext and hashed (|1|...) hostname entries.
// This populates ssh.ClientConfig.HostKeyAlgorithms so Go's SSH library negotiates
// the same algorithm that is stored in known_hosts, preventing false "key mismatch" errors
// that occur when the server offers a different algorithm than the one recorded.
func knownHostsAlgorithms(knownHostsFile, address string) []string {
	f, err := os.Open(knownHostsFile)
	if err != nil {
		return nil
	}
	defer f.Close()

	normalized := knownhosts.Normalize(address)

	seen := make(map[string]bool)
	var algos []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "@") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		hostsField := fields[0]
		keyType := fields[1]
		if knownHostsFieldMatchesAddr(hostsField, normalized) && !seen[keyType] {
			seen[keyType] = true
			algos = append(algos, keyType)
		}
	}

	return algos
}

// knownHostsFieldMatchesAddr checks whether any pattern in a known_hosts hosts field
// (comma-separated) matches the given normalized address.
func knownHostsFieldMatchesAddr(hostsField, normalizedAddr string) bool {
	for _, pattern := range strings.Split(hostsField, ",") {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" || strings.HasPrefix(pattern, "!") {
			continue
		}
		if strings.HasPrefix(pattern, "|1|") {
			if knownHostsHashedEntryMatches(pattern, normalizedAddr) {
				return true
			}
		} else {
			if knownhosts.Normalize(pattern) == normalizedAddr {
				return true
			}
		}
	}
	return false
}

// knownHostsHashedEntryMatches checks whether the hashed known_hosts entry
// (|1|salt|hash format) matches the normalized address.
func knownHostsHashedEntryMatches(entry, normalizedAddr string) bool {
	parts := strings.Split(entry, "|")
	if len(parts) != 4 || parts[1] != "1" {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	expected, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(normalizedAddr))
	return hmac.Equal(mac.Sum(nil), expected)
}

// dialSSHClient establishes an SSH client connection, transparently handling ProxyJump.
// Returns (client, jumpClient, error). jumpClient is non-nil only when a ProxyJump is
// used; the caller must close jumpClient after closing client.
func dialSSHClient(cfg SSHConfig) (*ssh.Client, *ssh.Client, error) {
	authMethods, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, nil, err
	}

	khFile, err := resolveKnownHostsFile(cfg.KnownHostsFile)
	if err != nil {
		return nil, nil, err
	}
	hkCallback, err := knownhosts.New(khFile)
	if err != nil {
		return nil, nil, fmt.Errorf("loading known_hosts %s: %w", khFile, err)
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
	// Advertise only the algorithms present in known_hosts for this host.
	// Without this, Go's SSH library may negotiate a different algorithm than
	// what is stored, causing a "key mismatch" error even though the key is valid.
	if algos := knownHostsAlgorithms(khFile, addr); len(algos) > 0 {
		sshCfg.HostKeyAlgorithms = algos
	}

	var conn net.Conn
	var jumpClient *ssh.Client

	if cfg.JumpHost != nil {
		conn, jumpClient, err = dialThroughJump(*cfg.JumpHost, addr)
	} else {
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
