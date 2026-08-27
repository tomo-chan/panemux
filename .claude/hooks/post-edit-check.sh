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
	else
		# Exiting 0 is right (principle 4 — never block on something that
		# cannot be checked), but doing it silently is not: without jq this
		# gate stops running entirely and nothing ever says so, which is the
		# "reports the discipline as enforced while enforcing nothing" failure
		# this file's own header calls worse than no hook at all.
		printf 'G1: jq not found; the edit gate is not running\n' >&2
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

	# stdout is the list of unformatted files; stderr is a parse error. Merging
	# them reported a file that does not PARSE as a formatting problem, and
	# advised `make fmt`, which runs gofmt and fails identically — swallowing
	# the one diagnostic that would have helped.
	gofmt_err=$(mktemp)
	unformatted=$(gofmt -s -l "$file" 2> "$gofmt_err")
	parse_error=$(cat "$gofmt_err")
	rm -f "$gofmt_err"

	if [ -n "$parse_error" ]; then
		block "G1: $file does not parse:
$parse_error"
	fi
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

		if ! vet_output=$(cd "$repo_root" && go vet "./$rel" 2>&1); then
			# A package that does not COMPILE is the expected state halfway
			# through the repository's own TDD rule ("write tests first,
			# confirm they fail"): the test names a function that does not
			# exist yet. Blocking there would reject every red test at the
			# moment it is written and pressure the agent into writing the
			# implementation first — inverting the rule this gate exists to
			# support.
			#
			# go vet reports a load/type failure on a line starting with
			# "vet: ", while a genuine diagnostic is a bare "file:line:col:
			# message". That is the discriminator, verified both ways:
			#
			#   vet: sub/b_test.go:6:5: undefined: B          <- mid-TDD, allow
			#   sub/bad.go:5:26: fmt.Printf format %d has ... <- real, block
			if printf '%s' "$vet_output" | grep -q '^vet: '; then
				printf 'G1: ./%s does not compile yet, so go vet was skipped:\n%s\n' \
					"$rel" "$vet_output" >&2
				exit 0
			fi
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

	if ! tsc_output=$(cd "$repo_root/frontend" && npx --no-install tsc --noEmit 2>&1); then
		block "G1: tsc --noEmit failed:
$tsc_output"
	fi
	;;
esac

exit 0
