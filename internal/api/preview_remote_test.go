package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/preview"
	"panemux/internal/session"
)

// previewTestConn is an SSH host's connection as the preview uses it: SFTP
// served by pkg/sftp over an in-memory pipe from this machine's filesystem,
// and loopback dials sent to the addresses in loopback.
type previewTestConn struct {
	files    *sftp.Client
	loopback map[string]string
	done     chan struct{}
	closed   bool
	mu       sync.Mutex
}

func (c *previewTestConn) Dial(ctx context.Context, addr string) (net.Conn, error) {
	target, ok := c.loopback[addr]
	if !ok {
		return nil, errors.New("connection refused: " + addr)
	}
	var d net.Dialer
	return d.DialContext(ctx, "tcp", target)
}

func (c *previewTestConn) SFTP() *sftp.Client    { return c.files }
func (c *previewTestConn) Done() <-chan struct{} { return c.done }

func (c *previewTestConn) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.closed {
		c.closed = true
		_ = c.files.Close()
	}
	return nil
}

func (c *previewTestConn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// previewHostFixture stands in for the SSH hosts a handler's previews reach:
// each ssh_connections host name has its own loopback services.
type previewHostFixture struct {
	loopback map[string]map[string]string // by SSHConfig.Host
	dialed   []string
	conns    []*previewTestConn
	mu       sync.Mutex
}

func (f *previewHostFixture) dial(t *testing.T) func(session.SSHConfig) (preview.RemoteConn, error) {
	return func(cfg session.SSHConfig) (preview.RemoteConn, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.dialed = append(f.dialed, cfg.Host)
		services, ok := f.loopback[cfg.Host]
		if !ok {
			return nil, errors.New("no route to host " + cfg.Host)
		}
		serverSide, clientSide := net.Pipe()
		server, err := sftp.NewServer(serverSide)
		require.NoError(t, err)
		go func() { _ = server.Serve() }()
		t.Cleanup(func() { _ = server.Close() })
		client, err := sftp.NewClientPipe(clientSide, clientSide)
		require.NoError(t, err)
		conn := &previewTestConn{files: client, loopback: services, done: make(chan struct{})}
		t.Cleanup(func() { _ = conn.Close() })
		f.conns = append(f.conns, conn)
		return conn, nil
	}
}

// sshPreviewSession is an ssh pane: its working directory, its agent's
// worktrees, and the ssh_connections entry it was made from.
type sshPreviewSession struct {
	connection string
	mockCWDSession
}

func (s *sshPreviewSession) ConnectionName() string { return s.connection }

func sshCWDSession(id, connection, cwd string, active ...string) *sshPreviewSession {
	sess := &sshPreviewSession{mockCWDSession: *cwdSession(id, cwd, active...), connection: connection}
	sess.typ = session.TypeSSH
	return sess
}

func newRemotePreviewHandler(
	t *testing.T, hosts *previewHostFixture, sessions ...session.Session,
) *Handler {
	t.Helper()
	h := newPreviewHandler(t, sessions...)
	h.sshConfigPath = filepath.Join(t.TempDir(), "no-ssh-config")
	h.cfg.SSHConnections = map[string]config.SSHConnection{
		"alpha": {Host: "alpha.example.invalid", User: "demo"},
		"beta":  {Host: "beta.example.invalid", User: "demo"},
	}
	h.previewHosts.dial = hosts.dial(t)
	t.Cleanup(h.Close)
	return h
}

func getPreview(t *testing.T, h *Handler, url string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	setupRouterWithHandler(h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, url, nil))
	return rec
}

func TestPostSessionPreview_ReadsAnSSHPanesFileFromItsHost(t *testing.T) {
	cwd := previewDir(t)
	writePreviewFile(t, filepath.Join(cwd, "mockups", "index.html"), "<p>remote mock</p>")
	writePreviewFile(t, filepath.Join(cwd, "mockups", "style.css"), "p{}")
	hosts := &previewHostFixture{loopback: map[string]map[string]string{"alpha.example.invalid": {}}}
	h := newRemotePreviewHandler(t, hosts, sshCWDSession("pane-ssh", "alpha", cwd))

	rec := postPreview(t, h, "pane-ssh", `{"path":"mockups/index.html"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodePreview(t, rec)
	assert.Equal(t, previewResponse{
		Status: previewStatusReady, Kind: previewKindFile, URL: resp.URL,
		Path: filepath.Join(cwd, "mockups", "index.html"), Host: "alpha",
	}, resp)
	assert.Regexp(t, `^/preview/[0-9a-f]{32}/mockups/index\.html$`, resp.URL)
	assert.Equal(t, "<p>remote mock</p>", getPreview(t, h, resp.URL).Body.String())
	assert.Equal(t, http.StatusOK, getPreview(t, h, resp.URL[:len(resp.URL)-len("index.html")]+"style.css").Code)
	hosts.mu.Lock()
	defer hosts.mu.Unlock()
	assert.Equal(t, []string{"alpha.example.invalid"}, hosts.dialed)
}

func TestPostSessionPreview_SSHTmuxAndTaskAttachReadFromTheirHost(t *testing.T) {
	cwd := previewDir(t)
	worktree := previewDir(t)
	writePreviewFile(t, filepath.Join(cwd, "docs", "design.md"), "# main")
	writePreviewFile(t, filepath.Join(worktree, "docs", "design.md"), "# feature")
	hosts := &previewHostFixture{loopback: map[string]map[string]string{"beta.example.invalid": {}}}
	sess := sshCWDSession("board-1", "beta", cwd, worktree)
	sess.typ = session.TypeSSHTmux
	h := newRemotePreviewHandler(t, hosts, sess)

	rec := postPreview(t, h, "board-1", `{"path":"docs/design.md"}`)

	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp := decodePreview(t, rec)
	assert.Equal(t, previewStatusChoose, resp.Status)
	assert.Equal(t, "beta", resp.Host)
	assert.Equal(t, []string{filepath.Join(cwd, "docs", "design.md"), filepath.Join(worktree, "docs", "design.md")},
		resp.Candidates)

	rec = postPreview(t, h, "board-1", `{"path":`+jsonString(t, resp.Candidates[1])+`}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	resp = decodePreview(t, rec)
	assert.Contains(t, getPreview(t, h, resp.URL).Body.String(), "<h1>feature</h1>")
}

func TestPostSessionPreview_RemoteRefusals(t *testing.T) {
	cwd := previewDir(t)
	outside := previewDir(t)
	writePreviewFile(t, filepath.Join(outside, "secret.html"), "secret")
	hosts := &previewHostFixture{loopback: map[string]map[string]string{"alpha.example.invalid": {}}}
	unnamed := cwdSession("unnamed", cwd)
	unnamed.typ = session.TypeSSH
	h := newRemotePreviewHandler(t, hosts,
		sshCWDSession("pane-ssh", "alpha", cwd),
		sshCWDSession("pane-down", "beta", cwd),
		sshCWDSession("pane-gone", "removed", cwd),
		unnamed,
		cwdSession("pane-local", cwd),
	)

	for _, tc := range []struct {
		name, id, body, contains string
		code                     int
	}{
		{"outside the working directory", "pane-ssh",
			`{"path":` + jsonString(t, filepath.Join(outside, "secret.html")) + `}`, "outside", http.StatusForbidden},
		{"missing", "pane-ssh", `{"path":"missing.md"}`, "missing.md", http.StatusNotFound},
		{"an unreachable host", "pane-down", `{"path":"index.html"}`, "no route to host", http.StatusBadGateway},
		{"a connection no longer configured", "pane-gone", `{"path":"index.html"}`, "removed",
			http.StatusBadGateway},
		{"an ssh pane naming no connection", "unnamed", `{"path":"index.html"}`, "SSH connection",
			http.StatusBadRequest},
		{"a URL that is not loopback", "pane-ssh", `{"url":"https://example.com/"}`, "only http and https URLs",
			http.StatusBadRequest},
		{"a URL of a pane on the panemux host", "pane-local", `{"url":"http://localhost:5173/"}`, "SSH host",
			http.StatusBadRequest},
		{"a URL of an unreachable host", "pane-down", `{"url":"http://localhost:5173/"}`, "no route to host",
			http.StatusBadGateway},
		{"a URL of a connection no longer configured", "pane-gone", `{"url":"http://localhost:5173/"}`, "removed",
			http.StatusBadGateway},
		{"a URL nothing answers at", "pane-ssh", `{"url":"http://localhost:5173/"}`,
			"alpha: connection refused: localhost:5173",
			http.StatusBadGateway},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := postPreview(t, h, tc.id, tc.body)
			assert.Equal(t, tc.code, rec.Code, rec.Body.String())
			assert.Contains(t, rec.Body.String(), tc.contains)
		})
	}
}

// startDevServer stands in for a development server on a host's loopback
// interface, answering every request with name.
func startDevServer(t *testing.T, name string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s %s", name, r.URL.RequestURI()) //nolint:gosec // a test server echoing its request
	}))
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

func fetch(t *testing.T, raw string) string {
	t.Helper()
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Get(raw) //nolint:noctx // a test server's URL
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

func TestPostSessionPreview_RelaysEachHostsLoopbackURLToAPortOfItsOwn(t *testing.T) {
	hosts := &previewHostFixture{loopback: map[string]map[string]string{
		"alpha.example.invalid": {"localhost:5173": startDevServer(t, "alpha")},
		"beta.example.invalid":  {"localhost:5173": startDevServer(t, "beta")},
	}}
	h := newRemotePreviewHandler(t, hosts,
		sshCWDSession("pane-a", "alpha", "/workspace/user/project"),
		sshCWDSession("pane-b", "beta", "/workspace/user/project"),
	)

	relayed := map[string]string{}
	for id, host := range map[string]string{"pane-a": "alpha", "pane-b": "beta"} {
		rec := postPreview(t, h, id, `{"url":"http://localhost:5173/app?x=1"}`)
		require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
		resp := decodePreview(t, rec)
		assert.Equal(t, previewStatusReady, resp.Status)
		assert.Equal(t, previewKindURL, resp.Kind)
		assert.Equal(t, "http://localhost:5173/app?x=1", resp.Path)
		assert.Equal(t, host, resp.Host)
		assert.Regexp(t, `^http://127\.0\.0\.1:\d+/app\?x=1$`, resp.URL)
		assert.NotContains(t, resp.URL, ":5173/")
		relayed[host] = resp.URL
	}
	assert.NotEqual(t, relayed["alpha"], relayed["beta"])
	assert.Equal(t, "alpha /app?x=1", fetch(t, relayed["alpha"]))
	assert.Equal(t, "beta /app?x=1", fetch(t, relayed["beta"]))

	// A tab opened on the relay keeps working once the pane is gone.
	require.NoError(t, h.manager.Remove("pane-a"))
	assert.Equal(t, "alpha /app?x=1", fetch(t, relayed["alpha"]))
}

func TestPreviewKeepsTheHostItWasOpenedOn(t *testing.T) {
	cwd := previewDir(t)
	writePreviewFile(t, filepath.Join(cwd, "index.html"), "<p>x</p>")
	hosts := &previewHostFixture{loopback: map[string]map[string]string{
		"alpha.example.invalid": {"localhost:5173": startDevServer(t, "alpha")},
		"moved.example.invalid": {"localhost:5173": startDevServer(t, "moved")},
	}}
	h := newRemotePreviewHandler(t, hosts, sshCWDSession("pane-a", "alpha", cwd))

	rec := postPreview(t, h, "pane-a", `{"url":"http://localhost:5173/"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	before := decodePreview(t, rec).URL
	rec = postPreview(t, h, "pane-a", `{"url":"http://localhost:5173/"}`)
	assert.Equal(t, before, decodePreview(t, rec).URL, "the same host and port keep their relay")

	// The entry is edited to name another host: what was opened stays on
	// the first, and the next preview reaches the new one.
	h.cfg.SSHConnections["alpha"] = config.SSHConnection{Host: "moved.example.invalid", User: "demo"}
	rec = postPreview(t, h, "pane-a", `{"url":"http://localhost:5173/"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	after := decodePreview(t, rec).URL
	assert.NotEqual(t, before, after)
	assert.Equal(t, "alpha /", fetch(t, before))
	assert.Equal(t, "moved /", fetch(t, after))
}

func TestHandlerCloseClosesThePreviewHosts(t *testing.T) {
	cwd := previewDir(t)
	writePreviewFile(t, filepath.Join(cwd, "index.html"), "<p>x</p>")
	hosts := &previewHostFixture{loopback: map[string]map[string]string{"alpha.example.invalid": {}}}
	h := newRemotePreviewHandler(t, hosts, sshCWDSession("pane-a", "alpha", cwd))
	rec := postPreview(t, h, "pane-a", `{"path":"index.html"}`)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	h.Close()
	hosts.mu.Lock()
	first := hosts.conns[0]
	hosts.mu.Unlock()
	assert.True(t, first.isClosed())
	rec = postPreview(t, h, "pane-a", `{"url":"http://localhost:5173/"}`)
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	assert.Contains(t, rec.Body.String(), "closed")
}
