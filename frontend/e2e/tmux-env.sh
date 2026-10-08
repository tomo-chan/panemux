# Private tmux environment for the task-dashboard E2E fixture. Sourced.
# A short, unique directory avoids the Unix socket path limit on macOS.
e2e_tmux_env() {
    unset TMUX
    E2E_TMUX_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/pmx-e2e-tmux.XXXXXX") || return
    TMUX_TMPDIR=$E2E_TMUX_ROOT
    export TMUX_TMPDIR
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
