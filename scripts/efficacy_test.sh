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
# The frontend half is exercised only through `--changed-tests`, which stops
# before running anything: driving vitest would mean a node_modules tree per
# fixture, and what is worth pinning here is the scoping decision, not that
# vitest can run a file.
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

# rewrite <file> <old> <new> — replace a fixed string, without `sed -i`.
# `sed -i` with no suffix is a GNU extension; BSD/macOS sed reads the next
# argument as the backup suffix and then fails. macOS is a supported developer
# platform (.goreleaser.yaml builds darwin) and this suite runs inside
# `make check`, so it has to work there too.
rewrite() {
	rw_file=$1
	rw_old=$2
	rw_new=$3
	OLD=$rw_old NEW=$rw_new awk '
		BEGIN { old = ENVIRON["OLD"]; new = ENVIRON["NEW"] }
		{
			p = index($0, old)
			if (p > 0) $0 = substr($0, 1, p - 1) new substr($0, p + length(old))
			print
		}
	' "$rw_file" > "$rw_file.tmp" && mv "$rw_file.tmp" "$rw_file"
}

git_init() {
	git init -q -b base .
	git config user.email "test@example.invalid"
	git config user.name "efficacy test"
}

# new_fixture — a git repository with a base commit carrying one function and
# one passing test. Prints its path.
new_fixture() {
	dir=$(mktemp -d)
	(
		cd "$dir" || exit 1
		git_init

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

# expect_output <exit> <substring> <name> <dir> — exit status AND evidence.
# Exit status alone is a weak assertion for this gate: several different
# failures all exit 1, and "it went red" is exactly the claim that has to be
# true *for the right reason*.
expect_output() {
	want=$1
	needle=$2
	name=$3
	dir=$4
	checks=$((checks + 1))

	output=$(run_efficacy "$dir")
	got=$?
	if [ "$got" -eq "$want" ] && printf '%s' "$output" | grep -q "$needle"; then
		pass "$name"
	else
		fail "$name: wanted exit $want and output matching '$needle', got exit $got" "$output"
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
expect_output 1 "SURVIVOR: ./. TestSub" "a tautological test is caught" "$tauto"

# --- a genuinely-red test must not vouch for a tautology beside it -----------
#
# The verdict has to be per test, not per invocation. `go test` exits nonzero
# if ANY selected test fails, so judging the whole changed set with one command
# means one honest test covers for every tautology in the same branch — which
# is most branches, since almost every PR changes more than one test.
#
# Package a gets a real test; package b gets a pure tautology. The gate must
# still name b.
masking=$(mktemp -d)
(
	cd "$masking" || exit 1
	git_init
	printf 'module sample\n\ngo 1.25\n' > go.mod
	mkdir -p a b
	printf 'package a\n' > a/a.go
	printf 'package a\n\nimport "testing"\n\nfunc TestNothingA(t *testing.T) {}\n' > a/a_test.go
	printf 'package b\n' > b/b.go
	printf 'package b\n\nimport "testing"\n\nfunc TestNothingB(t *testing.T) {}\n' > b/b_test.go
	git add -A && git commit -q -m "base"
	git checkout -q -b work

	cat >> a/a.go <<'GO'

func Mul(x, y int) int { return x * y }
GO
	cat >> a/a_test.go <<'GO'

func TestMul(t *testing.T) {
	if Mul(2, 3) != 6 {
		t.Fatal("Mul is wrong")
	}
}
GO
	cat >> b/b.go <<'GO'

func Bye() string { return "bye" }
GO
	cat >> b/b_test.go <<'GO'

func TestBye(t *testing.T) {
	if len("bye") != 3 {
		t.Fatal("strings are broken")
	}
}
GO
	git add -A && git commit -q -m "one real test, one tautology"
)
expect_output 1 "SURVIVOR: ./b TestBye" \
	"a real test in one package does not vouch for a tautology in another" "$masking"

# --- the root package must build in the scratch worktree ---------------------
#
# main.go has `//go:embed frontend/dist` and .gitignore excludes it, so a fresh
# `git worktree add` checkout cannot build the root package at all. Left alone,
# every root-package test "goes red" with zero files reverted and the gate
# passes everything — vacuously, for the packages where most of this
# repository's bootstrap and command-center tests live.
#
# The fixture reproduces that exact shape, with a tautology inside it. The
# assertion is that the tautology is *named*: a build failure would also exit 1,
# so exit status alone would not tell the fix from the bug.
embedded=$(mktemp -d)
(
	cd "$embedded" || exit 1
	git_init
	printf 'module sample\n\ngo 1.25\n' > go.mod
	printf 'frontend/dist/\n' > .gitignore
	cat > main.go <<'GO'
package main

import "embed"

//go:embed frontend/dist
var assets embed.FS

func main() { _ = assets }
GO
	printf 'package main\n\nimport "testing"\n\nfunc TestNothing(t *testing.T) {}\n' > main_test.go
	git add -A && git commit -q -m "base"
	git checkout -q -b work

	cat >> main.go <<'GO'

func Sub(a, b int) int { return a - b }
GO
	cat >> main_test.go <<'GO'

func TestSub(t *testing.T) {
	if 3-1 != 2 {
		t.Fatal("arithmetic is broken")
	}
}
GO
	git add -A && git commit -q -m "add Sub with a tautology"
)
expect_output 1 "SURVIVOR: ./. TestSub" \
	"a gitignored go:embed target does not make the gate vacuous" "$embedded"

# --- a branch already red at HEAD is an error, not a pass --------------------
#
# "It failed after the revert" only means something if it passed before. A test
# that was already failing would otherwise be reported as protecting code it
# has never once agreed with.
alreadyred=$(new_fixture)
(
	cd "$alreadyred" || exit 1
	cat >> sample.go <<'GO'

func Mul(a, b int) int {
	return a * b
}
GO
	cat >> sample_test.go <<'GO'

func TestMul(t *testing.T) {
	if Mul(2, 3) != 7 {
		t.Fatal("Mul is wrong")
	}
}
GO
	git add -A && git commit -q -m "add Mul with a test that does not pass"
)
expect_output 1 "already fails at HEAD" \
	"a test that is already red at HEAD is reported as an error" "$alreadyred"

# --- an edited assertion counts as a changed test ----------------------------
#
# The mapping is by touched LINE, not by "+func Test": strengthening an
# assertion inside an existing test must bring that test into scope, or the
# gate would be trivially avoided by never adding a new test function.
edited=$(new_fixture)
(
	cd "$edited" || exit 1
	# Add a bug-fix to Add, and tighten the existing test to notice it.
	rewrite sample.go 'return a + b' 'return a + b + 0'
	rewrite sample_test.go 'if Add(1, 2) != 3 {' 'if Add(1, 2) != 3 || Add(0, 0) != 0 {'
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
# drag an untouched test into scope — and the gate would then spend its verdict
# on a test this branch never wrote.
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

# --- frontend scope is the changed case, not the whole file ------------------
#
# Same reasoning as the Go half: running the whole file means the pre-existing
# cases in it go red for their own reasons and report as evidence about the new
# one.
fescope=$(mktemp -d)
(
	cd "$fescope" || exit 1
	git_init
	mkdir -p frontend/src
	printf 'export const add = (a: number, b: number) => a + b\n' > frontend/src/math.ts
	cat > frontend/src/math.test.ts <<'TS'
import { describe, it, expect } from 'vitest'
import { add } from './math'

describe('math', () => {
  it('adds', () => {
    expect(add(1, 2)).toBe(3)
  })
})
TS
	git add -A && git commit -q -m "base"
	git checkout -q -b work

	printf 'export const mul = (a: number, b: number) => a * b\n' >> frontend/src/math.ts
	# The heredoc below replaces the file wholesale — that is what produces the
	# diff this fixture scopes, so there is nothing for `rewrite` to do here.
	cat > frontend/src/math.test.ts <<'TS'
import { describe, it, expect } from 'vitest'
import { add, mul } from './math'

describe('math', () => {
  it('adds', () => {
    expect(add(1, 2)).toBe(3)
  })

  it('multiplies', () => {
    expect(mul(2, 3)).toBe(6)
  })
})
TS
	git add -A && git commit -q -m "add mul with its test"
)
checks=$((checks + 1))
listing=$(run_efficacy "$fescope" --changed-tests)
cases=$(printf '%s' "$listing" | sed -n '/^frontend cases:/,$p' | sed '1d')
if printf '%s' "$cases" | grep -q 'multiplies' && ! printf '%s' "$cases" | grep -q "	adds"; then
	pass "frontend scope is the changed it() case, not the whole file"
else
	fail "frontend scope is the changed it() case, not the whole file" "$listing"
fi
rm -rf "$fescope"

# ...with a fallback that is over-wide rather than wrong. A touched line that
# belongs to no case at all — describe-level setup, an import, a helper — has
# no case name to narrow to, so the whole file runs.
fefallback=$(mktemp -d)
(
	cd "$fefallback" || exit 1
	git_init
	mkdir -p frontend/src
	printf 'export const add = (a: number, b: number) => a + b\n' > frontend/src/math.ts
	cat > frontend/src/math.test.ts <<'TS'
import { describe, it, expect, beforeEach } from 'vitest'
import { add } from './math'

describe('math', () => {
  beforeEach(() => {})

  it('adds', () => {
    expect(add(1, 2)).toBe(3)
  })
})
TS
	git add -A && git commit -q -m "base"
	git checkout -q -b work

	printf 'export const mul = (a: number, b: number) => a * b\n' >> frontend/src/math.ts
	rewrite frontend/src/math.test.ts 'beforeEach(() => {})' 'beforeEach(() => { /* reset */ })'
	git add -A && git commit -q -m "touch describe-level setup"
)
checks=$((checks + 1))
listing=$(run_efficacy "$fefallback" --changed-tests)
if printf '%s' "$listing" | grep -q '(whole file)'; then
	pass "a touched line outside every case falls back to the whole file"
else
	fail "a touched line outside every case falls back to the whole file" "$listing"
fi
rm -rf "$fefallback"

# --- the frontend half's verdict is per case, not per invocation -------------
#
# This one drives a real vitest run, because the bug it guards cannot be seen
# any other way: `-t` selects by unanchored regex and the summary line is an
# aggregate, so a tautological `it('renders')` was reported red on the strength
# of its sibling `it('renders empty state')` going red in the same invocation.
# `--changed-tests` stops before running anything and would not have caught it.
#
# It needs the repository's own node_modules, which `make check` has by the time
# this runs. Without it the check reports itself skipped rather than passed.
checks=$((checks + 1))
if [ ! -d "$scripts_dir/../frontend/node_modules" ]; then
	echo "skip frontend/node_modules missing — the per-case frontend check is skipped"
else
	percase=$(mktemp -d)
	(
		cd "$percase" || exit 1
		git_init
		mkdir -p frontend/src
		printf 'node_modules/\n' > .gitignore
		printf '{"name":"efficacy-fixture","private":true,"type":"module"}\n' > frontend/package.json
		ln -s "$(CDPATH='' cd -- "$scripts_dir/../frontend/node_modules" && pwd)" frontend/node_modules
		printf 'export const add = (a: number, b: number) => a + b\n' > frontend/src/widget.ts
		cat > frontend/src/widget.test.ts <<'TS'
import { describe, it, expect } from 'vitest'
import { add } from './widget'

describe('widget', () => {
  it('adds', () => {
    expect(add(1, 2)).toBe(3)
  })
})
TS
		git add -A && git commit -q -m "base"
		git checkout -q -b work

		printf 'export const renderAll = (xs: number[]) => xs.join(",")\n' >> frontend/src/widget.ts
		cat > frontend/src/widget.test.ts <<'TS'
import { describe, it, expect } from 'vitest'
import { add, renderAll } from './widget'

describe('widget', () => {
  it('adds', () => {
    expect(add(1, 2)).toBe(3)
  })

  it('renders', () => {
    expect('a,b').toBe('a,b')
  })

  it('renders empty state', () => {
    expect(renderAll([])).toBe('')
  })
})
TS
		git add -A && git commit -q -m "add renderAll with one real test and one tautology"
	)
	percase_out=$(run_efficacy "$percase")
	percase_status=$?
	if [ "$percase_status" -eq 1 ] &&
		printf '%s' "$percase_out" | grep -q 'SURVIVOR: src/widget.test.ts > renders still passes' &&
		printf '%s' "$percase_out" | grep -q 'red: src/widget.test.ts > renders empty state'; then
		pass "a frontend tautology is not vouched for by its red sibling"
	else
		fail "a frontend tautology is not vouched for by its red sibling" \
			"exit $percase_status
$percase_out"
	fi
	rm -rf "$percase"
fi

# --- the frontend exemption marker, in all three shapes ----------------------
#
# The marker is where scope and the fallback meet, and getting the meeting
# wrong INVERTS it: if "every hit case is exempt" is indistinguishable from
# "nothing could be narrowed", applying the marker widens scope to the whole
# file and the exempted case runs anyway — leaving the branch worse off than
# without it. These drive `--changed-tests`, which is where scope is decided.
fe_scope() {
	fs_dir=$(mktemp -d)
	(
		cd "$fs_dir" || exit 1
		git_init
		mkdir -p frontend/src
		printf 'export const add = (a: number, b: number) => a + b\n' > frontend/src/math.ts
		cat > frontend/src/math.test.ts <<'TS'
import { it, expect } from 'vitest'
import { add } from './math'

it('adds', () => {
  expect(add(1, 2)).toBe(3)
})
TS
		git add -A && git commit -q -m "base"
		git checkout -q -b work
		printf 'export const mul = (a: number, b: number) => a * b\n' >> frontend/src/math.ts
		printf '%s' "$1" >> frontend/src/math.test.ts
		git add -A && git commit -q -m "change"
	)
	run_efficacy "$fs_dir" --changed-tests | sed -n '/^frontend cases:/,$p' | sed '1d'
	rm -rf "$fs_dir"
}

checks=$((checks + 1))
all_exempt=$(fe_scope '
//efficacy:exempt — mul is not this branch'"'"'s implementation
it('"'"'multiplies'"'"', () => {
  expect(2 * 3).toBe(6)
})
')
if [ -z "$(printf '%s' "$all_exempt" | tr -d '[:space:]')" ]; then
	pass "an all-exempt frontend file drops out of scope instead of widening to the whole file"
else
	fail "an all-exempt frontend file drops out of scope instead of widening to the whole file" \
		"wanted no cases, got:
$all_exempt"
fi

checks=$((checks + 1))
some_exempt=$(fe_scope '
//efficacy:exempt — not this branch'"'"'s implementation
it('"'"'multiplies'"'"', () => {
  expect(2 * 3).toBe(6)
})

it('"'"'multiplies via mul'"'"', () => {
  expect(mul(2, 3)).toBe(6)
})
')
if printf '%s' "$some_exempt" | grep -q 'multiplies via mul' &&
	! printf '%s' "$some_exempt" | grep -qE '\smultiplies$'; then
	pass "a partly-exempt frontend file narrows to the cases that are not marked"
else
	fail "a partly-exempt frontend file narrows to the cases that are not marked" "$some_exempt"
fi

checks=$((checks + 1))
unmappable=$(fe_scope '
it.each([1, 2])('"'"'handles %i'"'"', (n) => {
  expect(n).toBeGreaterThan(0)
})
')
if printf '%s' "$unmappable" | grep -q '(whole file)'; then
	pass "a case whose name is not a literal is left for location-based resolution"
else
	fail "a case whose name is not a literal is left for location-based resolution" "$unmappable"
fi

# ...and that resolution, end to end. An `it.each` block cannot be named
# statically, and running the whole file instead would let its siblings' red
# vouch for it — the last place this gate still judged by aggregate.
checks=$((checks + 1))
if [ ! -d "$scripts_dir/../frontend/node_modules" ]; then
	echo "skip frontend/node_modules missing — the location-resolution check is skipped"
else
	byloc=$(mktemp -d)
	(
		cd "$byloc" || exit 1
		git_init
		mkdir -p frontend/src
		printf 'node_modules/\n' > .gitignore
		printf '{"name":"efficacy-fixture","private":true,"type":"module"}\n' > frontend/package.json
		ln -s "$(CDPATH='' cd -- "$scripts_dir/../frontend/node_modules" && pwd)" frontend/node_modules
		printf 'export const add = (a: number, b: number) => a + b\n' > frontend/src/widget.ts
		cat > frontend/src/widget.test.ts <<'TS'
import { describe, it, expect } from 'vitest'
import { add } from './widget'

describe('widget', () => {
  it('adds', () => {
    expect(add(1, 2)).toBe(3)
  })
})
TS
		git add -A && git commit -q -m "base"
		git checkout -q -b work

		printf 'export const renderAll = (xs: number[]) => xs.join(",")\n' >> frontend/src/widget.ts
		cat > frontend/src/widget.test.ts <<'TS'
import { describe, it, expect } from 'vitest'
import { add, renderAll } from './widget'

describe('widget', () => {
  it('adds', () => {
    expect(add(1, 2)).toBe(3)
  })

  it.each([[[], ''], [[1], '1']])('renders %j', (xs, want) => {
    expect(renderAll(xs as number[])).toBe(want)
  })
})
TS
		git add -A && git commit -q -m "add renderAll, covered only by an it.each block"
	)
	byloc_out=$(run_efficacy "$byloc")
	byloc_status=$?
	if [ "$byloc_status" -eq 0 ] &&
		printf '%s' "$byloc_out" | grep -q 'red: src/widget.test.ts > renders' &&
		! printf '%s' "$byloc_out" | grep -q 'whole file' &&
		! printf '%s' "$byloc_out" | grep -q '> adds'; then
		pass "an it.each block is resolved by source location, not by running the whole file"
	else
		fail "an it.each block is resolved by source location, not by running the whole file" \
			"exit $byloc_status
$byloc_out"
	fi
	rm -rf "$byloc"
fi

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
expect_output 0 "WARNING" \
	"implementation with no changed test warns without blocking" "$implonly"

# --- each stack is judged only against its own revert ------------------------
#
# Nothing Go-side is reverted when only frontend implementation changed, so
# failing a Go test there would be a verdict on a mutation that never happened.
crossstack=$(mktemp -d)
(
	cd "$crossstack" || exit 1
	git_init
	printf 'module sample\n\ngo 1.25\n' > go.mod
	mkdir -p frontend/src
	printf 'export const add = (a: number, b: number) => a + b\n' > frontend/src/math.ts
	printf 'package sample\n\nfunc Add(a, b int) int { return a + b }\n' > sample.go
	printf 'package sample\n\nimport "testing"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal("Add is wrong")\n\t}\n}\n' > sample_test.go
	git add -A && git commit -q -m "base"
	git checkout -q -b work

	printf 'export const mul = (a: number, b: number) => a * b\n' >> frontend/src/math.ts
	cat >> sample_test.go <<'GO'

func TestAddZero(t *testing.T) {
	if Add(0, 0) != 0 {
		t.Fatal("Add is wrong")
	}
}
GO
	git add -A && git commit -q -m "frontend implementation, Go test"
)
expect_output 0 "WARNING" \
	"a Go test is not red-checked when only frontend implementation changed" "$crossstack"

# The mirror: Go implementation changed, a frontend test touched, no frontend
# implementation. The frontend half must be skipped rather than failed — and
# skipped without needing node_modules, since there is nothing to run.
mirror=$(mktemp -d)
(
	cd "$mirror" || exit 1
	git_init
	printf 'module sample\n\ngo 1.25\n' > go.mod
	mkdir -p frontend/src
	printf 'package sample\n\nfunc Add(a, b int) int { return a + b }\n' > sample.go
	printf 'package sample\n\nimport "testing"\n\nfunc TestAdd(t *testing.T) {\n\tif Add(1, 2) != 3 {\n\t\tt.Fatal("Add is wrong")\n\t}\n}\n' > sample_test.go
	cat > frontend/src/math.test.ts <<'TS'
import { it, expect } from 'vitest'

it('adds', () => {
  expect(1 + 2).toBe(3)
})
TS
	git add -A && git commit -q -m "base"
	git checkout -q -b work

	cat >> sample.go <<'GO'

func Mul(a, b int) int { return a * b }
GO
	cat >> sample_test.go <<'GO'

func TestMul(t *testing.T) {
	if Mul(2, 3) != 6 {
		t.Fatal("Mul is wrong")
	}
}
GO
	rewrite frontend/src/math.test.ts "expect(1 + 2).toBe(3)" "expect(2 + 1).toBe(3)"
	git add -A && git commit -q -m "Go implementation, frontend test touched"
)
expect_output 0 "red: ./. TestMul" \
	"a frontend test is not red-checked when only Go implementation changed" "$mirror"

# --- a run that selected nothing is not a red -------------------------------
#
# `go test` exits 0 when -run matches no test, so "the command did not go red"
# and "the tests passed" are different statements. Benchmarks are the common
# case: -run never selects them, so a branch whose only new test function is a
# benchmark used to be failed with `[no tests to run]` quoted as the evidence.
benchonly=$(new_fixture)
(
	cd "$benchonly" || exit 1
	cat >> sample.go <<'GO'

func Mul(a, b int) int {
	return a * b
}
GO
	cat >> sample_test.go <<'GO'

func BenchmarkMul(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = Mul(2, 3)
	}
}
GO
	git add -A && git commit -q -m "add Mul with only a benchmark"
)
expect_output 0 "WARNING" \
	"a benchmark-only change is not failed for selecting no tests" "$benchonly"

# A test skipped at HEAD cannot be red-checked — that is a reason to say so,
# not to call it red. Beside a test that CAN be checked, the branch still
# passes.
skipped=$(new_fixture)
(
	cd "$skipped" || exit 1
	cat >> sample.go <<'GO'

func Mul(a, b int) int {
	return a * b
}
GO
	cat >> sample_test.go <<'GO'

func TestMulSkipped(t *testing.T) {
	t.Skip("not yet")
	if Mul(2, 3) != 6 {
		t.Fatal("Mul is wrong")
	}
}

func TestMul(t *testing.T) {
	if Mul(2, 3) != 6 {
		t.Fatal("Mul is wrong")
	}
}
GO
	git add -A && git commit -q -m "add Mul with one skipped test and one real one"
)
expect_output 0 "skipped at HEAD" \
	"a test skipped at HEAD is reported, not counted as red" "$skipped"

# ...but a branch where EVERY changed test is unrunnable has been checked
# against nothing, which is "could not check", not "nothing to check". Same
# argument as the missing base ref and the missing node_modules.
allskipped=$(new_fixture)
(
	cd "$allskipped" || exit 1
	cat >> sample.go <<'GO'

func Mul(a, b int) int {
	return a * b
}
GO
	cat >> sample_test.go <<'GO'

func TestMul(t *testing.T) {
	t.Skip("needs a real agmsg install")
	if Mul(2, 3) != 6 {
		t.Fatal("Mul is wrong")
	}
}
GO
	git add -A && git commit -q -m "add Mul with only a skipped test"
)
expect_output 1 "nothing was red-checked" \
	"a branch whose every changed test is unrunnable is an error, not a pass" "$allskipped"

# The per-test escape hatch. A test in a package this branch's implementation
# never touched was never going to go red, and the PR-wide label is too blunt
# for it — applying it to get past one unrelated test also exempts a genuine
# tautology elsewhere in the same branch.
marked=$(mktemp -d)
(
	cd "$marked" || exit 1
	git_init
	printf 'module sample\n\ngo 1.25\n' > go.mod
	mkdir -p a b
	printf 'package a\n' > a/a.go
	printf 'package a\n\nimport "testing"\n\nfunc TestNothingA(t *testing.T) {}\n' > a/a_test.go
	printf 'package b\n\nfunc Bye() string { return "bye" }\n' > b/b.go
	printf 'package b\n\nimport "testing"\n\nfunc TestBye(t *testing.T) {\n\tif Bye() != "bye" {\n\t\tt.Fatal("Bye is wrong")\n\t}\n}\n' > b/b_test.go
	git add -A && git commit -q -m "base"
	git checkout -q -b work

	# Package a gets a real implementation and a real test for it.
	cat >> a/a.go <<'GO'

func Mul(x, y int) int { return x * y }
GO
	cat >> a/a_test.go <<'GO'

func TestMul(t *testing.T) {
	if Mul(2, 3) != 6 {
		t.Fatal("Mul is wrong")
	}
}
GO
	# Package b's implementation is untouched; only an assertion is tightened,
	# so nothing under this test will be reverted and it cannot go red.
	rewrite b/b_test.go 'if Bye() != "bye" {' 'if Bye() != "bye" || len(Bye()) != 3 {'
	git add -A && git commit -q -m "one real test, one in an untouched package"
)
checks=$((checks + 1))
before_marker=$(run_efficacy "$marked")
before_status=$?
(
	cd "$marked" || exit 1
	rewrite b/b_test.go 'func TestBye(t *testing.T) {' '//efficacy:exempt — b'"'"'s implementation is not part of this branch
func TestBye(t *testing.T) {'
	git add -A && git commit -q -m "mark TestBye exempt"
)
after_marker=$(run_efficacy "$marked")
after_status=$?
if [ "$before_status" -eq 1 ] && printf '%s' "$before_marker" | grep -q 'SURVIVOR: ./b TestBye' &&
	[ "$after_status" -eq 0 ]; then
	pass "an efficacy:exempt marker takes one test out of scope, and nothing else"
else
	fail "an efficacy:exempt marker takes one test out of scope, and nothing else" \
		"before: exit $before_status
$before_marker
after: exit $after_status
$after_marker"
fi
rm -rf "$marked"

# --- "could not check" is never a pass ---------------------------------------
#
# The two fail-open paths. A required check that goes green having checked
# nothing is the one failure mode a required check exists to rule out.
checks=$((checks + 1))
noref=$(new_fixture)
(
	cd "$noref" || exit 1
	cat >> sample.go <<'GO'

func Mul(a, b int) int { return a * b }
GO
	git add -A && git commit -q -m "impl"
)
output=$(cd "$noref" && EFFICACY_BASE=origin/no-such-ref "$efficacy" 2>&1)
status=$?
if [ "$status" -eq 1 ] && printf '%s' "$output" | grep -q 'ERROR'; then
	pass "a missing base ref is an error, not a silent pass"
else
	fail "a missing base ref is an error, not a silent pass" "exit $status
$output"
fi
rm -rf "$noref"

# Missing node_modules used to drop the entire frontend red-check with no
# output at all: green, having checked nothing.
nodeps=$(mktemp -d)
(
	cd "$nodeps" || exit 1
	git_init
	mkdir -p frontend/src
	printf 'export const add = (a: number, b: number) => a + b\n' > frontend/src/math.ts
	cat > frontend/src/math.test.ts <<'TS'
import { it, expect } from 'vitest'
import { add } from './math'

it('adds', () => {
  expect(add(1, 2)).toBe(3)
})
TS
	git add -A && git commit -q -m "base"
	git checkout -q -b work

	printf 'export const mul = (a: number, b: number) => a * b\n' >> frontend/src/math.ts
	cat >> frontend/src/math.test.ts <<'TS'

it('multiplies', () => {
  expect(mul(2, 3)).toBe(6)
})
TS
	git add -A && git commit -q -m "add mul with its test"
)
expect_output 1 "node_modules is missing" \
	"a missing node_modules is an error, not a silent frontend skip" "$nodeps"

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
