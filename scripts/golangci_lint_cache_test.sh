#!/bin/sh
# Tests for scripts/golangci_lint_cache.sh, which picks the directory
# `make lint-go` hands golangci-lint as GOLANGCI_LINT_CACHE.
#
# What it protects: inside the Claude Code sandbox the user cache directory is
# read-only, and golangci-lint then warns on every package and reports results
# cached for other checkouts (issue #315). Where it is writable — everywhere
# else — the cache stays exactly where golangci-lint would put it.
#
# Run with: make test-golangci-lint-cache

set -u

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
script="$here/golangci_lint_cache.sh"

work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-golangci-cache-test.XXXXXX") || exit 1
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT
cd "$work" || exit 1

failures=0
checks=0
expect() { # name want got
	checks=$((checks + 1))
	if [ "$2" = "$3" ]; then
		echo "ok   $1"
	else
		failures=$((failures + 1))
		echo "FAIL: $1"
		echo "      want: $2"
		echo "      got:  $3"
	fi
}

case $(uname -s) in
Darwin) user_cache() { echo "$1/Library/Caches"; } ;;
*) user_cache() { echo "$1/.cache"; } ;;
esac

# An explicit GOLANGCI_LINT_CACHE always wins.
got=$(GOLANGCI_LINT_CACHE="$work/explicit" HOME="$work/h1" TMPDIR="$work/t" sh "$script")
expect 'an explicit GOLANGCI_LINT_CACHE is kept' "$work/explicit" "$got"

# A writable user cache directory is used as is.
mkdir -p "$work/h2"
got=$(env -u GOLANGCI_LINT_CACHE -u XDG_CACHE_HOME HOME="$work/h2" TMPDIR="$work/t" sh "$script")
expect 'a writable user cache directory is kept' "$(user_cache "$work/h2")/golangci-lint" "$got"

# An unwritable one falls back to $TMPDIR.
mkdir -p "$(user_cache "$work/h3")"
chmod a-w "$(user_cache "$work/h3")"
got=$(env -u GOLANGCI_LINT_CACHE -u XDG_CACHE_HOME HOME="$work/h3" TMPDIR="$work/t" sh "$script")
expect 'an unwritable user cache directory falls back to $TMPDIR' "$work/t/golangci-lint-cache" "$got"

if [ "$failures" -ne 0 ]; then
	echo "golangci-lint-cache tests: $failures of $checks failed"
	exit 1
fi
echo "golangci-lint-cache tests: all $checks passed"
