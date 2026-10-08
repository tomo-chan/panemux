#!/bin/sh
# Tests for scripts/golangci_lint_cache.sh, which picks the directory
# `make lint-go` hands golangci-lint as GOLANGCI_LINT_CACHE.
#
# What it protects: every checkout — the main one and each feature worktree —
# keeps its lint cache in its own ignored .cache/golangci-lint/ (issue #325).
# The directory follows from where the script lives, never from the caller's
# working directory or an inherited GOLANGCI_LINT_CACHE, so a cache exported in
# one worktree is not picked up in another, and lint never needs the user cache
# directory, which the Claude Code sandbox cannot write (issue #315).
#
# Run with: make test-golangci-lint-cache

set -u

here=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd -P)
script="$here/golangci_lint_cache.sh"
repo=$(dirname -- "$here")

work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-golangci-cache-test.XXXXXX") || exit 1
trap 'chmod -R u+w "$work" 2>/dev/null; rm -rf "$work"' EXIT
work=$(CDPATH='' cd -- "$work" && pwd -P) || exit 1
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

# Two throwaway checkouts, each with its own copy of the script, stand in for
# the main checkout and a feature worktree.
for c in a b; do
	mkdir -p "$work/$c/scripts" "$work/$c/frontend/src"
	cp "$script" "$work/$c/scripts/golangci_lint_cache.sh"
done
mkdir -p "$work/elsewhere" "$work/home"
a_cache="$work/a/.cache/golangci-lint"
b_cache="$work/b/.cache/golangci-lint"

run() { # dir checkout [env assignments...]
	dir=$1 checkout=$2
	shift 2
	(cd "$dir" && env -u GOLANGCI_LINT_CACHE -u XDG_CACHE_HOME HOME="$work/home" "$@" \
		sh "$work/$checkout/scripts/golangci_lint_cache.sh")
}

# The working directory never changes the answer.
expect 'from the checkout root' "$a_cache" "$(run "$work/a" a)"
expect 'from a subdirectory (frontend)' "$a_cache" "$(run "$work/a/frontend/src" a)"
expect 'from outside any checkout' "$a_cache" "$(run "$work/elsewhere" a)"
expect 'from another checkout' "$a_cache" "$(run "$work/b" a)"
expect 'through a relative script path' "$a_cache" \
	"$(cd "$work/a/frontend" && env -u GOLANGCI_LINT_CACHE HOME="$work/home" sh ../scripts/golangci_lint_cache.sh)"

# Two checkouts never share a cache.
expect 'a second checkout gets its own cache' "$b_cache" "$(run "$work/b" b)"

# An inherited GOLANGCI_LINT_CACHE is ignored, wherever it points.
expect 'an inherited cache of another checkout is ignored' "$a_cache" \
	"$(run "$work/a" a GOLANGCI_LINT_CACHE="$b_cache")"
expect 'an inherited cache in the user cache directory is ignored' "$a_cache" \
	"$(run "$work/a" a GOLANGCI_LINT_CACHE="$work/home/.cache/golangci-lint")"
expect 'an inherited relative cache is ignored' "$a_cache" \
	"$(run "$work/a/frontend" a GOLANGCI_LINT_CACHE=.cache/golangci-lint)"
expect 'XDG_CACHE_HOME does not move it' "$a_cache" \
	"$(run "$work/a" a XDG_CACHE_HOME="$work/xdg")"

# The directory is created, and nothing is written outside the checkout.
checks=$((checks + 1))
if [ -d "$a_cache" ] && [ -d "$b_cache" ] && [ -z "$(ls -A "$work/home")" ] && [ ! -e "$work/xdg" ] &&
	[ -z "$(ls -A "$work/elsewhere")" ] && [ ! -e "$work/a/frontend/.cache" ]; then
	echo "ok   the cache is created inside the checkout and nowhere else"
else
	failures=$((failures + 1))
	echo "FAIL: the cache is created inside the checkout and nowhere else"
	(cd "$work" && find . -name '*cache*' -o -path './home/*')
fi

# A checkout whose cache cannot be created fails, rather than falling back to
# another checkout's or the user's cache.
mkdir -p "$work/c/scripts"
cp "$script" "$work/c/scripts/golangci_lint_cache.sh"
chmod a-w "$work/c"
checks=$((checks + 1))
if out=$(run "$work/c" c 2>/dev/null); then
	failures=$((failures + 1))
	echo "FAIL: an unwritable checkout fails"
	echo "      got: exit 0, '$out'"
else
	echo "ok   an unwritable checkout fails"
fi
chmod u+w "$work/c"

# The cache this repository's own copy picks is inside it, and Git ignores it.
expect "this checkout's cache is its own .cache/golangci-lint" "$repo/.cache/golangci-lint" \
	"$(cd "$work/elsewhere" && env -u GOLANGCI_LINT_CACHE sh "$script")"
checks=$((checks + 1))
if git -C "$repo" check-ignore -q .cache/golangci-lint/probe; then
	echo "ok   .cache/golangci-lint/ is ignored by Git"
else
	failures=$((failures + 1))
	echo "FAIL: .cache/golangci-lint/ is ignored by Git"
fi

if [ "$failures" -ne 0 ]; then
	echo "golangci-lint-cache tests: $failures of $checks failed"
	exit 1
fi
echo "golangci-lint-cache tests: all $checks passed"
