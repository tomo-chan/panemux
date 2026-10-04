# Staging helpers for run-panemux-screenshots.sh and the capture's
# teardown. Sourced, not run. Tested by screenshots-env_test.sh.

SHOT_MARKER=.panemux-screenshots

# The run's private root: the fake HOME, the tmux socket and the build.
shot_root() {
    printf '%s/panemux-screenshots' "${TMPDIR:-/tmp}"
}

# Points HOME at a throwaway directory and clears every variable through
# which a program would still find the developer's own configuration or
# state: tmux and git read $XDG_CONFIG_HOME after ~, so a fake HOME alone
# leaves a developer's status line or commit signing in effect.
shot_isolate_env() {
    export HOME="$1"
    unset XDG_CONFIG_HOME XDG_CACHE_HOME XDG_DATA_HOME XDG_STATE_HOME XDG_RUNTIME_DIR \
        XDG_CONFIG_DIRS XDG_DATA_DIRS \
        GIT_CONFIG_GLOBAL GIT_CONFIG_SYSTEM GIT_CONFIG_COUNT GIT_CONFIG_PARAMETERS \
        GIT_DIR GIT_WORK_TREE
    export GIT_CONFIG_NOSYSTEM=1
}

# The panes' shell, which they inherit from panemux's environment. macOS's
# /bin/bash prints a notice that the default shell is now zsh on every
# interactive start unless BASH_SILENCE_DEPRECATION_WARNING is set, and the
# notice would land in every local pane of the images.
shot_shell_env() {
    export SHELL=/bin/bash
    export LANG=C.UTF-8
    export BASH_SILENCE_DEPRECATION_WARNING=1
}

# Starts the tmux server on TMUX_TMPDIR's private socket with the fake
# HOME's .tmux.conf as its only configuration (-f replaces the system and
# XDG config files as well as ~/.tmux.conf). Inherited TMUX would override
# TMUX_TMPDIR; clear it in a subshell for every start and stop.
shot_start_tmux() (
    unset TMUX
    tmux -f "$HOME/.tmux.conf" new-session -d "$@"
)

# Stops the server shot_start_tmux started. The server daemonizes, so
# stopping panemux leaves it, and the command its session runs, behind.
shot_stop_tmux() (
    unset TMUX
    if command -v tmux >/dev/null 2>&1; then
        tmux kill-server 2>/dev/null || true
    fi
)

# The capture's teardown (global-teardown.ts): stops the run's tmux server
# by the same socket directory the runner gave it.
shot_teardown() {
    TMUX_TMPDIR="$(shot_root)/tmux" shot_stop_tmux
}

# Makes $1 an empty directory this script owns. A directory the script
# created carries a marker file and is emptied; anything else already at
# the path is refused untouched, since these are fixed, generic paths
# (/tmp/sample-project) a developer may use for their own work.
shot_claim_dir() {
    if [ -e "$1" ] || [ -L "$1" ]; then
        if [ ! -d "$1" ] || [ -L "$1" ] || [ ! -f "$1/$SHOT_MARKER" ]; then
            echo "run-panemux-screenshots: $1 exists and was not created by this script; move it away and run again" >&2
            return 1
        fi
        rm -rf "$1"
    fi
    mkdir -p "$1"
    : >"$1/$SHOT_MARKER"
}

# Takes the lock directory $1 for process $2. Two runs would otherwise
# empty each other's fixed directories. A lock whose process has exited is
# stale (the capture's server is killed rather than stopped) and is taken
# over.
shot_lock() {
    if ! mkdir "$1" 2>/dev/null; then
        holder=$(cat "$1/pid" 2>/dev/null || true)
        if [ -n "$holder" ] && kill -0 "$holder" 2>/dev/null; then
            echo "run-panemux-screenshots: another run (pid $holder) holds $1" >&2
            return 1
        fi
        rm -rf "$1"
        mkdir "$1"
    fi
    echo "$2" >"$1/pid"
}
