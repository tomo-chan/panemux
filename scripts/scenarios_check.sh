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

report_missing() {
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

row_count=$(printf '%s\n' "$rows" | wc -l | tr -d ' ')

# Each row is checked on its own so a failure can name the scenario it came
# from; a bare "TestFoo not found" would send the reader grepping.
#
# `printf`, not `echo`: `make check-scenarios` runs this under `sh`, which on
# Debian and Ubuntu is dash, whose builtin `echo` expands backslash escapes
# with no `-e`. A `\|` — the only way to write a literal pipe inside a
# GitHub-flavoured markdown cell — would be rewritten to `|` before the row was
# ever tokenised, which also splits it into different awk fields and misreports
# the scenario ID.
#
# The loop's findings are captured through a pipeline rather than a temp file.
# The temp file was a fail-open: a redirect that cannot be opened (a read-only
# or full TMPDIR, a stale exported TMPDIR) left `$output` empty, and empty is
# how this script says "everything resolved". `__CHECKED__` below is what
# replaces that trust — the loop reports how many rows it actually processed,
# and a scan that did not run to completion cannot look like a clean one.
#
# Only stdout is captured. Folding stderr in made any tool error — a `find`
# hitting an unreadable directory, a `grep` rejecting a pattern — get announced
# under "these auto rows name something that does not exist", which is the one
# headline guaranteed to send the reader looking in the wrong place.
output=$(printf '%s\n' "$rows" | { seen=0
	resolved=0
	while IFS= read -r row; do
		seen=$((seen + 1))
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
				# -prune, not -not -path: a filter still descends into
				# frontend/node_modules and .git, which is tens of thousands of
				# stat calls per unresolved token inside a target `make check`
				# runs on every push.
				elif [ -n "$(find "$repo_root" \( -name node_modules -o -name .git -o -name dist \) -prune -o -path "*/$target" -print -quit)" ]; then
					resolved=$((resolved + 1))
				else
					report_missing "$id names $token, which does not exist"
				fi
				;;
			# A Go test function named in full, possibly with a trailing wildcard
			# for a family of them (TestBuildBootstrapInstruction_*).
			#
			# A name written WITHOUT the wildcard is matched exactly. Allowing
			# the loose suffix everywhere made the most common kind of rename
			# invisible: renames overwhelmingly append
			# (TestFoo -> TestFoo_RemoteVariant), so a row naming the old,
			# shorter name still matched the new, longer function and the gate
			# passed on a row that no longer points anywhere.
			Test*)
				pattern=${token%\*}
				if [ "$token" != "$pattern" ]; then
					expr="^func ${pattern}[A-Za-z0-9_]*\("
				else
					expr="^func ${pattern}\("
				fi
				if grep -rqE "$expr" --include='*_test.go' "$repo_root"; then
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
	done
	printf '__CHECKED__ %s %s\n' "$seen" "$resolved"
})

checked=$(printf '%s\n' "$output" | sed -n 's/^__CHECKED__ //p')
findings=$(printf '%s\n' "$output" | grep -v '^__CHECKED__ ' || true)

if [ -z "$checked" ]; then
	echo "scenarios: the row scan did not run to completion — the check did NOT run."
	echo "           Whatever it managed to print before stopping:"
	printf '%s\n' "$findings" | sed 's/^/           /'
	exit 1
fi

seen=${checked%% *}
resolved=${checked##* }

if [ "$seen" != "$row_count" ]; then
	echo "scenarios: read $seen of $row_count auto rows — the check did NOT run to completion."
	exit 1
fi

if [ -n "$findings" ]; then
	echo "FAIL: these auto rows name something that does not exist."
	echo "      A row that reads as coverage and resolves to nothing is worse than no row:"
	echo "      it answers the question the ledger exists to answer, incorrectly."
	echo
	printf '%s\n' "$findings"
	exit 1
fi

echo "  ok — $resolved path and test names across $seen auto rows all resolve"
exit 0
