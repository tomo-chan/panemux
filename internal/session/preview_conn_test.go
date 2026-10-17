package session

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
)

func startPreviewTestSSHServer(
	t *testing.T, echoAddr string, sftp bool,
) (*PreviewConn, *testSSHTransport, *forwardRecorder, error) {
	t.Helper()
	rec := &forwardRecorder{}
	client, transport := startTestSSHTransport(t, nil, echoAddr, rec)
	transport.mu.Lock()
	transport.sftp = sftp
	transport.mu.Unlock()
	conn, err := newPreviewConn(client, nil, newReadActivity())
	return conn, transport, rec, err
}

func TestPreviewConnReadsFilesOverSFTP(t *testing.T) {
	conn, _, _, err := startPreviewTestSSHServer(t, "", true)
	require.NoError(t, err)
	defer conn.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "index.html")
	require.NoError(t, os.WriteFile(file, []byte("<p>remote</p>"), 0o600))
	f, err := conn.SFTP().Open(file)
	require.NoError(t, err)
	defer f.Close()
	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	assert.Equal(t, "<p>remote</p>", string(buf[:n]))
}

func TestPreviewConnDialsTheRemoteLoopback(t *testing.T) {
	conn, _, rec, err := startPreviewTestSSHServer(t, startEchoServer(t), true)
	require.NoError(t, err)
	defer conn.Close()

	tcp, err := conn.Dial(context.Background(), "localhost:5173")
	require.NoError(t, err)
	assertLoopbackRoundTrip(t, tcp, "hmr")
	addrs, ports := rec.snapshot()
	assert.Equal(t, []string{"localhost"}, addrs)
	assert.Equal(t, []uint32{5173}, ports)
}

func TestPreviewConnReportsAFailedDial(t *testing.T) {
	// The server refuses direct-tcpip when it forwards nowhere.
	rec := &forwardRecorder{}
	client, transport := startTestSSHTransport(t, nil, "", rec)
	transport.mu.Lock()
	transport.sftp = true
	transport.mu.Unlock()
	conn, err := newPreviewConn(client, nil, newReadActivity())
	require.NoError(t, err)
	defer conn.Close()

	_, err = conn.Dial(context.Background(), "localhost:5173")
	require.ErrorContains(t, err, "dialing localhost:5173 on the remote host")
}

func TestPreviewConnWithoutSFTPFailsAndClosesTheConnection(t *testing.T) {
	_, _, _, err := startPreviewTestSSHServer(t, "", false)
	require.ErrorContains(t, err, "start sftp")
}

func TestPreviewConnDoneClosesWhenTheConnectionEnds(t *testing.T) {
	conn, transport, _, err := startPreviewTestSSHServer(t, "", true)
	require.NoError(t, err)
	select {
	case <-conn.Done():
		t.Fatal("done before the connection ended")
	default:
	}
	transport.closeConnections()
	select {
	case <-conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Done did not close")
	}
	require.Error(t, conn.Close())
}

func TestDialPreviewConnReportsAnUnreachableHost(t *testing.T) {
	_, err := DialPreviewConn(SSHConfig{Host: "127.0.0.1", Port: 1, User: "u", Password: "p"})
	require.Error(t, err)
}

// The preview reaches a pane's host with the connection the pane itself
// was dialed with (issue #387): an ssh_connections entry edited afterwards
// does not move it. Only where the shell starts is left out — it is the
// pane's, not the host's.
func TestSSHSessionsKeepTheConnectionTheyWereDialedWith(t *testing.T) {
	withBrowserShimEnabled(t, false)
	jump := &SSHConfig{Host: "jump.example.invalid", User: "demo"}
	cfg := SSHConfig{
		Host: "devbox.example.invalid", Port: 2222, User: "demo", KeyFile: "/workspace/user/.ssh/id",
		KnownHostsFile: "/workspace/user/.ssh/known_hosts", ConnectionName: "devbox", JumpHost: jump,
		Cwd: "/remote/home/demo/project", Shell: "/bin/zsh",
	}
	want := cfg
	want.Cwd, want.Shell = "", ""

	client, _ := startSessionTestSSHServer(t, nil)
	sess, err := newSSHSessionFromClient("pane-ssh", "SSH", cfg, client, nil)
	require.NoError(t, err)
	defer sess.Close()
	assert.Equal(t, want, sess.PreviewSSHConfig())

	client, _ = startSessionTestSSHServer(t, nil)
	tmux, err := newTmuxSSHSessionFromClient("pane-tmux", "tmux", "work", cfg, client, nil)
	require.NoError(t, err)
	defer tmux.Close()
	assert.Equal(t, want, tmux.PreviewSSHConfig())

	var _ PreviewTarget = sess
	var _ PreviewTarget = tmux
}

func TestPreviewConnPingAnswersWhileTheHostDoes(t *testing.T) {
	conn, transport, _, err := startPreviewTestSSHServer(t, "", true)
	require.NoError(t, err)
	defer conn.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, conn.Ping(ctx))

	transport.closeConnections()
	<-conn.Done()
	require.Error(t, conn.Ping(ctx))

	canceled, stop := context.WithCancel(context.Background())
	stop()
	require.ErrorIs(t, conn.Ping(canceled), context.Canceled)
}

// startTrackedPreviewConn is a PreviewConn over a transport whose reads are
// recorded, as DialPreviewConn's is.
func startTrackedPreviewConn(t *testing.T) *PreviewConn {
	t.Helper()
	client, transport := startTestSSHTransport(t, nil, "", &forwardRecorder{})
	transport.mu.Lock()
	transport.sftp = true
	transport.mu.Unlock()
	raw, err := net.Dial("tcp", client.RemoteAddr().String())
	require.NoError(t, err)
	activity := newReadActivity()
	sshConn, chans, reqs, err := ssh.NewClientConn(activity.wrap(raw), "test", &ssh.ClientConfig{
		User:            "tester",
		HostKeyCallback: ssh.InsecureIgnoreHostKey(), //nolint:gosec // ephemeral in-process test server
	})
	require.NoError(t, err)
	conn, err := newPreviewConn(ssh.NewClient(sshConn, chans, reqs), nil, activity)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func TestPreviewConnLastReadFollowsWhatArrives(t *testing.T) {
	conn := startTrackedPreviewConn(t)
	before := conn.LastRead()
	require.False(t, before.IsZero(), "the handshake's answers count")

	time.Sleep(20 * time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, conn.Ping(ctx))
	after := conn.LastRead()
	assert.True(t, after.After(before), "a keepalive reply is something arriving")

	time.Sleep(20 * time.Millisecond)
	_, err := conn.SFTP().Getwd()
	require.NoError(t, err)
	assert.True(t, conn.LastRead().After(after), "and so is an SFTP answer")
}

func TestReadActivityCountsOnlyBytesRead(t *testing.T) {
	activity := newReadActivity()
	start := activity.last()
	client, server := net.Pipe()
	tracked := activity.wrap(client)
	defer tracked.Close()

	time.Sleep(5 * time.Millisecond)
	go func() { _ = server.Close() }()
	_, err := tracked.Read(make([]byte, 1))
	require.Error(t, err)
	assert.Equal(t, start, activity.last(), "a read that brings nothing is not activity")
}
