#!/bin/sh
#
# Tests for frontend/screenshots/screenshots-env.sh, the staging helpers
# run-panemux-screenshots.sh boots the screenshot fixture with.
#
# What they protect: nothing from the developer's own configuration reaches
# the images, nothing the developer owns is deleted, and nothing the run
# started outlives it. The tmux checks report themselves as skipped where
# tmux is not installed.
#
# Run with: make test-screenshots-check

set -u

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
lib="$here/screenshots-env.sh"

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
}
pass() { echo "ok   $1"; }
skip() { echo "skip $1"; }
check() { checks=$((checks + 1)); }

work=$(mktemp -d)
# tmux's socket lives under these directories, and a socket path has a
# hard limit (104 bytes on macOS, 108 on Linux) that macOS's long
# per-user $TMPDIR, plus the run's own subdirectories, overruns. /tmp
# keeps it short on both.
short=$(mktemp -d /tmp/pmx-shot.XXXXXX)
trap 'rm -rf "$work" "$short"' EXIT

# ── shot_isolate_env: the developer's XDG and git configuration stay out ────

# A developer configuration under XDG_CONFIG_HOME that, if read, both leaks
# into the images (tmux status line) and breaks the run (commit signing).
real="$work/real"
mkdir -p "$real/tmux" "$real/git" "$work/fakehome"
echo "set -g status-left '[LEAK] '" >"$real/tmux/tmux.conf"
printf '[commit]\n\tgpgsign = true\n' >"$real/git/config"
printf '[commit]\n\tgpgsign = true\n' >"$work/global-gitconfig"

check
output=$(
	XDG_CONFIG_HOME="$real" XDG_CACHE_HOME="$real" XDG_DATA_HOME="$real" \
		XDG_STATE_HOME="$real" XDG_RUNTIME_DIR="$real" \
		GIT_CONFIG_GLOBAL="$work/global-gitconfig" \
		sh -c '. "$1"; shot_isolate_env "$2"; env' sh "$lib" "$work/fakehome" 2>&1
)
case $output in
*XDG_*=* | *GIT_CONFIG_GLOBAL=*) fail 'XDG and git config variables are cleared' "$output" ;;
*"HOME=$work/fakehome"*GIT_CONFIG_NOSYSTEM=1* | *GIT_CONFIG_NOSYSTEM=1*"HOME=$work/fakehome"*)
	pass 'XDG and git config variables are cleared' ;;
*) fail 'XDG and git config variables are cleared: HOME or GIT_CONFIG_NOSYSTEM missing' "$output" ;;
esac

# git runs outside any repository, so the answer depends only on the global
# and system files shot_isolate_env is responsible for, not on the local
# configuration of whatever repository this test happens to run from.
check
mkdir -p "$work/norepo"
output=$(
	cd "$work/norepo" &&
		XDG_CONFIG_HOME="$real" GIT_CONFIG_GLOBAL="$work/global-gitconfig" GIT_CEILING_DIRECTORIES="$work" \
			sh -c '. "$1"; shot_isolate_env "$2"; git config --get commit.gpgsign' sh "$lib" "$work/fakehome" 2>&1
)
if [ -z "$output" ]; then
	pass 'git reads none of the developer configuration'
else
	fail 'git reads none of the developer configuration' "$output"
fi

# ── shot_start_tmux / shot_stop_tmux ────────────────────────────────────────

# Each stop check first proves the server it is about to stop is running:
# without that, a server that never started (a socket path too long, say)
# reads as one the helper stopped.
if command -v tmux >/dev/null 2>&1; then
	tmuxdir="$short/tmux"
	mkdir -p "$tmuxdir" && chmod 700 "$tmuxdir"
	echo "set -g status-left '[sample-server] '" >"$work/fakehome/.tmux.conf"

	check
	output=$(
		XDG_CONFIG_HOME="$real" TMUX_TMPDIR="$tmuxdir" \
			sh -c '. "$1"; shot_isolate_env "$2"; shot_start_tmux -s t "sleep 30"; tmux show -g status-left' \
			sh "$lib" "$work/fakehome" 2>&1
	)
	case $output in
	*'[sample-server]'*) pass 'tmux reads only the fake HOME .tmux.conf' ;;
	*) fail 'tmux reads only the fake HOME .tmux.conf' "$output" ;;
	esac

	check
	if ! TMUX_TMPDIR="$tmuxdir" tmux has-session 2>/dev/null; then
		fail 'shot_stop_tmux stops the private tmux server: no server was running to stop'
	else
		TMUX_TMPDIR="$tmuxdir" sh -c '. "$1"; shot_stop_tmux' sh "$lib"
		if TMUX_TMPDIR="$tmuxdir" tmux has-session 2>/dev/null; then
			fail 'shot_stop_tmux stops the private tmux server'
			TMUX_TMPDIR="$tmuxdir" tmux kill-server 2>/dev/null
		else
			pass 'shot_stop_tmux stops the private tmux server'
		fi
	fi

	check
	if TMUX_TMPDIR="$tmuxdir" sh -c '. "$1"; shot_stop_tmux' sh "$lib"; then
		pass 'shot_stop_tmux succeeds with no server running'
	else
		fail 'shot_stop_tmux succeeds with no server running'
	fi

	check
	rundir="$short/tmp/panemux-screenshots/tmux"
	mkdir -p "$rundir" && chmod 700 "$rundir"
	if ! output=$(TMUX_TMPDIR="$rundir" tmux -f /dev/null new-session -d -s t 'sleep 30' 2>&1) ||
		! TMUX_TMPDIR="$rundir" tmux has-session 2>/dev/null; then
		fail 'shot_teardown stops the server under the run root: the server did not start' "$output"
	else
		TMPDIR="$short/tmp" sh -c '. "$1"; shot_teardown' sh "$lib"
		if TMUX_TMPDIR="$rundir" tmux has-session 2>/dev/null; then
			fail 'shot_teardown stops the server under the run root'
			TMUX_TMPDIR="$rundir" tmux kill-server 2>/dev/null
		else
			pass 'shot_teardown stops the server under the run root'
		fi
	fi
else
	skip 'tmux checks: tmux is not installed'
fi

# ── shot_shell_env: the panes' shell prints only what the images expect ────

# macOS's /bin/bash announces on every interactive start that the default
# shell is now zsh unless BASH_SILENCE_DEPRECATION_WARNING is set; the
# panes inherit panemux's environment, so the variable has to be set here.
check
output=$(sh -c '. "$1"; shot_shell_env; env' sh "$lib" 2>&1)
case $output in
*BASH_SILENCE_DEPRECATION_WARNING=1*) pass 'the bash deprecation notice is silenced' ;;
*) fail 'the bash deprecation notice is silenced' "$output" ;;
esac

check
if printf '%s\n' "$output" | grep -qx 'SHELL=/bin/bash' && printf '%s\n' "$output" | grep -qx 'LANG=C.UTF-8'; then
	pass 'the panes run bash in C.UTF-8'
else
	fail 'the panes run bash in C.UTF-8' "$output"
fi

# ── shot_claim_dir: only a directory this script created is emptied ────────

claim() { sh -c '. "$1"; shot_claim_dir "$2"' sh "$lib" "$1" 2>&1; }

check
d="$work/new"
if claim "$d" >/dev/null && [ -d "$d" ] && [ -f "$d/.panemux-screenshots" ]; then
	pass 'a missing directory is created and marked'
else
	fail 'a missing directory is created and marked'
fi

check
echo stale >"$d/old-file"
if claim "$d" >/dev/null && [ ! -e "$d/old-file" ] && [ -f "$d/.panemux-screenshots" ]; then
	pass 'a marked directory is emptied and stays marked'
else
	fail 'a marked directory is emptied and stays marked'
fi

check
d="$work/someone-elses"
mkdir -p "$d" && echo keep >"$d/work.txt"
if output=$(claim "$d"); then
	fail 'an unmarked directory is refused' "$output"
elif [ "$(cat "$d/work.txt" 2>/dev/null)" != keep ]; then
	fail 'an unmarked directory is refused: its contents were touched' "$output"
else
	case $output in
	*"$d"*) pass 'an unmarked directory is refused' ;;
	*) fail 'an unmarked directory is refused: the message does not name it' "$output" ;;
	esac
fi

check
f="$work/a-file"
echo keep >"$f"
if output=$(claim "$f"); then
	fail 'a file in the way is refused' "$output"
elif [ "$(cat "$f")" = keep ]; then
	pass 'a file in the way is refused'
else
	fail 'a file in the way is refused: it was touched' "$output"
fi

# ── shot_lock: two runs never stage at once ─────────────────────────────────

lock() { sh -c '. "$1"; shot_lock "$2" "$3"' sh "$lib" "$1" "$2" 2>&1; }

check
l="$work/run.lock"
if lock "$l" 4242 >/dev/null && [ "$(cat "$l/pid")" = 4242 ]; then
	pass 'a free lock is taken'
else
	fail 'a free lock is taken'
fi

check
sleep 30 &
live=$!
echo "$live" >"$l/pid"
if output=$(lock "$l" 4343); then
	fail 'a lock held by a running process is refused' "$output"
else
	case $output in
	*"$live"*) pass 'a lock held by a running process is refused' ;;
	*) fail 'a lock held by a running process is refused: the message does not name the holder' "$output" ;;
	esac
fi
kill "$live" 2>/dev/null
wait "$live" 2>/dev/null

check
# $live has exited, so the lock is stale: the run that held it was killed.
if lock "$l" 4444 >/dev/null && [ "$(cat "$l/pid")" = 4444 ]; then
	pass 'a stale lock is taken over'
else
	fail 'a stale lock is taken over'
fi

if [ "$failures" -ne 0 ]; then
	echo "screenshots-env tests: $failures of $checks failed"
	exit 1
fi
echo "screenshots-env tests: all $checks passed"
