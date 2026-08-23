#!/bin/sh
#
# Gate G1 (edit) from docs/quality-gateway.md, as a Claude Code PostToolUse
# hook: after a file is written, check that one file.
#
# Scope is the point. `make lint` covers the repository and takes tens of
# seconds; this runs on the file that was just touched and its package, so it
# answers in the time it takes to read the next line of the diff. Gate G2
# (the tests) belongs to stop-check.sh, and everything from G3 onward stays
# with .githooks/pre-push and CI — decision D6.
#
# Exit codes are the Claude Code hook contract: 0 = fine, 2 = blocking error,
# with the reason on stderr. Anything this script cannot check (an unknown file
# type, a deleted path, a missing toolchain) exits 0. Design principle 4: a
# gate that produces false positives loses its credibility and gets bypassed,
# and it takes the rest of the gates with it.
#
# Usage:
#   post-edit-check.sh              # reads Claude Code's hook JSON on stdin
#   post-edit-check.sh --file PATH  # same checks against PATH (used by the tests)

set -u

hooks_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
repo_root=$(CDPATH='' cd -- "$hooks_dir/../.." && pwd)

file=""
if [ "${1:-}" = "--file" ]; then
	file=${2:-}
else
	# Claude Code reports the written path under tool_input.file_path for Edit
	# and under tool_response.filePath for some other tools. Read both, and
	# treat unparseable input as "nothing to check" rather than as a failure —
	# this hook must never be the reason a turn stops.
	if command -v jq > /dev/null 2>&1; then
		file=$(jq -r '.tool_input.file_path // .tool_response.filePath // empty' 2> /dev/null)
	fi
fi

[ -n "$file" ] || exit 0
[ -f "$file" ] || exit 0

block() {
	printf '%s\n' "$1" >&2
	exit 2
}

case "$file" in
*.go)
	command -v gofmt > /dev/null 2>&1 || exit 0

	unformatted=$(gofmt -s -l "$file" 2>&1)
	if [ -n "$unformatted" ]; then
		block "G1: $file is not gofmt -s clean. Run: make fmt"
	fi

	# go vet needs a package, not a file, and needs to run inside the module.
	# A file outside this repository (the tests use temp files) has no package
	# here, so the vet step is skipped rather than guessed at.
	case "$file" in
	"$repo_root"/*)
		command -v go > /dev/null 2>&1 || exit 0
		pkg_dir=$(dirname "$file")
		rel=${pkg_dir#"$repo_root"}
		rel=${rel#/}
		[ -n "$rel" ] || rel="."

		vet_output=$(cd "$repo_root" && go vet "./$rel" 2>&1)
		if [ $? -ne 0 ]; then
			block "G1: go vet ./$rel failed:
$vet_output"
		fi
		;;
	esac
	;;
*.ts | *.tsx)
	# tsc is configured per project, not per file: frontend/tsconfig.json
	# decides what "type-checks" even means here, so the whole project is
	# checked. It is still seconds, and it is the same command make
	# lint-frontend runs.
	case "$file" in
	"$repo_root"/frontend/*) ;;
	*) exit 0 ;;
	esac
	[ -d "$repo_root/frontend/node_modules" ] || exit 0

	tsc_output=$(cd "$repo_root/frontend" && npx --no-install tsc --noEmit 2>&1)
	if [ $? -ne 0 ]; then
		block "G1: tsc --noEmit failed:
$tsc_output"
	fi
	;;
esac

exit 0
