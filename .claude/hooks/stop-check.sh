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
# Scope is what the working tree says changed, not the whole repository, so an
# untouched package is never re-run.
#
# Exit codes are the Claude Code hook contract: 0 = the turn may end, 2 =
# blocking, reason on stderr. Anything that cannot be checked exits 0 (design
# principle 4).

set -u

changed=$(git status --porcelain 2> /dev/null | awk '{ print $NF }')
[ -n "$changed" ] || exit 0

go_files=""
frontend_files=""
for f in $changed; do
	[ -f "$f" ] || continue
	case "$f" in
	*.go) go_files="$go_files $f" ;;
	frontend/src/*.ts | frontend/src/*.tsx | frontend/src/**/*.ts | frontend/src/**/*.tsx)
		frontend_files="$frontend_files $f"
		;;
	esac
done

problems=""

note() {
	problems="$problems
$1"
}

if [ -n "$go_files" ] && command -v gofmt > /dev/null 2>&1; then
	# shellcheck disable=SC2086 # word splitting is the intent: one arg per file
	unformatted=$(gofmt -s -l $go_files 2>&1)
	if [ -n "$unformatted" ]; then
		note "G1: these files are not gofmt -s clean (run: make fmt):
$unformatted"
	fi
fi

if [ -n "$go_files" ] && command -v go > /dev/null 2>&1; then
	# Only the packages this turn touched. `go test` without -race here on
	# purpose: pre-push and CI run the race detector over everything, and the
	# point of this gate is a fast answer, not a second full suite.
	pkgs=""
	for f in $go_files; do
		dir=$(dirname "$f")
		case " $pkgs " in
		*" ./$dir "*) ;;
		*) pkgs="$pkgs ./$dir" ;;
		esac
	done

	# shellcheck disable=SC2086 # word splitting is the intent: one arg per package
	test_output=$(go test $pkgs 2>&1)
	if [ $? -ne 0 ]; then
		note "G2: go test$pkgs failed:
$test_output"
	fi
fi

if [ -n "$frontend_files" ] && [ -d frontend/node_modules ]; then
	tsc_output=$(cd frontend && npx --no-install tsc --noEmit 2>&1)
	if [ $? -ne 0 ]; then
		note "G1: tsc --noEmit failed:
$tsc_output"
	fi

	# vitest related resolves the test files that import each changed module,
	# so an edited hook runs its own tests without running all 704 of them.
	rel_files=""
	for f in $frontend_files; do
		rel_files="$rel_files ${f#frontend/}"
	done
	# shellcheck disable=SC2086 # word splitting is the intent: one arg per file
	vitest_output=$(cd frontend && npx --no-install vitest related --run $rel_files 2>&1)
	if [ $? -ne 0 ]; then
		note "G2: vitest related failed:
$vitest_output"
	fi
fi

if [ -n "$problems" ]; then
	printf 'The turn cannot end with these unresolved (docs/quality-gateway.md gates G1/G2):%s\n' "$problems" >&2
	exit 2
fi

exit 0
