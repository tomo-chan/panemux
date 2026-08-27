#!/bin/sh
#
# Gates G1 (edit) and G2 (unit) from docs/quality-gateway.md, as a Claude Code
# Stop hook: before a turn is allowed to end, verify what this turn changed.
#
# **This is deliberately not `make check`** — decision D6. Putting the whole
# gate here would run the frontend build, the -race suite and eventually E2E on
# every single turn, and a gate that sacrifices fast feedback is a gate that
# gets bypassed. G3 onward stays with .githooks/pre-push and CI, which is where
# the expensive checks belong and where they cannot be skipped anyway.
#
# Exit codes are the Claude Code hook contract: 0 = the turn may end, 2 =
# blocking, reason on stderr. Anything that cannot be checked exits 0 (design
# principle 4).
#
# A note on the failure that matters most here, because this script had it and
# a review caught it: a gate that reports success while checking NOTHING is
# worse than no gate. Three separate paths did that — a session whose cwd was
# a subdirectory, a wholly-new package directory, and a deleted .go file — and
# each is now covered by hooks_test.sh. When changing this file, prefer
# erroring loudly over skipping quietly.

set -u

# Claude Code sets stop_hook_active when it is ALREADY continuing because of a
# previous Stop-hook block. Without this, a condition the turn cannot fix — a
# pre-existing dirty file, a test the user deliberately left red, or the
# test-first state DEVELOPMENT.md mandates — blocks, resumes, and blocks again
# forever. Bounding that is exactly what the field is for.
#
# Reading stdin is bounded, not merely guarded by [ -t 0 ]. Claude Code writes
# the payload and closes the pipe, so a plain `cat` returns — but anything that
# invokes this script with an inherited, idle pipe (make, a shell wrapper) would
# make `cat` block forever, and a Stop hook that HANGS is worse than one that
# answers wrongly: the turn cannot end at all. That is not hypothetical; it
# hung this repository's own test suite before the timeout was added.
read_hook_payload() {
	[ -t 0 ] && return 0
	if command -v timeout > /dev/null 2>&1; then
		timeout 5 cat 2> /dev/null || true
	else
		cat 2> /dev/null || true
	fi
}

if command -v jq > /dev/null 2>&1; then
	payload=$(read_hook_payload)
	if [ -n "$payload" ] &&
		[ "$(printf '%s' "$payload" | jq -r '.stop_hook_active // false' 2> /dev/null)" = "true" ]; then
		exit 0
	fi
fi

# git status --porcelain always prints paths relative to the REPOSITORY ROOT,
# whatever the cwd is. Without this the [ -f ] test below missed every file
# when the session's cwd was a subdirectory, and the gate passed everything
# silently. Deriving the root from git rather than from $0 keeps hooks_test.sh's
# throwaway-repo fixtures working, since those run with cwd inside the fixture.
repo_root=$(git rev-parse --show-toplevel 2> /dev/null) || exit 0
cd "$repo_root" || exit 0

# --untracked-files=all: without it git collapses a wholly-new directory into a
# single "?? newpkg/" entry, so a turn that adds an entire package is never
# checked — the single largest kind of change, invisible to the gate.
#
# -z plus cut -c4-: the porcelain status prefix is a fixed three characters, so
# taking everything after it yields the path verbatim. Field-splitting instead
# (awk '{print $NF}') breaks on paths containing spaces, which git additionally
# double-quotes — another silent skip.
changed=$(git status --porcelain=v1 -z --untracked-files=all 2> /dev/null | tr '\0' '\n' | cut -c4-)
[ -n "$changed" ] || exit 0

go_files=""
go_pkgs=""
frontend_files=""

add_pkg() {
	case "|$go_pkgs|" in
	*"|$1|"*) ;;
	*) go_pkgs="$go_pkgs|$1" ;;
	esac
}

# Iterating over lines, not words, for the same reason -z is used above.
printf '%s\n' "$changed" | {
	while IFS= read -r f; do
		[ -n "$f" ] || continue
		case "$f" in
		*.go)
			# The package is added whether or not the file still exists:
			# deleting a .go file is one of the likeliest ways to break a
			# package's build, and skipping it here meant a delete-only turn
			# ran no tests at all. Only gofmt needs the file to be present.
			add_pkg "./$(dirname "$f")"
			[ -f "$f" ] && go_files="$go_files $f"
			;;
		frontend/src/*.ts | frontend/src/*.tsx)
			[ -f "$f" ] && frontend_files="$frontend_files $f"
			;;
		esac
	done

	problems=""
	note() { problems="$problems
$1"; }

	if [ -n "$go_files" ] && command -v gofmt > /dev/null 2>&1; then
		# stderr is kept separate: gofmt writes syntax errors there and the
		# list of unformatted files to stdout. Merging them reported a file
		# that does not parse as a formatting problem, and told the agent to
		# run `make fmt`, which runs gofmt and fails the same way.
		# shellcheck disable=SC2086 # word splitting is the intent: one arg per file
		unformatted=$(gofmt -s -l $go_files 2> "$repo_root/.git/stop-check-gofmt-err")
		gofmt_err=$(cat "$repo_root/.git/stop-check-gofmt-err" 2> /dev/null)
		rm -f "$repo_root/.git/stop-check-gofmt-err"

		if [ -n "$gofmt_err" ]; then
			note "G1: these files do not parse:
$gofmt_err"
		elif [ -n "$unformatted" ]; then
			note "G1: these files are not gofmt -s clean (run: make fmt):
$unformatted"
		fi
	fi

	if [ -n "$go_pkgs" ] && command -v go > /dev/null 2>&1; then
		pkgs=$(printf '%s' "$go_pkgs" | tr '|' ' ')
		# Only the packages this turn touched. `go test` without -race here on
		# purpose: pre-push and CI run the race detector over everything, and
		# the point of this gate is a fast answer, not a second full suite.
		# shellcheck disable=SC2086 # word splitting is the intent: one arg per package
		if ! test_output=$(go test $pkgs 2>&1); then
			note "G2: go test$pkgs failed:
$test_output"
		fi
	fi

	if [ -n "$frontend_files" ] && [ -d frontend/node_modules ]; then
		if ! tsc_output=$(cd frontend && npx --no-install tsc --noEmit 2>&1); then
			note "G1: tsc --noEmit failed:
$tsc_output"
		fi

		rel_files=""
		for f in $frontend_files; do rel_files="$rel_files ${f#frontend/}"; done
		# --passWithNoTests: `vitest related` exits non-zero when no test
		# imports the changed module, and modules with no importing test exist
		# in this tree (main.tsx, several components) — a new file has none by
		# construction. Without the flag, editing one refuses to end an
		# entirely healthy turn.
		# shellcheck disable=SC2086 # word splitting is the intent: one arg per file
		if ! vitest_output=$(cd frontend && npx --no-install vitest related --run --passWithNoTests $rel_files 2>&1); then
			note "G2: vitest related failed:
$vitest_output"
		fi
	fi

	if [ -n "$problems" ]; then
		printf 'The turn cannot end with these unresolved (docs/quality-gateway.md gates G1/G2):%s\n' \
			"$problems" >&2
		exit 2
	fi
	exit 0
}
