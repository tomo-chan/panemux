#!/bin/sh
#
# Gate G1 (edit) from docs/quality-gateway.md, as a Claude Code PostToolUse
# hook: after a file is written, check that one file.
#
# Scope is the point. `make lint` covers the repository and takes tens of
# seconds; this runs on the file that was just touched and its package, so it
# answers in the time it takes to read the next line of the diff. Gate G2
# (the tests) belongs to stop-check.sh, and everything from G3 onward stays
# with CI and `make check` — decisions D6 and D14.
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
#   post-edit-check.sh --file PATH --repo DIR
#                                   # ...treating DIR's repository as this hook's own

set -u

# Pin direct execution and every Go subprocess to this checkout's go.mod.
panemux_toolchain_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd)
panemux_runtime_scripts="$panemux_toolchain_root/scripts"
. "$panemux_runtime_scripts/runtime-env.sh"
panemux_runtime "$panemux_toolchain_root" || exit 2

file=""
own_repo=$panemux_toolchain_root
if [ "${1:-}" = "--file" ]; then
	file=${2:-}
	[ "${3:-}" != "--repo" ] || own_repo=${4:-}
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

# The checkout to check in is the one that holds the edited file, not the one
# that holds this hook. Claude Code runs "$CLAUDE_PROJECT_DIR"/.claude/hooks/,
# which is the MAIN checkout even for a session in a worktree nested inside it
# (<repo>/.claude/worktrees/<name>), and vetting that worktree's package from
# the main module fails on every edit (issue #350).
#
# --show-prefix, not stripping --show-toplevel off the path: git prints the
# top level with symbolic links resolved (macOS's /tmp and $TMPDIR are links
# into /private), so the edited path need not start with it. The prefix is the
# directory relative to the top level, with a trailing slash, or empty at the
# top level itself. A file outside any git checkout leaves both empty, and the
# vet and tsc steps below are skipped rather than guessed at.
#
# Only checkouts of this hook's own repository are checked: the main checkout
# and any worktree of it, which share one git common directory. A file in any
# other repository (an agent's throwaway reproduction under $TMPDIR) gets
# gofmt and nothing else. Vetting it blocked on errors that say nothing about
# the change, such as a missing go.mod, and the tsc step ran that repository's
# own node_modules/.bin/tsc. The common directories are compared after
# resolving symbolic links, for the same reason as --show-prefix.
common_dir() {
	d=$(git -C "$1" rev-parse --path-format=absolute --git-common-dir 2> /dev/null) || return 1
	[ -n "$d" ] && CDPATH='' cd -- "$d" 2> /dev/null && pwd -P
}
file_dir=$(dirname "$file")
repo_root=$(git -C "$file_dir" rev-parse --show-toplevel 2> /dev/null) || repo_root=""
if [ -n "$repo_root" ]; then
	file_common=$(common_dir "$file_dir") || file_common=""
	own_common=$(common_dir "$own_repo") || own_common=""
	{ [ -n "$own_common" ] && [ "$file_common" = "$own_common" ]; } || repo_root=""
fi
rel=""
[ -z "$repo_root" ] || rel=$(git -C "$file_dir" rev-parse --show-prefix 2> /dev/null) || repo_root=""
rel=${rel%/}

case "$file" in
*.go)
	command -v gofmt > /dev/null 2>&1 || exit 0

	# stdout is the list of unformatted files; stderr is a parse error. Merging
	# them reported a file that does not PARSE as a formatting problem, and
	# advised `make fmt`, which runs gofmt and fails identically — swallowing
	# the one diagnostic that would have helped.
	gofmt_err=$(mktemp "${TMPDIR:-/tmp}/panemux-gofmt-err.XXXXXX") || block "G1: could not create a temporary file for gofmt's stderr"
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
	# A file outside any git checkout (the tests use temp files) has no
	# package to name, so the vet step is skipped rather than guessed at.
	if [ -n "$repo_root" ]; then
		command -v go > /dev/null 2>&1 || exit 0
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
			# The go command itself could not load the module: a worktree
			# whose go.mod asks for a newer Go than this hook's pinned
			# toolchain, say. That is not a finding about the edit
			# (principle 4), so it is reported and not blocked on.
			#
			# The go command also prints progress with the same prefix
			# ("go: downloading ...") and then runs vet, so those lines are
			# dropped first, and the output counts as a load error only when
			# every remaining line is "go: ..." or its indented continuation.
			# A "# pkg" header or a file:line diagnostic anywhere means vet
			# ran, and it blocks.
			load_output=$(printf '%s\n' "$vet_output" |
				grep -vE '^go: (downloading|finding|found|extracting) ')
			if printf '%s\n' "$load_output" | grep -q '^go: ' &&
				! printf '%s\n' "$load_output" | grep -qvE '^(go: |[[:space:]]|$)'; then
				printf 'G1: go could not load ./%s, so go vet was skipped:\n%s\n' \
					"$rel" "$vet_output" >&2
				exit 0
			fi
			block "G1: go vet ./$rel failed:
$vet_output"
		fi
	fi
	;;
*.ts | *.tsx)
	# tsc is configured per project, not per file: frontend/tsconfig.json
	# decides what "type-checks" even means here, so the whole project is
	# checked. It is still seconds, and it is the same command make
	# lint-frontend runs.
	[ -n "$repo_root" ] || exit 0
	case "$rel/" in
	frontend/*) ;;
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
