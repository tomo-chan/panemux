package preview

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRemoteConn is an SSH host's connection as the preview uses it: SFTP
// served by pkg/sftp's server over an in-memory pipe, reading this
// machine's filesystem, and loopback dials sent to the addresses in loopback.
type fakeRemoteConn struct {
	files    *sftp.Client
	loopback map[string]string
	done     chan struct{}
	closed   bool
	mu       sync.Mutex
}

func (c *fakeRemoteConn) Dial(ctx context.Context, addr string) (net.Conn, error) {
	target, ok := c.loopback[addr]
	if !ok {
		return nil, errors.New("connection refused: " + addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", target)
}

func (c *fakeRemoteConn) SFTP() *sftp.Client    { return c.files }
func (c *fakeRemoteConn) Done() <-chan struct{} { return c.done }

func (c *fakeRemoteConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		_ = c.files.Close()
	}
	return nil
}

// end makes the connection look as a dropped SSH connection does.
func (c *fakeRemoteConn) end() {
	_ = c.Close()
	close(c.done)
}

func newFakeRemoteConn(t *testing.T, loopback map[string]string) *fakeRemoteConn {
	t.Helper()
	serverSide, clientSide := net.Pipe()
	server, err := sftp.NewServer(serverSide)
	require.NoError(t, err)
	go func() { _ = server.Serve() }()
	t.Cleanup(func() { _ = server.Close() })
	client, err := sftp.NewClientPipe(clientSide, clientSide)
	require.NoError(t, err)
	conn := &fakeRemoteConn{files: client, loopback: loopback, done: make(chan struct{})}
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

// fakeRemote is a Remote whose connections are fakeRemoteConns, counting
// how many it made.
type fakeRemote struct {
	fail error
	*Remote
	conns []*fakeRemoteConn
}

func newFakeRemote(t *testing.T, loopback map[string]string) *fakeRemote {
	t.Helper()
	f := &fakeRemote{}
	f.Remote = NewRemote("devbox", func() (RemoteConn, error) {
		if f.fail != nil {
			return nil, f.fail
		}
		conn := newFakeRemoteConn(t, loopback)
		f.conns = append(f.conns, conn)
		return conn, nil
	})
	t.Cleanup(f.Close)
	return f
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
	assert.Len(t, remote.conns, 1, "one connection serves every lookup")
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
	remote.fail = errors.New("connection refused")

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
	remote.conns[0].end()
	_, err = ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.NoError(t, err)
	assert.Len(t, remote.conns, 2)

	remote.Close()
	_, err = ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.ErrorIs(t, err, ErrRemote)
	assert.True(t, remote.conns[1].closed)
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

	remote.fail = errors.New("no route to host")
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
	remote.fail = errors.New("no route to host")
	remote.conns[0].end()
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
	// The SFTP client is gone but the connection has not reported its end:
	// what fails is the connection, not the file, and is reported so.
	f := newResolveFixture(t)
	remote := newFakeRemote(t, nil)
	_, err := ResolveRemote(remote.Remote, "mockups/index.html", []string{f.cwd})
	require.NoError(t, err)
	require.NoError(t, remote.conns[0].Close())

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
