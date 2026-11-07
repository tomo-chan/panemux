package agmsg

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"panemux/internal/homedir"
	"panemux/internal/host"
	"panemux/internal/hostexec"
)

// The connection runs the real SSH command construction through a login shell,
// without a network, a real terminal, or an upstream store.
type agmsgLoginConn struct {
	hostexec.Conn
	shell string
	home  string
}

func (c agmsgLoginConn) Run(ctx context.Context, command string, stdin io.Reader) ([]byte, error) {
	//nolint:gosec // shell resolved with LookPath, production command under test
	cmd := exec.CommandContext(ctx, c.shell, "-l", "-c", command)
	cmd.Env = append(os.Environ(), "HOME="+c.home, "ZDOTDIR="+c.home)
	cmd.Stdin = stdin
	return cmd.CombinedOutput()
}

func (agmsgLoginConn) Close() error { return nil }

// Local panemux normally starts from the user's login shell; model that parent
// while retaining Pool's actual local sh/argv/stdin execution boundaries.
type callerLogin struct {
	*hostexec.Pool
	shell string
}

const callerLoginScript = `script=$1; shift; exec sh -c "$script" board "$@"`

func (e callerLogin) Run(ctx context.Context, target host.Host, script string, args ...string) ([]byte, error) {
	if target == host.Local {
		args = append([]string{e.shell, "-l", "-c", callerLoginScript, "login", script}, args...)
		script = execArguments
	}
	return e.Pool.Run(ctx, target, script, args...)
}

func (e callerLogin) RunInput(
	ctx context.Context, target host.Host, script string, stdin io.Reader, args ...string,
) ([]byte, error) {
	if target == host.Local {
		args = append([]string{e.shell, "-l", "-c", callerLoginScript, "login", script}, args...)
		script = execArguments
	}
	return e.Pool.RunInput(ctx, target, script, stdin, args...)
}

func startupEnvironment(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	homedir.SetForTest(t, home)
	t.Setenv("ZDOTDIR", home)
	startup := filepath.Join(home, "startup")
	require.NoError(t, os.WriteFile(startup, []byte("export AGMSG_SELF_NAME=on\n"), 0600))
	for _, name := range []string{"BASH_ENV", "ENV"} {
		t.Setenv(name, startup)
	}
	for _, name := range []string{"TMUX", "TMUX_PANE", "HERDR_ENV", "HERDR_SOCKET_PATH", "HERDR_PANE_ID",
		"ORCA_TERMINAL_HANDLE", "CLAUDE_CODE_ENTRYPOINT", "CODEX_THREAD_ID"} {
		t.Setenv(name, "inherited-pane")
	}
	t.Setenv("AGMSG_STORAGE_PATH", "/workspace/sample-store")
	return home
}

func TestAgmsgBashStartupAndPaneIsolation(t *testing.T) {
	for _, shell := range []string{"sh", "bash", "zsh"} {
		for _, target := range []host.Host{host.Local, "remote"} {
			t.Run(shell+"/"+target.BoardID(), func(t *testing.T) {
				path, err := exec.LookPath(shell)
				if err != nil {
					t.Skip(shell + " unavailable")
				}
				home := startupEnvironment(t)
				conn := agmsgLoginConn{shell: path, home: home}
				pool := hostexec.New(hostexec.Options{
					Hosts: func() []string { return []string{"remote"} },
					Dial:  func(string) (hostexec.Conn, error) { return conn, nil },
				})
				t.Cleanup(pool.Close)
				verifyAgmsgStartup(t, callerLogin{Pool: pool, shell: path}, target)
			})
		}
	}
}

func verifyAgmsgStartup(t *testing.T, pool callerLogin, target host.Host) {
	t.Helper()
	root := t.TempDir()
	require.NoError(t, os.Mkdir(filepath.Join(root, "scripts"), 0700))
	observer, err := filepath.Abs("testdata/self-name/observer.sh")
	require.NoError(t, err)
	for _, name := range []string{"send.sh", "api.sh"} {
		require.NoError(t, os.Symlink(observer, filepath.Join(root, "scripts", name)))
	}
	capture := filepath.Join(t.TempDir(), "argv")
	t.Setenv("PANEMUX_TEST_CAPTURE", capture)
	client := NewRemoteAgmsgClient(target.BoardID(), root, host.Bind(pool, target))
	t.Run("send", func(t *testing.T) {
		require.NoError(t, client.Send(context.Background(), "team", "from", "to", "body ' $(literal)"))
		args, readErr := os.ReadFile(capture)
		require.NoError(t, readErr)
		require.Equal(t, "team\x00from\x00to\x00body ' $(literal)\x00--force\x00", string(args))
	})
	t.Run("read", func(t *testing.T) {
		_, readErr := client.Since(context.Background(), "team", "", 100)
		require.NoError(t, readErr)
	})
	contents, err := os.ReadFile(observer)
	require.NoError(t, err)
	archive := installerFixture(t, string(contents))
	for _, mode := range []string{"", "--update"} {
		t.Run("installer/"+mode, func(t *testing.T) {
			_, installErr := pool.RunInput(context.Background(), target, installerScript, bytes.NewReader(archive), mode)
			require.NoError(t, installErr)
		})
	}
}
