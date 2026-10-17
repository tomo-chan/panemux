package preview

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

// fakeRemoteConn is an SSH host's connection as the preview uses it: SFTP
// served by pkg/sftp's server over an in-memory pipe, reading this
// machine's filesystem, and loopback dials sent to the addresses in loopback.
// stall makes the host stop answering, as a stopped sshd or a dropped
// network does, without the connection ending. LastRead follows what the
// SFTP client reads, and flow stands in for relay data arriving on the same
// connection, as it does on a slow link while a page streams a bundle.
type fakeRemoteConn struct {
	files     *sftp.Client
	loopback  map[string]string
	done      chan struct{}
	server    *stallingConn
	pipe      net.Conn
	lastRead  atomic.Int64
	closed    bool
	stalled   bool
	slowPings bool
	pings     int
	mu        sync.Mutex
}

func (c *fakeRemoteConn) LastRead() time.Time { return time.Unix(0, c.lastRead.Load()) }

// flow keeps data arriving on the connection until the returned func is
// called.
func (c *fakeRemoteConn) flow() (stop func()) {
	done := make(chan struct{})
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				c.lastRead.Store(time.Now().UnixNano())
			}
		}
	}()
	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// slowPing makes keepalive replies wait behind data, as they do when the
// link is slower than what is being sent over it: a ping is never answered
// within its timeout, while data keeps arriving if flow says so.
func (c *fakeRemoteConn) slowPing() {
	c.mu.Lock()
	c.slowPings = true
	c.mu.Unlock()
}

// readTracker records when the SFTP client last read from its pipe.
type readTracker struct {
	net.Conn
	conn *fakeRemoteConn
}

func (r readTracker) Read(p []byte) (int, error) {
	n, err := r.Conn.Read(p)
	if n > 0 {
		r.conn.lastRead.Store(time.Now().UnixNano())
	}
	return n, err
}

func (c *fakeRemoteConn) Dial(ctx context.Context, addr string) (net.Conn, error) {
	c.mu.Lock()
	broken := c.closed
	c.mu.Unlock()
	if broken {
		return nil, io.EOF
	}
	target, ok := c.loopback[addr]
	if !ok {
		// What sshd answers when nothing listens at addr on the host.
		return nil, &ssh.OpenChannelError{Reason: ssh.ConnectionFailed, Message: "connection refused: " + addr}
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", target)
}

func (c *fakeRemoteConn) SFTP() *sftp.Client    { return c.files }
func (c *fakeRemoteConn) Done() <-chan struct{} { return c.done }

func (c *fakeRemoteConn) Ping(ctx context.Context) error {
	c.mu.Lock()
	c.pings++
	stalled := c.stalled || c.slowPings
	c.mu.Unlock()
	if stalled {
		<-ctx.Done()
		return ctx.Err()
	}
	return nil
}

func (c *fakeRemoteConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		// The transport first, as an SSH connection's: a request stalled
		// on it holds the SFTP client until it ends.
		_ = c.pipe.Close()
		_ = c.files.Close()
	}
	return nil
}

func (c *fakeRemoteConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// end makes the connection look as a dropped SSH connection does.
func (c *fakeRemoteConn) end() {
	_ = c.Close()
	close(c.done)
}

// stall makes the host stop answering: SFTP requests and keepalives get no
// reply, and the connection does not end.
func (c *fakeRemoteConn) stall() {
	c.mu.Lock()
	c.stalled = true
	c.mu.Unlock()
	c.server.stall()
}

// stallingConn is the SFTP server's end of the pipe; while stalled the
// server reads nothing, so it answers nothing.
type stallingConn struct {
	net.Conn
	gate   chan struct{}
	closed chan struct{}
	once   sync.Once
	mu     sync.Mutex
}

func newStallingConn(conn net.Conn) *stallingConn {
	gate := make(chan struct{})
	close(gate)
	return &stallingConn{Conn: conn, gate: gate, closed: make(chan struct{})}
}

func (c *stallingConn) stall() {
	c.mu.Lock()
	c.gate = make(chan struct{})
	c.mu.Unlock()
}

func (c *stallingConn) resume() {
	c.mu.Lock()
	select {
	case <-c.gate:
	default:
		close(c.gate)
	}
	c.mu.Unlock()
}

func (c *stallingConn) Read(p []byte) (int, error) {
	c.mu.Lock()
	gate := c.gate
	c.mu.Unlock()
	select {
	case <-gate:
	case <-c.closed:
		return 0, net.ErrClosed
	}
	return c.Conn.Read(p)
}

func (c *stallingConn) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func newFakeRemoteConn(t *testing.T, loopback map[string]string) *fakeRemoteConn {
	t.Helper()
	serverPipe, clientSide := net.Pipe()
	serverSide := newStallingConn(serverPipe)
	server, err := sftp.NewServer(serverSide)
	require.NoError(t, err)
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = serverSide.Close() })
	conn := &fakeRemoteConn{loopback: loopback, done: make(chan struct{}), server: serverSide, pipe: clientSide}
	conn.lastRead.Store(time.Now().UnixNano())
	client, err := sftp.NewClientPipe(readTracker{Conn: clientSide, conn: conn}, clientSide)
	require.NoError(t, err)
	conn.files = client
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// fakeRemote is a Remote whose connections are fakeRemoteConns, counting
// how many it made. Relays connect from their own goroutines, so its
// fields are read through its methods.
type fakeRemote struct {
	fail error
	*Remote
	made []*fakeRemoteConn
	mu   sync.Mutex
}

func newFakeRemote(t *testing.T, loopback map[string]string) *fakeRemote {
	t.Helper()
	f := &fakeRemote{}
	f.Remote = NewRemote("devbox", func() (RemoteConn, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		if f.fail != nil {
			return nil, f.fail
		}
		conn := newFakeRemoteConn(t, loopback)
		f.made = append(f.made, conn)
		return conn, nil
	})
	t.Cleanup(f.Close)
	return f
}

// conns is every connection made so far.
func (f *fakeRemote) conns() []*fakeRemoteConn {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*fakeRemoteConn(nil), f.made...)
}

// failWith makes every later connection fail with err.
func (f *fakeRemote) failWith(err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = err
}

func TestResolveRemote(t *testing.T) {
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)

	for _, tc := range []struct {
		name string
		path string
		dirs []string
		want []Found
	}{
		{"relative, in the cwd", "mockups/index.html", []string{f.cwd, f.other},
			[]Found{{Root: f.cwd, File: filepath.Join(f.cwd, "mockups", "index.html")}}},
		{"relative, in both", "docs/design.md", []string{f.cwd, f.other}, []Found{
			{Root: f.cwd, File: filepath.Join(f.cwd, "docs", "design.md")},
			{Root: f.other, File: filepath.Join(f.other, "docs", "design.md")},
		}},
		{"absolute, inside", filepath.Join(f.other, "sub", "page.html"), []string{f.cwd, f.other},
			[]Found{{Root: f.other, File: filepath.Join(f.other, "sub", "page.html")}}},
		{"innermost root", filepath.Join(f.sub, "page.html"), []string{f.other, f.sub},
			[]Found{{Root: f.sub, File: filepath.Join(f.sub, "page.html")}}},
		{"dot-dot staying inside", "../sub/page.html", []string{f.sub2, f.other},
			[]Found{{Root: f.other, File: filepath.Join(f.other, "sub", "page.html")}}},
		{"a link inside the root, relative", "inside.md", []string{f.cwd},
			[]Found{{Root: f.cwd, File: filepath.Join(f.cwd, "inside.md")}}},
		{"a link inside the root, absolute", "absolute.md", []string{f.cwd},
			[]Found{{Root: f.cwd, File: filepath.Join(f.cwd, "absolute.md")}}},
		{"the same file twice", "docs/design.md", []string{f.cwd, f.cwd + "/"},
			[]Found{{Root: f.cwd, File: filepath.Join(f.cwd, "docs", "design.md")}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveRemote(remote.Remote, tc.path, tc.dirs)
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
	assert.Len(t, remote.conns(), 1, "one connection serves every lookup")
}

func TestResolveRemoteRefuses(t *testing.T) {
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)

	for _, tc := range []struct {
		want error
		name string
		path string
		dirs []string
	}{
		{ErrNotPreviewable, "not previewable", "notes.txt", []string{f.cwd}},
		{ErrNoWorkdir, "no working directory", "index.html", nil},
		{ErrNoWorkdir, "only relative working directories", "index.html", []string{"relative/dir"}},
		{ErrEscapes, "absolute, outside", filepath.Join(f.outside, "secret.html"), []string{f.cwd}},
		{ErrEscapes, "dot-dot leaving", "../" + filepath.Base(f.outside) + "/secret.html", []string{f.cwd}},
		{ErrEscapes, "a link leading out", "link.html", []string{f.cwd}},
		{ErrEscapes, "a linked directory leading out", "linkdir/secret.html", []string{f.cwd}},
		{ErrNotFound, "missing", "missing.html", []string{f.cwd}},
		{ErrNotFound, "a directory", "folder.md", []string{f.cwd}},
		{ErrNotFound, "below a file", "plain/x.md", []string{f.cwd}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ResolveRemote(remote.Remote, tc.path, tc.dirs)
			require.ErrorIs(t, err, tc.want)
		})
	}
}

func TestResolveRemoteThroughALinkedWorkingDirectory(t *testing.T) {
	f := newResolveFixture(t)
	alias := filepath.Join(realDir(t), "alias")
	require.NoError(t, os.Symlink(f.cwd, alias))
	remote := newFakeRemote(t, nil)

	// The shell prints the path it was given; the root is where it leads.
	got, err := ResolveRemote(remote.Remote, "mockups/index.html", []string{alias})
	require.NoError(t, err)
	assert.Equal(t, []Found{{Root: f.cwd, File: filepath.Join(alias, "mockups", "index.html")}}, got)

	_, err = ResolveRemote(remote.Remote, "link.html", []string{alias})
	require.ErrorIs(t, err, ErrEscapes)
}

func TestResolveRemoteReportsAnUnreachableHost(t *testing.T) {
	remote := newFakeRemote(t, nil)
	remote.failWith(errors.New("connection refused"))

	_, err := ResolveRemote(remote.Remote, "index.html", []string{"/workspace/user/project"})
	require.ErrorIs(t, err, ErrRemote)
	assert.Contains(t, err.Error(), "devbox")
	assert.Contains(t, err.Error(), "connection refused")
}

func TestRealPathStopsALinkLoop(t *testing.T) {
	dir := realDir(t)
	require.NoError(t, os.Symlink("b", filepath.Join(dir, "a")))
	require.NoError(t, os.Symlink("a", filepath.Join(dir, "b")))
	remote := newFakeRemote(t, nil)
	files, err := remote.files()
	require.NoError(t, err)

	// A loop must end, and quickly: a count that never reaches its bound
	// would spin here instead of failing.
	done := make(chan error, 1)
	go func() {
		_, loopErr := realPath(files, filepath.Join(dir, "a"))
		done <- loopErr
	}()
	select {
	case err = <-done:
	case <-time.After(time.Second):
		t.Fatal("realPath did not stop following the loop")
	}
	require.ErrorIs(t, err, errRemoteLinkLoop)
	_, err = ResolveRemote(remote.Remote, "a.md", []string{dir})
	require.ErrorIs(t, err, ErrNotFound)
}

func TestRealPathFollowsUpToFortyLinks(t *testing.T) {
	dir := realDir(t)
	writeFile(t, filepath.Join(dir, "page.html"), "x")
	// l0 -> l1 -> ... -> l39 -> page.html: forty links, the most followed.
	for i := 0; i < maxRemoteLinks; i++ {
		target := fmt.Sprintf("l%d", i+1)
		if i == maxRemoteLinks-1 {
			target = "page.html"
		}
		require.NoError(t, os.Symlink(target, filepath.Join(dir, fmt.Sprintf("l%d", i))))
	}
	require.NoError(t, os.Symlink("l0", filepath.Join(dir, "more")))
	remote := newFakeRemote(t, nil)
	files, err := remote.files()
	require.NoError(t, err)

	got, err := realPath(files, filepath.Join(dir, "l0"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "page.html"), got)
	_, err = realPath(files, filepath.Join(dir, "more"))
	require.ErrorIs(t, err, errRemoteLinkLoop, "a forty-first link is one too many")
}

func TestRealPathFollowsDotDotPhysically(t *testing.T) {
	// "link/.." is the parent of where link leads, not the directory
	// holding link: a lexical clean would get this wrong.
	base := realDir(t)
	target := filepath.Join(base, "deep", "target")
	require.NoError(t, os.MkdirAll(target, 0o755))
	writeFile(t, filepath.Join(base, "deep", "page.html"), "x")
	require.NoError(t, os.MkdirAll(filepath.Join(base, "top"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join("..", "deep", "target", "..", "page.html"),
		filepath.Join(base, "top", "page.html")))
	remote := newFakeRemote(t, nil)
	files, err := remote.files()
	require.NoError(t, err)

	got, err := realPath(files, filepath.Join(base, "top", "page.html"))
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(base, "deep", "page.html"), got)
}

func TestRemoteReconnectsAfterTheConnectionEnds(t *testing.T) {
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)

	_, err := ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.NoError(t, err)
	remote.conns()[0].end()
	_, err = ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.NoError(t, err)
	assert.Len(t, remote.conns(), 2)

	remote.Close()
	_, err = ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.ErrorIs(t, err, ErrRemote)
	assert.True(t, remote.conns()[1].isClosed())
}

func serveRemote(t *testing.T, r *Registry, url string) *httptest.ResponseRecorder {
	t.Helper()
	rest := strings.TrimPrefix(url, RoutePrefix+"/")
	token, file, _ := strings.Cut(rest, "/")
	rec := httptest.NewRecorder()
	r.Serve(rec, httptest.NewRequest(http.MethodGet, url, nil), token, file)
	return rec
}

func TestRegistryOpenRemoteServesTheFileAndItsNeighbours(t *testing.T) {
	f := newResolveFixture(t)
	writeFile(t, filepath.Join(f.cwd, "mockups", "style.css"), "p{}")
	remote := newFakeRemote(t, nil)
	r := NewRegistry()

	url, err := r.OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "mockups", "index.html"))
	require.NoError(t, err)
	assert.Regexp(t, `^/preview/[0-9a-f]{32}/mockups/index\.html$`, url)

	rec := serveRemote(t, r, url)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "<p>cwd</p>", rec.Body.String())
	assert.Equal(t, htmlPolicy, rec.Header().Get("Content-Security-Policy"))

	css := strings.TrimSuffix(url, "index.html") + "style.css"
	rec = serveRemote(t, r, css)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "p{}", rec.Body.String())
	assert.Equal(t, otherPolicy, rec.Header().Get("Content-Security-Policy"))

	md, err := r.OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "docs", "design.md"))
	require.NoError(t, err)
	rec = serveRemote(t, r, md)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "<h1>cwd</h1>")
	assert.Equal(t, markdownPolicy, rec.Header().Get("Content-Security-Policy"))

	again, err := r.OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "mockups", "index.html"))
	require.NoError(t, err)
	assert.Equal(t, url, again, "the same file keeps its token")
}

func TestRegistryOpenRemoteRefuses(t *testing.T) {
	f := newResolveFixture(t)
	writeFile(t, filepath.Join(f.cwd, "large.html"), strings.Repeat("x", int(MaxDocumentBytes)+1))
	writeFile(t, filepath.Join(f.cwd, "limit.html"), strings.Repeat("x", int(MaxDocumentBytes)))
	remote := newFakeRemote(t, nil)
	r := NewRegistry()
	_, err := r.OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "limit.html"))
	require.NoError(t, err, "a file of exactly the limit opens")

	for _, tc := range []struct {
		want             error
		name, root, file string
	}{
		{ErrNotPreviewable, "not previewable", f.cwd, filepath.Join(f.cwd, "plain")},
		{ErrEscapes, "a link leading out", f.cwd, filepath.Join(f.cwd, "link.html")},
		{ErrEscapes, "outside the root", f.other, filepath.Join(f.cwd, "mockups", "index.html")},
		{ErrNotFound, "missing", f.cwd, filepath.Join(f.cwd, "missing.html")},
		{ErrNotFound, "a directory", f.cwd, filepath.Join(f.cwd, "folder.md")},
		{ErrTooLarge, "too large", f.cwd, filepath.Join(f.cwd, "large.html")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, openErr := r.OpenRemote(remote.Remote, tc.root, tc.file)
			require.ErrorIs(t, openErr, tc.want)
		})
	}

	remote.failWith(errors.New("no route to host"))
	remote.Close()
	_, err = r.OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "mockups", "index.html"))
	require.ErrorIs(t, err, ErrRemote)
}

func TestServeRemoteRefusesWhatLeavesTheRoot(t *testing.T) {
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)
	r := NewRegistry()
	url, err := r.OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "mockups", "index.html"))
	require.NoError(t, err)
	base := strings.TrimSuffix(url, "mockups/index.html")

	// A link made after the preview opened is refused when it is requested.
	require.NoError(t, os.Symlink(filepath.Join(f.outside, "secret.html"), filepath.Join(f.cwd, "late.html")))
	for _, rest := range []string{"link.html", "linkdir/secret.html", "late.html", "folder.md", "missing.html", "../x"} {
		rec := serveRemote(t, r, base+rest)
		assert.Equal(t, http.StatusNotFound, rec.Code, rest)
		assert.NotContains(t, rec.Body.String(), "secret", rest)
	}
	rec := serveRemote(t, r, base+"inside.md")
	assert.Equal(t, http.StatusOK, rec.Code)

	// Once the host cannot be reached, nothing is served.
	remote.failWith(errors.New("no route to host"))
	remote.conns()[0].end()
	rec = serveRemote(t, r, url)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestRemoteWithin(t *testing.T) {
	for _, tc := range []struct {
		dir, file string
		want      bool
	}{
		{"/w", "/w/a.html", true},
		{"/w", "/w", false},
		{"/w", "/wx/a.html", false},
		{"/", "/a.html", true},
		{"/", "/", false},
		{"/", "a.html", false},
	} {
		assert.Equal(t, tc.want, remoteWithin(tc.dir, tc.file), "%s in %s", tc.file, tc.dir)
	}
}

func TestRemoteLookupsOverABrokenConnection(t *testing.T) {
	// The SFTP client is gone but the connection has not reported its end,
	// and the host cannot be connected to again: what fails is the
	// connection, not the file, and is reported so.
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)
	_, err := ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.NoError(t, err)
	require.NoError(t, remote.conns()[0].Close())
	remote.failWith(errors.New("no route to host"))

	_, err = ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.ErrorIs(t, err, ErrRemote)
	_, err = NewRegistry().OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "mockups", "index.html"))
	require.ErrorIs(t, err, ErrRemote)
	assert.NotErrorIs(t, err, ErrNotFound)
}

func TestRemoteFilesWithoutPermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permissions do not bind root")
	}
	f := newResolveFixture(t)
	locked := filepath.Join(f.cwd, "locked")
	writeFile(t, filepath.Join(locked, "page.html"), "x")
	unreadable := writeFile(t, filepath.Join(f.cwd, "unreadable.html"), "x")
	remote := newFakeRemote(t, nil)
	r := NewRegistry()
	url, err := r.OpenRemote(remote.Remote, f.cwd, unreadable)
	require.NoError(t, err)
	require.NoError(t, os.Chmod(unreadable, 0))
	require.NoError(t, os.Chmod(locked, 0))
	t.Cleanup(func() {
		_ = os.Chmod(locked, 0o755)
		_ = os.Chmod(unreadable, 0o600)
	})

	_, err = r.OpenRemote(remote.Remote, f.cwd, filepath.Join(locked, "page.html"))
	require.ErrorIs(t, err, ErrNotFound)
	assert.Contains(t, err.Error(), "permission denied")
	assert.Equal(t, http.StatusNotFound, serveRemote(t, r, url).Code)
}

// The connection is checked, not trusted (issue #387's review): a host that
// stops answering, or whose SFTP subsystem goes away while the SSH
// connection stays up, is connected to again rather than left failing.

func TestRemoteConnectsAgainAfterItsSFTPGoesAway(t *testing.T) {
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)
	r := NewRegistry()
	url, err := r.OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "mockups", "index.html"))
	require.NoError(t, err)

	// sftp-server ends; the SSH connection does not.
	require.NoError(t, remote.conns()[0].files.Close())

	rec := serveRemote(t, r, url)
	assert.Equal(t, http.StatusOK, rec.Code, "the tab is served over a new connection")
	_, err = ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.NoError(t, err)
	require.Len(t, remote.conns(), 2)
	assert.True(t, remote.conns()[0].isClosed(), "the broken connection is closed")
}

func TestRemoteDoesNotWaitForAHostThatStopsAnswering(t *testing.T) {
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)
	remote.lookupTimeout = 200 * time.Millisecond
	r := NewRegistry()
	url, err := r.OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "mockups", "index.html"))
	require.NoError(t, err)
	remote.conns()[0].stall()

	start := time.Now()
	rec := serveRemote(t, r, url)
	assert.Less(t, time.Since(start), 5*time.Second, "a lookup is bounded")
	assert.Equal(t, http.StatusOK, rec.Code, "and answered over a new connection")
	assert.True(t, remote.conns()[0].isClosed())
	assert.Len(t, remote.conns(), 2)
}

func TestRemoteKeepaliveEndsAConnectionThatStopsAnswering(t *testing.T) {
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)
	remote.keepaliveInterval = 20 * time.Millisecond
	remote.keepaliveTimeout = 50 * time.Millisecond
	_, err := ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.NoError(t, err)

	first := remote.conns()[0]
	require.Eventually(t, func() bool {
		first.mu.Lock()
		defer first.mu.Unlock()
		return first.pings >= 2
	}, 5*time.Second, 10*time.Millisecond, "an answering connection is kept and checked")
	assert.False(t, first.isClosed())

	first.stall()
	require.Eventually(t, first.isClosed, 5*time.Second, 10*time.Millisecond,
		"a connection that stops answering is closed with no request waiting on it")
	_, err = ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.NoError(t, err)
	assert.Len(t, remote.conns(), 2)
}

func TestRemoteDoesNotHoldItsLockWhileConnecting(t *testing.T) {
	release := make(chan struct{})
	var dials atomic.Int32
	remote := NewRemote("devbox", func() (RemoteConn, error) {
		dials.Add(1)
		<-release
		return newFakeRemoteConn(t, nil), nil
	})

	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := remote.files()
			results <- err
		}()
	}
	require.Eventually(t, func() bool { return dials.Load() == 1 }, 5*time.Second, time.Millisecond)

	closed := make(chan struct{})
	go func() {
		remote.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("Close waited for the connection being made")
	}
	close(release)
	for range 2 {
		require.ErrorIs(t, <-results, ErrRemote, "a host closed while connecting is closed")
	}
	assert.Equal(t, int32(1), dials.Load(), "both callers waited for the one connection")
}

func TestRemoteReportsAFailedConnectionToEveryoneWaiting(t *testing.T) {
	release := make(chan struct{})
	remote := NewRemote("devbox", func() (RemoteConn, error) {
		<-release
		return nil, errors.New("connection refused")
	})
	t.Cleanup(remote.Close)
	results := make(chan error, 2)
	for range 2 {
		go func() {
			_, err := remote.files()
			results <- err
		}()
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	for range 2 {
		err := <-results
		require.ErrorIs(t, err, ErrRemote)
		assert.Contains(t, err.Error(), "connection refused")
	}
}

func TestRemoteSharesTheConnectionBeingMade(t *testing.T) {
	release := make(chan struct{})
	var dials atomic.Int32
	remote := NewRemote("devbox", func() (RemoteConn, error) {
		dials.Add(1)
		<-release
		return newFakeRemoteConn(t, nil), nil
	})
	t.Cleanup(remote.Close)
	results := make(chan *sftp.Client, 2)
	for range 2 {
		go func() {
			files, err := remote.files()
			assert.NoError(t, err)
			results <- files
		}()
	}
	require.Eventually(t, func() bool { return dials.Load() == 1 }, 5*time.Second, time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	close(release)
	first, second := <-results, <-results
	assert.Same(t, first, second, "both callers got the one connection")
	assert.Equal(t, int32(1), dials.Load())
}

func TestRemoteGivesUpAfterASecondBrokenConnection(t *testing.T) {
	// Every connection the host gives has lost its SFTP subsystem: the
	// lookup is tried on a new one once, then reported.
	f := newResolveFixture(t)
	var dials atomic.Int32
	remote := NewRemote("devbox", func() (RemoteConn, error) {
		dials.Add(1)
		conn := newFakeRemoteConn(t, nil)
		_ = conn.files.Close()
		return conn, nil
	})
	t.Cleanup(remote.Close)

	_, err := ResolveRemote(remote, "mockups/index.html", []string{f.cwd})
	require.ErrorIs(t, err, ErrRemote)
	assert.Equal(t, int32(2), dials.Load())
}

func TestRemoteDialKeepsAConnectionTheHostAnswersOn(t *testing.T) {
	remote := newFakeRemote(t, nil)

	// Nothing listens at the address: the host said so, so its connection
	// is fine and stays.
	_, err := remote.Dial(context.Background(), "localhost:5173")
	var refused *ssh.OpenChannelError
	require.ErrorAs(t, err, &refused)
	assert.Contains(t, err.Error(), "devbox")
	_, err = remote.Dial(context.Background(), "localhost:5173")
	require.Error(t, err)
	assert.Len(t, remote.conns(), 1)
	assert.False(t, remote.conns()[0].isClosed())

	// The connection itself fails: it is dropped, and the next dial makes
	// a new one.
	_ = remote.conns()[0].Close()
	_, err = remote.Dial(context.Background(), "localhost:5173")
	require.ErrorIs(t, err, io.EOF)
	_, err = remote.Dial(context.Background(), "localhost:5173")
	require.ErrorAs(t, err, &refused)
	assert.Len(t, remote.conns(), 2)
}

// A slow link is not a dead one (issue #387's review): keepalive replies and
// SFTP answers wait behind relay data on the same connection, so on a link
// slower than what a page streams they take longer than any fixed bound.
// Data still arriving proves the host answers, so a busy connection is kept;
// one that receives nothing is still closed (the two tests above).

func TestRemoteKeepsABusyConnectionWhosePingsAreSlow(t *testing.T) {
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)
	remote.keepaliveInterval = 20 * time.Millisecond
	remote.keepaliveTimeout = 50 * time.Millisecond
	_, err := ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.NoError(t, err)
	conn := remote.conns()[0]
	stop := conn.flow()
	conn.slowPing()

	time.Sleep(400 * time.Millisecond)
	assert.False(t, conn.isClosed(), "a connection data keeps arriving on is kept")

	stop()
	require.Eventually(t, conn.isClosed, 5*time.Second, 10*time.Millisecond,
		"once nothing arrives, a ping that is not answered still ends it")
}

func TestRemoteLookupWaitsWhileDataArrives(t *testing.T) {
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)
	remote.lookupTimeout = 100 * time.Millisecond
	r := NewRegistry()
	url, err := r.OpenRemote(remote.Remote, f.cwd, filepath.Join(f.cwd, "mockups", "index.html"))
	require.NoError(t, err)
	conn := remote.conns()[0]

	// The SFTP answer is held up behind other data for four times the
	// lookup's bound, and arrives.
	conn.server.stall()
	stop := conn.flow()
	defer stop()
	go func() {
		time.Sleep(400 * time.Millisecond)
		conn.server.resume()
	}()

	rec := serveRemote(t, r, url)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.False(t, conn.isClosed(), "the busy connection was not closed")
	assert.Len(t, remote.conns(), 1, "and no other was made")
}
