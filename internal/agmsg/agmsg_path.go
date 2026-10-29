package agmsg

import (
	"context"
	"errors"
	"fmt"
	"panemux/internal/host"
	"strings"
)

// remoteHomeProbeCmd is a fixed, non-tainted command run once per remote
// host to resolve $HOME for expanding a leading ~/ in agent_board.agmsg_path
// before it ever reaches a Runner.Run argument list — a literal ~
// inside quoteArgs's single-quoting never expands on the remote shell (see
// docs/agent-board.md's "~ in agmsg_path is expanded by panemux" note).
// printf, not "echo -n", because -n is not part of POSIX echo — some /bin/sh
// implementations print it literally instead of suppressing the trailing
// newline, which would otherwise corrupt the probed path in a way that's
// confusing to diagnose (a path literally starting with "-n ").
const remoteHomeProbeCmd = `printf '%s' "$HOME"`

// ResolveRemoteAgmsgPath expands a leading ~/ in path against the remote
// host's own $HOME, reached through executor. A path that doesn't start
// with ~/ (e.g. already absolute) is returned unchanged, with no
// Runner.Run call at all. Callers are expected to cache the result for
// the life of a connection — this function itself makes no attempt to
// cache, matching the adapter's existing per-call, stateless convention
// (see board.MessageClient).
func ResolveRemoteAgmsgPath(ctx context.Context, executor host.Runner, path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	out, err := executor.Run(ctx, execArguments, "sh", "-c", remoteHomeProbeCmd)
	if err != nil {
		return "", fmt.Errorf("agmsg: resolving remote $HOME: %w", err)
	}
	home := strings.TrimSpace(string(out))
	if home == "" {
		return "", errors.New("agmsg: remote $HOME is empty")
	}
	if !strings.HasPrefix(home, "/") {
		return "", fmt.Errorf("agmsg: remote $HOME is not an absolute path: %q", home)
	}
	return home + "/" + path[2:], nil
}
