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

	"github.com/pkg/sftp"
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
	// Done is closed once the connection has ended.
	Done() <-chan struct{}
	Close() error
}

// Remote is an SSH host previews are read from and relayed through. Its
// connection is made when first needed and made again once it ends, always
// to the host it was created for: a preview, or a tab opened on one, keeps
// the host it was opened on.
type Remote struct {
	connect func() (RemoteConn, error)
	conn    RemoteConn
	name    string
	mu      sync.Mutex
	closed  bool
}

// NewRemote returns the host called name, reached with connect.
func NewRemote(name string, connect func() (RemoteConn, error)) *Remote {
	return &Remote{name: name, connect: connect}
}

// Name is the host's ssh_connections name, as the preview shows it.
func (r *Remote) Name() string { return r.name }

func (r *Remote) get() (RemoteConn, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return nil, fmt.Errorf("%w: %s: closed", ErrRemote, r.name)
	}
	if r.conn != nil {
		select {
		case <-r.conn.Done():
			_ = r.conn.Close()
			r.conn = nil
		default:
			return r.conn, nil
		}
	}
	conn, err := r.connect()
	if err != nil {
		return nil, fmt.Errorf("%w: %s: %v", ErrRemote, r.name, err)
	}
	r.conn = conn
	return conn, nil
}

// Dial opens a TCP connection from the host to addr.
func (r *Remote) Dial(ctx context.Context, addr string) (net.Conn, error) {
	conn, err := r.get()
	if err != nil {
		return nil, err
	}
	tcp, err := conn.Dial(ctx, addr)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", r.name, err)
	}
	return tcp, nil
}

// Close closes the host's connection; nothing is reached through it after.
func (r *Remote) Close() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	if r.conn != nil {
		_ = r.conn.Close()
		r.conn = nil
	}
}

func (r *Remote) files() (*sftp.Client, error) {
	conn, err := r.get()
	if err != nil {
		return nil, err
	}
	return conn.SFTP(), nil
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
	files, err := remote.files()
	if err != nil {
		return nil, err
	}
	var roots []string
	for _, dir := range dirs {
		if resolved, err := realPath(files, dir); err == nil {
			roots = append(roots, resolved)
		}
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
	seen := map[string]bool{}
	for _, file := range paths {
		resolved, err := realPath(files, file)
		if err != nil {
			if !isFileError(err) {
				return nil, remoteFileError(file, err)
			}
			continue
		}
		root := remoteRootOf(resolved, roots)
		if root == "" {
			escaped = true
			continue
		}
		if info, err := files.Stat(resolved); err != nil || !info.Mode().IsRegular() || seen[resolved] {
			continue
		}
		seen[resolved] = true
		found = append(found, Found{Root: root, File: file})
	}
	if len(found) > 0 {
		return found, nil
	}
	if escaped {
		return nil, fmt.Errorf("%w: %s (allowed: %s)", ErrEscapes, name, strings.Join(dirs, ", "))
	}
	return nil, fmt.Errorf("%w: %s (looked in %s)", ErrNotFound, name, strings.Join(dirs, ", "))
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
	files, err := remote.files()
	if err != nil {
		return "", err
	}
	resolved, err := realPath(files, path.Clean(file))
	if err != nil {
		return "", remoteFileError(file, err)
	}
	root = path.Clean(root)
	if !remoteWithin(root, resolved) {
		return "", fmt.Errorf("%w: %s", ErrEscapes, file)
	}
	info, err := files.Stat(resolved)
	if err != nil { //coverage:exempt realPath has just looked the file up; only a change on the host in between fails this
		return "", remoteFileError(file, err)
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
	files, err := e.remote.files()
	if err != nil {
		return nil, nil, false
	}
	resolved, err := realPath(files, path.Join(e.root, rest))
	if err != nil || !remoteWithin(e.root, resolved) {
		return nil, nil, false
	}
	f, err := files.Open(resolved)
	if err != nil {
		return nil, nil, false
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, nil, false
	}
	return f, info, true
}
