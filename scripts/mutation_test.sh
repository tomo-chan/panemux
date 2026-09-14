#!/bin/sh
#
# Tests for scripts/mutation.sh.
#
# The suite never runs gremlins. That is deliberate, not a shortcut: `make test`
# runs this, `make check` runs `make test`, and `.githooks/pre-push` runs
# `make check` — so a suite that needed a tool `make install-deps` does not
# install would block every push on every machine that lacks it. scripts/
# mutation.sh takes `--report <file>` for exactly this reason, the way
# scripts/coverage_blocks.sh takes `--profile`: the half that runs the external
# tool and the half that decides what the results MEAN are separable, and only
# the second half holds the logic worth testing.
#
# Three properties carry this gate, and each is a way it could report green
# having decided nothing:
#
#   1. Scoping to the diff. Only survivors on lines this branch changed are
#      reported. The repository has 108 surviving mutants today (issue #180's
#      measurement); a gate that named all of them would start red, and
#      docs/quality-gateway.md principle 4 says what happens next.
#   2. Reporting every mutant that reached no verdict on a changed line.
#      `--diff` decides for itself which mutants to run, a mutant whose suite
#      timed out was never judged, and a status this gate does not recognise
#      tells it nothing at all. Counting any of those as "no survivor" states a
#      result nothing measured, so each is reported as undecided — in the
#      headline, not only in a section below it.
#   3. Warning, not failing, on a survivor. Roadmap item 6 of #180 says stage 3
#      starts as a warning, and the measurement says why: 34% of this
#      repository's survivors are ones nobody should "fix" — buffer sizes and
#      timeout constants whose killing test would be a tautology itself.
#      "Could not run" is still a failure, because a check that decided nothing
#      must never look like one that passed.
#
# Run with: make test-mutation

set -u

# The two variables DEVELOPMENT.md tells developers to set are exactly the two
# that would silently rewrite what this suite is testing — a base ref makes the
# no-base cases run the gate instead, and the branch-wide exemption makes every
# survivor case pass. scripts/coverage_blocks_test.sh unsets the same pair for
# the same reason.
unset MUTATION_BASE MUTATION_EXEMPT

scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
checker="$scripts_dir/mutation.sh"

failures=0
checks=0

fail() {
	failures=$((failures + 1))
	echo "FAIL: $1"
	[ -n "${2:-}" ] && printf '%s\n' "$2" | sed 's/^/      /'
	return 0
}
pass() { echo "ok   $1"; }

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

# new_repo — an empty git repository with a go.mod and one commit on `main`, so
# there is always a base ref to diff against. Prints its path.
#
# `mktemp -d`, not a counter: this runs in a command substitution, so a counter
# would be incremented in a subshell and never seen by the caller.
new_repo() {
	repo=$(mktemp -d "$work/repoXXXXXX")
	mkdir -p "$repo/pkg"
	(
		cd "$repo" || exit 1
		git init -q -b main .
		git config user.email dev@example.com
		git config user.name dev
		git config commit.gpgsign false
		printf 'module example\n\ngo 1.25\n' > go.mod
		git add go.mod
		git commit -qm base
		# The gate diffs HEAD against a base ref. Committing onto `main`
		# itself would make the merge base equal HEAD, and every case would
		# pass for the wrong reason — "nothing changed" rather than "nothing
		# survived".
		git checkout -qb feature
	) || return 1
	printf '%s\n' "$repo"
}

# commit_on_main <repo> <message> — commit pkg/ onto `main` and re-branch, so
# those files are part of the base rather than of the branch under test.
# Without this, a file meant to be pre-existing is added BY the branch, every
# line of it counts as changed, and a case meant to prove the gate ignores
# untouched code proves nothing.
commit_on_main() {
	(
		cd "$1" || exit 1
		git checkout -q main
		git add -A pkg
		git commit -qm "$2"
		git branch -f feature main
		git checkout -q feature
	) > /dev/null 2>&1
}

commit_on_branch() {
	(
		cd "$1" || exit 1
		git add -A
		git commit -qm "$2"
	) > /dev/null 2>&1
}

# report <file> <json-body> — write a gremlins --output report.
write_report() {
	printf '%s\n' "$2" > "$1"
}

run_checker() {
	rc_repo=$1
	shift
	(cd "$rc_repo" && sh "$checker" "$@" 2>&1)
}

# findings_only <output> — the report down to the first trailing section.
# A survivor that was waived, and a mutant that was never decided, are both
# still PRINTED — under "Exempt by" and "Undecided" — so grepping the whole
# output for a line number cannot tell "not reported as a finding" from
# "reported". The section boundary is what carries that distinction, and BOTH
# headers are boundaries: stopping only at "Exempt by" meant a report with no
# exempt section returned everything, and every "is not a finding" assertion
# against it passed for the wrong reason.
#
# awk rather than sed: the multi-header form needs alternation, and BRE's `\|` is
# a GNU extension that macOS's sed does not have — the portability class of bug
# #233 fixed in scenarios_check.sh.
#
# EVERY trailing section is a boundary, and this has now been wrong twice for the
# same reason: a header it does not know about is one whose rows leak back into
# what this returns, and every "is not a finding" assertion against them passes
# for the wrong reason. "Skipped by gremlins" is the third. A fourth section
# needs a fourth arm here in the same change.
findings_only() {
	printf '%s\n' "$1" | awk '/^  Exempt by/ || /^  Undecided/ || /^  Skipped by/ { exit } { print }'
}

# ── 1. Diff scoping ───────────────────────────────────────────────────────────

# The survivor that must NOT be reported sits in the SAME file as the one that
# must — on a line the branch did not touch. A separate untouched file would
# only exercise the file-level filter, which is a different mechanism: dropping
# the line-level check entirely would still pass such a case, since an untouched
# file never enters the loop at all. Confirmed by perturbation.
checks=$((checks + 1))
repo=$(new_repo)
cat > "$repo/pkg/mixed.go" <<'EOF'
package pkg

func Old(n int) bool {
	if n > 3 {
		return true
	}
	return false
}
EOF
commit_on_main "$repo" "pre-existing"
cat > "$repo/pkg/mixed.go" <<'EOF'
package pkg

func Old(n int) bool {
	if n > 3 {
		return true
	}
	return false
}

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/mixed.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5},
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":11,"column":5}]}]}'
commit_on_branch "$repo" "append New to mixed.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 1 ]; then
	fail "a survivor on a changed line fails the build" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'pkg/mixed.go:11'; then
	fail "a survivor on a changed line is reported" "$out"
elif printf '%s' "$out" | grep -q 'pkg/mixed.go:4'; then
	fail "a survivor on an UNCHANGED line of a CHANGED file is not reported" "$out"
elif ! printf '%s' "$out" | grep -q '1 surviving mutant'; then
	fail "exactly one of the two survivors is reported" "$out"
else
	pass "reports survivors on changed lines only"
fi

# ── 2. A skipped mutant on a changed line is reported as undecided ────────────
#
# The fail-open this closes: `gremlins --diff` decides for itself what changed.
# If its answer is narrower than the gate's, the mutants it skipped were never
# run, and counting them as "no survivor" states a result nothing measured.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"SKIPPED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
write_report "$repo/rep2.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"SKIPPED","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"KILLED","line":4,"column":5}]}]}'
out=$(run_checker "$repo" --base main --report rep2.json)
rc=$?
if ! printf '%s' "$out" | grep -q 'Skipped by gremlins'; then
	fail "a SKIPPED mutant on a changed line is reported" "$out"
elif ! printf '%s' "$out" | grep -q 'pkg/new.go:4.*SKIPPED'; then
	fail "the list says WHY THIS mutant has no verdict" "$out"
elif [ "$rc" -ne 0 ]; then
	# NOT a failure on its own, and this is the one status where that is the
	# answer. gremlins sets SKIPPED from its own diff alone
	# (internal/engine/engine.go: `if !Diff.IsChanged(pos) { status = Skipped }`)
	# and its changed-line window is an approximation — internal/diff/diff.go
	# takes each hunk's added lines to run contiguously from the fragment's
	# start. Measured on #231's real report: two mutants on a line that branch
	# added came back SKIPPED because git's `@@ -50 +51,11 @@` covers line 58
	# and gremlins' window does not. There is no edit to that line that changes
	# it, so failing would be a red build nobody can act on.
	fail "a SKIPPED mutant does not fail the build on its own" "exit $rc: $out"
elif printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:4'; then
	fail "a SKIPPED mutant is not claimed as a survivor" "$out"
else
	pass "a SKIPPED mutant is reported without failing the build"
fi

# ── 2b. …unless gremlins skipped everything ───────────────────────────────────
#
# The fail-open the case above would otherwise open. One skipped mutant is two
# diff implementations disagreeing about one hunk. A run in which NOTHING
# reached a verdict is "no surviving mutants" resting on nothing measured, which
# is the sentence this script's header forbids. The condition is total rather
# than a proportion on purpose: any threshold below "nothing was decided" would
# be a number nobody could defend.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"SKIPPED","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"SKIPPED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 1 ]; then
	fail "a run in which every scoped mutant was skipped fails" "exit $rc: $out"
elif ! printf '%s\n' "$out" | head -1 | grep -q 'FAIL'; then
	fail "the headline says the gate failed" "$out"
elif ! printf '%s' "$out" | grep -qi 'nothing was decided'; then
	fail "the headline says nothing was decided, not that nothing survived" "$out"
else
	pass "a run in which gremlins skipped everything fails"
fi

# ── 3. NOT COVERED is left to the per-block gate, not double-reported ─────────
#
# G4(d) (scripts/coverage_blocks.sh) already fails on a block on a changed line
# that never executed. Reporting the same line again here would make two gates
# argue about one defect, and the one with the clearer message is the other one.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
# The KILLED mutant beside it is load-bearing, and not padding. This case is
# about NOT COVERED not being reported as a finding, which is a claim about the
# MUTANT. A report containing nothing but the NOT COVERED one is also a run that
# reached no verdict about anything, which fails for a different reason entirely
# (case 43) and would make this case assert the two rules at once.
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"NOT COVERED","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"KILLED","line":4,"column":9}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
# Asserting the clean message as well as the absence: a bare "does not contain
# 'survivor'" would also hold for a script that failed to run at all.
if ! printf '%s' "$out" | grep -q 'no surviving'; then
	fail "a NOT COVERED mutant leaves the gate reporting a clean branch" "$out"
elif printf '%s' "$out" | grep -q 'pkg/new.go:4'; then
	fail "NOT COVERED is not reported as a survivor" "$out"
else
	pass "NOT COVERED is left to the per-block gate"
fi

# ── 4. //mutation:exempt ──────────────────────────────────────────────────────

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY] buffer size, a test pinning it would be a tautology
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q 'no surviving mutants'; then
	fail "an exempt survivor leaves no findings" "$out"
elif printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:5'; then
	fail "an exempt survivor is not reported as a finding" "$out"
elif ! printf '%s' "$out" | grep -q 'Exempt by'; then
	fail "an exempt survivor is still listed, so a reviewer can see it" "$out"
elif ! printf '%s' "$out" | grep -q 'pkg/new.go:5'; then
	fail "the exempt list names the waived line" "$out"
else
	pass "//mutation:exempt on the line above waives that survivor, and lists it"
fi

# ── 5. A bare marker exempts nothing ──────────────────────────────────────────
#
# Same rule as //coverage:exempt: "an exemption nobody has to justify is how an
# exemption stops being reviewable."

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
# It has to land in FINDINGS, not merely appear somewhere: a script that
# honoured the reasonless marker would still print the line, under "Exempt by".
if ! printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:5'; then
	fail "a bare //mutation:exempt exempts nothing" "$out"
elif ! printf '%s' "$out" | grep -q 'no reason after it exempts nothing'; then
	# The REASON note, specifically. A marker with neither a reason nor a type
	# breaks two rules at once, and both notes end in "exempts nothing" — so a
	# looser grep passes on the untyped note alone, and stops saying anything
	# about the reason rule. Confirmed by perturbation: deleting the reason
	# check left the looser form green.
	fail "a bare //mutation:exempt is called out for the missing REASON" "$out"
else
	pass "a bare //mutation:exempt exempts nothing"
fi

# ── 6. The exemption window does not leak onto the next line ──────────────────
#
# The bug #188 fixed in its own marker: reading `line-1`..`line` as one string
# let a marker on one construct's OPENING line also waive the construct on the
# next. Reproducing it needs the marker INLINE on a mutated line, with another
# mutated line directly below — a marker on its own comment line two rows up
# cannot reach the second construct whether the window leaks or not, so a
# fixture in that shape passes either way. Confirmed by perturbation.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n, m int) bool {
	if n > 7 { //mutation:exempt[CONDITIONALS_BOUNDARY] only the outer test is a tuning constant
		if m > 9 {
			return true
		}
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5},
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":6}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q '1 surviving mutant'; then
	fail "exactly one of the two survivors is waived" "$out"
elif printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:4'; then
	fail "the exempt line itself is waived" "$out"
elif ! printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:5'; then
	fail "the line BELOW an INLINE marker is still reported as a finding" "$out"
else
	pass "an inline exemption does not leak onto the next line"
fi

# ── 7. MUTATION_EXEMPT waives the branch, and says how many ───────────────────

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(cd "$repo" && MUTATION_EXEMPT=1 sh "$checker" --base main --report rep.json 2>&1)
if ! printf '%s' "$out" | grep -q 'MUTATION_EXEMPT'; then
	fail "MUTATION_EXEMPT=1 says it waived the branch" "$out"
elif ! printf '%s' "$out" | grep -q '1'; then
	fail "MUTATION_EXEMPT=1 says how many findings it waived" "$out"
else
	pass "MUTATION_EXEMPT=1 waives the branch and says how many"
fi

# ── 8. No Go implementation changed ───────────────────────────────────────────

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
printf 'docs only\n' > "$repo/README.md"
write_report "$repo/rep.json" '{"go_module":"example","files":[]}'
commit_on_branch "$repo" "docs"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "a docs-only branch exits 0" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'no Go implementation changed'; then
	fail "a docs-only branch says why it did nothing" "$out"
else
	pass "a docs-only branch skips, and says so"
fi

# ── 9. Test files are not the subject ─────────────────────────────────────────
#
# Mutating a _test.go file asks whether the tests test the tests. The red-check
# (G4(b)) is what judges changed tests; this gate judges changed implementation.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new_test.go" <<'EOF'
package pkg

import "testing"

func TestSomething(t *testing.T) {
	if 1 > 0 {
		t.Log("ok")
	}
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[]}'
commit_on_branch "$repo" "test only"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q 'no Go implementation changed'; then
	fail "a test-only branch is not the subject of this gate" "$out"
else
	pass "a test-only branch is left to the red-check"
fi

# ── 10. Could not run: missing base ref ───────────────────────────────────────
#
# "Could not check" is a failure, never a skip — the rule scripts/efficacy.sh
# and scripts/coverage_blocks.sh both state. A required check that decided
# nothing must not look like one that passed.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
printf 'package pkg\n\nfunc F() {}\n' > "$repo/pkg/new.go"
write_report "$repo/rep.json" '{"go_module":"example","files":[]}'
commit_on_branch "$repo" "add"
out=$(run_checker "$repo" --base does-not-exist --report rep.json)
rc=$?
if [ "$rc" -eq 0 ]; then
	fail "a missing base ref fails rather than skipping" "$out"
elif ! printf '%s' "$out" | grep -q 'does-not-exist'; then
	fail "a missing base ref names the ref" "$out"
else
	pass "a missing base ref fails, naming the ref"
fi

# ── 11. Could not run: missing report ─────────────────────────────────────────

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
printf 'package pkg\n\nfunc F(n int) bool { return n > 1 }\n' > "$repo/pkg/new.go"
commit_on_branch "$repo" "add"
out=$(run_checker "$repo" --base main --report absent.json)
rc=$?
# The message matters as much as the code: exit 127 from a missing script is
# also non-zero, and a case that accepted it would pass before this gate
# existed.
if [ "$rc" -eq 0 ]; then
	fail "a missing report fails rather than reporting no survivors" "$out"
elif ! printf '%s' "$out" | grep -q 'mutation: ERROR'; then
	fail "a missing report says what is wrong" "$out"
else
	pass "a missing report fails rather than reporting no survivors"
fi

# ── 12. Could not run: unparseable report ─────────────────────────────────────
#
# An empty or truncated report is what a killed gremlins run leaves behind. It
# has no `files` key, which must not read as "no survivors".

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
printf 'package pkg\n\nfunc F(n int) bool { return n > 1 }\n' > "$repo/pkg/new.go"
write_report "$repo/rep.json" 'not json at all'
commit_on_branch "$repo" "add"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -eq 0 ]; then
	fail "an unparseable report fails rather than reporting no survivors" "$out"
elif ! printf '%s' "$out" | grep -q 'mutation: ERROR'; then
	fail "an unparseable report says what is wrong" "$out"
else
	pass "an unparseable report fails rather than reporting no survivors"
fi

# ── 13. A clean branch says so ────────────────────────────────────────────────

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "a branch whose mutants were all killed exits 0" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'no surviving'; then
	fail "a clean branch says every mutant was killed" "$out"
else
	pass "a branch whose mutants were all killed says so"
fi

# ── 14. Module-prefixed paths in the report resolve ───────────────────────────
#
# gremlins reports repository-relative paths, but the profile-style
# `<module>/<path>` spelling is what coverage.out uses and what a future
# gremlins might. Accepting both costs one substitution; guessing wrong makes
# every finding silently unmatched, which is this gate reporting green.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"example/pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q 'pkg/new.go:4'; then
	fail "a module-prefixed path in the report resolves to a repository path" "$out"
else
	pass "a module-prefixed path in the report resolves"
fi

# ── 15. Runs from a subdirectory ──────────────────────────────────────────────
#
# The third fail-open #188 found: `git diff --name-only` prints
# repository-relative paths wherever it runs, but a pathspec after `--` resolves
# against the caller's cwd. From a subdirectory every pathspec misses, every
# touched-line set comes back empty, and the gate reports nothing having
# measured nothing. No root-only case can see this.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(cd "$repo/pkg" && sh "$checker" --base main --report ../rep.json 2>&1)
if ! printf '%s' "$out" | grep -q 'pkg/new.go:4'; then
	fail "the gate works when run from a subdirectory" "$out"
else
	pass "the gate works when run from a subdirectory"
fi

# ── 16. The summary counts survivors, not mutations ───────────────────────────

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n, m int) bool {
	if n > 7 {
		return true
	}
	if m > 9 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"LIVED","line":4,"column":5},
   {"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":7,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q '2 surviving'; then
	fail "the summary counts surviving mutants" "$out"
else
	pass "the summary counts surviving mutants"
fi

# ── 17. TIMED OUT is undecided, not silently dropped ──────────────────────────
#
# The same fail-open case 2 closes for SKIPPED, in the status where it bites
# hardest. A timed-out mutant was never given a verdict: the suite did not
# finish, so nothing is known about whether it would have been caught. #180's
# measurement is the evidence this is not hypothetical — with gremlins' default
# settings 465 of 1059 runnable mutants on this repository came back TIMED OUT,
# worker contention rather than infinite loops, and clearing them revealed 51
# survivors the timed-out run had reported nothing about. The pinned settings
# make that rare; they do not make it impossible, and a rare unknown reported as
# a verdict is worse than a common one.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"TIMED OUT","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 1 ]; then
	fail "a TIMED OUT mutant fails the build at stage 4" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'pkg/new.go:4'; then
	fail "a TIMED OUT mutant on a changed line is reported at all" "$out"
elif ! printf '%s' "$out" | grep -q 'pkg/new.go:4.*TIMED OUT'; then
	fail "the undecided list says WHY THIS mutant has no verdict" "$out"
elif printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:4'; then
	fail "a TIMED OUT mutant is undecided, not claimed as a survivor" "$out"
else
	pass "a TIMED OUT mutant on a changed line is reported as undecided"
fi

# ── 18. NOT VIABLE is a verdict, and not this gate's business ─────────────────
#
# The complement of case 17, and the reason "report every status this script
# does not act on" would be the wrong fix. A NOT VIABLE mutant did not compile,
# so no test could ever have noticed it behaving differently — there is no hole
# in the suite to report and nothing for a developer to do. Listing it would put
# noise in the one section whose whole value is that everything in it is
# genuinely unknown.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
# The KILLED mutant beside it is load-bearing, for the reason case 3 states: a
# report of nothing but the NOT VIABLE one is also a run that decided nothing,
# and this case is about the mutant rather than about the run.
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"NOT VIABLE","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"KILLED","line":4,"column":9}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q 'no surviving'; then
	fail "a NOT VIABLE mutant leaves the gate reporting a clean branch" "$out"
elif printf '%s' "$out" | grep -q 'pkg/new.go:4'; then
	fail "a NOT VIABLE mutant is not reported as undecided" "$out"
elif printf '%s' "$out" | grep -q 'undecided'; then
	fail "a NOT VIABLE mutant does not make the branch look undecided" "$out"
else
	pass "a NOT VIABLE mutant is neither a survivor nor an unknown"
fi

# ── 19. A status this gate does not recognise is undecided ────────────────────
#
# The fail-open that outlives every status named in this file. gremlins may add
# a status, or rename one, and the catch-all arm that used to swallow TIMED OUT
# would swallow that one too — silently, since a status nobody matched simply
# did not appear in the output. A gate cannot claim a mutant was killed by a
# verdict string it has never seen.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"ESCAPED IN A LATER RELEASE","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q 'pkg/new.go:4'; then
	fail "an unrecognised status is reported rather than silently dropped" "$out"
elif ! printf '%s' "$out" | grep -q 'pkg/new.go:4.*ESCAPED IN A LATER RELEASE'; then
	fail "an unrecognised status is quoted back, so a reader can act on it" "$out"
else
	pass "a status this gate does not recognise is reported as undecided"
fi

# ── 20. "Decided nothing" does not read as "found nothing" ────────────────────
#
# The headline is the line a reviewer reads; the sections below it are the line
# they read next, if at all. "no surviving mutants on lines this branch changed"
# is a true sentence about a run in which nothing was decided, and a false
# impression. The count has to be in the headline itself.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n, m int) bool {
	if n > 7 {
		return true
	}
	if m > 9 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"TIMED OUT","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"TIMED OUT","line":4,"column":5},
   {"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":7,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
headline=$(printf '%s\n' "$out" | head -1)
# The phrasing, not the digits. `grep -q '2'` and `grep -q '3'` cannot tell the
# numerator from the denominator, nor either from an unrelated number: a
# regression that swapped the operands, or took the denominator from the wrong
# counter and wrote "23", passed the three arms this replaces.
#
# The denominator moved into the scope note in this change, so the two numbers
# now sit in different clauses of the same sentence — which is why the whole
# sentence is pinned rather than either half.
if ! printf '%s' "$headline" | grep -q 'among 3 on lines this branch changed, 2 undecided'; then
	fail "the headline says how many mutants were undecided, of how many" "$out"
else
	pass "the headline reports undecided mutants, not just the sections below it"
fi

# ── 21. --help prints the header, all of it and nothing else ─────────────────
#
# It used to print a hand-counted line range, and the range was exact for the
# header it was written against — `124a64d`'s header ended at line 80 and the
# range was `2,80p`. Growing the header is what breaks it, and it breaks in the
# quiet direction: the range TRUNCATES. Against this branch's longer header the
# old range stops mid-sentence in "PINNED GREMLINS SETTINGS", printing neither
# `Exit codes:` nor any line of code.
#
# So the assertion is the LAST line, not the presence of a marker. It goes red
# in both directions at once: a range that stops short ends on some other
# sentence, and a range that overshoots ends on `set -u`. An earlier draft of
# this case grepped for `^set -u` instead and claimed the stale range had
# leaked it — neither the claim nor the assertion survived checking. The leak
# had happened, but to a hand-count made WHILE writing this change, not to the
# range in the repository.

checks=$((checks + 1))
out=$(sh "$checker" --help 2>&1)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "--help exits 0" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'UNDECIDED'; then
	fail "--help prints the middle of the header, not just its ends" "$out"
elif ! printf '%s\n' "$out" | grep -v '^[[:space:]]*$' | tail -1 | grep -q 'the gate could not run\.$'; then
	# The LAST line, because that one assertion carries both failure modes:
	# stopping short ends on another sentence, overshooting ends on `set -u`.
	#
	# It anchors on the closing words rather than on "Exit codes:", which is
	# where the same sentence began when it fitted on one line. Pinning the
	# opening of a paragraph cannot see a header that grows a line after it; the
	# end of the last sentence can. That this needed changing at all is the
	# assertion working: the exit-code contract gained a clause in this change
	# and wrapped onto a second line.
	fail "--help ends exactly at the header's last line" "$out"
else
	pass "--help prints the whole header and only the header"
fi

# ── 22. A marker waives the mutant TYPE it names ─────────────────────────────
#
# The line-scoped waiver is what #180's judgement note 3 records as unresolved:
# the marker matched file and line and never the type, so a reason written about
# the boundary mutant waived every other mutant gremlins produced on that line.
# Measured on this repository's own eleven markers, every marked line carries
# one to three further types (CONDITIONALS_NEGATION on all eleven, plus
# ARITHMETIC_BASE and INVERT_NEGATIVES on three) — all killed today, so nothing
# was hidden, but all eleven were waived by a reason that describes none of them.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY] at n == 7 the caller cannot tell the two apart
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q 'no surviving mutants'; then
	fail "a marker naming the mutant's own type waives it" "$out"
elif ! printf '%s' "$out" | grep -q 'Exempt by'; then
	fail "the waived mutant is still listed" "$out"
else
	pass "//mutation:exempt[TYPE] waives the type it names"
fi

# ── 23. A marker does NOT waive a type it does not name ──────────────────────
#
# The defect itself. Same line, same marker, a different mutant — and under the
# old file+line match this was silently waived, with nothing in the diff or the
# output to show for it. At stage 4 that is the difference between a red gate
# and a green one.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY] at n == 7 the caller cannot tell the two apart
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"LIVED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q '1 surviving mutant'; then
	fail "exactly one of the two mutants on the line is waived" "$out"
elif ! printf '%s' "$(findings_only "$out")" | grep -q 'CONDITIONALS_NEGATION'; then
	fail "a mutant of a type the marker does not name is still a finding" "$out"
elif printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:5.*CONDITIONALS_BOUNDARY'; then
	# The finding ROW, not the explanatory note under it — the note names the
	# waived type on purpose, and a bare grep of the section matches that too.
	fail "the type the marker DOES name is still waived" "$out"
elif ! printf '%s' "$out" | grep -q 'names CONDITIONALS_BOUNDARY'; then
	fail "the finding says the line carries a marker for another type" "$out"
else
	pass "//mutation:exempt[TYPE] does not waive a type it does not name"
fi

# ── 24. [*] waives the whole line, and says that it did ──────────────────────
#
# The line-wide waiver does not disappear; it stops being the accidental default
# and becomes something written down. A reviewer who sees `[*]` knows the claim
# covers mutants nobody has looked at, which is exactly what the old untyped
# form did without saying so.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[*] a tuning constant, every mutant here pins a number
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5},
   {"type":"ARITHMETIC_BASE","status":"LIVED","line":5,"column":9}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q 'no surviving mutants'; then
	fail "[*] waives every mutant on the line" "$out"
elif ! printf '%s' "$out" | grep -q '2 exempt'; then
	fail "[*] waives both mutants, not just the first" "$out"
elif ! printf '%s' "$out" | grep -q 'line-wide'; then
	fail "the exempt list marks a [*] waiver as line-wide" "$out"
else
	pass "//mutation:exempt[*] waives the line and is labelled as doing so"
fi

# ── 25. A list names several types, and only those ───────────────────────────

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY, ARITHMETIC_BASE] both pin the same tuning constant
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5},
   {"type":"ARITHMETIC_BASE","status":"LIVED","line":5,"column":9},
   {"type":"INVERT_NEGATIVES","status":"LIVED","line":5,"column":9}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -q '1 surviving mutant'; then
	fail "a two-type list waives exactly those two" "$out"
elif ! printf '%s' "$(findings_only "$out")" | grep -q 'INVERT_NEGATIVES'; then
	fail "the type outside the list is still a finding" "$out"
else
	pass "//mutation:exempt[A, B] waives A and B and nothing else"
fi

# ── 26. An untyped marker waives nothing ─────────────────────────────────────
#
# Same rule the reasonless marker has always had, for the same reason: a waiver
# whose scope nobody stated is one nobody reviewed. It is a contract change, so
# it has to be loud — the eleven markers in this repository were all rewritten
# in the change that introduced it.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt a perfectly good reason, with no type to attach it to
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:5'; then
	fail "an untyped //mutation:exempt waives nothing" "$out"
elif ! printf '%s' "$out" | grep -q 'mutation:exempt\[' ; then
	fail "the run says what the typed form looks like" "$out"
else
	pass "an untyped //mutation:exempt waives nothing, and says what to write"
fi

# ── 27. A typed marker still needs a reason ──────────────────────────────────
#
# The new syntax must not become a way around the older rule. `[TYPE]` says
# WHICH mutant is waived; the reason says WHY, and neither substitutes for the
# other.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY]
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:5'; then
	fail "a typed marker with no reason exempts nothing" "$out"
elif ! printf '%s' "$out" | grep -q 'exempts nothing'; then
	fail "a reasonless typed marker is called out" "$out"
else
	pass "a typed //mutation:exempt still needs a reason"
fi

# ── 28. An unclosed [ is reported, not read as an untyped marker ─────────────
#
# A typo in the one part of the marker a reader skims. Read as untyped it would
# waive nothing either, so the behaviour is the same — but the message is not,
# and the message is the whole difference between a fixable typo and a
# mysteriously ineffective waiver.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY at n == 7 nothing can tell them apart
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:5'; then
	fail "an unclosed [ waives nothing" "$out"
elif ! printf '%s' "$out" | grep -qi 'unclosed'; then
	fail "an unclosed [ is named as the problem" "$out"
else
	pass "an unclosed [ is reported as a malformed marker"
fi

# ── 29. An empty entry in the type list matches nothing ──────────────────────
#
# `[CONDITIONALS_BOUNDARY,]` — a trailing comma, the easiest typo in the new
# syntax — splits into a second, empty type. The report parser accepts a
# mutation whose `type` is absent (it requires only file, line and status), so
# an empty type CAN reach the matcher, and an empty entry matching an empty type
# is the one shape where this marker waives a mutant nobody wrote a word about.
# Found by re-reading the diff, not by a failing case: it waived silently.
#
# Only [*] may waive a mutant whose type is unknown, and it says so.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY,] a trailing comma, easily typed
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"status":"LIVED","line":5,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"LIVED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:5'; then
	fail "an empty type entry does not waive a mutant with no reported type" "$out"
elif ! printf '%s' "$out" | grep -q '2 surviving mutant'; then
	fail "neither mutant on the line is waived" "$out"
elif ! printf '%s' "$out" | grep -q 'names CONDITIONALS_BOUNDARY$'; then
	# Anchored, because the empty entry must not reach the note either: without
	# the guard the list reads "CONDITIONALS_BOUNDARY, " with a dangling
	# separator, and a message about a typo should not contain one.
	fail "the empty entry is left out of the list the note names" "$out"
else
	pass "an empty entry in the type list matches nothing and is not named"
fi

# ── 30. A run that analysed nothing does not read as a clean branch ──────────
#
# The last instance of the rule the header states, and the one #235 did not
# reach. #235 made "could not decide" visible one mutant at a time; this is the
# case where there was no mutant to decide about at all, and the old headline
# for it was BYTE-IDENTICAL to a run that analysed mutants and killed them all.
#
# Measured, not hypothetical: #234 — six non-test Go files, 35 hunks, 317
# changed lines — put exactly 5 mutants on a changed line out of 128 in those
# files. A gate that cannot tell 5 from 0 cannot be given the power to fail.
#
# The changed file HAS mutants here; none sits on a changed line. That is the
# shape the line-scope produces, so the message has to name both numbers.

checks=$((checks + 1))
repo=$(new_repo)
cat > "$repo/pkg/mixed.go" <<'EOF'
package pkg

func Old(n int) bool {
	if n > 3 {
		return true
	}
	return false
}
EOF
commit_on_main "$repo" "pre-existing"
cat > "$repo/pkg/mixed.go" <<'EOF'
package pkg

func Old(n int) bool {
	if n > 3 {
		return true
	}
	return false
}

type Added struct {
	Name  string
	Kinds map[string]bool
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/mixed.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"KILLED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "append a struct declaration"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "a branch with nothing to mutate still exits 0" "exit $rc: $out"
elif printf '%s' "$out" | grep -q 'no surviving mutants'; then
	fail "analysing nothing does not report as 'no surviving mutants'" "$out"
elif ! printf '%s' "$out" | grep -qi 'nothing was measured'; then
	fail "a run that analysed nothing says so" "$out"
elif ! printf '%s' "$out" | grep -q '2'; then
	fail "the message names how many mutants the touched files did hold" "$out"
elif printf '%s' "$out" | grep -q 'pkg/mixed.go:4'; then
	fail "the mutants on untouched lines are not reported as findings" "$out"
else
	pass "a run that analysed nothing says so instead of reporting a clean branch"
fi

# ── 31. The headline carries the denominator ─────────────────────────────────
#
# "no surviving mutants" answers a question whose size the reader cannot see.
# Five analysed and fifty analysed are different evidence for the same
# sentence, and stage 4 — whether a survivor should fail the build — cannot be
# decided without knowing which one a typical branch produces.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n, m int) bool {
	if n > 7 {
		return true
	}
	if m > 9 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"KILLED","line":4,"column":5},
   {"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":7,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
headline=$(printf '%s\n' "$out" | head -1)
if ! printf '%s' "$headline" | grep -q 'no surviving'; then
	fail "a branch whose mutants were all killed still says so" "$out"
elif ! printf '%s' "$headline" | grep -q '3'; then
	fail "the headline says how many mutants that verdict rests on" "$out"
else
	pass "the headline carries the denominator, not only the verdict"
fi

# ── 32. A survivor headline carries it too ───────────────────────────────────
#
# "2 survivors" out of 2 and out of 200 are different branches. The denominator
# belongs on both headlines or on neither.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n, m int) bool {
	if n > 7 {
		return true
	}
	if m > 9 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"KILLED","line":4,"column":5},
   {"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":7,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
headline=$(printf '%s\n' "$out" | head -1)
if ! printf '%s' "$headline" | grep -q '1 surviving mutant'; then
	fail "the survivor count is still reported" "$out"
elif ! printf '%s' "$headline" | grep -q '3'; then
	fail "the survivor headline says how many mutants were analysed" "$out"
else
	pass "the survivor headline carries the denominator"
fi

# ── 33. A deletion-only file still counts toward what the files held ────────
#
# `touched_lines` reports the lines a diff ADDS, so a file this branch only
# deleted from has an empty set and the loop skips it. The file-level counter
# sat after that skip, so such a file contributed nothing — and a zero-scope run
# then said "gremlins produced no mutants at all in the files this branch
# touched" about a file that still holds plenty. The sentence names the files
# the branch touched, and a file it deleted from is one of them.
#
# Reported by an automated reviewer on #237; the mechanism was confirmed by
# reading the loop before this case was written.

checks=$((checks + 1))
repo=$(new_repo)
cat > "$repo/pkg/del.go" <<'EOF'
package pkg

func Keep(n int) bool {
	if n > 7 {
		return true
	}
	return false
}

func Drop(n int) bool {
	return n > 1
}
EOF
commit_on_main "$repo" "pre-existing"
cat > "$repo/pkg/del.go" <<'EOF'
package pkg

func Keep(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/del.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "drop the Drop function"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "a deletion-only branch exits 0" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -qi 'nothing was measured'; then
	fail "a deletion-only branch measured nothing and says so" "$out"
elif printf '%s' "$out" | grep -q 'no mutants at all'; then
	# The distinction the whole message exists to draw: the line scope threw
	# this file's mutants away, it did not find a file with none.
	fail "a file the branch only deleted from still counts toward the file total" "$out"
elif ! printf '%s' "$out" | grep -q 'produced 1 mutant'; then
	fail "the file total names the mutant that file still holds" "$out"
else
	pass "a deletion-only file counts toward what the touched files held"
fi

# ── 34. RUNNABLE is a documented status, and is named as one ─────────────────
#
# gremlins defines SEVEN statuses, not six, and RUNNABLE is the one an earlier
# draft of this change miscounted away: `internal/mutator/mutator.go` lists
# NotCovered, Runnable, Skipped, Lived, Killed, NotViable, TimedOut. It means
# the mutant was identified and is covered, so it CAN be run — which is exactly
# "never reached a verdict", so the catch-all handles it correctly.
#
# What the catch-all cannot do is explain it. The trailer's "anything else is a
# status this gate does not recognise" sends a reader off to investigate a
# documented status as though a later gremlins had invented it. RUNNABLE is
# reachable in a real report — `--dry-run` emits every mutant without applying
# it (`internal/engine/executor.go` returns early on `m.dryRun`), so the status
# survives into the JSON — so this is a message a reader can actually meet.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"RUNNABLE","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 1 ]; then
	fail "a RUNNABLE mutant fails the build at stage 4" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'pkg/new.go:4.*RUNNABLE'; then
	fail "a RUNNABLE mutant on a changed line is reported as undecided" "$out"
elif ! printf '%s\n' "$out" | grep -v 'pkg/new.go' | grep -q 'RUNNABLE'; then
	# In the EXPLANATION, not only in the row. Naming it in the row is what the
	# catch-all already did; what this pins is that the trailer accounts for it
	# instead of calling it a status nobody recognises.
	fail "the explanation names RUNNABLE rather than calling it unrecognised" "$out"
else
	pass "RUNNABLE is reported as undecided and explained, not treated as unknown"
fi

# ── 35. A survivor fails the build ────────────────────────────────────────────
#
# Stage 4 of roadmap item 6 in #180. The exit code is the whole change: stages 1
# to 3 built a reporter nobody had to act on, and a warning nobody has to act on
# is a gate only in the sense that it occupies the slot where one would go.
#
# Pinned separately from case 1 rather than by editing it, because the two
# assert different things. Case 1 asserts WHICH mutants are reported — the line
# scope — and would pass with any exit code. This asserts the verdict.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 1 ]; then
	fail "a surviving mutant fails the build" "exit $rc: $out"
elif ! printf '%s\n' "$out" | head -1 | grep -q 'FAIL'; then
	# In the HEADLINE. An exit code is read by CI; the headline is read by the
	# person who has to do something about it, and "1 surviving mutant(s)" on
	# its own said the same thing when the answer was "carry on".
	fail "the headline says the gate failed, not only the exit code" "$out"
elif printf '%s' "$out" | grep -qi 'does not fail the build'; then
	fail "the trailer no longer calls itself a warning" "$out"
elif ! printf '%s' "$out" | grep -q 'mutation:exempt'; then
	fail "the trailer still says how to waive the one mutant" "$out"
else
	pass "a surviving mutant fails the build"
fi

# ── 36. An undecided mutant fails the build too ───────────────────────────────
#
# The fail-open that gating would otherwise open, and the reason #235 exists.
# Before stage 4 both arms exited 0, so "every mutant on the diff timed out" and
# "every mutant was killed" differed only in wording. Once a survivor fails, a
# run that reached no verdict would be the one remaining way to get a green tick
# out of a gate that decided nothing — and #180's measurement found timeouts
# hiding 51 survivors, so that is not a hypothetical.
#
# This follows the rule scripts/efficacy.sh states in its own header and this
# repository already lives with: "Could not check" is a failure, never a skip.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"TIMED OUT","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 1 ]; then
	fail "a mutant that reached no verdict fails the build" "exit $rc: $out"
elif ! printf '%s\n' "$out" | head -1 | grep -q 'FAIL'; then
	fail "the undecided headline says the gate failed" "$out"
elif printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:4'; then
	# Failing is not the same as calling it a survivor. The distinction #235
	# drew has to survive the exit code changing: this mutant is not evidence
	# of a missing assertion, it is evidence of a run that did not finish.
	fail "an undecided mutant fails without being claimed as a survivor" "$out"
else
	pass "an undecided mutant fails the build without becoming a survivor"
fi

# ── 37. The marker is what keeps a survivor green ─────────────────────────────
#
# The complement of 35, and the half that decides whether stage 4 is livable.
# 34% of this repository's survivors are ones nobody should "fix"; if the marker
# did not actually clear the exit code, the only escape would be the
# branch-wide label and the gate would be all-or-nothing.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY] a tuning constant, not a boundary
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"LIVED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "an exempted survivor does not fail the build" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'Exempt by'; then
	fail "the exempted survivor is still listed" "$out"
else
	pass "a waived survivor keeps the build green and is still listed"
fi

# ── 38. A waived line does not fail on a slow runner ──────────────────────────
#
# The false positive stage 4 introduces if the marker only ever reached the
# LIVED arm. A mutant the author has already waived by type is one they have
# said need not be killed; whether the runner reached a verdict about it is a
# fact about the runner, not about the code. Failing there would fail a build
# for a mutant nobody is asking anyone to kill — principle 4 in
# docs/quality-gateway.md, and the exact shape that gets a gate routed around.
#
# TIMED OUT is the status that makes this reachable in practice: #180's
# measurement found 44% of runnable mutants timing out under gremlins' own
# defaults, and this repository's eleven markers all sit on lines carrying one
# to three further types.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY] a tuning constant, not a boundary
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"TIMED OUT","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "a marker waives an undecided mutant of the type it names" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'TIMED OUT'; then
	# Waived is not hidden. The reader has to be able to see that the verdict
	# behind this waiver was "no verdict", not "killed".
	fail "the waived mutant still shows the status it reached" "$out"
else
	pass "a marker waives an undecided mutant of the type it names"
fi

# ── 39. A marker that names another type does not waive an undecided mutant ───
#
# The complement of 38, and what keeps it from being a hole: if any marker on
# the line silenced any undecided mutant, #236's whole point — that a waiver
# covers the type it names and not its neighbours — would hold for survivors
# and quietly not hold here.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY] a tuning constant, not a boundary
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_NEGATION","status":"TIMED OUT","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 1 ]; then
	fail "a marker for another type leaves an undecided mutant undecided" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'pkg/new.go:5.*CONDITIONALS_NEGATION'; then
	fail "the unwaived undecided mutant is still named" "$out"
else
	pass "a marker for another type does not waive an undecided mutant"
fi

# ── 40. The branch-wide label still clears an undecided-only branch ───────────
#
# MUTATION_EXEMPT=1 is the blunter tool and has to stay blunt: once undecided
# mutants can fail the build, a branch whose only finding is a timeout needs the
# same escape a branch full of survivors has. Case 7 covers the survivor half.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"TIMED OUT","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(cd "$repo" && MUTATION_EXEMPT=1 sh "$checker" --base main --report rep.json 2>&1)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "MUTATION_EXEMPT=1 clears a branch whose only finding is undecided" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'TIMED OUT'; then
	fail "the waived branch still shows what was undecided" "$out"
else
	pass "MUTATION_EXEMPT=1 clears an undecided-only branch"
fi

# ── 41. A clean branch says so in the same vocabulary ─────────────────────────
#
# Symmetry with 35: once one headline says FAIL, the passing one has to say
# something equally scannable, or "no FAIL in the output" becomes the way people
# read the result — which is exactly how a truncated or crashed run gets read as
# a pass. scripts/efficacy.sh already uses this pair.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"KILLED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "a clean branch exits 0" "exit $rc: $out"
elif ! printf '%s\n' "$out" | head -1 | grep -q 'ok'; then
	fail "a clean branch says ok in the headline" "$out"
elif printf '%s\n' "$out" | head -1 | grep -q 'FAIL'; then
	fail "a clean branch does not say FAIL" "$out"
else
	pass "a clean branch says ok, in the same vocabulary as the failure"
fi

# ── 43. "Nothing was decided" is not only about SKIPPED ───────────────────────
#
# Review finding, reproduced against the real script before being believed. The
# guard was keyed on `skipped_count > 0`, so a run whose every scoped mutant was
# NOT COVERED took none of the failing arms and printed "ok — no surviving
# mutants among 1", which is a clean verdict resting on nothing measured.
#
# NOT COVERED is G4(d)'s defect and this gate still does not report the mutants
# themselves — the claim being corrected is about the RUN, not the mutant. The
# case that makes it worth failing on: `make coverage-blocks` reports a changed
# file in a package COVERAGE_PKGS excludes as "not measured" rather than failing,
# so there are diffs where no other gate says anything either.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"NOT COVERED","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 1 ]; then
	fail "a run whose every mutant was NOT COVERED decided nothing and fails" "exit $rc: $out"
elif printf '%s\n' "$out" | head -1 | grep -q 'no surviving mutants'; then
	fail "it does not claim a clean verdict it never reached" "$out"
elif ! printf '%s' "$out" | grep -qi 'nothing was decided'; then
	fail "the headline says nothing was decided" "$out"
elif printf '%s' "$(findings_only "$out")" | grep -q 'pkg/new.go:4'; then
	# Still G4(d)'s mutant to report. The run failing and the mutant being
	# listed as a finding here are different things, and only the first changed.
	fail "the NOT COVERED mutant is still not reported as a finding" "$out"
else
	pass "a run that decided nothing fails even when nothing was skipped"
fi

# ── 44. …unless the author waived every one of them ───────────────────────────
#
# The other half of the same review finding, and the limit on 43. A marker is an
# explicit, typed, reviewable claim that a mutant need not be killed, and it
# does not depend on whether the mutant ran. If waiving every scoped mutant
# still failed, the marker would be powerless in exactly the run where the
# author has said the most about what they expect.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY] a tuning constant, not a boundary
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"SKIPPED","line":5,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 0 ]; then
	fail "waiving every scoped mutant clears the nothing-decided failure" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'Exempt by'; then
	fail "the waived mutant is still listed" "$out"
else
	pass "a run in which every scoped mutant was waived does not fail"
fi

# ── 45. A partial waiver does not clear it ────────────────────────────────────
#
# What keeps 44 from swallowing 43: "every scoped mutant was waived" has to mean
# every one. One waived mutant beside one that reached no verdict is still a run
# that decided nothing, and the author has spoken for only half of it.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n, m int) bool {
	//mutation:exempt[CONDITIONALS_BOUNDARY] a tuning constant, not a boundary
	if n > 7 {
		return true
	}
	if m > 9 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"CONDITIONALS_BOUNDARY","status":"SKIPPED","line":5,"column":5},
   {"type":"CONDITIONALS_BOUNDARY","status":"SKIPPED","line":8,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
rc=$?
if [ "$rc" -ne 1 ]; then
	fail "one waiver among two undecided mutants does not clear the run" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -qi 'nothing was decided'; then
	fail "the headline still says nothing was decided" "$out"
else
	pass "waiving some but not all scoped mutants still fails"
fi

# ── 46. The branch-wide label says what it actually waived ────────────────────
#
# Review finding. MUTATION_EXEMPT=1 on a run whose only failure was an undecided
# mutant printed "waived 0 finding(s)" — while being the only reason the run
# exited 0. A label that reports waiving nothing, in the run it rescued, is the
# same class of false statement this whole gate is about.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"TIMED OUT","line":4,"column":5}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(cd "$repo" && MUTATION_EXEMPT=1 sh "$checker" --base main --report rep.json 2>&1)
rc=$?
headline=$(printf '%s\n' "$out" | head -1)
if [ "$rc" -ne 0 ]; then
	fail "MUTATION_EXEMPT=1 still clears the run" "exit $rc: $out"
elif ! printf '%s' "$headline" | grep -q '1 undecided mutant'; then
	# The quantity that was missing. "waived 0 finding(s)" on its own was the
	# whole sentence, so the label reported waiving nothing in the run that
	# would have been red without it. `grep -q 'waived 0'` is NOT the assertion
	# to write here: "waived 0 finding(s) and 1 undecided mutant(s)" contains it
	# and is correct — there were no findings. What has to be present is the
	# count that explains the exit code.
	fail "the label names the undecided mutant it waived" "$out"
elif ! printf '%s' "$out" | grep -q 'waived the failure for deciding nothing'; then
	fail "the label says it also waived the nothing-decided failure" "$out"
else
	pass "MUTATION_EXEMPT=1 counts what it actually waived"
fi

# ── 47. The undecided explanation describes only what can be in that list ─────
#
# Review finding. SKIPPED moved to its own section and its own consequence, but
# the undecided trailer still opened with "SKIPPED means …" — so the first thing
# a reader saw about their timeout explained a status that cannot appear there,
# and contradicted the section directly below it about whether it fails.

checks=$((checks + 1))
repo=$(new_repo)
commit_on_main "$repo" "empty base"
cat > "$repo/pkg/new.go" <<'EOF'
package pkg

func New(n int) bool {
	if n > 7 {
		return true
	}
	return false
}
EOF
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[
   {"type":"INVERT_NEGATIVES","status":"TIMED OUT","line":4,"column":5},
   {"type":"CONDITIONALS_NEGATION","status":"KILLED","line":4,"column":9}]}]}'
commit_on_branch "$repo" "add new.go"
out=$(run_checker "$repo" --base main --report rep.json)
undecided_section=$(printf '%s\n' "$out" | awk '/^  Undecided/ { on = 1 } /^  Skipped by/ { on = 0 } on { print }')
if [ -z "$undecided_section" ]; then
	fail "the undecided section is present to check" "$out"
elif printf '%s' "$undecided_section" | grep -q 'SKIPPED'; then
	fail "the undecided explanation does not describe SKIPPED" "$undecided_section"
elif ! printf '%s' "$undecided_section" | grep -q 'TIMED OUT'; then
	fail "the undecided explanation still describes TIMED OUT" "$undecided_section"
else
	pass "the undecided explanation covers only statuses that can appear there"
fi

# ── Result ────────────────────────────────────────────────────────────────────

echo
if [ "$failures" -gt 0 ]; then
	echo "$failures of $checks mutation checks failed"
	exit 1
fi
echo "all $checks mutation checks passed"
