package board

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

// PTYWriter is the minimal capability Bootstrap needs from a pane's live
// session: the ability to write the bootstrap instruction into its PTY, the
// same Session.Write path already used for all terminal input (see
// docs/agent-board.md's Bootstrap flow).
type PTYWriter interface {
	Write(p []byte) (int, error)
}

// ExpandLocalAgmsgPath expands a leading "~" in agmsgPath using the local
// user's home directory (via the injectable userHomeDirFn — see
// DEVELOPMENT.md's testability rule). A path with no leading "~" is
// returned unchanged.
func ExpandLocalAgmsgPath(agmsgPath string) (string, error) {
	if agmsgPath != "~" && !strings.HasPrefix(agmsgPath, "~/") {
		return agmsgPath, nil
	}
	home, err := userHomeDirFn()
	if err != nil {
		return "", fmt.Errorf("resolving local home directory: %w", err)
	}
	if agmsgPath == "~" {
		return home, nil
	}
	return filepath.Join(home, agmsgPath[2:]), nil
}

// ExpandRemoteAgmsgPath expands a leading "~" in agmsgPath using an
// already-resolved remote home directory (obtained via
// session.BoardHomeDirer, cached per SSH connection). This must happen
// before agmsgPath ever reaches a BoardExecutor argument: a literal "~"
// placed inside RunBoardCommand's escaping never expands on the remote
// shell — see docs/agent-board.md's "Integration with agmsg". Remote paths
// always use "/" regardless of the local OS.
func ExpandRemoteAgmsgPath(agmsgPath, remoteHome string) string {
	if agmsgPath == "~" {
		return remoteHome
	}
	if strings.HasPrefix(agmsgPath, "~/") {
		return strings.TrimSuffix(remoteHome, "/") + "/" + agmsgPath[2:]
	}
	return agmsgPath
}

// HasAgmsgLocal reports whether scripts/api.sh exists under agmsgPath on
// the local host. Never uses `command -v agmsg` — see docs/agent-board.md's
// "Detection, not installation" for why that check is unreliable (the
// `agmsg` npm package on PATH is a thin bootstrapper, not agmsg itself).
func HasAgmsgLocal(agmsgPath string) bool {
	_, err := os.Stat(filepath.Join(agmsgPath, "scripts", "api.sh"))
	return err == nil
}

// remoteAgmsgProbeBin is an absolute path to a POSIX `test` binary,
// satisfying RunBoardCommand's requirement that scriptPath be an absolute
// remote path (see buildBoardCommand in internal/session/ssh.go). `test` is
// not itself an agmsg script, but HasAgmsgRemote reuses the same escaped
// exec channel RunBoardCommand already provides rather than adding a
// second, unescaped remote-command code path just for this one check.
const remoteAgmsgProbeBin = "/bin/test"

// HasAgmsgRemote reports whether scripts/api.sh exists under agmsgPath on a
// remote host, via `test -f` over the pane's BoardExecutor. Never uses
// `command -v agmsg` for the same reason as HasAgmsgLocal.
func HasAgmsgRemote(ctx context.Context, executor BoardExecutor, agmsgPath string) bool {
	candidate := agmsgPath + "/scripts/api.sh"
	_, err := executor.RunBoardCommand(ctx, remoteAgmsgProbeBin, []string{"-f", candidate})
	return err == nil
}

// BootstrapInstruction builds the one-time text panemux writes into a
// pane's PTY to onboard it onto agmsg for Agent Board, per
// docs/agent-board.md's Bootstrap flow. paneID is used as the agmsg
// agent_id — required, not the agent's own choice, since every other part
// of this design addresses panes by their panemux pane ID.
func BootstrapInstruction(paneID, team, mode string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[panemux Agent Board bootstrap]\n")
	fmt.Fprintf(&b,
		"Join the agmsg team %q using EXACTLY %q as your agent id (not a name of your own "+
			"choosing) via agmsg's own onboarding flow (e.g. /agmsg).\n",
		team, paneID,
	)
	fmt.Fprintf(&b,
		"For every board-related send (status self-reports, and any message addressed to "+
			"another pane or to \"_panemux\"), call send.sh directly rather than /agmsg send, "+
			"always with --force:\n  <agmsg-path>/scripts/send.sh %s <from> <to> \"<body>\" --force\n",
		team,
	)
	b.WriteString(
		"On every status update, address the body to \"_panemux\" and shape it exactly as:\n" +
			`  {"kind":"board_status","state":"working|idle|waiting_approval","cwd":"...",` +
			`"branch":"...","repo":"...","pr_url":"...","last_tool":"...","summary":"..."}` + "\n",
	)
	switch mode {
	case "turn", "both":
		fmt.Fprintf(&b, "Also run: /agmsg mode %s\n", mode)
	}
	return b.String()
}

// Bootstrap detects agmsg on hostID (local via HasAgmsgLocal, remote via
// HasAgmsgRemote through executor) and, if found, writes the bootstrap
// instruction into the pane's PTY exactly once via pty.Write. If agmsg is
// not found, or no BoardExecutor is available for a remote host, it logs a
// warning naming the pane and returns without touching the pane's session
// at all — board is additive, never load-bearing for the pane to function
// (see docs/agent-board.md's Design principles). Callers are responsible
// for invoking this at most once per pane (e.g. on first detecting the
// pane's interactive agent process).
func Bootstrap(
	ctx context.Context,
	paneID, hostID, team, mode, agmsgPath string,
	pty PTYWriter,
	executor BoardExecutor,
	logf func(format string, args ...any),
) {
	if logf == nil {
		logf = log.Printf
	}

	var found bool
	if hostID == LocalHostID {
		found = HasAgmsgLocal(agmsgPath)
	} else if executor == nil {
		logf("board bootstrap: pane %q on host %q has no BoardExecutor available; skipping", paneID, hostID)
		return
	} else {
		found = HasAgmsgRemote(ctx, executor, agmsgPath)
	}

	if !found {
		logf("board bootstrap: agmsg not found at %q for pane %q (host %q); skipping bootstrap", agmsgPath, paneID, hostID)
		return
	}

	instruction := BootstrapInstruction(paneID, team, mode)
	if _, err := pty.Write([]byte(instruction)); err != nil {
		logf("board bootstrap: writing instruction to pane %q failed: %v", paneID, err)
	}
}
