#!/bin/sh
#
# Tests for scripts/efficacy.sh (gate G4(b), red-check).
#
# The gate's whole value is that it can tell a test that protects its
# implementation from one that only appears to. Both verdicts are driven here
# against real throwaway repositories with a real `go test` run — the tautology
# case in particular cannot be faked, because what makes it a tautology is that
# the compiler and the test runner are both perfectly happy with it.
#
# Run with: make test-efficacy

set -u

scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
efficacy="$scripts_dir/efficacy.sh"

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
}

pass() { echo "ok   $1"; }

# new_fixture — a git repository with a base commit carrying one function and
# one passing test. Prints its path.
new_fixture() {
	dir=$(mktemp -d)
	(
		cd "$dir" || exit 1
		git init -q -b base .
		git config user.email "test@example.invalid"
		git config user.name "efficacy test"

		printf 'module sample\n\ngo 1.25\n' > go.mod
		cat > sample.go <<'GO'
package sample

func Add(a, b int) int {
	return a + b
}
GO
		cat > sample_test.go <<'GO'
package sample

import "testing"

func TestAdd(t *testing.T) {
	if Add(1, 2) != 3 {
		t.Fatal("Add is wrong")
	}
}
GO
		git add -A
		git commit -q -m "base"
		git checkout -q -b work
	)
	echo "$dir"
}

run_efficacy() {
	dir=$1
	shift
	(cd "$dir" && EFFICACY_BASE=base "$efficacy" "$@" 2>&1)
}

expect() {
	want=$1
	name=$2
	dir=$3
	shift 3
	checks=$((checks + 1))

	output=$(run_efficacy "$dir" "$@")
	got=$?
	if [ "$got" -eq "$want" ]; then
		pass "$name"
	else
		fail "$name: wanted exit $want, got $got" "$output"
	fi
	rm -rf "$dir"
}

# --- a test that genuinely protects its implementation -----------------------
#
# TestMul calls Mul. Revert sample.go and Mul is gone, so the package does not
# even build: the strongest possible red.
good=$(new_fixture)
(
	cd "$good" || exit 1
	cat >> sample.go <<'GO'

func Mul(a, b int) int {
	return a * b
}
GO
	cat >> sample_test.go <<'GO'

func TestMul(t *testing.T) {
	if Mul(2, 3) != 6 {
		t.Fatal("Mul is wrong")
	}
}
GO
	git add -A && git commit -q -m "add Mul with its test"
)
expect 0 "a test that needs its implementation passes the gate" "$good"

# --- a tautological test -----------------------------------------------------
#
# TestSub never mentions Sub. Reverting sample.go removes Sub and the test
# still passes — it asserts nothing about the change it claims to cover. This
# is the exact shape docs/quality-gateway.md says the current gates cannot see,
# and it compiles and passes cleanly under `make check`.
tauto=$(new_fixture)
(
	cd "$tauto" || exit 1
	cat >> sample.go <<'GO'

func Sub(a, b int) int {
	return a - b
}
GO
	cat >> sample_test.go <<'GO'

func TestSub(t *testing.T) {
	got := 3 - 1
	if got != 2 {
		t.Fatal("arithmetic is broken")
	}
}
GO
	git add -A && git commit -q -m "add Sub with a test that does not test it"
)
expect 1 "a tautological test is caught" "$tauto"

# --- an edited assertion counts as a changed test ----------------------------
#
# The mapping is by touched LINE, not by "+func Test": strengthening an
# assertion inside an existing test must bring that test into scope, or the
# gate would be trivially avoided by never adding a new test function.
edited=$(new_fixture)
(
	cd "$edited" || exit 1
	# Add a bug-fix to Add, and tighten the existing test to notice it.
	sed -i 's/return a + b/return a + b + 0/' sample.go
	sed -i 's/if Add(1, 2) != 3 {/if Add(1, 2) != 3 || Add(0, 0) != 0 {/' sample_test.go
	git add -A && git commit -q -m "tighten TestAdd"
)
checks=$((checks + 1))
listing=$(run_efficacy "$edited" --changed-tests)
if printf '%s' "$listing" | grep -q 'go test funcs: *TestAdd'; then
	pass "an edited assertion brings its test into scope"
else
	fail "an edited assertion brings its test into scope" "$listing"
fi
rm -rf "$edited"

# ...and the complement, which is the one that bites. Appending a new test to
# the end of a file touches the blank line that separates it from the previous
# one. If that line were attributed to the function above, every append would
# drag an untouched test into the -run set — and because the gate is satisfied
# when the whole set goes red, an unrelated failure would then mask a
# tautological test. Scope must be exactly the appended function.
appended=$(new_fixture)
(
	cd "$appended" || exit 1
	cat >> sample.go <<'GO'

func Mul(a, b int) int {
	return a * b
}
GO
	cat >> sample_test.go <<'GO'

// TestMul has a doc comment, which belongs to it and not to TestAdd above.
func TestMul(t *testing.T) {
	if Mul(2, 3) != 6 {
		t.Fatal("Mul is wrong")
	}
}
GO
	git add -A && git commit -q -m "append TestMul"
)
checks=$((checks + 1))
listing=$(run_efficacy "$appended" --changed-tests)
funcs=$(printf '%s' "$listing" | sed -n 's/^go test funcs: *//p')
if [ "$funcs" = "TestMul" ]; then
	pass "appending a test does not drag the untouched test above it into scope"
else
	fail "appending a test does not drag the untouched test above it into scope" \
		"wanted exactly TestMul, got: '$funcs'"
fi
rm -rf "$appended"

# --- skips, which are as important as the failures ---------------------------
#
# Principle 4: a gate that fires on a change it has no opinion about gets
# bypassed, and takes the other gates with it.

docs=$(new_fixture)
(
	cd "$docs" || exit 1
	echo "# notes" > README.md
	git add -A && git commit -q -m "docs only"
)
expect 0 "a docs-only branch is skipped" "$docs"

testonly=$(new_fixture)
(
	cd "$testonly" || exit 1
	cat >> sample_test.go <<'GO'

func TestAddZero(t *testing.T) {
	if Add(0, 0) != 0 {
		t.Fatal("Add is wrong")
	}
}
GO
	git add -A && git commit -q -m "test only"
)
expect 0 "a test-only branch is skipped (no implementation to revert)" "$testonly"

implonly=$(new_fixture)
(
	cd "$implonly" || exit 1
	cat >> sample.go <<'GO'

func Neg(a int) int {
	return -a
}
GO
	git add -A && git commit -q -m "implementation with no test"
)
checks=$((checks + 1))
output=$(run_efficacy "$implonly")
status=$?
if [ "$status" -eq 0 ] && printf '%s' "$output" | grep -q 'WARNING'; then
	pass "implementation with no changed test warns without blocking"
else
	fail "implementation with no changed test warns without blocking" "$output"
fi
rm -rf "$implonly"

# The documented escape hatch for a change whose tests genuinely should not go
# red — a pure refactor. It must work, and it must say what it is for.
exempt=$(new_fixture)
(
	cd "$exempt" || exit 1
	cat >> sample.go <<'GO'

func Sub(a, b int) int {
	return a - b
}
GO
	cat >> sample_test.go <<'GO'

func TestSub(t *testing.T) {
	got := 3 - 1
	if got != 2 {
		t.Fatal("arithmetic is broken")
	}
}
GO
	git add -A && git commit -q -m "tautology, but exempt"
)
checks=$((checks + 1))
output=$(cd "$exempt" && EFFICACY_BASE=base EFFICACY_EXEMPT=1 "$efficacy" 2>&1)
status=$?
if [ "$status" -eq 0 ] && printf '%s' "$output" | grep -q 'refactor'; then
	pass "EFFICACY_EXEMPT=1 skips, and explains what it is for"
else
	fail "EFFICACY_EXEMPT=1 skips, and explains what it is for" "$output"
fi
rm -rf "$exempt"

# The gate must never leave the real checkout half-reverted.
checks=$((checks + 1))
dirty=$(new_fixture)
(
	cd "$dirty" || exit 1
	cat >> sample.go <<'GO'

func Mul(a, b int) int {
	return a * b
}
GO
	cat >> sample_test.go <<'GO'

func TestMul(t *testing.T) {
	if Mul(2, 3) != 6 {
		t.Fatal("Mul is wrong")
	}
}
GO
	git add -A && git commit -q -m "add Mul"
)
run_efficacy "$dirty" > /dev/null 2>&1
leftover=$(cd "$dirty" && git status --porcelain)
worktrees=$(cd "$dirty" && git worktree list | wc -l)
if [ -z "$leftover" ] && [ "$worktrees" -eq 1 ]; then
	pass "the real checkout and worktree list are left untouched"
else
	fail "the real checkout and worktree list are left untouched" "status: $leftover
worktrees: $worktrees"
fi
rm -rf "$dirty"

echo
if [ "$failures" -eq 0 ]; then
	echo "all $checks efficacy checks passed"
	exit 0
fi
echo "$failures of $checks efficacy checks failed"
exit 1
