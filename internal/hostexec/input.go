package hostexec

import (
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
// bytes stay on stdin and operands are quoted at the SSH construction boundary.
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
	operands := append([]string{"exec 2>&1\n" + script, "panemux-input"}, args...)
	var cmd strings.Builder
	cmd.WriteString("sh -c")
	for _, arg := range operands {
		cmd.WriteString(" '")
		cmd.WriteString(strings.ReplaceAll(arg, "'", `'\''`))
		cmd.WriteString("'")
	}
	out, err := conn.Run(ctx, cmd.String(), input)
	return s.finishRun(ctx, string(h), conn, out, err)
}

func runLocalInput(ctx context.Context, script string, input io.Reader, args ...string) ([]byte, error) {
	home, err := homedir.Dir()
	if err != nil {
		return nil, fmt.Errorf("resolve home dir for host input execution: %w", err)
	}
	cmd := exec.CommandContext(ctx, "sh", "-c")
	cmd.Args = append(cmd.Args, script, "panemux-input")
	cmd.Args = append(cmd.Args, args...)
	cmd.Stdin = input
	cmd.Env = append(os.Environ(), "HOME="+home)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = localWaitDelay
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, &RunError{Err: ctx.Err(), TimedOut: true}
	}
	if err != nil {
		return out, &RunError{Err: err}
	}
	return out, nil
}
