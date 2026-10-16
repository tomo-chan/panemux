package session

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	conn, err := newPreviewConn(client, nil)
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
	conn, err := newPreviewConn(client, nil)
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
