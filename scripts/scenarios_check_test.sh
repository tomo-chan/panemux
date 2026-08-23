#!/bin/sh
#
# Tests for scripts/scenarios_check.sh.
#
# The gate's value rests entirely on its false-positive rate. It reads prose,
# and prose is full of things that look like paths and identifiers but are not:
# keystrokes (Cmd/Ctrl+Shift+B), HTTP routes (/ws/board-command), build
# artifacts (bin/panemux). Every one of those was a false positive in the first
# version, so each is asserted here — a gate that cries wolf over a keystroke
# is one people learn to ignore.
#
# Run with: make test-scenarios-check

set -u

scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
checker="$scripts_dir/scenarios_check.sh"

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
}
pass() { echo "ok   $1"; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# expect <want-exit> <name> <ledger-body>
expect() {
	want=$1
	name=$2
	body=$3
	checks=$((checks + 1))

	ledger="$work/ledger.md"
	{
		echo "| # | Scenario | Expected | Verification |"
		echo "|---|---|---|---|"
		printf '%s\n' "$body"
	} > "$ledger"

	output=$("$checker" "$ledger" 2>&1)
	got=$?
	if [ "$got" -eq "$want" ]; then
		pass "$name"
	else
		fail "$name: wanted exit $want, got $got" "$output"
	fi
}

# A row naming things that really exist.
expect 0 "a row naming a real file and a real test passes" \
	'| X1 | Something | Works | `auto`: `scripts/scenarios_check.sh`, `internal/config` — `TestValidate_AgentBoardMode_InvalidValue_Error` |'

expect 1 "a row naming a file that does not exist fails" \
	'| X2 | Something | Works | `auto`: `internal/nonesuch/missing_test.go` |'

expect 1 "a row naming a Go test that does not exist fails" \
	'| X3 | Something | Works | `auto`: `TestThisWasRenamedLongAgo` |'

# The wildcard and "..." shorthands the real ledger uses.
expect 0 "a trailing-wildcard test family resolves" \
	'| X4 | Something | Works | `auto`: `TestValidate_AgentBoard*` |'

expect 1 "a trailing-wildcard family with no members fails" \
	'| X5 | Something | Works | `auto`: `TestNothingStartsWithThis*` |'

expect 0 "the ... suffix shorthand resolves" \
	'| X6 | Something | Works | `auto`: `..._AgentBoardMode_InvalidValue_Error` |'

expect 1 "the ... suffix shorthand fails when nothing matches" \
	'| X7 | Something | Works | `auto`: `..._NoTestEndsLikeThisAtAll` |'

# `manual` rows name steps, not tests, and must never be resolved. The ledger
# also carries a real auto row here, because a file with no auto rows at all is
# itself a failure (asserted further down) and would mask this one.
expect 0 "a manual row's contents are not resolved" \
	'| X8a | Something | Works | `auto`: `scripts/scenarios_check.sh` |
| X8b | Something | Works | `manual`: open `frontend/src/nonesuch.ts` by hand |'

# The false positives that made the first version unusable.
expect 0 "a keystroke is not a path" \
	'| X9 | Something | Works | `auto`: `Cmd/Ctrl+Shift+B` opens it — `scripts/scenarios_check.sh` |'

expect 0 "an HTTP route is not a path" \
	'| X10 | Something | Works | `auto`: `/ws/board-command` is not registered — `scripts/scenarios_check.sh` |'

expect 0 "a build artifact is not a source path" \
	'| X11 | Something | Works | `auto`: CI builds `bin/panemux` — `scripts/scenarios_check.sh` |'

expect 0 "a bare script name with no directory is not resolved" \
	'| X12 | Something | Works | `auto`: what `api.sh` prints — `scripts/scenarios_check.sh` |'

# A path written relative to the test that owns it, as G1 does.
expect 0 "a path relative to the test that uses it resolves" \
	'| X13 | Something | Works | `auto`: `internal/board/agmsg_fixture_test.go` against `testdata/agmsg-v1.2.0/` |'

# A ledger whose table shape has changed out from under the checker must fail
# loudly rather than silently pass with zero rows checked — the way a gate
# quietly stops working.
checks=$((checks + 1))
printf '# Nothing here\n\nNo table at all.\n' > "$work/empty.md"
if "$checker" "$work/empty.md" > /dev/null 2>&1; then
	fail "a ledger with no auto rows must fail rather than silently pass"
else
	pass "a ledger with no auto rows fails rather than silently passing"
fi

checks=$((checks + 1))
if "$checker" "$work/does-not-exist.md" > /dev/null 2>&1; then
	fail "a missing ledger must fail"
else
	pass "a missing ledger fails"
fi

echo
if [ "$failures" -eq 0 ]; then
	echo "all $checks scenarios-check checks passed"
	exit 0
fi
echo "$failures of $checks scenarios-check checks failed"
exit 1
