package session

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocal_Default(t *testing.T) {
	sess, err := NewLocal("test-id", "", "", "Test Title")
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, "test-id", sess.ID())
	assert.Equal(t, TypeLocal, sess.Type())
	assert.Equal(t, "Test Title", sess.Title())
	assert.Equal(t, StateConnected, sess.State())
}

func TestNewLocal_ExplicitShell(t *testing.T) {
	sess, err := NewLocal("test-id", "/bin/sh", "", "shell test")
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, StateConnected, sess.State())
}

func TestNewLocal_InvalidShell_Error(t *testing.T) {
	_, err := NewLocal("test-id", "/nonexistent/shell/xyz", "", "bad shell")
	assert.Error(t, err)
}

func TestNewLocal_State(t *testing.T) {
	sess, err := NewLocal("state-test", "/bin/sh", "", "state")
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, StateConnected, sess.State())
}

func TestNewLocal_Write_Read(t *testing.T) {
	sess, err := NewLocal("rw-test", "/bin/sh", "", "rw")
	require.NoError(t, err)
	defer sess.Close()

	_, err = sess.Write([]byte("echo hi\n"))
	require.NoError(t, err)

	type result struct {
		n   int
		err error
	}
	ch := make(chan result, 1)
	go func() {
		buf := make([]byte, 1024)
		n, err := sess.Read(buf)
		ch <- result{n, err}
	}()

	select {
	case r := <-ch:
		assert.NoError(t, r.err)
		assert.Greater(t, r.n, 0)
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for session read")
	}
}

func TestNewLocal_Resize(t *testing.T) {
	sess, err := NewLocal("resize-test", "/bin/sh", "", "resize")
	require.NoError(t, err)
	defer sess.Close()

	err = sess.Resize(120, 40)
	assert.NoError(t, err)
}

func TestNewLocal_Close(t *testing.T) {
	sess, err := NewLocal("close-test", "/bin/sh", "", "close")
	require.NoError(t, err)

	err = sess.Close()
	assert.NoError(t, err)

	// Allow background goroutine to update state
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, StateExited, sess.State())
}

func TestNewLocal_RelativeShell_Error(t *testing.T) {
	_, err := NewLocal("test-id", "sh", "", "relative shell")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "absolute path")
}

func TestNewLocal_WithCwd(t *testing.T) {
	tmpDir := os.TempDir()
	sess, err := NewLocal("cwd-test", "/bin/sh", tmpDir, "cwd")
	require.NoError(t, err)
	defer sess.Close()

	assert.Equal(t, StateConnected, sess.State())
}

func TestLocalSessionGetCWD(t *testing.T) {
	tmpDir := os.TempDir()
	sess, err := NewLocal("cwd-live-test", "/bin/sh", tmpDir, "cwd-live")
	require.NoError(t, err)
	defer sess.Close()

	cwd, err := sess.GetCWD()
	require.NoError(t, err)
	assert.NotEmpty(t, cwd)
}

func TestValidateShell_InEtcShells_OK(t *testing.T) {
	got, err := validateShell("/bin/sh")
	assert.NoError(t, err)
	assert.Equal(t, "/bin/sh", got)
}

func TestValidateShell_NotInEtcShells_Error(t *testing.T) {
	// Create a real executable that is not listed in /etc/shells.
	dir := t.TempDir()
	fakePath := dir + "/fakeshell"
	require.NoError(t, os.WriteFile(fakePath, []byte("#!/bin/sh\n"), 0755))

	_, err := validateShell(fakePath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an allowed shell")
}

func TestValidateShell_InvalidChars_Error(t *testing.T) {
	_, err := validateShell("/bin/sh; rm -rf /")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid characters")
}

func TestDetectLocalShell_ReturnsAbsolutePath(t *testing.T) {
	shell, err := DetectLocalShell()
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(shell), "expected absolute shell path, got %q", shell)
}

func TestDetectLocalShellFrom_MatchesCurrentUID(t *testing.T) {
	currentUser, err := user.Current()
	require.NoError(t, err)

	// Build content with the current user's entry mapping to /usr/bin/bash.
	// Only prepend a separate root entry if we are NOT root, to avoid having
	// two lines with the same UID (which would cause the first one to win).
	var content string
	if currentUser.Uid != "0" {
		content = "root:x:0:0:root:/root:/bin/false\n"
	}
	content += currentUser.Username + ":x:" + currentUser.Uid + ":1000::/home/user:/usr/bin/bash\n"
	tmpFile := filepath.Join(t.TempDir(), "passwd")
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0644))

	shell, err := detectLocalShellFrom(tmpFile)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin/bash", shell)
}

func TestDetectLocalShellFrom_UserNotFound_Error(t *testing.T) {
	content := "nobody:x:99999:99999::/nonexistent:/bin/false\n"
	tmpFile := filepath.Join(t.TempDir(), "passwd")
	require.NoError(t, os.WriteFile(tmpFile, []byte(content), 0644))

	_, err := detectLocalShellFrom(tmpFile)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shell not found")
}

func TestDetectLocalShellDscl_ParsesOutput(t *testing.T) {
	runner := func(username string) ([]byte, error) {
		return []byte("UserShell: /bin/zsh\n"), nil
	}
	shell, err := detectLocalShellDscl("tomo", runner)
	require.NoError(t, err)
	assert.Equal(t, "/bin/zsh", shell)
}

func TestDetectLocalShellDscl_NoUserShellLine_Error(t *testing.T) {
	runner := func(username string) ([]byte, error) {
		return []byte("No such key: UserShell\n"), nil
	}
	_, err := detectLocalShellDscl("tomo", runner)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "UserShell not found")
}
