package hostexec

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"

	"panemux/internal/homedir"
	"panemux/internal/host"
)

var _ host.InputExecutor = (*Pool)(nil)

// RunInput puts only trusted application script text in sh -c; arbitrary input
// bytes stay on stdin. The login shell sees only a fixed one-line bootstrap and
// octal operands; decoding happens inside POSIX sh.
// Unlike Run's sh -s lane, the shell never reads commands from the input stream.
func (s *Pool) RunInput(
	ctx context.Context, h host.Host, script string, input io.Reader, args ...string,
) ([]byte, error) {
	if h == host.Local {
		return runLocalInput(ctx, script, input, args...)
	}
	conn, err := s.connect(ctx, string(h), true)
	if err != nil {
		return nil, err
	}
	// Conn.Run captures stdout. Merge stderr in the fixed wrapper so installer
	// diagnostics survive nonzero exits, without changing ordinary read calls.
	operands := append([]string{"exec 2>&1\n" + script}, args...)
	var cmd strings.Builder
	cmd.WriteString("sh -c '")
	cmd.WriteString(inputBootstrap)
	cmd.WriteString("' panemux-input")
	for _, arg := range operands {
		cmd.WriteString(" '")
		for i := 0; i < len(arg); i++ {
			fmt.Fprintf(&cmd, "\\0%03o", arg[i])
		}
		cmd.WriteString("'")
	}
	out, err := conn.Run(ctx, cmd.String(), input)
	return s.finishRun(ctx, string(h), conn, out, err)
}

// No newlines, history expansion, nested quotes or double backslashes reach the
// login shell (including csh and fish). The X suffix preserves trailing newlines
// during command substitution. Rotate decoded arguments without evaluating them.
const inputBootstrap = `for arg do value=$(printf "%bX" "$arg"); set -- "$@" "${value%X}"; shift; done; ` +
	`script=$1; shift; exec sh -c "$script" panemux-input "$@"`

// RunLocalSplit uses the local input execution boundary with separate output
// streams. Binary/structured stdout stays data; stderr is diagnostic only.
// RunInput retains its existing combined-output contract on every host.
func RunLocalSplit(
	ctx context.Context, script string, input io.Reader, args ...string,
) ([]byte, []byte, error) {
	var stdout, stderr bytes.Buffer
	err := runLocalInputCommand(ctx, script, input, &stdout, &stderr, args...)
	return stdout.Bytes(), stderr.Bytes(), err
}

func runLocalInput(ctx context.Context, script string, input io.Reader, args ...string) ([]byte, error) {
	var combined bytes.Buffer
	err := runLocalInputCommand(ctx, script, input, &combined, &combined, args...)
	return combined.Bytes(), err
}

func runLocalInputCommand(
	ctx context.Context, script string, input io.Reader, stdout, stderr io.Writer, args ...string,
) error {
	home, err := homedir.Dir()
	if err != nil {
		return fmt.Errorf("resolve home dir for host input execution: %w", err)
	}
	cmd := exec.CommandContext(ctx, "sh", "-c")
	cmd.Args = append(cmd.Args, script, "panemux-input")
	cmd.Args = append(cmd.Args, args...)
	cmd.Stdin = input
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = localWaitDelay
	err = cmd.Run()
	if ctx.Err() != nil {
		return &RunError{Err: ctx.Err(), TimedOut: true}
	}
	if err != nil {
		return &RunError{Err: err}
	}
	return nil
}
