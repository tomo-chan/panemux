package session

import (
	"bufio"
	"bytes"
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec // G505: OpenSSH hashed known_hosts entries use HMAC-SHA1
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
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

const invalidRemotePathMsg = "must be an absolute path with no shell metacharacters"

// SSHSession manages an SSH connection with a PTY.
type SSHSession struct {
	client     *ssh.Client
	session    *ssh.Session
	jumpClient *ssh.Client // non-nil when connected via ProxyJump; closed after client
	stdin      io.WriteCloser
	// combined reader for stdout+stderr
	reader         io.Reader
	id             string
	title          string
	connectionName string
	state          State
	mu             sync.RWMutex
}

type sshSessionRunner interface {
	Output(cmd string) ([]byte, error)
	Close() error
}

// SSHConfig holds parameters for establishing an SSH connection.
type SSHConfig struct {
	JumpHost       *SSHConfig // non-nil when ProxyJump is configured
	Host           string
	User           string
	KeyFile        string
	Password       string
	KnownHostsFile string
	Cwd            string // initial working directory on the remote host
	Shell          string // override login shell (empty = use remote login shell)
	ConnectionName string // alias used in panemux (for VSCode Remote SSH)
	ProxyCommand   string // shell command used as stdin/stdout pipe (ProxyCommand directive)
	Port           int
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
	return dialSSHClientUntil(cfg, nowFn().Add(dialRetryBudget))
}

// dialSSHClientUntil is dialSSHClient with an explicit retry deadline. A
// ProxyJump hop shares the outer call's deadline (see dialThroughJump) rather
// than starting a fresh dialRetryBudget window, so a multi-hop chain's total
// retry time stays bounded by a single budget instead of multiplying per hop.
func dialSSHClientUntil(cfg SSHConfig, deadline time.Time) (*ssh.Client, *ssh.Client, error) {
	authMethods, err := buildAuthMethods(cfg)
	if err != nil {
		return nil, nil, err
	}

	hkCallback, knownHostsPath, err := buildHostKeyCallback(cfg.KnownHostsFile)
	if err != nil {
		return nil, nil, err
	}

	port := cfg.Port
	if port == 0 {
		port = 22
	}
	addr := net.JoinHostPort(cfg.Host, strconv.Itoa(port))

	sshCfg := &ssh.ClientConfig{
		User:              cfg.User,
		Auth:              authMethods,
		HostKeyCallback:   hkCallback,
		HostKeyAlgorithms: knownHostsAlgorithms(knownHostsPath, addr),
		// Timeout is documented as bounding TCP connection establishment, but
		// golang.org/x/crypto/ssh only reads it inside ssh.Dial(); it has no
		// effect on ssh.NewClientConn (called below), which is what this
		// package actually uses since it always dials its own net.Conn first.
		// The handshake itself is therefore currently unbounded regardless of
		// this value, for every transport (TCP, ProxyJump, and ProxyCommand
		// alike) — retrying the dial (dialTransportWithRetry, above) does not
		// change that. Kept here only so a future switch to ssh.Dial-style
		// usage picks up a sane default; do not rely on it as a real timeout.
		Timeout: 30 * time.Second,
	}

	conn, jumpClient, err := dialTransportWithRetry(cfg, addr, port, deadline)
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

const (
	// dialRetryMaxAttempts bounds how many times the transport-dial step (TCP
	// dial / ProxyJump / ProxyCommand) is retried before giving up. Retrying
	// here smooths over transient DNS blips (e.g. "no address associated with
	// hostname") on unstable networks; it deliberately does not cover the SSH
	// handshake/auth step, which fails fast on the first attempt.
	dialRetryMaxAttempts = 3
	// dialRetryInitialBackoff is the delay before the second attempt; it
	// doubles on each subsequent attempt (300ms, 600ms).
	dialRetryInitialBackoff = 300 * time.Millisecond
	// dialRetryBudget caps the total wall-clock time spent retrying, shared
	// across an entire ProxyJump chain via a single deadline (see
	// dialSSHClientUntil), so a caller synchronously waiting on dialSSHClient
	// (e.g. the /restart HTTP handler) never waits dramatically longer than
	// the ceiling a single dial attempt already tolerated before retries were
	// introduced.
	dialRetryBudget = 30 * time.Second
)

// sleepFn and nowFn are package-level so tests can inject deterministic
// timing without real delays or wall-clock dependence.
var (
	sleepFn = time.Sleep
	nowFn   = time.Now
)

// dialTransportFn is the transport-dial step, injectable for tests. Assigned
// in init (rather than at the var declaration) to avoid a spurious Go
// initialization-cycle error: dialTransport transitively calls back into
// dialTransportFn through dialThroughJump -> dialSSHClientUntil, and Go's
// init-order analysis does not distinguish "referenced in a function body"
// from "referenced at initialization time".
var dialTransportFn func(cfg SSHConfig, addr string, port int, deadline time.Time) (net.Conn, *ssh.Client, error)

func init() {
	dialTransportFn = dialTransport
}

// dialTransport establishes the raw transport (TCP, ProxyJump, or
// ProxyCommand) for an SSH connection, without performing the SSH handshake.
// The direct-dial timeout is clamped to whatever of deadline remains, so a
// retried attempt can never itself run past the shared retry budget.
func dialTransport(cfg SSHConfig, addr string, port int, deadline time.Time) (net.Conn, *ssh.Client, error) {
	switch {
	case cfg.JumpHost != nil:
		return dialThroughJump(*cfg.JumpHost, addr, deadline)
	case cfg.ProxyCommand != "":
		conn, err := dialViaProxyCommand(cfg.ProxyCommand, cfg.Host, port)
		return conn, nil, err
	default:
		timeout := deadline.Sub(nowFn())
		if timeout <= 0 {
			return nil, nil, fmt.Errorf("dial %s: retry budget exhausted", addr)
		}
		conn, err := net.DialTimeout("tcp", addr, timeout)
		return conn, nil, err
	}
}

// dialTransportWithRetry retries dialTransport with a short backoff on
// failure, bounded by dialRetryMaxAttempts and deadline. Only the transport
// step is retried; a successful transport dial followed by an SSH
// handshake/auth failure is never retried by this function since that
// happens in a separate step in dialSSHClientUntil.
//
// Each attempt's own timeout is clamped to the time remaining until deadline
// (see dialTransport), so a slow/hanging attempt that consumes the whole
// budget leaves no room for further attempts — the retry loop's own
// before-each-attempt deadline check alone cannot do this, since it can only
// observe elapsed time between attempts, not interrupt one already in flight.
func dialTransportWithRetry(cfg SSHConfig, addr string, port int, deadline time.Time) (net.Conn, *ssh.Client, error) {
	backoff := dialRetryInitialBackoff

	var conn net.Conn
	var jumpClient *ssh.Client
	var err error

	for attempt := 1; attempt <= dialRetryMaxAttempts; attempt++ {
		if !nowFn().Before(deadline) {
			if err == nil {
				err = fmt.Errorf("dial %s: retry budget exhausted before attempt %d", addr, attempt)
			}
			break
		}
		conn, jumpClient, err = dialTransportFn(cfg, addr, port, deadline)
		if err == nil {
			return conn, jumpClient, nil
		}
		if attempt == dialRetryMaxAttempts || !nowFn().Before(deadline) {
			break
		}
		sleepFn(backoff)
		backoff *= 2
	}
	return nil, nil, err
}

// dialThroughJump connects to targetAddr by tunneling through a ProxyJump host.
// Returns (conn to target, jumpClient, error). The jumpClient must be kept open
// as long as conn is in use and closed when the target session ends. The jump
// hop reuses the caller's deadline rather than a fresh retry budget, so a
// multi-hop chain's worst-case retry time doesn't multiply per hop.
func dialThroughJump(jumpCfg SSHConfig, targetAddr string, deadline time.Time) (net.Conn, *ssh.Client, error) {
	jumpClient, nestedJump, err := dialSSHClientUntil(jumpCfg, deadline)
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
// OpenSSH's ProxyCommand behavior where a subprocess acts as a transparent
// bidirectional pipe to the remote host.
type proxyCommandConn struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	raddr  net.Addr // host:port of the remote end, used for knownhosts verification
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
func (c *proxyCommandConn) LocalAddr() net.Addr                { return proxyAddr("127.0.0.1:0") }
func (c *proxyCommandConn) RemoteAddr() net.Addr               { return c.raddr }
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
	result = strings.ReplaceAll(result, "%p", strconv.Itoa(port))
	return strings.ReplaceAll(result, "\x00", "%")
}

// dialViaProxyCommand runs the ProxyCommand and returns a net.Conn backed by the
// subprocess stdin/stdout, mirroring how OpenSSH handles ProxyCommand.
// The command is passed to /bin/sh -c so shell quoting and features work as expected.
func dialViaProxyCommand(proxyCmd, host string, port int) (net.Conn, error) {
	cmd := substituteProxyCommand(proxyCmd, host, port)
	// Pass to /bin/sh -c so the command is interpreted by a shell, matching
	// OpenSSH behavior. /bin/sh is a hardcoded trusted binary.
	c := exec.Command("/bin/sh", "-c", cmd)
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
	return &proxyCommandConn{
		cmd:    c,
		stdin:  stdin,
		stdout: stdout,
		raddr:  proxyAddr(net.JoinHostPort(host, strconv.Itoa(port))),
	}, nil
}

// NewSSH creates and starts a new SSH terminal session.
func NewSSH(id, title string, cfg SSHConfig) (*SSHSession, error) {
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

	if err := startSSHShell(sess, cfg); err != nil {
		closeSSHResources(sess, client, jumpClient)
		return nil, err
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

	monitorSSHSession(sess, pw, func(state State) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.state = state
	})

	return s, nil
}

func setupSSHPTY(sess *ssh.Session) (io.WriteCloser, *io.PipeReader, *io.PipeWriter, error) {
	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", 24, 80, modes); err != nil {
		return nil, nil, nil, fmt.Errorf("request pty: %w", err)
	}
	stdin, err := sess.StdinPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("stdin pipe: %w", err)
	}
	pr, pw := io.Pipe()
	sess.Stdout = pw
	sess.Stderr = pw
	return stdin, pr, pw, nil
}

func startSSHShell(sess *ssh.Session, cfg SSHConfig) error {
	cmd, err := sshShellCommand(cfg)
	if err != nil {
		return err
	}
	if cmd == "" {
		return sess.Shell()
	}
	if err := sess.Start(cmd); err != nil {
		return fmt.Errorf("start shell: %w", err)
	}
	return nil
}

// sshShellCommand returns the remote command to pass to sess.Start, or "" to use
// sess.Shell. Paths are validated with the regex guard (CodeQL go/command-injection
// recommended pattern) before being embedded in the shell command.
// sess.Shell() and sess.Start() are mutually exclusive in the SSH protocol.
func sshShellCommand(cfg SSHConfig) (string, error) {
	if cfg.Shell != "" {
		if err := validateRemotePath("shell", cfg.Shell); err != nil {
			return "", err
		}
		if cfg.Cwd == "" {
			return "exec " + shellQuotePath(cfg.Shell), nil
		}
		if err := validateRemotePath("working directory", cfg.Cwd); err != nil {
			return "", err
		}
		return fmt.Sprintf(
			"cd %s && exec %s",
			shellQuotePath(cfg.Cwd),
			shellQuotePath(cfg.Shell),
		), nil
	}
	if cfg.Cwd == "" {
		return "", nil
	}
	if err := validateRemotePath("working directory", cfg.Cwd); err != nil {
		return "", err
	}
	return fmt.Sprintf("cd %s && exec $SHELL", shellQuotePath(cfg.Cwd)), nil
}

func validateRemotePath(label, path string) error {
	if validRemotePath.MatchString(path) {
		return nil
	}
	return fmt.Errorf("invalid %s %q: %s", label, path, invalidRemotePathMsg)
}

func closeSSHResources(sess *ssh.Session, client, jumpClient *ssh.Client) {
	if sess != nil {
		sess.Close()
	}
	client.Close()
	if jumpClient != nil {
		jumpClient.Close()
	}
}

func monitorSSHSession(sess *ssh.Session, pw *io.PipeWriter, markExited func(State)) {
	go func() {
		waitErr := sess.Wait()
		markExited(classifySSHWaitError(waitErr))
		pw.Close()
	}()
}

func classifySSHWaitError(err error) State {
	if err == nil {
		return StateExited
	}

	var exitErr *ssh.ExitError
	if errors.As(err, &exitErr) {
		return StateExited
	}

	var exitMissingErr *ssh.ExitMissingError
	if errors.As(err, &exitMissingErr) {
		return StateExited
	}

	return StateDisconnected
}

// DetectRemoteShell connects to the remote host via SSH and returns the value of $SHELL.
func DetectRemoteShell(cfg SSHConfig) (string, error) {
	client, jumpClient, err := dialSSHClient(cfg)
	if err != nil {
		return "", fmt.Errorf("connecting to remote host: %w", err)
	}
	defer client.Close()
	if jumpClient != nil {
		defer jumpClient.Close()
	}

	sess, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("creating session: %w", err)
	}
	defer sess.Close()

	out, err := sess.Output("echo $SHELL")
	if err != nil {
		return "", fmt.Errorf("detecting remote shell: %w", err)
	}

	shell := strings.TrimSpace(string(out))
	if shell == "" {
		return "", errors.New("remote $SHELL is not set")
	}
	return shell, nil
}

// ListRemoteDirectories connects to the remote host and returns immediate child
// directories below the requested path. When path is empty, the remote home
// directory is used.
func ListRemoteDirectories(cfg SSHConfig, path string, showHidden bool) ([]DirectoryEntry, string, error) {
	if path != "" {
		if err := validateRemotePath("directory path", path); err != nil {
			return nil, "", err
		}
	}

	client, jumpClient, err := dialSSHClient(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("connecting to remote host: %w", err)
	}
	defer client.Close()
	if jumpClient != nil {
		defer jumpClient.Close()
	}

	sess, err := client.NewSession()
	if err != nil {
		return nil, "", fmt.Errorf("creating session: %w", err)
	}
	defer sess.Close()

	out, err := sess.Output(remoteDirectoryListCommand(path, showHidden))
	if err != nil {
		return nil, "", fmt.Errorf("listing remote directories: %w", err)
	}

	resolvedPath, entries, err := parseRemoteDirectoryListOutput(out)
	if err != nil {
		return nil, "", err
	}
	return entries, resolvedPath, nil
}

func remoteDirectoryListCommand(path string, showHidden bool) string {
	target := "$PWD"
	if path != "" {
		target = shellQuotePath(path)
	}

	showHiddenFlag := "0"
	if showHidden {
		showHiddenFlag = "1"
	}

	return strings.Join([]string{
		"target=" + target,
		"show_hidden=" + showHiddenFlag,
		`if ! cd "$target" 2>/dev/null; then echo "__PANEMUX_NOT_DIR__"; exit 0; fi`,
		`pwd`,
		`find . -mindepth 1 -maxdepth 1 -type d -print | LC_ALL=C sort | while IFS= read -r entry; do`,
		`  [ -n "$entry" ] || continue`,
		`  name=${entry#./}`,
		`  if [ "$show_hidden" != "1" ] && [ "${name#*.}" != "$name" ]; then continue; fi`,
		`  has_children=0`,
		`  child=$(find "$entry" -mindepth 1 -maxdepth 1 -type d -print -quit 2>/dev/null)`,
		`  if [ -n "$child" ]; then has_children=1; fi`,
		`  printf '%s\t%s\t%s\n' "$name" "$PWD/${name}" "$has_children"`,
		`done`,
	}, "\n")
}

func parseRemoteDirectoryListOutput(out []byte) (string, []DirectoryEntry, error) {
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) == "" {
		return "", nil, errors.New("remote directory listing returned no path")
	}
	if strings.TrimSpace(lines[0]) == "__PANEMUX_NOT_DIR__" {
		return "", nil, errors.New("directory path does not exist")
	}

	resolvedPath := strings.TrimSpace(lines[0])
	entries := make([]DirectoryEntry, 0, len(lines))
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "" {
			continue
		}
		parts := strings.SplitN(line, "\t", 3)
		if len(parts) != 3 {
			return "", nil, fmt.Errorf("invalid remote directory listing row %q", line)
		}
		entries = append(entries, DirectoryEntry{
			Name:        parts[0],
			Path:        parts[1],
			HasChildren: parts[2] == "1",
		})
	}
	sort.Slice(entries, func(i, j int) bool {
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
	return resolvedPath, entries, nil
}

// buildHostKeyCallback resolves the known_hosts file path and returns a
// HostKeyCallback together with the resolved path.
func buildHostKeyCallback(knownHostsFile string) (ssh.HostKeyCallback, string, error) {
	path, err := resolveKnownHostsFile(knownHostsFile)
	if err != nil {
		return nil, "", err
	}
	cb, err := knownhosts.New(path)
	if err != nil {
		return nil, "", fmt.Errorf("loading known_hosts %s: %w", path, err)
	}
	return cb, path, nil
}

// knownHostsAlgorithms returns the host-key algorithm types stored in
// knownHostsPath that match hostport ("host:port" format). Setting
// ssh.ClientConfig.HostKeyAlgorithms to this list ensures the server presents
// a key type that matches our known_hosts entry.
func knownHostsAlgorithms(knownHostsPath, hostport string) []string {
	f, err := os.Open(knownHostsPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	normalized := knownhosts.Normalize(hostport)
	seen := make(map[string]bool)
	var algos []string

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 || line[0] == '#' || line[0] == '@' {
			continue
		}
		fields := bytes.Fields(line)
		if len(fields) < 3 {
			continue
		}
		patterns := string(fields[0])
		keyType := string(fields[1])
		for _, pattern := range strings.Split(patterns, ",") {
			if knownHostsFieldMatchesAddr(pattern, normalized) {
				if !seen[keyType] {
					seen[keyType] = true
					algos = append(algos, keyType)
				}
				break
			}
		}
	}
	return algos
}

func knownHostsFieldMatchesAddr(field, normalized string) bool {
	if strings.HasPrefix(field, "|1|") {
		return knownHostsHashedEntryMatches(field, normalized)
	}
	if strings.HasPrefix(field, "!") {
		return false
	}
	return field == normalized
}

func knownHostsHashedEntryMatches(encoded, normalized string) bool {
	parts := strings.SplitN(encoded, "|", 4)
	if len(parts) != 4 || parts[1] != "1" {
		return false
	}
	salt, err := base64.StdEncoding.DecodeString(parts[2])
	if err != nil {
		return false
	}
	hash, err := base64.StdEncoding.DecodeString(parts[3])
	if err != nil {
		return false
	}
	mac := hmac.New(sha1.New, salt)
	mac.Write([]byte(normalized))
	return hmac.Equal(mac.Sum(nil), hash)
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

	// If no explicit auth method, try common default key files (mirrors OpenSSH behavior).
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
		return nil, errors.New("no auth methods configured for SSH connection")
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

// sshGetCWDCmd is the shell command used by SSHSession.GetCWD to detect the
// current working directory of the interactive shell.
//
// A new exec channel always starts in the user's home directory, so running
// plain `pwd` would always return home regardless of where the user has
// navigated. Instead, we use the SSH connection's process tree:
//
//  1. $PPID inside the exec channel is the sshd process that handles this
//     connection — the same parent process as the interactive shell.
//  2. `pgrep -P $PPID -o` returns the oldest child of that sshd, which is
//     the interactive shell (started before any exec-channel children).
//  3. We read the CWD of that PID via /proc (Linux) or lsof (macOS).
//  4. If neither technique is available, we fall back to `pwd` (home dir),
//     which is the previous behavior.
const sshGetCWDCmd = `PID=$(pgrep -P $PPID -o 2>/dev/null) && [ -n "$PID" ] && ` +
	`{ readlink /proc/$PID/cwd 2>/dev/null || ` +
	`lsof -a -p $PID -d cwd -Fn 2>/dev/null | awk '/^n/{print substr($0,2)}'; } || ` +
	`pwd`

const sshListProcessesCmd = `ps -Ao pid=,ppid=,command=`
const sshShellPIDCmd = `pgrep -P $PPID -o 2>/dev/null`
const sshOpenFilesCmdTemplate = `{ ls -1 /proc/%[1]d/fd 2>/dev/null | ` +
	`while read -r fd; do readlink /proc/%[1]d/fd/"$fd" 2>/dev/null; done; } || ` +
	`{ lsof -a -p %[1]d -Fn 2>/dev/null | awk '/^n/{print substr($0,2)}'; }`
const sshPIDCWDCmdTemplate = `{ readlink /proc/%[1]d/cwd 2>/dev/null || ` +
	`lsof -a -p %[1]d -d cwd -Fn 2>/dev/null | awk '/^n/{print substr($0,2)}'; }`
const sshGitContextCmdTemplate = `cd %[1]s && ` +
	`root=$(git rev-parse --show-toplevel 2>&1); status=$?; ` +
	`if [ $status -ne 0 ]; then ` +
	`printf '__PANEMUX_GIT_CONTEXT_ERROR__\nshow-toplevel\n%%s\n' "$root"; exit $status; fi && ` +
	`common=$(git rev-parse --path-format=absolute --git-common-dir 2>&1); status=$?; ` +
	`if [ $status -ne 0 ]; then ` +
	`printf '__PANEMUX_GIT_CONTEXT_ERROR__\ngit-common-dir\n%%s\n' "$common"; exit $status; fi && ` +
	`branch=$(git branch --show-current 2>/dev/null || true) && ` +
	`origin=$(git config --get remote.origin.url 2>/dev/null || true) && ` +
	`printf '%%s\n%%s\n%%s\n%%s\n' "$root" "$common" "$branch" "$origin"`

// GetCWD returns the current working directory of the interactive shell by
// inspecting the sshd process tree. See sshGetCWDCmd for the full rationale.
func (s *SSHSession) GetCWD() (string, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return "", fmt.Errorf("new ssh session for cwd: %w", err)
	}
	defer sess.Close()
	out, err := sess.Output(sshGetCWDCmd)
	if err != nil {
		return "", fmt.Errorf("cwd over ssh: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

// GetActiveWorkdirs returns every distinct working directory currently in
// play for the newest active interactive Codex or Claude process on the SSH
// connection, including worktrees only visited by a delegated Claude Task
// subagent. Returns an empty slice when none is active.
func (s *SSHSession) GetActiveWorkdirs() ([]string, error) {
	baseCWD, err := s.GetCWD()
	if err != nil {
		return nil, err
	}

	return activeRemoteWorkdirsFromSessionFactory(
		func() (sshSessionRunner, error) {
			return s.client.NewSession()
		},
		fmt.Sprintf("session=%s type=%s", s.id, s.Type()),
		baseCWD,
	)
}

// InspectGitContext resolves Git metadata on the remote host for the provided
// absolute working directory.
func (s *SSHSession) InspectGitContext(cwd string) (GitContext, error) {
	sess, err := s.client.NewSession()
	if err != nil {
		return GitContext{}, fmt.Errorf("new ssh session for git context: %w", err)
	}
	defer sess.Close()

	return remoteGitContext(sess, cwd)
}

func remoteShellPID(runner sshSessionRunner) (int, error) {
	out, err := runner.Output(sshShellPIDCmd)
	if err != nil {
		return 0, fmt.Errorf("remote shell pid: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil {
		return 0, fmt.Errorf("parse remote shell pid: %w", err)
	}
	if pid <= 0 {
		return 0, errors.New("remote shell pid missing")
	}
	return pid, nil
}

func activeRemoteWorkdirsFromSessionFactory(
	newRunner func() (sshSessionRunner, error),
	logScope, baseCWD string,
) ([]string, error) {
	rootRunner, err := newRunner()
	if err != nil {
		return nil, fmt.Errorf("new ssh session for remote shell pid: %w", err)
	}
	defer rootRunner.Close()

	rootPID, err := remoteShellPID(rootRunner)
	if err != nil {
		return nil, err
	}

	return activeRemoteWorkdirsWithOutput(
		outputFromSessionFactory(newRunner),
		logScope,
		baseCWD,
		rootPID,
	)
}

func activeRemoteWorkdirs(runner sshSessionRunner, logScope, baseCWD string, rootPID int) ([]string, error) {
	return activeRemoteWorkdirsWithOutput(runner.Output, logScope, baseCWD, rootPID)
}

func activeRemoteWorkdirsWithOutput(
	run remoteOutputFunc,
	logScope, baseCWD string,
	rootPID int,
) ([]string, error) {
	log.Printf("%s resolving active workdirs from base_cwd=%q root_pid=%d", logScope, baseCWD, rootPID)

	out, err := run(sshListProcessesCmd)
	if err != nil {
		log.Printf("%s list remote processes failed: %v", logScope, err)
		return nil, fmt.Errorf("list remote processes: %w", err)
	}

	processes, err := parsePSOutput(append([]byte("PID PPID COMMAND\n"), out...))
	if err != nil {
		log.Printf("%s parse remote processes failed: %v", logScope, err)
		return nil, err
	}

	agentPID, ok := newestInteractiveAgentDescendantPID(processes, rootPID)
	if !ok {
		log.Printf("%s no interactive codex/claude process found beneath root_pid=%d", logScope, rootPID)
		return nil, nil
	}
	if proc, found := processByPID(processes, agentPID); found {
		log.Printf("%s selected interactive agent pid=%d command=%q", logScope, agentPID, proc.Command)
	}

	sessionCWDs, sessionErr := remoteInteractiveAgentSessionCWDs(
		run,
		logScope,
		processes,
		agentPID,
	)
	if sessionErr == nil && len(sessionCWDs) > 0 {
		log.Printf("%s resolved active workdirs from interactive agent session data: %q", logScope, sessionCWDs)
		return sessionCWDs, nil
	} else if sessionErr != nil {
		log.Printf("%s interactive agent session workdir lookup failed: %v", logScope, sessionErr)
	} else {
		log.Printf("%s no interactive agent session workdir found; falling back to descendant cwd scan", logScope)
	}

	cwd, err := resolveRemoteInteractiveAgentWorkdir(logScope, run, processes, agentPID, baseCWD)
	if err != nil {
		log.Printf("%s descendant cwd resolution failed: %v", logScope, err)
		return nil, err
	}
	if cwd == "" {
		log.Printf("%s descendant cwd scan found no override; keeping base_cwd=%q", logScope, baseCWD)
		return nil, nil
	}
	log.Printf("%s descendant cwd scan selected %q", logScope, cwd)
	return []string{cwd}, nil
}

func resolveRemoteInteractiveAgentWorkdir(
	logScope string,
	run remoteOutputFunc,
	processes []processInfo,
	agentPID int,
	baseCWD string,
) (string, error) {
	candidatePIDs := descendantPIDs(processes, agentPID)
	sort.Slice(candidatePIDs, func(i, j int) bool {
		return candidatePIDs[i] > candidatePIDs[j]
	})

	var agentCWD string
	for _, pid := range candidatePIDs {
		cwd, err := remotePIDCWD(run, pid)
		if err != nil || cwd == "" {
			continue
		}
		if pid == agentPID {
			agentCWD = cwd
		}
		if cwd != baseCWD {
			log.Printf("%s descendant cwd candidate pid=%d cwd=%q", logScope, pid, cwd)
			return cwd, nil
		}
	}

	if agentCWD != "" {
		return agentCWD, nil
	}
	return "", nil
}

type remoteOutputFunc func(string) ([]byte, error)

func outputFromSessionFactory(
	newRunner func() (sshSessionRunner, error),
) remoteOutputFunc {
	return func(cmd string) ([]byte, error) {
		runner, err := newRunner()
		if err != nil {
			return nil, err
		}
		defer runner.Close()
		return runner.Output(cmd)
	}
}

func remotePIDCWD(run remoteOutputFunc, pid int) (string, error) {
	out, err := run(fmt.Sprintf(sshPIDCWDCmdTemplate, pid))
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func remoteCodexSessionCWD(
	run remoteOutputFunc,
	logScope string,
	processes []processInfo,
	agentPID int,
) (string, error) {
	proc, ok := processByPID(processes, agentPID)
	if !ok || !isCodexCommand(proc.Command) {
		return "", nil
	}

	paths, err := remoteOpenFiles(run, agentPID)
	if err != nil {
		log.Printf("%s open files lookup failed pid=%d: %v", logScope, agentPID, err)
		return "", err
	}
	sessionPath, ok := codexSessionPath(paths)
	if !ok {
		log.Printf("%s no codex session log found for pid=%d", logScope, agentPID)
		return "", nil
	}
	log.Printf("%s found codex session log for pid=%d path=%q", logScope, agentPID, sessionPath)

	return remoteCachedAgentLogCWD(
		run,
		"ssh:"+logScope+":codex:"+sessionPath,
		logScope,
		sessionPath,
		shellQuotePath(sessionPath),
		"codex session log",
		parseCodexSessionCWD,
	)
}

func remoteInteractiveAgentSessionCWDs(
	run remoteOutputFunc,
	logScope string,
	processes []processInfo,
	agentPID int,
) ([]string, error) {
	proc, ok := processByPID(processes, agentPID)
	if !ok {
		return nil, nil
	}

	switch {
	case isCodexCommand(proc.Command):
		cwd, err := remoteCodexSessionCWD(run, logScope, processes, agentPID)
		if err != nil || cwd == "" {
			return nil, err
		}
		return []string{cwd}, nil
	case isClaudeCommand(proc.Command):
		return remoteClaudeSessionCWDs(run, logScope, processes, agentPID)
	default:
		return nil, nil
	}
}

// remoteClaudeSessionCWDs returns every distinct workdir found across the
// Claude session's own remote transcript and any Task subagent transcripts
// recorded alongside it under a remote "subagents" directory. See
// claudeSessionCWDs (local.go) for why subagent transcripts matter: a
// delegated subagent's worktree-relative work is otherwise invisible because
// it is never reflected in the parent transcript.
func remoteClaudeSessionCWDs(
	run remoteOutputFunc,
	logScope string,
	processes []processInfo,
	agentPID int,
) ([]string, error) {
	proc, ok := processByPID(processes, agentPID)
	if !ok || !isClaudeCommand(proc.Command) {
		return nil, nil
	}

	sessionMeta, err := remoteClaudeSessionMeta(run, logScope, agentPID)
	if err != nil {
		return nil, err
	}
	if sessionMeta == nil || sessionMeta.SessionID == "" || sessionMeta.CWD == "" {
		return nil, nil
	}
	if !validClaudeSessionID.MatchString(sessionMeta.SessionID) {
		return nil, nil
	}

	projectPath := remoteClaudeProjectPath(sessionMeta)

	cwds := make([]string, 0, 1)
	seen := make(map[string]bool)
	addCandidate := func(displayPath, shellPath string) {
		cwd, lookupErr := remoteCachedAgentLogCWD(
			run,
			"ssh:"+logScope+":claude:"+displayPath,
			logScope,
			displayPath,
			shellPath,
			"claude transcript",
			parseClaudeProjectCWD,
		)
		if lookupErr != nil || cwd == "" || seen[cwd] {
			return
		}
		seen[cwd] = true
		cwds = append(cwds, cwd)
	}

	addCandidate(projectPath, remoteClaudeProjectShellPath(sessionMeta))

	subagentNames, err := remoteClaudeSubagentTranscriptNames(run, logScope, sessionMeta)
	if err != nil {
		log.Printf("%s listing claude subagent transcripts failed: %v", logScope, err)
		return cwds, nil
	}
	for _, name := range subagentNames {
		addCandidate(
			filepath.Join(filepath.Dir(projectPath), sessionMeta.SessionID, "subagents", name),
			remoteClaudeSubagentShellPath(sessionMeta, name),
		)
	}

	return cwds, nil
}

// remoteClaudeSubagentTranscriptNames lists the ".jsonl" filenames in the
// remote session's "subagents" directory, or returns an empty list (no
// error) when that directory does not exist, matching older Claude Code
// session layouts that never created it.
func remoteClaudeSubagentTranscriptNames(
	run remoteOutputFunc,
	logScope string,
	meta *claudeSessionMeta,
) ([]string, error) {
	out, err := run("ls -1 " + remoteClaudeSubagentsDirShellPath(meta) + " 2>/dev/null || true")
	if err != nil {
		return nil, err
	}

	var names []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || !strings.HasSuffix(line, ".jsonl") {
			continue
		}
		names = append(names, line)
	}
	sort.Strings(names)
	log.Printf("%s found %d claude subagent transcript(s)", logScope, len(names))
	return names, nil
}

func remoteClaudeSubagentsDirShellPath(meta *claudeSessionMeta) string {
	return "~/.claude/projects/" + shellQuotePath(
		filepath.Join(claudeProjectDirName(meta.CWD), meta.SessionID, "subagents"),
	)
}

func remoteClaudeSubagentShellPath(meta *claudeSessionMeta, name string) string {
	return "~/.claude/projects/" + shellQuotePath(
		filepath.Join(claudeProjectDirName(meta.CWD), meta.SessionID, "subagents", name),
	)
}

func remoteClaudeSessionMeta(run remoteOutputFunc, logScope string, agentPID int) (*claudeSessionMeta, error) {
	sessionPath := fmt.Sprintf("~/.claude/sessions/%d.json", agentPID)
	out, err := run("cat " + sessionPath)
	if err != nil {
		log.Printf("%s reading claude session metadata failed path=%q: %v", logScope, sessionPath, err)
		return nil, err
	}

	var meta claudeSessionMeta
	if err := json.Unmarshal(out, &meta); err != nil {
		log.Printf("%s parsing claude session metadata failed path=%q: %v", logScope, sessionPath, err)
		return nil, err
	}
	if meta.PID != agentPID {
		return nil, nil
	}
	return &meta, nil
}

func remoteClaudeProjectPath(meta *claudeSessionMeta) string {
	return filepath.Join(
		"~/.claude/projects",
		claudeProjectDirName(meta.CWD),
		meta.SessionID+".jsonl",
	)
}

func remoteClaudeProjectShellPath(meta *claudeSessionMeta) string {
	return "~/.claude/projects/" + shellQuotePath(
		filepath.Join(claudeProjectDirName(meta.CWD), meta.SessionID+".jsonl"),
	)
}

func remoteFileFingerprintCmd(shellPath string) string {
	return "if out=$(stat -c '%s %Y' " + shellPath + " 2>/dev/null); then " +
		"printf '%s\\n' \"$out\"; " +
		"elif out=$(stat -f '%z %m' " + shellPath + " 2>/dev/null); then " +
		"printf '%s\\n' \"$out\"; " +
		"else bytes=$(wc -c < " + shellPath + " 2>/dev/null) || exit 1; printf '%s -\\n' \"$bytes\"; fi"
}

func remoteFileFingerprint(run remoteOutputFunc, shellPath string) (agentLogFingerprint, error) {
	out, err := run(remoteFileFingerprintCmd(shellPath))
	if err != nil {
		return "", err
	}
	return agentLogFingerprint(strings.TrimSpace(string(out))), nil
}

func remoteCachedAgentLogCWD(
	run remoteOutputFunc,
	cacheKey, logScope, displayPath, shellPath, label string,
	parse func([]byte) (string, error),
) (string, error) {
	fingerprint, err := remoteFileFingerprint(run, shellPath)
	if err == nil {
		return cachedAgentLogCWD(cacheKey, fingerprint, func() (string, error) {
			return readAndParseRemoteAgentLog(
				run,
				logScope,
				displayPath,
				shellPath,
				label,
				parse,
			)
		})
	}

	log.Printf("%s fingerprint lookup failed for %s path=%q: %v", logScope, label, displayPath, err)

	return readAndParseRemoteAgentLog(
		run,
		logScope,
		displayPath,
		shellPath,
		label,
		parse,
	)
}

func readAndParseRemoteAgentLog(
	run remoteOutputFunc,
	logScope, displayPath, shellPath, label string,
	parse func([]byte) (string, error),
) (string, error) {
	out, readErr := run("cat " + shellPath)
	if readErr != nil {
		log.Printf("%s reading %s failed path=%q: %v", logScope, label, displayPath, readErr)
		return "", readErr
	}

	cwd, parseErr := parse(out)
	if parseErr != nil {
		log.Printf("%s parsing %s failed path=%q: %v", logScope, label, displayPath, parseErr)
		return "", parseErr
	}
	log.Printf("%s parsed %s path=%q cwd=%q", logScope, label, displayPath, cwd)
	return cwd, nil
}

func remoteGitContext(runner sshSessionRunner, cwd string) (GitContext, error) {
	if err := validateRemotePath("working directory", cwd); err != nil {
		return GitContext{}, NewGitContextError(
			"ssh",
			"validate remote working directory",
			cwd,
			GitContextCauseInvalidCWD,
			err,
			"",
		)
	}

	out, err := runner.Output(fmt.Sprintf(sshGitContextCmdTemplate, shellQuotePath(cwd)))
	if err != nil {
		return GitContext{}, classifyRemoteGitContextError(cwd, string(out), err)
	}

	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 4 {
		return GitContext{}, NewGitContextError(
			"ssh",
			"git context command",
			cwd,
			GitContextCauseIncomplete,
			errors.New("git context over ssh: incomplete response"),
			string(out),
		)
	}

	root := strings.TrimSpace(lines[0])
	return GitContext{
		Branch:    strings.TrimSpace(lines[2]),
		CommonDir: strings.TrimSpace(lines[1]),
		OriginURL: strings.TrimSpace(lines[3]),
		Repo:      filepath.Base(root),
		Root:      root,
	}, nil
}

func classifyRemoteGitContextError(cwd, output string, err error) error {
	trimmed := strings.TrimSpace(output)
	if trimmed == "" {
		return NewGitContextError("ssh", "git context command", cwd, GitContextCauseSSHSessionFailed, err, "")
	}

	const marker = "__PANEMUX_GIT_CONTEXT_ERROR__"
	lines := strings.Split(trimmed, "\n")
	if len(lines) >= 3 && lines[0] == marker {
		operation := strings.TrimSpace(lines[1])
		stderr := strings.TrimSpace(strings.Join(lines[2:], "\n"))
		// Keep SSH-side unknowns as unknown. Unlike the local path, we cannot
		// reliably distinguish a remote repository metadata problem from shell or
		// environment differences once we only have stderr text back.
		return NewGitContextError(
			"ssh",
			sshGitOperationName(operation),
			cwd,
			ClassifyGitFailureCause(stderr, err),
			err,
			stderr,
		)
	}

	return NewGitContextError("ssh", "git context command", cwd, GitContextCauseSSHSessionFailed, err, trimmed)
}

func sshGitOperationName(operation string) string {
	switch operation {
	case "show-toplevel":
		return "git rev-parse --show-toplevel"
	case "git-common-dir":
		return "git rev-parse --path-format=absolute --git-common-dir"
	default:
		return "git context command"
	}
}

func remoteOpenFiles(run remoteOutputFunc, pid int) ([]string, error) {
	out, err := run(fmt.Sprintf(sshOpenFilesCmdTemplate, pid))
	if err != nil {
		return nil, err
	}
	paths := []string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			paths = append(paths, line)
		}
	}
	return paths, nil
}
