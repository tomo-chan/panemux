package preview

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLoopbackURL(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		host string
		port int
	}{
		{"http://localhost:5173/", "localhost", 5173},
		{"http://LOCALHOST:5173/app?x=1#top", "localhost", 5173},
		{"http://127.0.0.1:3000", "127.0.0.1", 3000},
		{"http://127.0.0.2:3000", "127.0.0.2", 3000},
		{"http://[::1]:8080/", "::1", 8080},
		{"http://localhost/", "localhost", 80},
		{"https://localhost/", "localhost", 443},
		{"https://localhost:8443/", "localhost", 8443},
		{"http://localhost:80/", "localhost", 80},
		{"http://localhost:1/", "localhost", 1},
		{"http://localhost:65535/", "localhost", 65535},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			_, host, port, err := ParseLoopbackURL(tc.raw)
			require.NoError(t, err)
			assert.Equal(t, tc.host, host)
			assert.Equal(t, tc.port, port)
		})
	}
	for _, raw := range []string{
		"", "localhost:5173", "ftp://localhost:21/", "javascript:alert(1)", "http://example.com:5173/",
		"http://0.0.0.0:5173/", "http://192.168.1.10:5173/", "http://localhost:0/", "http://localhost:65536/",
		"http://localhost:abc/", "http://localhost.example.com/", "file:///etc/passwd", "http://%zz/",
	} {
		_, _, _, err := ParseLoopbackURL(raw)
		assert.ErrorIs(t, err, ErrNotLoopbackURL, raw)
	}
}

// startRemoteDevServer stands in for a development server on an SSH host's
// loopback interface. It answers every page with name, and echoes name and
// each message on its WebSocket.
func startRemoteDevServer(t *testing.T, name string) string {
	t.Helper()
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, msg, err := conn.ReadMessage()
			if err != nil {
				return
			}
			_ = conn.WriteMessage(websocket.TextMessage, []byte(name+":"+string(msg)))
		}
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprintf(w, "%s %s", name, r.URL.RequestURI()) //nolint:gosec // a test server echoing its request
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.Listener.Addr().String()
}

func get(t *testing.T, raw string) string {
	t.Helper()
	// A connection of its own each time, so every request is relayed anew.
	client := &http.Client{Transport: &http.Transport{DisableKeepAlives: true}}
	resp, err := client.Get(raw) //nolint:noctx // a test server's URL
	require.NoError(t, err)
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(body)
}

func TestRelaysKeepTheSamePortOfEachHostApart(t *testing.T) {
	// Two hosts both serving on 5173, and this host's own 5173 taken.
	local, err := net.Listen("tcp", "127.0.0.1:5173")
	if err == nil {
		t.Cleanup(func() { _ = local.Close() })
	}
	alpha := newFakeRemote(t, map[string]string{"localhost:5173": startRemoteDevServer(t, "alpha")})
	beta := newFakeRemote(t, map[string]string{"localhost:5173": startRemoteDevServer(t, "beta")})
	relays := NewRelays()
	t.Cleanup(relays.Close)

	alphaURL, err := relays.Open(alpha.Remote, "http://localhost:5173/app?x=1#top")
	require.NoError(t, err)
	betaURL, err := relays.Open(beta.Remote, "http://localhost:5173/app")
	require.NoError(t, err)

	a, err := url.Parse(alphaURL)
	require.NoError(t, err)
	b, err := url.Parse(betaURL)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", a.Hostname())
	assert.NotEqual(t, "5173", a.Port())
	assert.NotEqual(t, a.Port(), b.Port())
	assert.Equal(t, "/app", a.Path)
	assert.Equal(t, "x=1", a.RawQuery)
	assert.Equal(t, "top", a.Fragment)

	assert.Equal(t, "alpha /app?x=1", get(t, alphaURL))
	assert.Equal(t, "beta /app", get(t, betaURL))

	for name, raw := range map[string]string{"alpha": alphaURL, "beta": betaURL} {
		u, _ := url.Parse(raw)
		ws, resp, dialErr := websocket.DefaultDialer.Dial("ws://"+u.Host+"/ws", nil)
		require.NoError(t, dialErr)
		_ = resp.Body.Close()
		require.NoError(t, ws.WriteMessage(websocket.TextMessage, []byte("hmr")))
		_, msg, readErr := ws.ReadMessage()
		require.NoError(t, readErr)
		assert.Equal(t, name+":hmr", string(msg))
		_ = ws.Close()
	}

	again, err := relays.Open(alpha.Remote, "http://localhost:5173/other")
	require.NoError(t, err)
	o, _ := url.Parse(again)
	assert.Equal(t, a.Port(), o.Port(), "a host's port keeps its relay")
}

func TestRelaysTellLoopbackAddressesApart(t *testing.T) {
	remote := newFakeRemote(t, map[string]string{
		"localhost:3000": startRemoteDevServer(t, "name"),
		"127.0.0.1:3000": startRemoteDevServer(t, "v4"),
		"[::1]:3000":     startRemoteDevServer(t, "v6"),
	})
	relays := NewRelays()
	t.Cleanup(relays.Close)

	for raw, want := range map[string]string{
		"http://localhost:3000/": "name /",
		"http://127.0.0.1:3000/": "v4 /",
		"http://[::1]:3000/":     "v6 /",
	} {
		relayed, err := relays.Open(remote.Remote, raw)
		require.NoError(t, err)
		assert.Equal(t, want, get(t, relayed), raw)
	}
}

func TestRelayOutlivesAConnectionThatEnds(t *testing.T) {
	remote := newFakeRemote(t, map[string]string{"localhost:5173": startRemoteDevServer(t, "alpha")})
	relays := NewRelays()
	t.Cleanup(relays.Close)
	relayed, err := relays.Open(remote.Remote, "http://localhost:5173/")
	require.NoError(t, err)
	assert.Equal(t, "alpha /", get(t, relayed))

	remote.conns()[0].end()
	assert.Equal(t, "alpha /", get(t, relayed))
	assert.Len(t, remote.conns(), 2, "the host is connected to again")
}

func TestRelayClosesABrowserConnectionTheHostRefuses(t *testing.T) {
	// The server stops after the relay opened.
	stopped, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	remote := newFakeRemote(t, map[string]string{"localhost:5173": stopped.Addr().String()})
	relays := NewRelays()
	t.Cleanup(relays.Close)
	relayed, err := relays.Open(remote.Remote, "http://localhost:5173/")
	require.NoError(t, err)
	require.NoError(t, stopped.Close())
	u, _ := url.Parse(relayed)

	conn, err := net.Dial("tcp", u.Host)
	require.NoError(t, err)
	defer conn.Close()
	require.NoError(t, conn.SetDeadline(time.Now().Add(5*time.Second)))
	_, err = fmt.Fprintf(conn, "GET / HTTP/1.1\r\nHost: %s\r\n\r\n", u.Host)
	require.NoError(t, err)
	// Closed without an answer: an EOF, or a reset where the request was
	// still unread.
	_, err = bufio.NewReader(conn).ReadByte()
	require.Error(t, err)
	var timeout net.Error
	assert.False(t, errors.As(err, &timeout) && timeout.Timeout(), "the relay closes the connection")
}

func TestRelaysReportWhatDoesNotAnswer(t *testing.T) {
	remote := newFakeRemote(t, nil)
	relays := NewRelays()
	t.Cleanup(relays.Close)

	_, err := relays.Open(remote.Remote, "http://localhost:5173/")
	require.ErrorIs(t, err, ErrNoAnswer)
	assert.Contains(t, err.Error(), "devbox: ssh: rejected: connect failed (connection refused: localhost:5173)")

	remote.failWith(errors.New("no route to host"))
	remote.Close()
	_, err = relays.Open(remote.Remote, "http://localhost:5173/")
	require.ErrorIs(t, err, ErrRemote)
	assert.NotErrorIs(t, err, ErrNoAnswer)
}

func TestRelaysRefuse(t *testing.T) {
	server := startRemoteDevServer(t, "alpha")
	remote := newFakeRemote(t, map[string]string{
		"localhost:5173": server, "localhost:5174": server, "localhost:5175": server,
	})
	relays := NewRelays()

	_, err := relays.Open(remote.Remote, "http://example.com:5173/")
	require.ErrorIs(t, err, ErrNotLoopbackURL)

	relays.listen = func(string, string) (net.Listener, error) { return nil, errors.New("no ports left") }
	_, err = relays.Open(remote.Remote, "http://localhost:5173/")
	require.ErrorContains(t, err, "no ports left")

	relays.listen = net.Listen
	relayed, err := relays.Open(remote.Remote, "http://localhost:5174/")
	require.NoError(t, err)
	relays.Close()
	relays.Close()
	_, err = relays.Open(remote.Remote, "http://localhost:5175/")
	require.Error(t, err)

	u, _ := url.Parse(relayed)
	port, _ := strconv.Atoi(u.Port())
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), time.Second)
	if err == nil {
		_ = conn.Close()
	}
	assert.Error(t, err, "a closed relay no longer listens")
}

func TestRelayedURLKeepsWhatTheBrowserChecks(t *testing.T) {
	server := startRemoteDevServer(t, "dev")
	remote := newFakeRemote(t, map[string]string{
		"localhost:5173": server, "localhost:8443": server, "127.0.0.1:8443": server, "[::1]:8443": server,
	})
	relays := NewRelays()
	t.Cleanup(relays.Close)

	for _, tc := range []struct {
		raw, host, path, query string
		scheme                 string
	}{
		// A root URL printed without its slash is the root.
		{"http://localhost:5173", "127.0.0.1", "/", "", "http"},
		{"http://localhost:5173?x=1", "127.0.0.1", "/", "x=1", "http"},
		// TLS checks the certificate against the name the browser asks
		// for, and sends it as SNI: a localhost certificate needs localhost.
		{"https://localhost:8443/app", "localhost", "/app", "", "https"},
		{"https://127.0.0.1:8443/app", "127.0.0.1", "/app", "", "https"},
		// The relay listens on 127.0.0.1 only, so [::1] is reached there.
		{"https://[::1]:8443/app", "127.0.0.1", "/app", "", "https"},
	} {
		t.Run(tc.raw, func(t *testing.T) {
			relayed, err := relays.Open(remote.Remote, tc.raw)
			require.NoError(t, err)
			u, err := url.Parse(relayed)
			require.NoError(t, err)
			assert.Equal(t, tc.scheme, u.Scheme)
			assert.Equal(t, tc.host, u.Hostname())
			assert.Equal(t, tc.path, u.Path)
			assert.Equal(t, tc.query, u.RawQuery)
			assert.NotEmpty(t, u.Port())
		})
	}
}
