#!/bin/sh
# Creates and prints the directory `make lint-go` passes to golangci-lint as
# GOLANGCI_LINT_CACHE: .cache/golangci-lint/ in the checkout this script
# belongs to (issue #325).
#
# Each checkout — the main one and every feature worktree — keeps its own lint
# cache, so one never reports issues cached for another, and lint never writes
# to the user cache directory, which the Claude Code sandbox cannot write
# (issue #315). The directory follows from this script's own location, not
# from the caller's working directory, and an inherited GOLANGCI_LINT_CACHE is
# ignored: it may have been exported for a different checkout. A cache that
# cannot be created is an error, never a reason to fall back to a shared one.

set -u

root=$(CDPATH='' cd -- "$(dirname -- "$0")/.." && pwd -P) || exit 1
cache="$root/.cache/golangci-lint"

if ! mkdir -p "$cache"; then
	echo "golangci_lint_cache.sh: cannot create $cache" >&2
	exit 1
fi
printf '%s\n' "$cache"
