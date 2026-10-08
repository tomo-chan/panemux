#!/bin/sh
# Usage: require_pty.sh skip|fail <target>
#
# Checks that a pseudo-terminal can be opened before <target> starts panemux,
# whose local panes each need one. The Claude Code sandbox on macOS denies
# ptys (issue #315), and without this check every pane would fail to start
# and the run would report that as a pile of unrelated test failures.
#
# Exit 0: a pty is available, carry on.
# Exit 3: none, mode is skip, and CI is unset: the caller reports the target
#         as skipped (make test-e2e — CI runs it).
# Exit 1: none, and either mode is fail (make screenshots, which has to be run
#         outside the sandbox) or CI is set (CI never skips).

set -u

mode=$1
target=$2

case $(uname -s) in
Darwin) probe() { script -q /dev/null true; } ;;
*) probe() { script -qec true /dev/null; } ;;
esac

if err=$(probe < /dev/null 2>&1 > /dev/null); then
	exit 0
fi

if [ -n "${CI:-}" ]; then
	echo "$target: cannot open a pseudo-terminal in CI, where it must run: $err" >&2
	exit 1
fi
if [ "$mode" = skip ]; then
	echo "$target: skipped — cannot open a pseudo-terminal here (the Claude Code sandbox denies it; CI runs it): $err" >&2
	exit 3
fi
echo "$target: cannot open a pseudo-terminal: $err" >&2
echo "Run it outside the Claude Code sandbox." >&2
exit 1
