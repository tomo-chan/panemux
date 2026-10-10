# Staging helpers for run-panemux-screenshots.sh and the capture's
# teardown. Sourced, not run. Tested by screenshots-env_test.sh.

SHOT_MARKER=.panemux-screenshots
SHOT_ROOT_NAME=panemux-screenshots

# The run's private root: the fake HOME, the tmux socket, the build, and the
# directories the fixed paths link to.
shot_root() {
    printf '%s/%s' "${TMPDIR:-/tmp}" "$SHOT_ROOT_NAME"
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

# Makes $1 an empty directory this script owns, carrying the marker. See
# shot_clear for what already at the path it replaces and what it refuses,
# and for the names $2... .
shot_claim_dir() {
    shot_clear "$@" || return 1
    mkdir -p "$1"
    : >"$1/$SHOT_MARKER"
}

# Makes the fixed path $1, which the images show (/tmp/sample-project), a
# link to $2, a directory inside the run's own root. Nothing this script
# keeps lives at the fixed path itself, so nothing there needs a marker that
# can be lost while the directories stay (issue #328). A link this script
# made is one whose target is <...>/panemux-screenshots/<the link's own
# name>; it is replaced, and what it pointed to is left alone. Anything else
# already at the path goes through shot_clear.
shot_link_dir() {
    if [ -L "$1" ]; then
        case $(readlink "$1") in
        */"$SHOT_ROOT_NAME/${1##*/}") rm -f "$1" ;;
        *)
            shot_refuse "$1"
            return 1
            ;;
        esac
    else
        shot_clear "$1" || return 1
    fi
    mkdir -p "$2"
    ln -s "$2" "$1"
}

# Removes $1 when this script left it: a directory carrying the marker, or a
# directory holding nothing but empty directories — what a run's directory
# becomes when something deletes its files, the marker included, and keeps
# the directories; rmdir can remove nothing else. macOS's cleaners do that:
# tmp_cleaner deletes files under /tmp unread for three days, and the marker
# is never read. With names $2... given, a directory whose every entry is one
# of them is removed too: the run root, under the user's own $TMPDIR, which
# dirhelper cleans the same way, can lose the marker and keep files read
# since. Anything else is refused untouched, since these are fixed paths
# (/tmp/sample-project) a developer may use for their own work.
shot_clear() {
    if [ ! -e "$1" ] && [ ! -L "$1" ]; then
        return 0
    fi
    if [ -d "$1" ] && [ ! -L "$1" ]; then
        if [ -f "$1/$SHOT_MARKER" ]; then
            rm -rf "$1"
            return
        fi
        if shot_clear_entries=$(find "$1" ! -type d -print 2>/dev/null) && [ -z "$shot_clear_entries" ]; then
            find "$1" -depth -type d -exec rmdir {} \;
            return
        fi
        if [ $# -gt 1 ] && shot_only_names "$@"; then
            rm -rf "$1"
            return
        fi
    fi
    shot_refuse "$1"
    return 1
}

# Succeeds when every entry of directory $1 is named one of $2... .
shot_only_names() {
    shot_on_dir=$1
    shift
    for shot_on_entry in "$shot_on_dir"/* "$shot_on_dir"/.[!.]* "$shot_on_dir"/..?*; do
        [ -e "$shot_on_entry" ] || [ -L "$shot_on_entry" ] || continue
        shot_on_known=
        for shot_on_name in "$@"; do
            [ "${shot_on_entry##*/}" = "$shot_on_name" ] && shot_on_known=1
        done
        [ -n "$shot_on_known" ] || return 1
    done
}

shot_refuse() {
    echo "run-panemux-screenshots: $1 exists and was not created by this script; move it away and run again" >&2
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
