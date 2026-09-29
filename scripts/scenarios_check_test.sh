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

# A rename that APPENDS is the common one (TestFoo -> TestFoo_RemoteVariant),
# and a row naming the old name is exactly the rot this gate exists to catch.
# Without the wildcard, the match has to be exact.
expect 1 "an exact test name that only matches by prefix fails" \
	'| X14 | Something | Works | `auto`: `TestValidate_AgentBoardMode_InvalidValue` |'

expect 0 "...and the wildcard form of that same name still resolves" \
	'| X15 | Something | Works | `auto`: `TestValidate_AgentBoardMode_InvalidValue*` |'

# The gate used to collect its findings in $TMPDIR. A redirect that cannot be
# opened left the findings empty, and empty is how this script says "clean" —
# so a read-only or stale TMPDIR turned a failing ledger green.
checks=$((checks + 1))
printf '%s\n%s\n%s\n' \
	'| # | Scenario | Expected | Verification |' \
	'|---|---|---|---|' \
	'| X16 | Something | Works | `auto`: `TestThisWasRenamedLongAgo` |' > "$work/tmpdir.md"
if TMPDIR=/nonexistent-dir "$checker" "$work/tmpdir.md" > /dev/null 2>&1; then
	fail "an unwritable TMPDIR must not turn a failing ledger green"
else
	pass "an unwritable TMPDIR does not turn a failing ledger green"
fi

# A tool that errors must not have its stderr counted as a finding: announcing
# an infrastructure failure as "these auto rows name something that does not
# exist" is the one message guaranteed to send the reader to the wrong place.
checks=$((checks + 1))
printf '%s\n%s\n%s\n' \
	'| # | Scenario | Expected | Verification |' \
	'|---|---|---|---|' \
	'| X17 | Something | Works | `auto`: `Test(bad` |' > "$work/stderr.md"
stdout_only=$("$checker" "$work/stderr.md" 2> /dev/null)
if printf '%s' "$stdout_only" | grep -q 'X17 names Go test' &&
	! printf '%s' "$stdout_only" | grep -qi 'unmatched'; then
	pass "a tool error is diagnostics, not a missing-row finding"
else
	fail "a tool error is diagnostics, not a missing-row finding" "$stdout_only"
fi

# `make check-scenarios` runs under dash, whose builtin `echo` expands
# backslash escapes with no `-e`. A backslash sequence in a cell — `\|` is the
# only way to write a literal pipe in a GitHub-flavoured table, and `\n` or
# `\t` are plausible in a column quoting commands — would then be rewritten
# before the row was ever tokenised. This row carries a literal `\n`: expanded,
# it breaks one row into two, and the failure comes back naming an empty
# scenario instead of X18.
checks=$((checks + 1))
printf '%s\n%s\n%s\n' \
	'| # | Scenario | Expected | Verification |' \
	'|---|---|---|---|' \
	'| X18 | Something | Works, see \n below | `auto`: `TestThisWasRenamedLongAgo` |' > "$work/escapes.md"
escaped=$("$checker" "$work/escapes.md" 2>&1)
if printf '%s' "$escaped" | grep -q 'X18 names Go test TestThisWasRenamedLongAgo'; then
	pass "a backslash escape in a row is not expanded before the row is tokenised"
else
	fail "a backslash escape in a row is not expanded before the row is tokenised" "$escaped"
fi

# Rows that ARE read but resolve nothing. `__CHECKED__` proves the loop
# finished and the empty-ledger check proves there were rows; neither proves
# anything was recognised. Tokens are read from backticked values only, and
# every auto row carries the `auto` token itself, which is skipped — so a
# Verification column that stops backticking its test names and paths yields
# one skipped token per row, no findings, and a clean bill of health. This is
# the fail-open that needs no infrastructure failure at all, only a markdown
# reformat.
checks=$((checks + 1))
printf '%s\n%s\n%s\n%s\n' \
	'| # | Scenario | Expected | Verification |' \
	'|---|---|---|---|' \
	'| Z1 | Something | Works | `auto`: TestDoesNotExistAtAll |' \
	'| Z2 | Something | Works | `auto`: internal/nope/missing.go |' > "$work/unbackticked.md"
unbackticked=$("$checker" "$work/unbackticked.md" 2>&1)
if [ $? -eq 1 ] && printf '%s' "$unbackticked" | grep -q 'format must have changed'; then
	pass "rows that resolve nothing at all fail rather than reporting all clear"
else
	fail "rows that resolve nothing at all fail rather than reporting all clear" "$unbackticked"
fi

# ...and the complement, so the new guard cannot be satisfied by simply failing
# every ledger whose rows do not all resolve: a row naming something real
# alongside one naming something missing must still report the MISSING row,
# not the format.
checks=$((checks + 1))
printf '%s\n%s\n%s\n' \
	'| # | Scenario | Expected | Verification |' \
	'|---|---|---|---|' \
	'| Z3 | Something | Works | `auto`: `scripts/scenarios_check.sh`, `TestThisWasRenamedLongAgo` |' > "$work/mixed.md"
mixed=$("$checker" "$work/mixed.md" 2>&1)
if [ $? -eq 1 ] && printf '%s' "$mixed" | grep -q 'Z3 names Go test TestThisWasRenamedLongAgo' &&
	! printf '%s' "$mixed" | grep -q 'format must have changed'; then
	pass "a missing name is reported as missing, not as a format change"
else
	fail "a missing name is reported as missing, not as a format change" "$mixed"
fi

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
