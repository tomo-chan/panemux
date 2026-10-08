#!/bin/sh
# Prints the directory `make lint-go` passes to golangci-lint as
# GOLANGCI_LINT_CACHE.
#
# golangci-lint caches under the user cache directory (os.UserCacheDir). The
# Claude Code sandbox cannot write there, and golangci-lint then warns on every
# package and reports issues it cached for other checkouts (issue #315). So:
# an explicit GOLANGCI_LINT_CACHE wins; otherwise golangci-lint's own default
# where it is writable; otherwise a directory under $TMPDIR.

set -u

if [ -n "${GOLANGCI_LINT_CACHE:-}" ]; then
	printf '%s\n' "$GOLANGCI_LINT_CACHE"
	exit 0
fi

case $(uname -s) in
Darwin) base="$HOME/Library/Caches" ;;
*) base="${XDG_CACHE_HOME:-$HOME/.cache}" ;;
esac
default="$base/golangci-lint"

if mkdir -p "$default" 2>/dev/null && probe=$(mktemp "$default/.write-probe.XXXXXX" 2>/dev/null); then
	rm -f "$probe"
	printf '%s\n' "$default"
else
	printf '%s\n' "${TMPDIR:-/tmp}/golangci-lint-cache"
fi
