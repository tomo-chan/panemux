package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"panemux/internal/config"
	"panemux/internal/portforward"
	"panemux/internal/session"
)

// remoteMockSession is a pane whose shell runs on another host: it can dial
// that host's loopback ports, so it is eligible for port forwarding.
type remoteMockSession struct {
	*mockSession
	echoAddr string
}

func newRemoteMockSession(id, echoAddr string) *remoteMockSession {
	m := newMockSession(id)
	m.typ = session.TypeSSH
	return &remoteMockSession{mockSession: m, echoAddr: echoAddr}
}

func (m *remoteMockSession) DialLoopback(ctx context.Context, port int) (net.Conn, error) {
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", m.echoAddr)
	if err != nil {
		return nil, fmt.Errorf("dialing pane host: %w", err)
	}
	return conn, nil
}

func startEchoServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := ln.Addr().(*net.TCPAddr).Port
	require.NoError(t, ln.Close())
	return port
}

func openURLTestConfig() *config.Config {
	return &config.Config{
		Server: config.ServerConfig{Port: 8080, Host: loopbackIPv4},
		Layout: config.LayoutNode{
			Direction: "horizontal",
			Children: []config.LayoutChild{
				{Size: 100, Pane: &config.PaneConfig{ID: "main", Type: "local"}},
			},
		},
	}
}

func newOpenURLHandler(t *testing.T, sessions ...session.Session) (*Handler, *portforward.Registry) {
	t.Helper()
	mgr := session.NewManager()
	for _, s := range sessions {
		mgr.Add(s)
	}
	h := NewHandler(openURLTestConfig(), mgr, nil, nil)
	h.sshConfigPath = filepath.Join(os.TempDir(), "panemux-test-ssh-config-nonexistent")
	registry := portforward.New(portforward.Options{SweepInterval: -1})
	t.Cleanup(registry.Close)
	h.SetPortForwards(registry)
	return h, registry
}

func postOpenURL(t *testing.T, h *Handler, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	r := setupRouterWithHandler(h)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+id+"/open-url", strings.NewReader(body))
	r.ServeHTTP(rec, req)
	return rec
}

func decodeOpenURL(t *testing.T, rec *httptest.ResponseRecorder) openURLResponse {
	t.Helper()
	var resp openURLResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	return resp
}

func TestPostOpenURL_RemotePane_ForwardsCallbackPort(t *testing.T) {
	port := freeLoopbackPort(t)
	sess := newRemoteMockSession("remote", startEchoServer(t))
	h, registry := newOpenURLHandler(t, sess)

	url := "https://example.com/auth?redirect_uri=http%3A%2F%2Flocalhost%3A" +
		strconv.Itoa(port) + "%2Fcallback"
	rec := postOpenURL(t, h, "remote", `{"url":"`+url+`"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	resp := decodeOpenURL(t, rec)
	assert.True(t, resp.Forwarded)
	assert.Equal(t, port, resp.Port)
	assert.Equal(t, []int{port}, registry.Ports("remote"))

	// The forward is live: bytes reach the pane host's echo server.
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 2*time.Second)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(2*time.Second)))
	_, err = io.WriteString(conn, "cb")
	require.NoError(t, err)
	buf := make([]byte, 2)
	_, err = io.ReadFull(conn, buf)
	require.NoError(t, err)
	assert.Equal(t, "cb", string(buf))
}

func TestPostOpenURL_RemotePane_RepeatedCallIsIdempotent(t *testing.T) {
	port := freeLoopbackPort(t)
	sess := newRemoteMockSession("remote", startEchoServer(t))
	h, registry := newOpenURLHandler(t, sess)
	body := `{"url":"http://localhost:` + strconv.Itoa(port) + `/dashboard"}`

	first := postOpenURL(t, h, "remote", body)
	second := postOpenURL(t, h, "remote", body)

	require.Equal(t, http.StatusOK, first.Code)
	require.Equal(t, http.StatusOK, second.Code)
	assert.True(t, decodeOpenURL(t, second).Forwarded)
	assert.Equal(t, []int{port}, registry.Ports("remote"))
}

func TestPostOpenURL_RemotePane_NoCallbackPortInURL(t *testing.T) {
	sess := newRemoteMockSession("remote", startEchoServer(t))
	h, registry := newOpenURLHandler(t, sess)

	rec := postOpenURL(t, h, "remote", `{"url":"https://example.com/docs"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	resp := decodeOpenURL(t, rec)
	assert.False(t, resp.Forwarded)
	assert.Equal(t, 0, resp.Port)
	assert.NotEmpty(t, resp.Reason)
	assert.Nil(t, registry.Ports("remote"))
}

func TestPostOpenURL_LocalPane_NeedsNoForward(t *testing.T) {
	port := freeLoopbackPort(t)
	h, registry := newOpenURLHandler(t, newMockSession("local"))

	url := "https://example.com/auth?redirect_uri=http%3A%2F%2Flocalhost%3A" +
		strconv.Itoa(port) + "%2Fcallback"
	rec := postOpenURL(t, h, "local", `{"url":"`+url+`"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	resp := decodeOpenURL(t, rec)
	assert.False(t, resp.Forwarded)
	assert.Contains(t, resp.Reason, "panemux host")
	assert.Nil(t, registry.Ports("local"))
}

func TestPostOpenURL_PortHeldByAnotherPane_Conflict(t *testing.T) {
	port := freeLoopbackPort(t)
	echo := startEchoServer(t)
	first := newRemoteMockSession("remote-1", echo)
	second := newRemoteMockSession("remote-2", echo)
	h, _ := newOpenURLHandler(t, first, second)
	body := `{"url":"http://127.0.0.1:` + strconv.Itoa(port) + `/cb"}`

	require.Equal(t, http.StatusOK, postOpenURL(t, h, "remote-1", body).Code)
	rec := postOpenURL(t, h, "remote-2", body)

	assert.Equal(t, http.StatusConflict, rec.Code)
	assert.Contains(t, rec.Body.String(), strconv.Itoa(port))
}

func TestPostOpenURL_PortHeldByAnotherProcess_Conflict(t *testing.T) {
	blocker, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer blocker.Close()
	port := blocker.Addr().(*net.TCPAddr).Port

	sess := newRemoteMockSession("remote", startEchoServer(t))
	h, _ := newOpenURLHandler(t, sess)

	rec := postOpenURL(t, h, "remote", `{"url":"http://localhost:`+strconv.Itoa(port)+`/cb"}`)

	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestPostOpenURL_UnknownSession_404(t *testing.T) {
	h, _ := newOpenURLHandler(t)

	rec := postOpenURL(t, h, "missing", `{"url":"https://example.com/"}`)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestPostOpenURL_InvalidBody_400(t *testing.T) {
	h, _ := newOpenURLHandler(t, newMockSession("local"))

	rec := postOpenURL(t, h, "local", `{"url":`)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestPostOpenURL_RejectedURLs_422(t *testing.T) {
	h, _ := newOpenURLHandler(t, newMockSession("local"))

	for _, body := range []string{
		`{"url":""}`,
		`{}`,
		`{"url":"file:///etc/passwd"}`,
		`{"url":"javascript:alert(1)"}`,
		`{"url":"ssh://example.com"}`,
	} {
		rec := postOpenURL(t, h, "local", body)
		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code, "body %s", body)
	}
}

func TestPostOpenURL_WithoutRegistry_ReportsUnavailable(t *testing.T) {
	mgr := session.NewManager()
	mgr.Add(newRemoteMockSession("remote", startEchoServer(t)))
	h := NewHandler(openURLTestConfig(), mgr, nil, nil)

	port := freeLoopbackPort(t)
	rec := postOpenURL(t, h, "remote", `{"url":"http://localhost:`+strconv.Itoa(port)+`/cb"}`)

	require.Equal(t, http.StatusOK, rec.Code)
	resp := decodeOpenURL(t, rec)
	assert.False(t, resp.Forwarded)
	assert.NotEmpty(t, resp.Reason)
}

func TestDeleteSession_ClosesPortForwards(t *testing.T) {
	port := freeLoopbackPort(t)
	sess := newRemoteMockSession("remote", startEchoServer(t))
	h, registry := newOpenURLHandler(t, sess)
	h.cfg.Layout.Children = append(h.cfg.Layout.Children, config.LayoutChild{
		Size: 100, Pane: &config.PaneConfig{ID: "remote", Type: "ssh"},
	})
	require.Equal(t, http.StatusOK,
		postOpenURL(t, h, "remote", `{"url":"http://localhost:`+strconv.Itoa(port)+`/cb"}`).Code)
	require.Equal(t, []int{port}, registry.Ports("remote"))

	r := setupRouterWithHandler(h)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/sessions/remote", nil))

	require.Equal(t, http.StatusNoContent, rec.Code)
	assert.Nil(t, registry.Ports("remote"))
	_, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	assert.Error(t, err, "forward should stop accepting connections once the pane is gone")
}

func TestRestartSession_ClosesPortForwards(t *testing.T) {
	port := freeLoopbackPort(t)
	echo := startEchoServer(t)
	sess := newRemoteMockSession("main", echo)
	h, registry := newOpenURLHandler(t, sess)
	h.createSession = func(pane *config.PaneConfig, _ map[string]config.SSHConnection) (session.Session, error) {
		return newRemoteMockSession(pane.ID, echo), nil
	}
	require.Equal(t, http.StatusOK,
		postOpenURL(t, h, "main", `{"url":"http://localhost:`+strconv.Itoa(port)+`/cb"}`).Code)
	require.Equal(t, []int{port}, registry.Ports("main"))

	r := setupRouterWithHandler(h)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/sessions/main/restart", nil))

	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, registry.Ports("main"), "a restarted pane's stale forwards must be dropped")
}
