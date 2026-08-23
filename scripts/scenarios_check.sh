#!/bin/sh
#
# Gate G5's ledger cross-check, and half of G0: every `auto` row in
# docs/scenarios.md must name something that actually exists.
#
# scenarios.md states that "a row whose verification is `manual` is a
# legitimate answer; a row that is silently absent is not". That rule has no
# teeth against a third failure it never anticipated: a row that names an
# automated test which has since been renamed, moved, or deleted. Such a row
# reads as coverage and is worth nothing, and it is the most likely kind of rot
# in a living ledger — nobody grepping for a test name expects to find it in a
# markdown table.
#
# So every `auto` and `auto (opt-in)` row is read, and each backticked token in
# its Verification column that names a path or a Go test function is resolved
# against the repository.
#
# Usage:
#   make check-scenarios
#   scripts/scenarios_check.sh path/to/other.md   # used by the tests

set -u

repo_root=$(git rev-parse --show-toplevel 2> /dev/null || pwd)
ledger=${1:-$repo_root/docs/scenarios.md}

if [ ! -f "$ledger" ]; then
	echo "scenarios: $ledger not found"
	exit 1
fi

missing=0
resolved=0

report_missing() {
	missing=$((missing + 1))
	printf '  %s\n' "$1"
}

# Table rows claiming automation. A row is a table row (starts with "|") whose
# Verification column says `auto` — which covers `auto (opt-in)` too.
rows=$(grep '^|' "$ledger" | grep '`auto')

if [ -z "$rows" ]; then
	echo "scenarios: no \`auto\` rows found in $ledger — is the table format still what this expects?"
	exit 1
fi

echo "scenarios: checking what $ledger's auto rows name"

# Each row is checked on its own so a failure can name the scenario it came
# from; a bare "TestFoo not found" would send the reader grepping.
echo "$rows" | while IFS= read -r row; do
	id=$(printf '%s' "$row" | awk -F'|' '{ gsub(/^[ \t]+|[ \t]+$/, "", $2); print $2 }')

	tokens=$(printf '%s' "$row" | grep -o '`[^` ]*`' | tr -d '`')
	[ -n "$tokens" ] || continue

	for token in $tokens; do
		case "$token" in
		# Prose, commands, keystrokes (Cmd/Ctrl+Shift+B), HTTP routes
		# (/ws/board-command) and absolute or home-relative paths are not
		# references into this repository. Excluding them by shape keeps the
		# check free of the false positives that would make it noise.
		auto | manual | make | -* | /* | '~'/* | *+*) continue ;;
		esac

		case "$token" in
		# A path into this repository: either under a source root, or ending in
		# a source extension. `bin/panemux` is neither — it is a build
		# artifact, and a gate that demanded it exist would fail on a clean
		# checkout.
		internal/* | frontend/* | docs/* | scripts/* | .github/* | testdata/* | \
			*.go | *.ts | *.tsx | *.yml | *.yaml | *.sh)
			case "$token" in
			*/*) ;;
			*) continue ;;
			esac
			target=${token%/}
			if [ -e "$repo_root/$target" ]; then
				resolved=$((resolved + 1))
			# A path written relative to the test that uses it — the fixture
			# directory in `internal/board/testdata/...` is named as
			# `testdata/...` in the row beside the test file itself.
			elif [ -n "$(find "$repo_root" -path "*/$target" -not -path '*/node_modules/*' -print -quit)" ]; then
				resolved=$((resolved + 1))
			else
				report_missing "$id names $token, which does not exist"
			fi
			;;
		# A Go test function named in full, possibly with a trailing wildcard
		# for a family of them (TestBuildBootstrapInstruction_*).
		Test*)
			pattern=${token%\*}
			if grep -rqE "^func ${pattern}[A-Za-z0-9_]*\(" --include='*_test.go' "$repo_root"; then
				resolved=$((resolved + 1))
			else
				report_missing "$id names Go test $token, which no _test.go file defines"
			fi
			;;
		# The ledger's shorthand for "same prefix as the row above":
		# `..._WriteError_RetriedUpToLimitThenGivesUp`.
		...*)
			suffix=${token#...}
			if grep -rqE "^func Test[A-Za-z0-9_]*${suffix}\(" --include='*_test.go' "$repo_root"; then
				resolved=$((resolved + 1))
			else
				report_missing "$id names Go test ...$suffix, which no _test.go file defines"
			fi
			;;
		esac
	done
done > "${TMPDIR:-/tmp}/scenarios_check.$$" 2>&1

output=$(cat "${TMPDIR:-/tmp}/scenarios_check.$$")
rm -f "${TMPDIR:-/tmp}/scenarios_check.$$"

if [ -n "$output" ]; then
	echo "FAIL: these auto rows name something that does not exist."
	echo "      A row that reads as coverage and resolves to nothing is worse than no row:"
	echo "      it answers the question the ledger exists to answer, incorrectly."
	echo
	printf '%s\n' "$output"
	exit 1
fi

echo "  ok — every path and test name in the auto rows resolves"
exit 0
