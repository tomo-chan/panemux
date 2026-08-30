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
#   2. Reporting what gremlins SKIPPED on a changed line. `--diff` decides for
#      itself which mutants to run, and if its notion of "changed" is narrower
#      than the gate's, the survivors it never ran would be invisible — the
#      gate would print "no survivors" about lines nothing analysed. A skipped
#      mutant on a changed line is reported as unanalysed, not as passing.
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

# findings_only <output> — the part of the report before the exempt list.
# An exempt survivor is still PRINTED, under "Exempt by", so grepping the whole
# output for its line number cannot tell "not reported as a finding" from
# "reported". The section boundary is what carries that distinction.
findings_only() {
	printf '%s\n' "$1" | sed -n '1,/Exempt by/p' | sed '$d'
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
if [ "$rc" -ne 0 ]; then
	fail "a survivor on a changed line warns rather than failing" "exit $rc: $out"
elif ! printf '%s' "$out" | grep -q 'pkg/mixed.go:11'; then
	fail "a survivor on a changed line is reported" "$out"
elif printf '%s' "$out" | grep -q 'pkg/mixed.go:4'; then
	fail "a survivor on an UNCHANGED line of a CHANGED file is not reported" "$out"
elif ! printf '%s' "$out" | grep -q '1 surviving mutant'; then
	fail "exactly one of the two survivors is reported" "$out"
else
	pass "reports survivors on changed lines only, and warns rather than failing"
fi

# ── 2. A skipped mutant on a changed line is reported as unanalysed ───────────
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
out=$(run_checker "$repo" --base main --report rep.json)
if ! printf '%s' "$out" | grep -qi 'not analysed\|not analyzed'; then
	fail "a SKIPPED mutant on a changed line is reported as unanalysed" "$out"
else
	pass "a SKIPPED mutant on a changed line is reported as unanalysed"
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
write_report "$repo/rep.json" '{"go_module":"example","files":[
 {"file_name":"pkg/new.go","mutations":[{"type":"CONDITIONALS_BOUNDARY","status":"NOT COVERED","line":4,"column":5}]}]}'
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
	//mutation:exempt buffer size, a test pinning it would be a tautology
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
elif ! printf '%s' "$out" | grep -q 'exempts nothing'; then
	fail "a bare //mutation:exempt is called out, not silently ignored" "$out"
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
	if n > 7 { //mutation:exempt only the outer test is a tuning constant
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

# ── Result ────────────────────────────────────────────────────────────────────

echo
if [ "$failures" -gt 0 ]; then
	echo "$failures of $checks mutation checks failed"
	exit 1
fi
echo "all $checks mutation checks passed"
