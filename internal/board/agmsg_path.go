package board

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// remoteHomeProbeCmd is a fixed, non-tainted command run once per remote
// host to resolve $HOME for expanding a leading ~/ in agent_board.agmsg_path
// before it ever reaches a RunBoardCommand argument list — a literal ~
// inside quoteArgs's single-quoting never expands on the remote shell (see
// docs/agent-board.md's "~ in agmsg_path is expanded by panemux" note).
const remoteHomeProbeCmd = `echo -n "$HOME"`

// ResolveRemoteAgmsgPath expands a leading ~/ in path against the remote
// host's own $HOME, reached through executor. A path that doesn't start
// with ~/ (e.g. already absolute) is returned unchanged, with no
// RunBoardCommand call at all. Callers are expected to cache the result for
// the life of a connection — this function itself makes no attempt to
// cache, matching internal/board's existing per-call, stateless convention
// (see AgmsgClient's own doc comments).
func ResolveRemoteAgmsgPath(ctx context.Context, executor BoardExecutor, path string) (string, error) {
	if !strings.HasPrefix(path, "~/") {
		return path, nil
	}

	out, err := executor.RunBoardCommand(ctx, []string{"sh", "-c", remoteHomeProbeCmd})
	if err != nil {
		return "", fmt.Errorf("agmsg: resolving remote $HOME: %w", err)
	}
	home := strings.TrimSpace(string(out))
	if home == "" {
		return "", errors.New("agmsg: remote $HOME is empty")
	}
	return home + "/" + path[2:], nil
}
