package hostexec

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"panemux/internal/homedir"
	"panemux/internal/host"
)

func TestRunInputTransfersArchiveOnStdin(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	var archive bytes.Buffer
	w := tar.NewWriter(&archive)
	body := "archive payload\x00\n$(exit 9)"
	require.NoError(t, w.WriteHeader(&tar.Header{Name: "sample.txt", Mode: 0600, Size: int64(len(body))}))
	_, err := w.Write([]byte(body))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	p := New(Options{})
	defer p.Close()
	out, err := p.RunInput(poolTestContext(t), host.Local, "tar -xOf - sample.txt", &archive)
	require.NoError(t, err)
	require.Equal(t, body, string(out))
}

type inputExitError struct{}

func (inputExitError) Error() string   { return "exit 7" }
func (inputExitError) ExitStatus() int { return 7 }

type inputFailureConn struct {
	pingErr error
	cancel  context.CancelFunc
	testConn
}

func (c *inputFailureConn) Run(ctx context.Context, cmd string, in io.Reader) ([]byte, error) {
	if c.cancel != nil {
		c.cancel()
	}
	return c.testConn.Run(ctx, cmd, in)
}
func (c *inputFailureConn) Ping(context.Context) error { return c.pingErr }

func TestRunInputRemoteFailureKeepsDiagnosticsAndConnectionPolicy(t *testing.T) {
	for _, tc := range []struct {
		failure                  error
		name                     string
		cancel, badPing, dropped bool
	}{
		{inputExitError{}, "exit", false, false, false},
		{errors.New("offline"), "transport", false, false, true},
		{errors.New("channel closed"), "timeout healthy", true, false, false},
		{errors.New("channel closed"), "timeout dead", true, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(poolTestContext(t))
			defer cancel()
			c := &inputFailureConn{testConn: testConn{fail: tc.failure}}
			if tc.cancel {
				c.cancel = cancel
			}
			if tc.badPing {
				c.pingErr = errors.New("ping failed")
			}
			p := New(Options{Hosts: func() []string { return []string{"dev"} },
				Dial: func(string) (Conn, error) { return c, nil }})
			defer p.Close()
			out, err := p.RunInput(ctx, "dev", "cat", strings.NewReader("archive"))
			require.Equal(t, "ok", string(out))
			var run *RunError
			require.ErrorAs(t, err, &run)
			require.Equal(t, tc.cancel, run.TimedOut)
			if tc.cancel {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, tc.failure)
			}
			if tc.dropped {
				require.Nil(t, p.Current("dev"))
				require.Equal(t, 1, c.closed)
			} else {
				require.Same(t, c, p.Current("dev"))
				require.Zero(t, c.closed)
			}
		})
	}
}

func TestRunInputLocalPreservesBinaryAndQuotesArguments(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	p := New(Options{})
	defer p.Close()
	data := []byte{0, 1, '\n', '\'', 255}
	arg := "a'\n$(exit 9);`exit 8`"
	out, err := p.RunInput(poolTestContext(t), host.Local, `printf '%s' "$1"; cat`, bytes.NewReader(data), arg)
	require.NoError(t, err)
	require.Equal(t, append([]byte(arg), data...), out)
	out, err = p.RunInput(poolTestContext(t), host.Local, `printf '%s' "$HOME"`, nil)
	require.NoError(t, err)
	home, err := homedir.Dir()
	require.NoError(t, err)
	require.Equal(t, home, string(out))
}

func TestRunInputRemoteCommandAndStdinAreSeparate(t *testing.T) {
	c := &testConn{}
	p := New(Options{Hosts: func() []string { return []string{"dev"} },
		Dial: func(string) (Conn, error) { return c, nil }})
	defer p.Close()
	data := "\x00archive\xff\n$(exit 9)"
	_, err := p.RunInput(poolTestContext(t), "dev", `printf '%s' "$1"; cat`, strings.NewReader(data), "it's; $(exit 9)")
	require.NoError(t, err)
	require.Equal(t, []string{data}, c.scripts)
	// Execute the constructed SSH command locally to verify both quoting boundaries.
	//nolint:gosec // Test intentionally executes the constructed SSH command to verify its quoting.
	cmd := exec.CommandContext(poolTestContext(t), "sh", "-c", c.commands[0])
	cmd.Stdin = strings.NewReader(data)
	out, err := cmd.Output()
	require.NoError(t, err)
	require.Equal(t, "it's; $(exit 9)"+data, string(out))
	_, err = p.RunInput(poolTestContext(t), "unknown", "cat", nil)
	require.ErrorIs(t, err, ErrUnknownHost)
}

func TestRunInputLocalFailuresKeepOutputExitAndTimeout(t *testing.T) {
	homedir.SetForTest(t, t.TempDir())
	p := New(Options{})
	defer p.Close()
	out, err := p.RunInput(poolTestContext(t), host.Local, `printf out; printf reason >&2; exit 7`, nil)
	require.Equal(t, "outreason", string(out))
	var exit *exec.ExitError
	require.ErrorAs(t, err, &exit)
	require.Equal(t, 7, exit.ExitCode())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = p.RunInput(ctx, host.Local, `sleep 30`, nil)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	homedir.SetFailingForTest(t, errors.New("no home"))
	_, err = p.RunInput(poolTestContext(t), host.Local, "cat", nil)
	require.ErrorContains(t, err, "no home")
}
