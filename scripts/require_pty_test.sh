#!/bin/sh
# Tests for scripts/require_pty.sh, which `make test-e2e` and
# `make screenshots` run before starting panemux, whose local panes need a
# pseudo-terminal the Claude Code sandbox denies (issue #315).
#
# Run with: make test-require-pty

set -u

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
script="$here/require_pty.sh"

work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-require-pty-test.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT

failures=0
checks=0
expect() { # name want-status got-status output want-substring
	checks=$((checks + 1))
	case $4 in
	*"$5"*) matched=yes ;;
	*) matched=no ;;
	esac
	if [ "$2" = "$3" ] && [ "$matched" = yes ]; then
		echo "ok   $1"
	else
		failures=$((failures + 1))
		echo "FAIL: $1 (want exit $2 and \"$5\", got exit $3)"
		printf '%s\n' "$4" | sed 's/^/      /'
	fi
}

# A `script` stand-in: the probe opens its pty through script(1).
for verdict in ok denied; do
	mkdir -p "$work/$verdict"
	if [ "$verdict" = ok ]; then
		printf '#!/bin/sh\nexit 0\n' > "$work/$verdict/script"
	else
		printf '#!/bin/sh\necho "script: openpty: Operation not permitted" >&2\nexit 1\n' > "$work/$verdict/script"
	fi
	chmod +x "$work/$verdict/script"
done

run() { # path-dir CI mode
	out=$(PATH="$work/$1:$PATH" CI="$2" sh "$script" "$3" 'make test-e2e' 2>&1)
	status=$?
}

run ok '' skip
expect 'a pty: carry on' 0 "$status" "$out" ''
run ok true fail
expect 'a pty in CI: carry on' 0 "$status" "$out" ''
run denied '' skip
expect 'no pty, skip mode: exit 3 so make can skip' 3 "$status" "$out" 'skipped'
run denied true skip
expect 'no pty in CI: fail even in skip mode' 1 "$status" "$out" 'CI'
run denied '' fail
expect 'no pty, fail mode: fail and say where to run it' 1 "$status" "$out" 'outside the Claude Code sandbox'

if [ "$failures" -ne 0 ]; then
	echo "require-pty tests: $failures of $checks failed"
	exit 1
fi
echo "require-pty tests: all $checks passed"
