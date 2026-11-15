package agmsg

import (
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	cmd := exec.CommandContext(ctx, c.shell, "-l", "-c", observeLoginEnvironment+command)
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

// Login profiles may legitimately change PATH. Add a unique inert entry after
// login and snapshot that value, so a child shell's default PATH cannot pass.
const observeLoginEnvironment = `PATH="$PATH:$PANEMUX_TEST_PATH_SENTINEL"; ` +
	`export PATH PANEMUX_TEST_EXPECT_PATH="$PATH"; `

const callerLoginScript = observeLoginEnvironment + `script=$1; shift; exec sh -c "$script" board "$@"`

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
	t.Setenv("PANEMUX_TEST_EXPECT_HOME", home)
	t.Setenv("PANEMUX_TEST_PATH_SENTINEL", filepath.Join(home, "path-sentinel"))
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
	preservedAgmsgSettings(t)
	return home
}

func preservedAgmsgSettings(t *testing.T) {
	t.Helper()
	path, err := filepath.Abs("testdata/self-name/preserved-settings.env")
	require.NoError(t, err)
	loadPreservedAgmsgSettings(t, path)
	require.Equal(t, "/workspace/sample-windows", os.Getenv("SystemRoot"),
		"the fixture must seed the case-sensitive Windows system-directory fallback")
}

func loadPreservedAgmsgSettings(t *testing.T, path string) {
	t.Helper()
	contents, err := os.ReadFile(path)
	require.NoError(t, err)
	t.Setenv("PANEMUX_TEST_EXPECT_SETTINGS", path)
	for _, line := range strings.Split(string(contents), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		require.True(t, found)
		require.True(t, strings.HasPrefix(name, "AGMSG_") || strings.HasPrefix(name, "AGMSGD_") || name == "SystemRoot")
		t.Setenv(name, value)
	}
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
		require.Equal(t, "team\x00from\x00to\x00--body\x00-\x00--force\x00", string(args))
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
