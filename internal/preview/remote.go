package preview

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// ErrRemote reports that the SSH host a preview is read from, or relayed
// through, could not be reached.
var ErrRemote = errors.New("the SSH host could not be reached")

// RemoteConn is one SSH connection to the host of an ssh or ssh_tmux pane,
// kept by the preview rather than by the pane, so a preview opened in its
// own tab outlives the pane and its session (issue #387). Files are read
// over SFTP and loopback URLs reached over direct-tcpip channels, so no
// shell on the host parses anything a preview names.
// *session.PreviewConn is the production implementation.
type RemoteConn interface {
	// Dial opens a TCP connection from the host to addr.
	Dial(ctx context.Context, addr string) (net.Conn, error)
	SFTP() *sftp.Client
	// Ping reports whether the host still answers on the connection.
	Ping(ctx context.Context) error
	// Done is closed once the connection has ended.
	Done() <-chan struct{}
	Close() error
}

// How a host's connection is checked. They bound how long a host that
// stops answering is waited for, never how long a preview or a tab lasts.
const (
	defaultKeepaliveInterval = 15 * time.Second
	defaultKeepaliveTimeout  = 15 * time.Second
	// defaultLookupTimeout bounds one request's lookups on the host: the
	// LSTAT, READLINK and STAT of resolving a path, and the OPEN.
	defaultLookupTimeout = 15 * time.Second
)

// Remote is an SSH host previews are read from and relayed through. Its
// connection is made when first needed, always to the host it was created
// for — a preview, or a tab opened on one, keeps the host it was opened on —
// and made again once it ends, stops answering, or loses its SFTP
// subsystem. It is checked with a keepalive while it is open.
type Remote struct {
	connect func() (RemoteConn, error)
	conn    RemoteConn
	dialing *dialAttempt
	name    string

	keepaliveInterval time.Duration
	keepaliveTimeout  time.Duration
	lookupTimeout     time.Duration

	mu     sync.Mutex
	closed bool
}

// dialAttempt is a connection being made; everyone who needs the host
// while it is made waits for it rather than making another.
type dialAttempt struct {
	done chan struct{}
	conn RemoteConn
	err  error
}

// NewRemote returns the host called name, reached with connect.
func NewRemote(name string, connect func() (RemoteConn, error)) *Remote {
	return &Remote{
		name:              name,
		connect:           connect,
		keepaliveInterval: defaultKeepaliveInterval,
		keepaliveTimeout:  defaultKeepaliveTimeout,
		lookupTimeout:     defaultLookupTimeout,
	}
}

// Name is the host's ssh_connections name, as the preview shows it.
func (r *Remote) Name() string { return r.name }

func (r *Remote) errClosed() error {
	return fmt.Errorf("%w: %s: closed", ErrRemote, r.name)
}

// get returns the host's connection, making it when there is none. The lock
// is never held while connecting, which can take as long as the SSH dial's
// own retries: Close, and every other caller, would wait behind it.
func (r *Remote) get() (RemoteConn, error) {
	r.mu.Lock()
	for {
		if r.closed {
			r.mu.Unlock()
			return nil, r.errClosed()
		}
		if r.conn != nil {
			select {
			case <-r.conn.Done():
				_ = r.conn.Close()
				r.conn = nil
			default:
				conn := r.conn
				r.mu.Unlock()
				return conn, nil
			}
		}
		attempt := r.dialing
		if attempt == nil {
			break
		}
		r.mu.Unlock()
		<-attempt.done
		if attempt.err != nil {
			return nil, attempt.err
		}
		r.mu.Lock()
	}
	attempt := &dialAttempt{done: make(chan struct{})}
	r.dialing = attempt
	r.mu.Unlock()

	conn, err := r.connect()
	r.mu.Lock()
	r.dialing = nil
	switch {
	case err != nil:
		attempt.err = fmt.Errorf("%w: %s: %v", ErrRemote, r.name, err)
	case r.closed:
		_ = conn.Close()
		attempt.err = r.errClosed()
	default:
		attempt.conn = conn
		r.conn = conn
		go r.keepAlive(conn)
	}
	r.mu.Unlock()
	close(attempt.done)
	return attempt.conn, attempt.err
}

// drop closes conn and, when it is still the host's connection, forgets it,
// so the next request connects again.
func (r *Remote) drop(conn RemoteConn) {
	r.mu.Lock()
	if r.conn == conn {
		r.conn = nil
	}
	r.mu.Unlock()
	_ = conn.Close()
}

// keepAlive pings conn while it is the host's connection, and drops it once
// the host does not answer: a stopped sshd or a dropped network leaves the
// connection open, but no request on it would ever be answered.
func (r *Remote) keepAlive(conn RemoteConn) {
	ticker := time.NewTicker(r.keepaliveInterval)
	defer ticker.Stop()
	for {
		select {
		case <-conn.Done():
			return
		case <-ticker.C:
		}
		r.mu.Lock()
		current := r.conn == conn
		r.mu.Unlock()
		if !current {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), r.keepaliveTimeout)
		err := conn.Ping(ctx)
		cancel()
		if err != nil {
			r.drop(conn)
			return
		}
	}
}

// Dial opens a TCP connection from the host to addr. A refusal from the
// host — nothing listens at addr — leaves the connection as it is; any
// other failure is the connection's, which is dropped.
func (r *Remote) Dial(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := r.get()
	if err != nil {
		return nil, err
	}
	tcp, err := conn.Dial(ctx, addr)
	if err != nil {
		var refused *ssh.OpenChannelError
		if !errors.As(err, &refused) && ctx.Err() == nil {
			r.drop(conn)
		}
		return nil, fmt.Errorf("%s: %w", r.name, err)
	}
	return tcp, nil
}

// Close closes the host's connection; nothing is reached through it after.
// It does not wait for a connection being made, which is closed once made.
func (r *Remote) Close() {
	r.mu.Lock()
	r.closed = true
	conn := r.conn
	r.conn = nil
	r.mu.Unlock()
	if conn != nil {
		_ = conn.Close()
	}
}

func (r *Remote) files() (*sftp.Client, error) {
	conn, err := r.get()
	if err != nil {
		return nil, err
	}
	return conn.SFTP(), nil
}

// withFiles runs lookup, a request's lookups on the host, over its SFTP
// client, bounded by lookupTimeout: a host that does not answer in time
// has its connection closed, which ends the lookup. A failure that is not
// about a file — the connection or its SFTP subsystem is gone — drops the
// connection, and lookup runs once more over a new one, so a tab is served
// again as soon as the host is.
func (r *Remote) withFiles(lookup func(*sftp.Client) error) error {
	var err error
	for range 2 {
		var conn RemoteConn
		if conn, err = r.get(); err != nil {
			return err
		}
		timer := time.AfterFunc(r.lookupTimeout, func() { r.drop(conn) })
		err = lookup(conn.SFTP())
		timer.Stop()
		if err == nil || isFileError(err) {
			return err
		}
		r.drop(conn)
	}
	return err
}

// ResolveRemote is Resolve for a pane whose shell runs on remote: it finds
// the files a clicked path may name inside the pane's working directories on
// that host. Paths there are POSIX paths. Each path, and each working
// directory, is resolved through its symbolic links one component at a time
// (see realPath), and a file is found only when the path it resolves to
// lies inside the path a working directory resolves to.
func ResolveRemote(remote *Remote, name string, workdirs []string) ([]Found, error) {
	if !IsPreviewable(name) {
		return nil, ErrNotPreviewable
	}
	dirs := cleanRemoteDirs(workdirs)
	if len(dirs) == 0 {
		return nil, ErrNoWorkdir
	}
	var paths []string
	if path.IsAbs(name) {
		paths = []string{path.Clean(name)}
	} else {
		for _, dir := range dirs {
			paths = append(paths, path.Join(dir, name))
		}
	}

	var found []Found
	escaped := false
	err := remote.withFiles(func(files *sftp.Client) error {
		var err error
		found, escaped, err = findRemote(files, dirs, paths)
		return err
	})
	if err != nil {
		return nil, remoteFileError(name, err)
	}
	if len(found) > 0 {
		return found, nil
	}
	if escaped {
		return nil, fmt.Errorf("%w: %s (allowed: %s)", ErrEscapes, name, strings.Join(dirs, ", "))
	}
	return nil, fmt.Errorf("%w: %s (looked in %s)", ErrNotFound, name, strings.Join(dirs, ", "))
}

// findRemote looks for paths on the host, inside the working directories
// dirs, as ResolveRemote describes; it reports whether any of them led
// outside, and fails only when the connection does.
func findRemote(files *sftp.Client, dirs, paths []string) ([]Found, bool, error) {
	var roots []string
	for _, dir := range dirs {
		resolved, err := realPath(files, dir)
		if err == nil {
			roots = append(roots, resolved)
		} else if !isFileError(err) {
			return nil, false, err
		}
	}
	var found []Found
	escaped := false
	seen := map[string]bool{}
	for _, file := range paths {
		resolved, err := realPath(files, file)
		if err != nil {
			//coverage:exempt the working directories were just looked up over this connection; only its loss fails here
			if !isFileError(err) {
				return nil, false, err
			}
			continue
		}
		root := remoteRootOf(resolved, roots)
		if root == "" {
			escaped = true
			continue
		}
		info, err := files.Stat(resolved)
		//coverage:exempt realPath has just looked the file up over this connection; only its loss in between fails here
		if err != nil && !isFileError(err) {
			return nil, false, fmt.Errorf("stat %s: %w", resolved, err)
		}
		if err != nil || !info.Mode().IsRegular() || seen[resolved] {
			continue
		}
		seen[resolved] = true
		found = append(found, Found{Root: root, File: file})
	}
	return found, escaped, nil
}

// maxRemoteLinks bounds the symbolic links one path may lead through, as
// the kernel's own ELOOP limit does, so a loop of links ends.
const maxRemoteLinks = 40

var errRemoteLinkLoop = errors.New("too many levels of symbolic links")

// realPath resolves name, an absolute path on the host, through its symbolic
// links: each component is looked up with Lstat and a link replaced by its
// target, relative to the directory holding it. The SFTP protocol's own
// REALPATH is not used, because servers differ in whether it follows links.
// The path returned names no link, as things stood when each component was
// looked up; a component that does not exist is an error.
func realPath(files *sftp.Client, name string) (string, error) {
	resolved := "/"
	pending := strings.Split(name, "/")
	links := 0
	for len(pending) > 0 {
		component := pending[0]
		pending = pending[1:]
		switch component {
		case "", ".":
			continue
		case "..":
			// resolved names no link, so its parent is its physical parent.
			resolved = path.Dir(resolved)
			continue
		}
		next := path.Join(resolved, component)
		info, err := files.Lstat(next)
		if err != nil {
			return "", fmt.Errorf("lstat %s: %w", next, err)
		}
		if info.Mode()&fs.ModeSymlink == 0 {
			resolved = next
			continue
		}
		links++
		if links > maxRemoteLinks {
			return "", fmt.Errorf("%s: %w", name, errRemoteLinkLoop)
		}
		target, err := files.ReadLink(next)
		if err != nil { //coverage:exempt Lstat has just reported the link; only its removal in between fails this
			return "", fmt.Errorf("readlink %s: %w", next, err)
		}
		if path.IsAbs(target) {
			resolved = "/"
		}
		pending = append(strings.Split(target, "/"), pending...)
	}
	return resolved, nil
}

// remoteRootOf is the innermost of roots that holds file, or "".
func remoteRootOf(file string, roots []string) string {
	root := ""
	for _, dir := range roots {
		// Two roots holding one file differ in length unless they are the
		// same path, which two working directories resolving alike give.
		//mutation:exempt[CONDITIONALS_BOUNDARY] EQUIVALENT: equal-length ancestors of one file are the same path
		if remoteWithin(dir, file) && len(dir) > len(root) {
			root = dir
		}
	}
	return root
}

func remoteWithin(dir, file string) bool {
	if dir == "/" {
		return file != "/" && strings.HasPrefix(file, "/")
	}
	return strings.HasPrefix(file, dir+"/")
}

func cleanRemoteDirs(dirs []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, dir := range dirs {
		if !path.IsAbs(dir) {
			continue
		}
		dir = path.Clean(dir)
		if !seen[dir] {
			seen[dir] = true
			out = append(out, dir)
		}
	}
	return out
}

// OpenRemote is Open for a file on remote, found by ResolveRemote under
// root, the resolved working directory holding it.
func (r *Registry) OpenRemote(remote *Remote, root, file string) (string, error) {
	if !IsPreviewable(file) {
		return "", ErrNotPreviewable
	}
	root = path.Clean(root)
	var resolved string
	var info fs.FileInfo
	err := remote.withFiles(func(files *sftp.Client) error {
		var err error
		if resolved, err = realPath(files, path.Clean(file)); err != nil {
			return err
		}
		if !remoteWithin(root, resolved) {
			return nil
		}
		//coverage:exempt realPath has just looked the file up; only a change on the host in between fails this
		if info, err = files.Stat(resolved); err != nil {
			return fmt.Errorf("stat %s: %w", resolved, err)
		}
		return nil
	})
	if err != nil {
		return "", remoteFileError(file, err)
	}
	if !remoteWithin(root, resolved) {
		return "", fmt.Errorf("%w: %s", ErrEscapes, file)
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s", ErrNotFound, file)
	}
	if info.Size() > MaxDocumentBytes {
		return "", ErrTooLarge
	}
	rel := strings.TrimPrefix(resolved, root)
	return r.register(entry{remote: remote, root: root, file: strings.TrimPrefix(rel, "/")})
}

// isFileError reports whether err is about the file looked up, rather than
// the connection it was looked up over: the SFTP server answered with a
// status, such as a missing file or a component that is not a directory.
func isFileError(err error) bool {
	var status *sftp.StatusError
	return errors.Is(err, fs.ErrNotExist) || errors.Is(err, fs.ErrPermission) ||
		errors.Is(err, errRemoteLinkLoop) || errors.As(err, &status)
}

func remoteFileError(file string, err error) error {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return fmt.Errorf("%w: %s: permission denied", ErrNotFound, file)
	case isFileError(err):
		return fmt.Errorf("%w: %s", ErrNotFound, file)
	}
	return fmt.Errorf("%w: %s: %v", ErrRemote, file, err)
}

// openRemote opens rest, a slash-separated path below a remote entry's root,
// resolving it as OpenRemote does: what it leads to must still lie inside
// the root, so a link added after the preview opened is refused too.
func openRemote(e entry, rest string) (fileReader, fs.FileInfo, bool) {
	var f *sftp.File
	var info fs.FileInfo
	err := e.remote.withFiles(func(files *sftp.Client) error {
		resolved, err := realPath(files, path.Join(e.root, rest))
		if err != nil {
			return err
		}
		if !remoteWithin(e.root, resolved) {
			return fs.ErrNotExist
		}
		opened, err := files.Open(resolved)
		if err != nil {
			return fmt.Errorf("open %s: %w", resolved, err)
		}
		//coverage:exempt a file just opened is described by the same handle; only the connection's loss in between fails this
		if info, err = opened.Stat(); err != nil {
			_ = opened.Close()
			return fmt.Errorf("stat %s: %w", resolved, err)
		}
		f = opened
		return nil
	})
	if err != nil {
		return nil, nil, false
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, false
	}
	return f, info, true
}
