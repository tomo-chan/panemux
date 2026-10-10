#!/bin/sh
#
# Tests for frontend/screenshots/screenshots-env.sh, the staging helpers
# run-panemux-screenshots.sh boots the screenshot fixture with.
#
# What they protect: nothing from the developer's own configuration reaches
# the images, nothing the developer owns is deleted, and nothing the run
# started outlives it. The tmux checks report themselves as skipped where
# tmux is not installed or cannot start a server, and fail instead in CI.
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

work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-screenshots-env-test.XXXXXX") || exit 1
short=""
unset TMUX
trap 'rm -rf "$work" ${short:+"$short"}' EXIT
. "$here/../e2e/tmux-env.sh"

# tmux's sockets live under $short, the deepest at
# $short/teardown/panemux-screenshots/tmux/tmux-<uid>/default, and a socket
# path has a hard limit (104 bytes on macOS, 108 on Linux). Inside the Claude
# Code sandbox $TMPDIR is short and the only place it can write; outside it,
# macOS's per-user $TMPDIR (/private/var/folders/.../T, about 57 bytes once
# tmux resolves it) leaves no room, and /tmp does. Where neither fits, the tmux
# checks fail saying so rather than with tmux's "File name too long".
short_err=$(tmux_short_dir /teardown/panemux-screenshots/tmux "${TMPDIR:-/tmp}" /tmp 2>&1 >"$work/short") &&
	short=$(cat "$work/short")

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
# Exit status 1 is git's "key not set"; stdout alone is the value. stderr is
# kept out of the comparison because macOS's /usr/bin/git shim (xcrun) warns
# there when, under the fake HOME, it cannot write its cache outside the
# Claude Code sandbox — noise that says nothing about the configuration read.
output=$(
	cd "$work/norepo" &&
		XDG_CONFIG_HOME="$real" GIT_CONFIG_GLOBAL="$work/global-gitconfig" GIT_CEILING_DIRECTORIES="$work" \
			sh -c '. "$1"; shot_isolate_env "$2"; git config --get commit.gpgsign' sh "$lib" "$work/fakehome" 2>"$work/git-stderr"
)
status=$?
if [ "$status" -eq 1 ] && [ -z "$output" ]; then
	pass 'git reads none of the developer configuration'
else
	fail 'git reads none of the developer configuration' "exit $status: $output $(cat "$work/git-stderr")"
fi

# ── shot_start_tmux / shot_stop_tmux ────────────────────────────────────────

# tmux_unusable prints why the tmux checks cannot run here, or nothing. Being
# installed is not enough: the Claude Code sandbox lets tmux run but denies the
# Unix socket its server listens on. Those checks are skipped there and left to
# CI, which never skips them — a server that fails to start in CI is a failure.
tmux_unusable() {
	command -v tmux >/dev/null 2>&1 || { echo "tmux is not installed"; return; }
	{ tmux -S "$short/probe" -f /dev/null new-session -d -s probe "sleep 5" &&
		tmux -S "$short/probe" has-session -t "=probe"; } >/dev/null 2>&1 || {
		tmux -S "$short/probe" kill-server >/dev/null 2>&1
		echo "tmux cannot create a socket under \$TMPDIR (the Claude Code sandbox denies Unix sockets; CI runs these checks)"
		return
	}
	tmux -S "$short/probe" kill-server 2>/dev/null
	rm -f "$short/probe"
}
if [ -n "$short" ]; then
	tmux_skip=$(tmux_unusable)
else
	check
	fail 'tmux checks: no directory leaves room for the tmux socket' "$short_err"
	tmux_skip="failure reported above"
fi

# CI never skips them, whatever the reason — tmux missing included, as
# internal/testcap.RequireTmux has it: a runner image that stops shipping tmux
# must fail here rather than turn every tmux check into a skip. Run this script
# again with CI set and every tmux on PATH hidden.
if [ -z "${SCREENSHOTS_ENV_TEST_NESTED:-}" ]; then
	check
	notmux_path=""
	i=0
	old_ifs=$IFS
	IFS=:
	for d in $PATH; do
		i=$((i + 1))
		if [ -x "$d/tmux" ]; then
			mkdir -p "$work/notmux/$i"
			for f in "$d"/*; do
				[ "$(basename "$f")" = tmux ] || ln -s "$f" "$work/notmux/$i/" 2>/dev/null
			done
			d="$work/notmux/$i"
		fi
		notmux_path="${notmux_path:+$notmux_path:}$d"
	done
	IFS=$old_ifs
	if out=$(PATH="$notmux_path" CI=true SCREENSHOTS_ENV_TEST_NESTED=1 sh "$0" 2>&1); then
		fail 'CI without tmux fails the tmux checks' "$out"
	elif ! printf '%s\n' "$out" | grep -q 'tmux checks cannot run in CI: tmux is not installed'; then
		fail 'CI without tmux fails the tmux checks, saying tmux is missing' "$out"
	else
		pass 'CI without tmux fails the tmux checks'
	fi
fi
if [ -n "$tmux_skip" ] && [ -n "${CI:-}" ]; then
	check
	fail "tmux checks cannot run in CI: $tmux_skip"
	tmux_skip="CI failure reported above"
fi

# Each stop check first proves the server it is about to stop is running:
# without that, a server that never started (a socket path too long, say)
# reads as one the helper stopped.
if [ -z "$tmux_skip" ]; then
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
	rundir="$short/panemux-screenshots/tmux"
	mkdir -p "$rundir" && chmod 700 "$rundir"
	if ! output=$(TMUX_TMPDIR="$rundir" tmux -f /dev/null new-session -d -s t 'sleep 30' 2>&1) ||
		! TMUX_TMPDIR="$rundir" tmux has-session 2>/dev/null; then
		fail 'shot_teardown stops the server under the run root: the server did not start' "$output"
	else
		TMPDIR="$short" sh -c '. "$1"; shot_teardown' sh "$lib"
		if TMUX_TMPDIR="$rundir" tmux has-session 2>/dev/null; then
			fail 'shot_teardown stops the server under the run root'
			TMUX_TMPDIR="$rundir" tmux kill-server 2>/dev/null
		else
			pass 'shot_teardown stops the server under the run root'
		fi
	fi
else
	skip "tmux checks: $tmux_skip"
fi

# An inherited TMUX must never route start, stop or teardown to the caller.
# Every probe and cleanup uses -S explicitly, even when testing broken code.
if [ -z "$tmux_skip" ]; then
	caller="$short/caller"
	if ! tmux -S "$caller" -f /dev/null new-session -d -s caller 'sleep 120' ||
		! tmux -S "$caller" has-session -t '=caller'; then
		fail 'inherited TMUX checks: the caller fixture did not start'
		exit 1
	fi
	for operation in start stop teardown; do
		check
		private="$short/$operation"
		mkdir -p "$private/panemux-screenshots/tmux/tmux-$(id -u)"
		chmod 700 "$private/panemux-screenshots/tmux/tmux-$(id -u)"
		socket="$private/panemux-screenshots/tmux/tmux-$(id -u)/default"
		if [ "$operation" != start ]; then
			if ! tmux -S "$socket" -f /dev/null new-session -d -s private 'sleep 120' ||
				! tmux -S "$socket" has-session -t '=private'; then
				fail "$operation: the private fixture did not start"
				continue
			fi
		fi
		HOME="$work/fakehome" TMUX="$caller,1,0" TMUX_TMPDIR="$private/panemux-screenshots/tmux" TMPDIR="$private" \
			sh -c '. "$1"; case $2 in start) shot_start_tmux -s private "sleep 120";; stop) shot_stop_tmux;; teardown) shot_teardown;; esac' \
			sh "$lib" "$operation"
		if ! tmux -S "$caller" has-session -t '=caller' 2>/dev/null; then
			fail "$operation preserves the caller server"
			tmux -S "$caller" -f /dev/null new-session -d -s caller 'sleep 120'
		elif [ "$operation" = start ]; then
			if tmux -S "$socket" has-session -t '=private' 2>/dev/null &&
				! tmux -S "$caller" has-session -t '=private' 2>/dev/null; then
				pass 'start uses the private server with inherited TMUX'
			else
				fail 'start uses the private server with inherited TMUX'
			fi
		elif tmux -S "$socket" has-session 2>/dev/null; then
			fail "$operation stops the private server with inherited TMUX"
		else
			pass "$operation stops only the private server with inherited TMUX"
		fi
		tmux -S "$socket" kill-server 2>/dev/null || true
	done
	check
	if HOME="$work/fakehome" TMUX="$caller,1,0" TMUX_TMPDIR="$short/ambient" \
		sh -eu -c '
			. "$1"
			e2e_tmux_env
			trap e2e_tmux_cleanup EXIT
			[ -z "${TMUX:-}" ] || exit 1
			[ "$TMUX_TMPDIR" != "$2" ] || exit 1
			tmux -f /dev/null new-session -d -s fixture "sleep 120"
			tmux display-message -p "#{socket_path}" >"$3"
			printf '%s\n' "$TMUX_TMPDIR" >"$3-root"
		' sh "$here/../e2e/tmux-env.sh" "$short/ambient" "$short/e2e-socket" &&
		tmux -S "$caller" has-session -t '=caller' 2>/dev/null &&
		! tmux -S "$(cat "$short/e2e-socket")" has-session 2>/dev/null &&
		[ ! -e "$(cat "$short/e2e-socket-root")" ]; then
		pass 'E2E isolates its server and cleans it up without touching the caller'
	else
		fail 'E2E isolates its server and cleans it up without touching the caller'
	fi
	tmux -S "$caller" kill-server 2>/dev/null || true
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
d="$work/emptied"
mkdir -p "$d/home" "$d/tmux/tmux-0"
if claim "$d" >/dev/null && [ -f "$d/.panemux-screenshots" ] && [ ! -e "$d/home" ]; then
	pass 'a directory holding only empty directories is claimed'
else
	fail 'a directory holding only empty directories is claimed'
fi

# The run root, $TMPDIR/panemux-screenshots, is also cleaned by the OS
# (macOS's dirhelper), which can take the never-read marker and leave files
# read since. A root whose every entry is a name the run itself creates is
# claimed without the marker; one other entry and it is refused untouched.
claim_root() { sh -c '. "$1"; shift; shot_claim_dir "$@"' sh "$lib" "$@" 2>&1; }

check
d="$work/root-shaped"
mkdir -p "$d/home" "$d/tmux"
echo old >"$d/home/.bashrc"
echo old >"$d/panemux"
if claim_root "$d" home tmux panemux >/dev/null && [ -f "$d/.panemux-screenshots" ] && [ ! -e "$d/panemux" ]; then
	pass 'an unmarked directory holding only the names the run creates is claimed'
else
	fail 'an unmarked directory holding only the names the run creates is claimed'
fi

check
d="$work/root-plus-one"
mkdir -p "$d/home"
echo keep >"$d/notes.txt"
if output=$(claim_root "$d" home tmux panemux); then
	fail 'an unmarked directory with a name the run does not create is refused' "$output"
elif [ "$(cat "$d/notes.txt" 2>/dev/null)" != keep ] || [ ! -d "$d/home" ]; then
	fail 'an unmarked directory with a name the run does not create is refused: it was touched' "$output"
else
	pass 'an unmarked directory with a name the run does not create is refused'
fi

check
d="$work/root-hidden"
mkdir -p "$d/home"
echo keep >"$d/.hidden"
if output=$(claim_root "$d" home tmux panemux); then
	fail 'a hidden entry the run does not create is refused' "$output"
elif [ "$(cat "$d/.hidden" 2>/dev/null)" != keep ]; then
	fail 'a hidden entry the run does not create is refused: it was touched' "$output"
else
	pass 'a hidden entry the run does not create is refused'
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

# ── tmux_socket_path_check: a socket path too long fails before the run ────

# A Unix socket path is limited to 104 bytes on macOS and 108 on Linux,
# counting the terminating NUL. tmux fails late and obscurely past it, so the
# fixtures check first. Run in a subshell: the helper only prints and returns.
case $(uname -s) in
Darwin) max=103 ;;
*) max=107 ;;
esac
suffix="/tmux-$(id -u)/default"
fits=$(printf '%*s' $((max - ${#suffix})) '' | tr ' ' a)
check
if output=$(sh -c '. "$1"; tmux_socket_path_check "/$2"' sh "$here/../e2e/tmux-env.sh" "${fits#?}" 2>&1); then
	pass 'a socket path at the limit is accepted'
else
	fail 'a socket path at the limit is accepted' "$output"
fi
check
if output=$(sh -c '. "$1"; tmux_socket_path_check "/$2"' sh "$here/../e2e/tmux-env.sh" "$fits" 2>&1); then
	fail 'a socket path one byte over the limit is refused'
else
	case $output in
	*TMPDIR*) pass 'a socket path one byte over the limit is refused' ;;
	*) fail 'a socket path one byte over the limit is refused: the message does not say what to change' "$output" ;;
	esac
fi
check
long="$work/$(printf '%*s' 120 '' | tr ' ' l)"
mkdir -p "$long"
if output=$(TMPDIR="$long" sh -c '. "$1"; e2e_tmux_env' sh "$here/../e2e/tmux-env.sh" 2>&1); then
	fail 'e2e_tmux_env refuses a $TMPDIR too long for the socket'
elif [ -n "$(ls -A "$long")" ]; then
	fail 'e2e_tmux_env refuses a $TMPDIR too long for the socket: it left its directory behind'
else
	pass 'e2e_tmux_env refuses a $TMPDIR too long for the socket'
fi

# tmux resolves its socket directory before it binds (realpath), so a link
# in the path — macOS's /tmp and /var are links into /private — counts at the
# length of its target. A path that fits as given but not as resolved fails.
# Under $short, the one directory known to leave room for a socket.
check
if [ -z "$short" ]; then
	skip 'the socket directory is measured as tmux resolves it: no short directory to test with'
else
	mkdir -p "$short/len"
	deep="$(cd -P "$short/len" && pwd -P)/dddddddddd"
	mkdir -p "$deep"
	ln -s "$deep" "$short/len/s"
	phys=$(sh -c '. "$1"; tmux_physical_path "$2"' sh "$here/../e2e/tmux-env.sh" "$short/len/s/not-yet/tmux")
	if [ "$phys" != "$deep/not-yet/tmux" ]; then
		fail 'the socket directory is measured as tmux resolves it' "got $phys, want $deep/not-yet/tmux"
	else
		# As long as the limit allows when measured as given, and so past it
		# once the link is followed.
		name=$(printf '%*s' $((max - ${#suffix} - ${#short} - 7)) '' | tr ' ' n)
		if output=$(sh -c '. "$1"; tmux_socket_path_check "$2"' sh "$here/../e2e/tmux-env.sh" "$short/len/s/$name" 2>&1); then
			fail 'the socket directory is measured as tmux resolves it: a link that resolves past the limit was accepted' "$output"
		else
			case $output in
			*"$deep/$name"*) pass 'the socket directory is measured as tmux resolves it' ;;
			*) fail 'the socket directory is measured as tmux resolves it: the message does not name the resolved path' "$output" ;;
			esac
		fi
	fi
fi

# tmux_short_dir: the first candidate with room for the socket is used, and
# none having room fails, saying what to change, without creating anything.
too_long="$work/$(printf '%*s' 110 '' | tr ' ' l)"
mkdir -p "$too_long"
short_dir() { sh -c '. "$1"; shift; tmux_short_dir "$@"' sh "$here/../e2e/tmux-env.sh" "$@"; }
check
if [ -z "$short" ]; then
	skip 'tmux_short_dir: no short directory to test with'
elif got=$(short_dir /s "$short" "$too_long" 2>&1) && [ "${got%/p.*}" = "$short" ] && [ -d "$got" ]; then
	pass 'tmux_short_dir uses the first candidate with room'
else
	fail 'tmux_short_dir uses the first candidate with room' "$got"
fi
check
if [ -z "$short" ]; then
	skip 'tmux_short_dir: no short directory to test with'
elif got=$(short_dir /s "$too_long" "$work/missing" "$short" 2>&1) && [ "${got%/p.*}" = "$short" ] && [ -d "$got" ] &&
	[ -z "$(ls -A "$too_long")" ]; then
	pass 'tmux_short_dir passes over a candidate too long or missing'
else
	fail 'tmux_short_dir passes over a candidate too long or missing' "$got"
fi
check
if got=$(short_dir /s "$too_long" "$work/missing" 2>&1); then
	fail 'tmux_short_dir fails when no candidate has room' "$got"
elif [ -n "$(ls -A "$too_long")" ]; then
	fail 'tmux_short_dir fails when no candidate has room: it left a directory behind'
else
	case $got in
	*TMPDIR*) pass 'tmux_short_dir fails when no candidate has room' ;;
	*) fail 'tmux_short_dir fails when no candidate has room: the message does not say what to change' "$got" ;;
	esac
fi

# ── shot_link_dir: the fixed paths the images show are links into the run ──

# /tmp/sample-project and the agmsg store are links to directories inside the
# run's own root. A link this script made (its target is
# <...>/panemux-screenshots/<the link's own name>) is replaced; so is a
# directory an older version of this script left — marked, or emptied of
# every file by something that kept the directories. Anything else is refused
# untouched.
link_dir() { sh -c '. "$1"; shot_link_dir "$2" "$3"' sh "$lib" "$1" "$2" 2>&1; }
mkdir -p "$work/fixed"
target="$work/run/panemux-screenshots/sample-project"
is_link_to() { [ -L "$1" ] && [ "$(readlink "$1")" = "$2" ] && [ -d "$2" ]; }
refused() {
	# refused <label> <path> <output>: the output names the path.
	case $3 in
	*"$2"*) pass "$1" ;;
	*) fail "$1: the message does not name the path" "$3" ;;
	esac
}

check
l="$work/fixed/sample-project"
if output=$(link_dir "$l" "$target") && is_link_to "$l" "$target"; then
	pass 'a missing fixed path becomes a link to the run'
else
	fail 'a missing fixed path becomes a link to the run' "$output"
fi

check
rm -f "$l"
ln -s "$work/old-run/panemux-screenshots/sample-project" "$l"
if output=$(link_dir "$l" "$target") && is_link_to "$l" "$target"; then
	pass 'a dangling link from an earlier run is replaced'
else
	fail 'a dangling link from an earlier run is replaced' "$output"
fi

check
rm -f "$l"
mkdir -p "$work/live-run/panemux-screenshots/sample-project"
echo keep >"$work/live-run/panemux-screenshots/sample-project/file"
ln -s "$work/live-run/panemux-screenshots/sample-project" "$l"
if output=$(link_dir "$l" "$target") && is_link_to "$l" "$target" &&
	[ "$(cat "$work/live-run/panemux-screenshots/sample-project/file")" = keep ]; then
	pass 'replacing an earlier link leaves what it pointed to alone'
else
	fail 'replacing an earlier link leaves what it pointed to alone' "$output"
fi

for case in elsewhere other-name; do
	check
	rm -f "$l"
	case $case in
	elsewhere) dest="$work/mine" ;;
	other-name) dest="$work/mine/panemux-screenshots/other-project" ;;
	esac
	mkdir -p "$dest" && echo keep >"$dest/work.txt"
	ln -s "$dest" "$l"
	if output=$(link_dir "$l" "$target"); then
		fail "a link to $case is refused" "$output"
	elif [ "$(readlink "$l")" != "$dest" ] || [ "$(cat "$dest/work.txt")" != keep ]; then
		fail "a link to $case is refused: it was touched" "$output"
	else
		refused "a link to $case is refused" "$l" "$output"
	fi
done

check
rm -f "$l"
mkdir -p "$l/cmd" "$l/internal/api" "$l/.git/objects"
if output=$(link_dir "$l" "$target") && is_link_to "$l" "$target"; then
	pass 'a directory holding only empty directories is replaced'
else
	fail 'a directory holding only empty directories is replaced' "$output"
fi

check
rm -f "$l"
mkdir -p "$l/cmd" "$l/internal/api"
echo old >"$l/internal/api/items.go"
echo "$l" >"$l/.panemux-screenshots"
if output=$(link_dir "$l" "$target") && is_link_to "$l" "$target"; then
	pass 'a marked directory from an older run is replaced'
else
	fail 'a marked directory from an older run is replaced' "$output"
fi

for case in file link; do
	check
	rm -f "$l"
	mkdir -p "$l/cmd" "$l/internal/api"
	kept="$l/internal/api/work"
	case $case in
	file) echo keep >"$kept" ;;
	link) ln -s "$work/mine" "$kept" ;;
	esac
	if output=$(link_dir "$l" "$target"); then
		fail "a directory with a $case deep inside is refused" "$output"
	elif [ -L "$l" ] || { [ ! -e "$kept" ] && [ ! -L "$kept" ]; }; then
		fail "a directory with a $case deep inside is refused: it was touched" "$output"
	else
		refused "a directory with a $case deep inside is refused" "$l" "$output"
	fi
	mv "$l" "$work/fixed/kept-$case"
done

check
echo keep >"$l"
if output=$(link_dir "$l" "$target"); then
	fail 'a file at a fixed path is refused' "$output"
elif [ "$(cat "$l")" != keep ]; then
	fail 'a file at a fixed path is refused: it was touched' "$output"
else
	refused 'a file at a fixed path is refused' "$l" "$output"
fi

if [ "$failures" -ne 0 ]; then
	echo "screenshots-env tests: $failures of $checks failed"
	exit 1
fi
echo "screenshots-env tests: all $checks passed"
