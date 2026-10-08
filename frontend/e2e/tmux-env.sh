# Private tmux environment for the task-dashboard E2E fixture. Sourced.
# A short, unique directory under $TMPDIR keeps the socket path short.
e2e_tmux_env() {
    unset TMUX
    E2E_TMUX_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/pmx-e2e-tmux.XXXXXX") || return
    if ! tmux_socket_path_check "$E2E_TMUX_ROOT"; then
        rm -rf "$E2E_TMUX_ROOT"
        return 1
    fi
    TMUX_TMPDIR=$E2E_TMUX_ROOT
    export TMUX_TMPDIR
}

# Fails, saying what to change, when the socket tmux makes under
# TMUX_TMPDIR=$1 would overrun the Unix socket path limit: 104 bytes on macOS
# and 108 on Linux, counting the terminating NUL. Past it tmux fails late with
# "File name too long". The fix is a shorter $TMPDIR, never a fallback to a
# fixed /tmp path the Claude Code sandbox cannot write.
tmux_socket_path_check() {
    tmux_socket="$1/tmux-$(id -u)/default"
    case $(uname -s) in
    Darwin) tmux_socket_max=103 ;;
    *) tmux_socket_max=107 ;;
    esac
    if [ "${#tmux_socket}" -gt "$tmux_socket_max" ]; then
        echo "tmux socket path is ${#tmux_socket} bytes, over the $tmux_socket_max this OS allows: $tmux_socket" >&2
        echo "Set TMPDIR to a shorter directory and run again." >&2
        return 1
    fi
}

# The runner keeps this environment until all of its child processes exit.
# Clearing TMUX here too ensures cleanup can only select the private server.
e2e_tmux_cleanup() (
    unset TMUX
    if [ -n "${E2E_TMUX_ROOT:-}" ]; then
        if command -v tmux >/dev/null 2>&1; then
            TMUX_TMPDIR="$E2E_TMUX_ROOT" tmux kill-server 2>/dev/null || true
        fi
        rm -rf "$E2E_TMUX_ROOT"
    fi
)
