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
# "File name too long". tmux binds the socket under the directory's resolved
# path (realpath), so the length is measured there: macOS's /tmp and
# /var/folders are links into /private, eight bytes longer than they read.
# The fix is a shorter $TMPDIR, never a fallback to a fixed /tmp path the
# Claude Code sandbox cannot write.
tmux_socket_path_check() {
    tmux_socket="$(tmux_physical_path "$1")/tmux-$(id -u)/default"
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

# Prints the absolute path $1 as realpath() would resolve it: its longest
# existing leading directory resolved, and the components that do not exist
# yet, which cannot be links, appended as given.
tmux_physical_path() {
    tmux_pp_dir=$1
    tmux_pp_rest=
    while [ -n "$tmux_pp_dir" ] && [ ! -d "$tmux_pp_dir" ]; do
        tmux_pp_rest="/${tmux_pp_dir##*/}$tmux_pp_rest"
        tmux_pp_dir=${tmux_pp_dir%/*}
    done
    if [ -z "$tmux_pp_dir" ]; then
        printf '%s\n' "$tmux_pp_rest"
    elif tmux_pp_dir=$(CDPATH='' cd -P -- "$tmux_pp_dir" 2>/dev/null && pwd -P); then
        printf '%s%s\n' "${tmux_pp_dir%/}" "$tmux_pp_rest"
    else
        printf '%s\n' "$1"
    fi
}

# Makes a fresh directory under the first of the candidate directories $2...
# that is writable and leaves room for a socket at
# TMUX_TMPDIR=<that new directory>$1, and prints its path. Fails, saying why,
# when none does. For screenshots-env_test.sh, whose sockets sit deeper than
# either fixture's: inside the Claude Code sandbox $TMPDIR is short, and the
# only place it can write; outside it, macOS's per-user $TMPDIR is too long
# for them and /tmp is not.
tmux_short_dir() {
    tmux_sd_suffix=$1
    shift
    for tmux_sd_base in "$@"; do
        [ -d "$tmux_sd_base" ] && [ -w "$tmux_sd_base" ] || continue
        tmux_socket_path_check "$tmux_sd_base/p.XXXXXX$tmux_sd_suffix" 2>/dev/null || continue
        tmux_sd_dir=$(mktemp -d "$tmux_sd_base/p.XXXXXX") || continue
        printf '%s\n' "$tmux_sd_dir"
        return 0
    done
    echo "No writable directory among: $* leaves room for the tmux socket." >&2
    for tmux_sd_base in "$@"; do
        tmux_socket_path_check "$tmux_sd_base/p.XXXXXX$tmux_sd_suffix" || true
    done
    return 1
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
